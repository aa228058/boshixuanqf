package account

import (
	"context"
	"fmt"
	"log"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/mtgo-labs/storage"
	"github.com/mtgo-labs/mtgo/telegram"
	"github.com/mtgo-labs/mtgo/tg"
	"tg-sender/internal/safego"
)

// MigrateDC 把账号会话从当前 DC 迁移到目标 DC（救活被机房路由封死的 DC，如 DC1→DC2）。
// 原理: 源DC导出授权(auth.exportAuthorization) → 目标DC匿名DH握手 →
//
//	导入授权(auth.importAuthorization) → 新auth_key覆盖库存会话。
//
// 前提: 必须已有一个活着的源DC连接（保活模式负责保持），导出令牌短时效须立即消费。
// 回滚: 迁移不注销源DC授权，原 .session 文件重新导入即可回到源DC。
func (e *Engine) MigrateDC(ctx context.Context, tenantID, accountID uuid.UUID, targetDC int32) error {
	if targetDC < 1 || targetDC > 5 {
		return fmt.Errorf("目标 DC 必须是 1-5，收到 %d", targetDC)
	}

	// 1. 拿活着的源DC客户端：优先保活的常驻客户端，其次池里现成连接
	key := accountKey(tenantID, accountID)
	e.mu.RLock()
	mc, managed := e.clients[key]
	e.mu.RUnlock()

	// 2. 读账号与旧会话元数据（UserID 迁移后不变）
	acc, err := e.repo.GetByID(ctx, tenantID, accountID)
	if err != nil {
		return err
	}
	var userID int64
	var oldDC int32
	if err := e.pool.QueryRow(ctx,
		`SELECT user_id, dc FROM sessions WHERE session_id = $1`, acc.SessionName,
	).Scan(&userID, &oldDC); err != nil {
		return fmt.Errorf("读取旧会话失败: %w", err)
	}
	if oldDC == targetDC {
		return fmt.Errorf("会话已在 DC%d，无需迁移", targetDC)
	}

	// K修复: 保活源连接必须持 mc.mu 使用(mtgo 非线程安全, 任务可能正占用)。
	// TryLock 失败说明任务执行中, 本轮迁移放弃稍后重试, 绝不阻塞等锁。
	if managed && mc.client != nil && mc.client.IsConnected() {
		if !mc.mu.TryLock() {
			return fmt.Errorf("账号 %s 正被任务占用, DC 迁移请稍后重试", accountID)
		}
		defer mc.mu.Unlock()
		log.Printf("[migrate] 账号 %s 使用保活连接作为源DC\n", accountID)
		return e.migrateDCWithSource(ctx, tenantID, accountID, acc.SessionName, mc.client, userID, oldDC, targetDC)
	}

	c, err := e.GetClient(ctx, tenantID, accountID)
	if err != nil {
		return fmt.Errorf("拿不到源DC连接（需先连上保活再迁移）: %w", err)
	}
	defer e.ReleaseClient(tenantID, accountID, c)
	return e.migrateDCWithSource(ctx, tenantID, accountID, acc.SessionName, c, userID, oldDC, targetDC)
}

// migrateDCWithSource 用已就位的源DC连接执行迁移主体。
// T修复: 目标DC客户端先建好连, 再导出授权令牌 —— 令牌窗口压到一次 RPC 内,
// 避免"先取令牌再 Connect(最长45s)"导致令牌过期、迁移失败进冷却。
func (e *Engine) migrateDCWithSource(ctx context.Context, tenantID, accountID uuid.UUID, sessionName string, src *telegram.Client, userID int64, oldDC, targetDC int32) error {
	// 3. 目标DC临时客户端：全新匿名DH握手，走HTTP传输（目标DC可达即可）
	cfg := &telegram.Config{
		APIID:         int32(e.cfg.TGAPIID),
		APIHash:       e.cfg.TGAPIHash,
		SessionName:   fmt.Sprintf("migrate_tmp_%s", sessionName),
		DC:            int(targetDC),
		InMemory:      true,
		HTTPTransport: httpTransportCfg(dcHTTPURLs(targetDC, "")),
	}
	dst, err := telegram.NewClient(cfg.APIID, cfg.APIHash, cfg)
	if err != nil {
		return fmt.Errorf("创建目标DC客户端失败: %w", err)
	}
	if err := dst.Connect(45 * time.Second); err != nil {
		dst.Stop()
		return fmt.Errorf("连接目标DC失败（该DC的HTTP传输也不通?）: %w", err)
	}
	defer dst.Stop()

	// 4. 源DC导出授权（目标已就绪, 令牌拿到立刻导入）
	expCtx, expCancel := context.WithTimeout(ctx, 60*time.Second)
	exp, err := src.Raw().AuthExportAuthorization(expCtx, &tg.AuthExportAuthorizationRequest{DCID: targetDC})
	expCancel()
	if err != nil {
		return fmt.Errorf("源DC导出授权失败: %w", err)
	}

	// 5. 目标DC导入授权 → 该 auth_key 获得账号身份
	impCtx, impCancel := context.WithTimeout(ctx, 60*time.Second)
	if _, err := dst.Raw().AuthImportAuthorization(impCtx, &tg.AuthImportAuthorizationRequest{
		ID:    exp.ID,
		Bytes: exp.Bytes,
	}); err != nil {
		impCancel()
		return fmt.Errorf("目标DC导入授权失败: %w", err)
	}
	impCancel()

	// 6. 抠出目标DC的 auth_key，覆盖库存会话
	newKey := dst.Session().AuthKey()
	if len(newKey) != 256 {
		return fmt.Errorf("目标DC auth_key 长度异常: %d", len(newKey))
	}
	if err := e.StoreImportedSession(ctx, sessionName, "", &storage.Session{
		SessionID: sessionName,
		DC:        int(targetDC),
		APIID:     int32(e.cfg.TGAPIID),
		AuthKey:   newKey,
		UserID:    userID,
	}); err != nil {
		return fmt.Errorf("写入新会话失败（旧会话未动，可重试）: %w", err)
	}

	// 7. 断开旧DC连接（保活/池化），状态回待机，下次取客户端自动走新DC
	_ = e.DisconnectAccount(ctx, tenantID, accountID)
	log.Printf("[migrate] 账号 %s 已从 DC%d 迁移到 DC%d\n", accountID, oldDC, targetDC)
	return nil
}

// autoMigrateInfo 迁移判定缓存条目: 会话名+DC 5分钟内免查库。
type autoMigrateInfo struct {
	sessionName string
	dc          int32
	at          time.Time
}

// maybeAutoMigrate 保活账号在线时的自动迁移判定（全自动，无需手动点按钮）。
// 会话 DC ∈ TG_AUTO_MIGRATE_FROM 黑名单 → 后台 MigrateDC（每账号 10 分钟冷却）。
// 迁移成功后 MigrateDC 内部 DisconnectAccount，保活循环随 stillManaged 检查退出，
// 账号转为新 DC 的按需模式（HTTP 传输直达）。
//
// Q修复: 原实现每次判定 2 次 DB 查询(账号+DC), 保活每 30s 触发一次,
// 且冷却检查在查询之后 —— 未命中黑名单的账号(绝大多数)每次都白查。
// 100 个协议号 = 200 次查询/30s。现改为: 冷却检查(纯内存)提前,
// 会话名+DC 判定结果缓存 5 分钟(TTL 内零 DB)。
func (e *Engine) maybeAutoMigrate(tenantID, accountID uuid.UUID) {
	from := strings.TrimSpace(e.cfg.TGAutoMigrateFrom)
	if from == "" {
		return
	}

	key := accountKey(tenantID, accountID)

	// 冷却检查先行(纯内存, 零 DB): 命中过黑名单且在冷却期内直接返回
	e.migrateMu.Lock()
	if e.migrateAttempts == nil {
		e.migrateAttempts = make(map[string]time.Time)
	}
	if last, ok := e.migrateAttempts[key]; ok && time.Since(last) < 10*time.Minute {
		e.migrateMu.Unlock()
		return
	}
	e.migrateMu.Unlock()

	// 判定缓存: 会话名+DC, 5 分钟 TTL, 热路径零 DB
	e.autoMigrateMu.Lock()
	info, cached := e.autoMigrateCache[key]
	e.autoMigrateMu.Unlock()
	if !cached || time.Since(info.at) > 5*time.Minute {
		qCtx, qCancel := context.WithTimeout(context.Background(), 5*time.Second)
		acc, err := e.repo.GetByID(qCtx, tenantID, accountID)
		if err != nil || acc == nil || acc.SessionName == "" {
			qCancel()
			return
		}
		var dc int32
		if err := e.pool.QueryRow(qCtx,
			`SELECT dc FROM sessions WHERE session_id = $1`, acc.SessionName).Scan(&dc); err != nil {
			qCancel()
			return
		}
		qCancel()
		info = autoMigrateInfo{sessionName: acc.SessionName, dc: dc, at: time.Now()}
		e.autoMigrateMu.Lock()
		if e.autoMigrateCache == nil { // 防御: 测试等场景下未走 NewEngine 构造
			e.autoMigrateCache = make(map[string]autoMigrateInfo)
		}
		e.autoMigrateCache[key] = info
		e.autoMigrateMu.Unlock()
	}

	hit := false
	for _, s := range strings.Split(from, ",") {
		if n, err := strconv.Atoi(strings.TrimSpace(s)); err == nil && int32(n) == info.dc {
			hit = true
			break
		}
	}
	if !hit {
		return
	}

	e.migrateMu.Lock()
	e.migrateAttempts[key] = time.Now()
	e.migrateMu.Unlock()

	target := e.cfg.TGAutoMigrateTo
	if target < 1 || target > 5 {
		target = 2
	}
	log.Printf("[migrate] 账号 %s (DC%d) 命中迁移黑名单，后台自动迁移到 DC%d\n", accountID, info.dc, target)
	safego.Go(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
		defer cancel()
		if err := e.MigrateDC(ctx, tenantID, accountID, target); err != nil {
			log.Printf("[migrate] 账号 %s 自动迁移失败(10分钟后重试): %v\n", accountID, err)
		} else {
			log.Printf("[migrate] 账号 %s 自动迁移完成，已切换到按需模式\n", accountID)
		}
	})
}
