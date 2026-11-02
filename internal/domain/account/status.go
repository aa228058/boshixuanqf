package account

import (
	"context"
	"crypto/rand"
	"math/big"
	"time"

	"tg-sender/internal/domain"

	"github.com/google/uuid"
)

func (e *Engine) GetAccountStatus(ctx context.Context, tenantID, accountID uuid.UUID) (domain.AccountStatus, error) {
	acc, err := e.repo.GetByID(ctx, tenantID, accountID)
	if err != nil {
		return "", err
	}
	return acc.Status, nil
}

// HealthCheck returns the number of connected accounts.
// O修复: 原实现只统计连接池, 保活协议号(常驻 e.clients)在线也报 0, 监控误判。
// 现同时统计: ① 保活注册且已连接的协议号; ② 连接池内已连接客户端。
func (e *Engine) HealthCheck() int {
	count := 0

	e.mu.RLock()
	managed := make([]*managedClient, 0, len(e.clients))
	for _, mc := range e.clients {
		managed = append(managed, mc)
	}
	e.mu.RUnlock()
	for _, mc := range managed {
		mc.mu.Lock()
		if mc.client != nil && mc.authPhase == "success" && mc.client.IsConnected() {
			count++
		}
		mc.mu.Unlock()
	}

	e.poolMu.RLock()
	defer e.poolMu.RUnlock()
	for _, pc := range e.poolClients {
		if pc.client != nil && pc.client.IsConnected() {
			count++
		}
	}
	return count
}

// GetTelegramDialogs returns all dialogs (groups, channels, and private chats) for an account.
// Results include type classification so the frontend can filter by category.

func (e *Engine) GetAccountLevel(ctx context.Context, tenantID, accountID uuid.UUID) domain.AccountLevel {
	acc, err := e.repo.GetByID(ctx, tenantID, accountID)
	if err != nil || acc == nil {
		return domain.AccountLevelNormal
	}
	return acc.Level
}

// GetAllClients returns all connected clients for a tenant (for monitor engine).

func (e *Engine) UpdateAccountStatus(ctx context.Context, tenantID, accountID uuid.UUID, status domain.AccountStatus) error {
	return e.updateStatus(ctx, tenantID, accountID, status)
}

// SendInterval returns the send interval for an account based on its level.
// Deprecated: use SendIntervalWithGuard for tenant-configurable intervals with guard-rails.
func (e *Engine) SendInterval(level domain.AccountLevel) time.Duration {
	return e.SendIntervalWithGuard(level, 0, 0)
}

// SendIntervalWithGuard returns a send interval with guard-rail offset applied.
// Tenant can set any min/max values (including 0); the system automatically offsets
// to a safe range based on account level and recent ban rate.
//
// Guard-rail logic:
//  1. Compute floor interval based on account level + recent ban rate.
//  2. If tenantMin < floor, offset min to floor + random jitter.
//  3. If tenantMax < tenantMin + spread, offset max to tenantMin + random spread.
//  4. Return random value in the final [min, max] range.
func (e *Engine) SendIntervalWithGuard(level domain.AccountLevel, tenantMin, tenantMax time.Duration) time.Duration {
	// Step 1: Compute floor based on account level + adaptive multipliers
	floor := e.intervalFloor(level)

	// Step 2: Apply guard-rail to tenant config
	min, max := e.applyGuardRail(tenantMin, tenantMax, floor)

	// Step 3: Return random value in [min, max]
	if max > min {
		diff := max - min
		return min + randomDuration(diff)
	}
	return min
}

// intervalFloor computes the absolute minimum interval for an account level,
// adjusted by recent ban rate. This is the "guard-rail floor" — tenant config
// cannot go below this (system silently offsets).
func (e *Engine) intervalFloor(level domain.AccountLevel) time.Duration {
	var base time.Duration
	switch level {
	case domain.AccountLevelNew:
		base = e.cfg.FloorIntervalNew
		if base <= 0 {
			base = 8 * time.Second
		}
	case domain.AccountLevelOld:
		base = e.cfg.FloorIntervalOld
		if base <= 0 {
			base = 1 * time.Second
		}
	default: // normal and unknown
		base = e.cfg.FloorIntervalNormal
		if base <= 0 {
			base = 3 * time.Second
		}
	}

	// Apply adaptive multiplier based on recent ban rate
	// (in production, this would read from account stats; here we use config defaults)
	//
	// I修复: 乘数配置为 0/负时原实现直接乘出 0 间隔(狂发秒封号);
	// 且高/中分支的兜底(base<=0 时再乘一个数)对 0 无效(0 乘任何数还是 0)。
	// 现统一: 乘数 <=0 视为 1.0(不调整), 乘完再加绝对地板 1s。
	banRate := e.recentBanRate()
	mult := e.cfg.BanRateLowMultiplier
	switch {
	case banRate > 0.3:
		mult = e.cfg.BanRateHighMultiplier
	case banRate > 0.1:
		mult = e.cfg.BanRateMediumMultiplier
	}
	if mult <= 0 {
		mult = 1.0
	}
	base = time.Duration(float64(base) * mult)
	if base <= 0 {
		base = time.Second // 绝对地板: 任何配置错误都不能归零发送间隔
	}

	return base
}

// applyGuardRail offsets tenant-configured intervals if they are too aggressive.
// Tenant can set any value; system silently adjusts to safe range.
func (e *Engine) applyGuardRail(tenantMin, tenantMax, floor time.Duration) (min, max time.Duration) {
	// If tenant didn't configure (0), use global defaults or floor
	if tenantMin <= 0 {
		if e.cfg.MinSendInterval > 0 {
			tenantMin = e.cfg.MinSendInterval
		} else {
			tenantMin = floor
		}
	}
	if tenantMax <= 0 {
		if e.cfg.MaxSendInterval > 0 && e.cfg.MaxSendInterval > tenantMin {
			tenantMax = e.cfg.MaxSendInterval
		} else {
			tenantMax = tenantMin + 5*time.Second
		}
	}

	// Guard-rail 1: min cannot go below floor
	if tenantMin < floor {
		jitter := randomDuration(e.guardJitterRange())
		tenantMin = floor + jitter
	}

	// Guard-rail 2: max must be at least min + spread
	minSpread := e.cfg.GuardMinSpread
	if minSpread <= 0 {
		minSpread = 2 * time.Second
	}
	maxSpread := e.cfg.GuardMaxSpread
	if maxSpread <= 0 {
		maxSpread = 5 * time.Second
	}
	if tenantMax < tenantMin+minSpread {
		spread := minSpread + randomDuration(maxSpread-minSpread)
		tenantMax = tenantMin + spread
	}

	return tenantMin, tenantMax
}

// recentBanRate returns the global 24h ban rate, refreshed every 5 minutes
// by the engine's background loop. Zero means no bans (normal multiplier branch).
func (e *Engine) recentBanRate() float64 {
	e.banRateMu.RLock()
	defer e.banRateMu.RUnlock()
	return e.banRate
}

// guardJitterRange returns the jitter range for floor offset.
func (e *Engine) guardJitterRange() time.Duration {
	minJ := e.cfg.GuardMinJitter
	maxJ := e.cfg.GuardMaxJitter
	if maxJ <= minJ {
		minJ = 0
		maxJ = 2 * time.Second
	}
	return maxJ - minJ
}

// randomDuration returns a random duration in [0, max).
func randomDuration(max time.Duration) time.Duration {
	if max <= 0 {
		return 0
	}
	n, _ := rand.Int(rand.Reader, big.NewInt(int64(max)))
	return time.Duration(n.Int64())
}

// SetAdaptiveProfile updates the send interval profile from adaptive mode.

func (e *Engine) SetAdaptiveProfile(minInterval, maxInterval time.Duration) {
	e.adaptiveProfile = &struct {
		MinSendInterval time.Duration
		MaxSendInterval time.Duration
	}{minInterval, maxInterval}
}

// GetAccountLevel returns the account level from DB.
