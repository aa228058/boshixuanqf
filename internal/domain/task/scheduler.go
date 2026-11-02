package task

import (
	"log"
	"os"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"tg-sender/internal/safego"
	"time"

	"github.com/google/uuid"
)

// SystemState represents the current resource pressure level.
type SystemState string

const (
	StateNormal   SystemState = "normal"   // memory < 60%
	StateLight    SystemState = "light"    // 60% <= memory < 70%
	StateWarning  SystemState = "warning"  // 70% <= memory < 80%
	StateCritical SystemState = "critical" // memory >= 80%
	StateRecovery SystemState = "recovery" // memory dropped below 55% after warning
)

// CompressionFactor maps state to concurrency multiplier.
var compressionFactors = map[SystemState]float64{
	StateNormal:   1.0,  // full speed
	StateLight:    0.8,  // slight reduction
	StateWarning:  0.6,  // moderate reduction
	StateCritical: 0.3,  // heavy reduction
	StateRecovery: 0.85, // gradual recovery
}

// TenantQuota tracks a tenant's plan quota and current usage.
type TenantQuota struct {
	TenantID      uuid.UUID
	MaxConcurrent int // plan quota (from Tenant.MaxConcurrentTasks)
	Current       int // currently running tasks for this tenant
	mu            sync.RWMutex
}

// AutoScheduler provides resource-aware, plan-based task scheduling.
type AutoScheduler struct {
	mu sync.RWMutex
	// tenant quotas: key = tenantID
	quotas map[uuid.UUID]*TenantQuota
	// system state
	state     SystemState
	stateTime time.Time
	// memory monitoring
	stopCh   chan struct{}
	stopOnce sync.Once
	// total system memory (cached)
	totalMemMB int64
}

// NewAutoScheduler creates a scheduler with memory monitoring.
// memoryLimitMB = 自适应并发的内存分母（Go进程内存 ÷ 此值），
// 应传 config.MemoryLimitMB（如 2800）——用系统总内存做分母会导致
// 进程内存占比永远偏低、自适应降并发永远不触发。
func NewAutoScheduler(memoryLimitMB int64) *AutoScheduler {
	if memoryLimitMB <= 0 {
		memoryLimitMB = 2800
	}
	s := &AutoScheduler{
		quotas:     make(map[uuid.UUID]*TenantQuota),
		state:      StateNormal,
		stateTime:  time.Now(),
		stopCh:     make(chan struct{}),
		totalMemMB: memoryLimitMB,
	}
	safego.Go(func() { s.monitorLoop() })
	return s
}

// RegisterTenant sets the plan quota for a tenant.
func (s *AutoScheduler) RegisterTenant(tenantID uuid.UUID, maxConcurrent int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if q, exists := s.quotas[tenantID]; exists {
		q.mu.Lock()
		q.MaxConcurrent = maxConcurrent
		q.mu.Unlock()
	} else {
		s.quotas[tenantID] = &TenantQuota{
			TenantID:      tenantID,
			MaxConcurrent: maxConcurrent,
			Current:       0,
		}
	}
}

// TotalMaxConcurrent 返回所有已注册租户 max_concurrent_tasks 的总和。
// 作为全局并发的"需求上限"：业务增长（租户/升套餐）时总容量自动水涨船高，
// 不用再改代码调档位。与硬件档位取 min 后作为最终全局上限。
func (s *AutoScheduler) TotalMaxConcurrent() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	total := 0
	for _, q := range s.quotas {
		q.mu.RLock()
		total += q.MaxConcurrent
		q.mu.RUnlock()
	}
	return total
}

// TryAcquire attempts to start a task for the given tenant.
// Returns true if the tenant has available quota under current system pressure.
func (s *AutoScheduler) TryAcquire(tenantID uuid.UUID) bool {
	s.mu.RLock()
	q, exists := s.quotas[tenantID]
	state := s.state
	s.mu.RUnlock()
	if !exists {
		// Unknown tenant: allow with default quota of 1
		return true
	}
	q.mu.Lock()
	defer q.mu.Unlock()
	limit := s.computeLimit(q.MaxConcurrent, state)
	if q.Current < limit {
		q.Current++
		return true
	}
	return false
}

// Release decrements the tenant's current task count.
func (s *AutoScheduler) Release(tenantID uuid.UUID) {
	s.mu.RLock()
	q, exists := s.quotas[tenantID]
	s.mu.RUnlock()
	if !exists {
		return
	}
	q.mu.Lock()
	if q.Current > 0 {
		q.Current--
	}
	q.mu.Unlock()
}

// GetTenantLimit returns the current effective concurrency limit for a tenant.
func (s *AutoScheduler) GetTenantLimit(tenantID uuid.UUID) int {
	s.mu.RLock()
	q, exists := s.quotas[tenantID]
	state := s.state
	s.mu.RUnlock()
	if !exists {
		return 1
	}
	q.mu.RLock()
	maxConcurrent := q.MaxConcurrent
	q.mu.RUnlock()
	return s.computeLimit(maxConcurrent, state)
}

// GetTenantCurrent returns how many tasks the tenant is currently running.
func (s *AutoScheduler) GetTenantCurrent(tenantID uuid.UUID) int {
	s.mu.RLock()
	q, exists := s.quotas[tenantID]
	s.mu.RUnlock()
	if !exists {
		return 0
	}
	q.mu.RLock()
	current := q.Current
	q.mu.RUnlock()
	return current
}

// GetState returns current system state and compression factor.
func (s *AutoScheduler) GetState() (SystemState, float64) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.state, compressionFactors[s.state]
}

// computeLimit calculates effective limit: max(1, floor(quota * factor))
func (s *AutoScheduler) computeLimit(quota int, state SystemState) int {
	if quota <= 0 {
		quota = 1
	}
	factor := compressionFactors[state]
	limit := int(float64(quota) * factor)
	if limit < 1 {
		limit = 1
	}
	return limit
}

// monitorLoop checks system memory every 5 seconds and adjusts state.
func (s *AutoScheduler) monitorLoop() {
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			s.checkMemory()
		case <-s.stopCh:
			return
		}
	}
}

// checkMemory reads system memory and updates state.
func (s *AutoScheduler) checkMemory() {
	usage := s.readMemUsage()
	s.mu.Lock()
	defer s.mu.Unlock()
	prevState := s.state
	switch {
	case usage >= 0.80:
		if s.state != StateCritical {
			log.Printf("[scheduler] 内存严重不足: %.1f%% -> 状态=危急，压缩至30%%", usage*100)
			s.state = StateCritical
			s.stateTime = time.Now()
		}
	case usage >= 0.70:
		if s.state != StateWarning && s.state != StateCritical {
			log.Printf("[scheduler] 内存警告: %.1f%% -> 状态=警告，压缩至60%%", usage*100)
			s.state = StateWarning
			s.stateTime = time.Now()
		}
		// If was critical and dropped to warning, stay in warning (don't bounce)
	case usage >= 0.60:
		if s.state != StateLight && s.state != StateWarning && s.state != StateCritical {
			log.Printf("[scheduler] 内存轻度占用: %.1f%% -> 状态=轻度，压缩至80%%", usage*100)
			s.state = StateLight
			s.stateTime = time.Now()
		}
	case usage < 0.55:
		if s.state == StateLight || s.state == StateWarning || s.state == StateCritical {
			log.Printf("[scheduler] 内存已恢复: %.1f%% -> 状态=恢复中，逐步恢复", usage*100)
			s.state = StateRecovery
			s.stateTime = time.Now()
		} else if s.state == StateRecovery {
			// After 30 seconds in recovery, go back to normal
			if time.Since(s.stateTime) > 30*time.Second {
				log.Printf("[scheduler] 内存正常: %.1f%% -> 状态=正常，完全并发已恢复", usage*100)
				s.state = StateNormal
				s.stateTime = time.Now()
			}
		}
	default:
		// 55-60%: maintain current state (hysteresis band)
	}
	if s.state != prevState {
		s.logTenantLimits()
	}
}

// logTenantLimits prints current effective limits for all tenants.
func (s *AutoScheduler) logTenantLimits() {
	for id, q := range s.quotas {
		limit := s.computeLimit(q.MaxConcurrent, s.state)
		log.Printf("[scheduler] 租户=%s 配额=%d 有效=%d 当前=%d 状态=%s",
			id.String()[:8], q.MaxConcurrent, limit, q.Current, s.state)
	}
}

// readMemUsage returns system memory usage ratio (0.0 - 1.0).
// On non-Linux systems, falls back to Go runtime memory stats.
func (s *AutoScheduler) readMemUsage() float64 {
	// 用 Go 进程内存（与 TG 客户端数量直接相关），而非系统内存。
	// 旧逻辑读 /proc/meminfo 系统内存——postgres/redis/系统本身吃掉的部分
	// 与 TG 发送并发无关，却把并发错误地压缩（4G 机上 PG 吃 1.5G 就到 60%+，
	// 专业版 5 并发被压到 1-3，用户只见"配额满"）。
	// 保底仍兼容：读不到进程内存时用 runtime 统计。
	// P2-79 审计修复: 内存指标统一为 Alloc(堆分配, 与 adaptive/engine.go 同口径——
	// 原来这里用 Sys, 两套指标对不上, 监控/自适应阈值失真)。Sys 含运行时预留,
	// 对"业务内存压力"不敏感。
	var m runtime.MemStats
	runtime.ReadMemStats(&m)
	if s.totalMemMB > 0 {
		return float64(m.Alloc/1024/1024) / float64(s.totalMemMB)
	}
	// totalMemMB 未知（<1GB）时退回系统内存
	data, err := os.ReadFile("/proc/meminfo") // #nosec G304 - hardcoded system path
	if err != nil {
		return 0.3 // assume 30% if we can't read
	}
	var totalKB, availableKB int64
	lines := strings.Split(string(data), "\n")
	for _, line := range lines {
		if strings.HasPrefix(line, "MemTotal:") {
			fields := strings.Fields(line)
			if len(fields) >= 2 {
				totalKB, _ = strconv.ParseInt(fields[1], 10, 64)
			}
		} else if strings.HasPrefix(line, "MemAvailable:") {
			fields := strings.Fields(line)
			if len(fields) >= 2 {
				availableKB, _ = strconv.ParseInt(fields[1], 10, 64)
			}
		}
	}
	if totalKB <= 0 {
		return 0.3
	}
	usedKB := totalKB - availableKB
	return float64(usedKB) / float64(totalKB)
}

// P2-5 审计清理: readTotalMemMB 死代码已删——全项目无调用点。

// Stop shuts down the memory monitor goroutine.
// stopOnce 防护: 双调 close 会 panic(当前只有 container 调一次, 属未来隐患)。
func (s *AutoScheduler) Stop() {
	s.stopOnce.Do(func() { close(s.stopCh) })
}
