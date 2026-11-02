package account

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net"
	"strings"
	"time"

	"github.com/mtgo-labs/mtgo/telegram"
	"tg-sender/internal/domain"
	"tg-sender/internal/safego"

	"github.com/google/uuid"
)

func (e *Engine) GetClient(ctx context.Context, tenantID, accountID uuid.UUID) (*telegram.Client, error) {
	// 有限重试: 并发窗口/double-check 拿到死连接时剔除后重走一遍，
	// 而不是把死连接交给调用方(任务拿到僵尸连接 = 发送失败/挂起, 重启才恢复)。
	const maxAttempts = 3
	for attempt := 0; attempt < maxAttempts; attempt++ {
		client, retry, err := e.getClientOnce(ctx, tenantID, accountID)
		if !retry {
			return client, err
		}
	}
	return nil, fmt.Errorf("connection for account %s failed after %d attempts", accountID, maxAttempts)
}

// getClientOnce 单次取连接。retry=true 表示拿到的是死连接已剔除, 调用方应重试。
// 成功返回时客户端锁由调用方持有(必须配对 ReleaseClient)。
func (e *Engine) getClientOnce(ctx context.Context, tenantID, accountID uuid.UUID) (client *telegram.Client, retry bool, err error) {
	key := accountKey(tenantID, accountID)

	// 0. 保活账号：直接复用常驻客户端（不重复建连，避免同号双连接被 TG 踢）
	e.mu.RLock()
	managedMc, isManaged := e.clients[key]
	e.mu.RUnlock()
	// R181: 死会话密钥熔断 —— 已判定密钥被 TG 吊销的账号在 TTL 内直接快速失败,
	// 不再拨号/入池/探活/重试(此前 AUTH_KEY_UNREGISTERED 引发每秒数次重拨风暴)
	if e.deadKeyBlocked(accountID) {
		return nil, false, fmt.Errorf("会话密钥已失效(需重新登录)")
	}

	if isManaged && managedMc != nil && managedMc.client != nil && managedMc.client.IsConnected() {
		// R161(真凶修复): 登录中的客户端严禁探活/摘除 —— 探活 RPC 在未授权状态必失败,
		// 会把进行中的登录流程掐死(用户验证码/2FA 永远报 no active auth flow, "验证码对的也失败"的根)。
		// 登录中的客户端直接交给调用方(调用方 RPC 失败自行容错), 注册与保活原样保留。
		managedMc.mu.Lock()
		_phase := managedMc.authPhase
		if _phase == "connecting" || _phase == "waiting_code" || _phase == "waiting_password" {
			// R161: 登录中免探活; 持锁返回, 由调用方 ReleaseClient 解锁
			return managedMc.client, false, nil
		}
		managedMc.mu.Unlock()
		// 2026-09-22 修复(协议号全功能失败的根因)：IsConnected 只是状态标志，会假活。
		// 必须探活兜底：死了就摘除保活注册、走下方池拨号拿全新连接（与正常号同款健康路径），
		// 否则保活号拿着僵尸连接，读操作偶发能过、写操作(群发/加群)全灭。
		// P1-11 审计修复: 原实现持 managedMc.mu 做网络探活(可能 45s), 期间保活
		// 循环和其他 GetClient 全部阻塞。改为先无锁探活——探活期间 client 被并发
		// Stop 只会探测失败, 无害(下方走池拨号); 探活成功再加锁占用并二次确认。
		if probeErr := e.probeClient(ctx, managedMc.client); probeErr == nil {
			managedMc.mu.Lock()
			managedMc.probeFails = 0
			if managedMc.client != nil && managedMc.client.IsConnected() {
				if err := e.updateStatus(ctx, tenantID, accountID, domain.AccountStatusOnline); err != nil {
					log.Printf("[client] failed to update status: %v", err)
				}
				return managedMc.client, false, nil
			}
			managedMc.mu.Unlock()
		} else {
			// R181: 死密钥(AUTH_KEY_UNREGISTERED)=永久死亡: 立即熔断+标"需重新登录", 不走"两次失败"容忍
			if isAuthKeyDead(probeErr) {
				e.markDeadKey(ctx, tenantID, accountID)
				return nil, false, fmt.Errorf("会话密钥已失效(需重新登录)")
			}
			// R162: 连续两次探活失败才摘除 —— 单次失败直接杀是误杀率极高的根(网络抖动/DC 切换)。
			managedMc.mu.Lock()
			managedMc.probeFails++
			fails := managedMc.probeFails
			if fails >= 2 {
				managedMc.mu.Unlock()
				log.Printf("[client] 保活连接探活失败(连续 %d 次), 摘除注册并改走池拨号 account=%s", fails, accountID)
			} else {
				log.Printf("[client] 探活失败(%v), 第 %d 次, 暂不摘除(连续失败才杀) account=%s", probeErr, fails, accountID)
				return managedMc.client, false, nil // 持锁返回
			}
		}
		e.mu.Lock()
		if cur, ok := e.clients[key]; ok && cur == managedMc {
			delete(e.clients, key)
		}
		e.mu.Unlock()
		// 崩溃类修复: 裸 go 里 Stop() panic 会崩进程, 自带 recover
		go func() {
			defer func() {
				if r := recover(); r != nil {
					log.Printf("[client] stop探活失败客户端 panic: %v", r)
				}
			}()
			managedMc.client.Stop()
		}()
	}

	// 1. Check connection pool。写锁: 命中时要更新 lastUsed(D/N修复: 原来在 RLock 下写字段, race)
	e.poolMu.Lock()
	pc, ok := e.poolClients[key]
	if ok && pc != nil && pc.client != nil && pc.client.IsConnected() {
		pc.lastUsed = time.Now()
		e.poolMu.Unlock()
		pc.mu.Lock()
		probeErr := e.probeClient(ctx, pc.client)
		if probeErr == nil {
			pc.probeFails = 0
			return pc.client, false, nil // 持锁返回, ReleaseClient 解锁
		}
		pc.probeFails++
		fails := pc.probeFails
		// R181: 死密钥立即熔断(区别于网络抖动的"两次失败"容忍)
		if isAuthKeyDead(probeErr) {
			pc.mu.Unlock()
			e.markDeadKey(ctx, tenantID, accountID)
			return nil, false, fmt.Errorf("会话密钥已失效(需重新登录)")
		}
		if fails >= 2 {
			pc.mu.Unlock()
			log.Printf("[client] 池内连接探活失败(连续 %d 次), 剔除重拨 account=%s", fails, accountID)
			e.InvalidatePooledClient(tenantID, accountID)
			return nil, true, nil
		}
		// R163: 单次失败不剔除(网络抖动), 持锁交调用方容错
		log.Printf("[client] 池内连接探活失败(%v), 第 %d 次, 暂不剔除(连续失败才重拨) account=%s", probeErr, fails, accountID)
		return pc.client, false, nil // 持锁返回
	}
	e.poolMu.Unlock()

	// 2. Check pool size limit before creating new connection
	e.poolMu.Lock()
	poolSize := len(e.poolClients)
	if poolSize >= e.maxPoolSize {
		// Try to evict the oldest idle connection (must hold write lock)
		if !e.evictOldestIdleClientLocked() {
			e.poolMu.Unlock()
			return nil, false, fmt.Errorf("connection pool full (%d/%d), cannot create new connection for account %s", poolSize, e.maxPoolSize, accountID)
		}
	}
	e.poolMu.Unlock()

	// 3. 同账号并发建连去重: 已有 goroutine 在建 → 等它完成, 探活后从池里取
	e.poolMu.RLock()
	if pc, ok := e.poolClients[key]; ok && pc != nil && pc.client != nil && pc.client.IsConnected() {
		e.poolMu.RUnlock()
		pc.mu.Lock()
		probeErr := e.probeClient(ctx, pc.client)
		if probeErr == nil {
			pc.probeFails = 0
			return pc.client, false, nil // 持锁返回
		}
		pc.probeFails++
		fails := pc.probeFails
		// R181: 死密钥立即熔断(区别于网络抖动的"两次失败"容忍)
		if isAuthKeyDead(probeErr) {
			pc.mu.Unlock()
			e.markDeadKey(ctx, tenantID, accountID)
			return nil, false, fmt.Errorf("会话密钥已失效(需重新登录)")
		}
		if fails >= 2 {
			pc.mu.Unlock()
			log.Printf("[client] 并发窗口探活失败(连续 %d 次), 剔除重拨 account=%s", fails, accountID)
			e.InvalidatePooledClient(tenantID, accountID)
			return nil, true, nil
		}
		log.Printf("[client] 并发窗口探活失败(%v), 第 %d 次, 暂不剔除 account=%s", probeErr, fails, accountID)
		return pc.client, false, nil // 持锁返回
	}
	e.poolMu.RUnlock()

	e.connectInflightMu.Lock()
	if ch, ok := e.connectInflight[key]; ok {
		e.connectInflightMu.Unlock()
		select {
		case <-ctx.Done():
			return nil, false, ctx.Err()
		case <-ch:
		}
		// M修复: 等待唤醒后同样探活, 不把死连接交给调用方
		e.poolMu.RLock()
		pc, ok := e.poolClients[key]
		e.poolMu.RUnlock()
		if ok && pc != nil && pc.client != nil && pc.client.IsConnected() {
			pc.mu.Lock()
			probeErr := e.probeClient(ctx, pc.client)
			if probeErr == nil {
				pc.probeFails = 0
				return pc.client, false, nil // 持锁返回
			}
			pc.probeFails++
			fails := pc.probeFails
			// R181: 死密钥立即熔断(区别于网络抖动的"两次失败"容忍)
			if isAuthKeyDead(probeErr) {
				pc.mu.Unlock()
				e.markDeadKey(ctx, tenantID, accountID)
				return nil, false, fmt.Errorf("会话密钥已失效(需重新登录)")
			}
			if fails >= 2 {
				pc.mu.Unlock()
				log.Printf("[client] 并发等待路径探活失败(连续 %d 次), 剔除重拨 account=%s", fails, accountID)
				e.InvalidatePooledClient(tenantID, accountID)
				return nil, true, nil
			}
			log.Printf("[client] 并发等待路径探活失败(%v), 第 %d 次, 暂不剔除 account=%s", probeErr, fails, accountID)
			return pc.client, false, nil // 持锁返回
		}
		return nil, false, fmt.Errorf("connection for account %s failed (in-flight connect error)", accountID)
	}
	ch := make(chan struct{})
	if e.connectInflight == nil { // 防御: 构造时未初始化的兜底
		e.connectInflight = make(map[string]chan struct{})
	}
	e.connectInflight[key] = ch
	e.connectInflightMu.Unlock()
	defer func() {
		e.connectInflightMu.Lock()
		delete(e.connectInflight, key)
		close(ch)
		e.connectInflightMu.Unlock()
	}()

	// 4. Lazy load: restore from saved session
	acc, err := e.repo.GetByID(ctx, tenantID, accountID)
	if err != nil {
		return nil, false, err
	}
	if acc.Status != domain.AccountStatusAuthenticated && acc.Status != domain.AccountStatusOnline && acc.Status != domain.AccountStatusOffline {
		return nil, false, fmt.Errorf("account %s not authenticated (status: %s)", accountID, acc.Status)
	}
	if acc.SessionName == "" {
		return nil, false, fmt.Errorf("account %s has no session_name, please re-authenticate", accountID)
	}

	// Use saved device profile for consistent device identity
	deviceType := ""
	if acc.DeviceProfile != nil {
		var ok bool
		deviceType, ok = acc.DeviceProfile["device_type"].(string)
		if !ok {
			log.Printf("[client] device_type type assertion failed for account %s", accountID)
		}
	}
	deviceCfg := deviceProfileToConfig(acc.DeviceProfile, acc.SessionName)
	if deviceCfg.DeviceModel == "" {
		deviceCfg = generateDeviceConfig(acc.SessionName, deviceType)
	}

	// Use shared PostgreSQL storage for session persistence
	pgStorage := e.pgStorage

	clientCfg := &telegram.Config{
		APIID:       int32(e.cfg.TGAPIID),
		APIHash:     e.cfg.TGAPIHash,
		SessionName: acc.SessionName,
		// R181: 池化客户端绝不携带手机号 —— mtgo 对带手机号且会话失效的客户端会自动发起
		// sendCode 登录流(此前日志 "login: sending code" 刷屏的根源)。登录流程在 auth.go 自带手机号。
		Device:      deviceCfg,
		Storage:     pgStorage,
		NoUpdates:   false,
		AutoConnect: false,
	}
	// 账号级代理(049): 连接该账号的所有客户端走 socks5
	applyProxy(clientCfg, acc.ProxyURL)

	// 协议号恢复会话时优先用导入时记录的原始 DC 地址（.session 里的真实地址），
	// 避开内置地址表中被路由抽风的 IP（机房到 TG 某些 IP 间歇超时场景）。
	var serverAddr string
	if err := e.pool.QueryRow(ctx, `SELECT server_address FROM sessions WHERE session_id = $1`, acc.SessionName).Scan(&serverAddr); err == nil && serverAddr != "" {
		clientCfg.ServerAddr = serverAddr
	}

	// HTTP 回退端点：按会话所属 DC 生成 5222/80 双端口候选，传输层自动轮转
	var dcID int32
	_ = e.pool.QueryRow(ctx, `SELECT dc FROM sessions WHERE session_id = $1`, acc.SessionName).Scan(&dcID)

	// R182.2 关键修复: 存档 server_address 与 dc 列可能错位(SaveSession 写的是 adapter 旧值/
	// fallback 地址, 实测 dc=5 但地址是 149.154.175.55=DC1)。拿 DC5 的 key 连 DC1 的服务器,
	// initial ping 必被 -404 拒——"正常号登录后必死"的物理根因。错位即丢弃, 走内置地址表按 dc 连。
	if serverAddr != "" && dcID > 0 {
		host, _, err := net.SplitHostPort(serverAddr)
		if err != nil {
			host = serverAddr
		}
		if !dcIPBelongs(dcID, host) {
			log.Printf("[client] R182 存档地址与 DC 错位(dc=%d addr=%s), 丢弃, 走内置地址表\n", dcID, serverAddr)
			serverAddr = ""
			clientCfg.ServerAddr = ""
		}
	}
	httpURLs := dcHTTPURLs(dcID, serverAddr)

	// 连接：TG_TRANSPORT 策略（auto = TCP→HTTP→MTProxy 逐级回退）
	// 具名返回值已有 client/err, 用 = 而非 := (修复: no new variables on left side)
	client, err = e.dialClient(clientCfg, httpURLs, dcID)
	if err != nil {
		log.Printf("[client] connection failed: %v, retrying...\n", err)
		select {
		case <-ctx.Done():
			return nil, false, ctx.Err()
		case <-time.After(e.cfg.ConnectRetrySleep):
			if client, err = e.dialClient(clientCfg, httpURLs, dcID); err != nil {
				if updateErr := e.updateStatus(ctx, tenantID, accountID, domain.AccountStatusOffline); updateErr != nil {
					log.Printf("[AUTH] failed to update status to offline for account %s: %v\n", accountID, updateErr)
				}
				e.recordRiskEvent(ctx, tenantID, accountID, "session_restore_failed", "high", err.Error())
				return nil, false, fmt.Errorf("restore session: %w", err)
			}
		}
	}

	// R181: 新连接入池前先探活验证密钥 —— 死密钥(AUTH_KEY_UNREGISTERED)在此即被捕获,
	// 避免把死连接放进池里让后续任务全部 401(重拨风暴的最后一道防线)
	probeCtx, probeCancel := context.WithTimeout(ctx, 8*time.Second)
	_, prePoolErr := client.GetMe(probeCtx)
	probeCancel()
	if prePoolErr != nil {
		client.Stop()
		if isAuthKeyDead(prePoolErr) {
			e.markDeadKey(ctx, tenantID, accountID)
			return nil, false, fmt.Errorf("会话密钥已失效(需重新登录)")
		}
		return nil, true, nil // 网络类失败交外层重试
	}

	// Add to pool and lock for exclusive use
	pc = &pooledClient{
		client:    client,
		accountID: accountID,
		tenantID:  tenantID,
		lastUsed:  time.Now(),
	}
	pc.mu.Lock()

	e.poolMu.Lock()
	// Double-check: another goroutine may have added this key while we were connecting
	if existing, exists := e.poolClients[key]; exists && existing != nil && existing.client != nil && existing.client.IsConnected() {
		// Use existing connection, discard new one
		pc.mu.Unlock() // unlock the new unused client
		existing.lastUsed = time.Now()
		e.poolMu.Unlock()
		client.Stop()
		// M修复: double-check 路径也探活, 死了就剔除走外层重试
		existing.mu.Lock()
		dcProbeErr := e.probeClient(ctx, existing.client)
		if dcProbeErr == nil {
			return existing.client, false, nil
		}
		existing.mu.Unlock()
		// R181: 死密钥 → 熔断, 不是普通死连接
		if isAuthKeyDead(dcProbeErr) {
			e.markDeadKey(ctx, tenantID, accountID)
			return nil, false, fmt.Errorf("会话密钥已失效(需重新登录)")
		}
		log.Printf("[client] double-check 路径拿到死连接, 剔除重拨 account=%s", accountID)
		e.InvalidatePooledClient(tenantID, accountID)
		return nil, true, nil
	}
	e.poolClients[key] = pc
	e.poolMu.Unlock()

	if err := e.updateStatus(ctx, tenantID, accountID, domain.AccountStatusOnline); err != nil {
		log.Printf("[client] failed to update status: %v", err)
	}
	// Keep lock held on success - caller must call ReleaseClient
	return client, false, nil
}

// ReleaseClient unlocks the client so other tasks can use it.
// 按客户端实例身份匹配要解锁的条目，而非按 key 盲找：死连接剔除/并发替换
// 会让同一 key 指向新条目，按 key 盲找会误解锁他人正在持有的锁
// （unlock of unlocked mutex，或双 goroutine 共用同一客户端会话）。
func (e *Engine) ReleaseClient(tenantID, accountID uuid.UUID, client *telegram.Client) {
	if client == nil {
		return
	}
	key := accountKey(tenantID, accountID)
	e.poolMu.RLock()
	pc, ok := e.poolClients[key]
	e.poolMu.RUnlock()
	if ok && pc != nil && pc.client == client {
		pc.mu.Unlock()
		return
	}
	// 保活账号的客户端不在池里：释放 GetClient 保活分支加的锁
	e.mu.RLock()
	managedMc, isManaged := e.clients[key]
	e.mu.RUnlock()
	if isManaged && managedMc != nil && managedMc.client == client {
		managedMc.mu.Unlock()
		return
	}
	// L修复(兜底): 池内 entry 已被断开/清理摘除, 但本 goroutine 仍握着它的锁。
	// 从孤儿锁表认领并解锁, 否则锁永久泄漏(该账号后续所有 GetClient 永远阻塞)。
	e.orphanMu.Lock()
	if orphan, ok := e.orphans[client]; ok {
		delete(e.orphans, client)
		e.orphanMu.Unlock()
		orphan.mu.Unlock()
		return
	}
	e.orphanMu.Unlock()
	// 都不匹配：该客户端已被死连接剔除接管（淘汰方持锁并负责 Stop），无需操作
}

// registerOrphan 登记"entry 已摘除但持有者可能仍握锁"的客户端,
// 供 ReleaseClient 兜底认领解锁。调用前必须已把 entry 从 poolClients 摘除。
func (e *Engine) registerOrphan(pc *pooledClient) {
	if pc == nil || pc.client == nil {
		return
	}
	e.orphanMu.Lock()
	if e.orphans == nil { // 防御: 测试等场景下未走 NewEngine 构造
		e.orphans = make(map[*telegram.Client]*pooledClient)
	}
	e.orphans[pc.client] = pc
	e.orphanMu.Unlock()
}

// stopPooledAsync 异步停止池化客户端: 先礼貌等锁(持有者在用), 等不到最后强停。
// 绝不持有 poolMu 调用, 也不在锁外裸 Stop(任务并发使用会损坏 mtgo 内部状态)。
func (e *Engine) stopPooledAsync(pc *pooledClient) {
	// 崩溃类修复: 本函数被裸 go 调用, Stop() panic 会崩进程, 必须自带 recover
	defer func() {
		if r := recover(); r != nil {
			log.Printf("[client] stopPooledAsync panic: %v", r)
		}
	}()
	if pc == nil || pc.client == nil {
		return
	}
	for i := 0; i < 30; i++ {
		if pc.mu.TryLock() {
			pc.client.Stop()
			pc.mu.Unlock()
			return
		}
		time.Sleep(time.Second)
	}
	// 持有者疑似失联(挂起/卡死): 强停。锁由孤儿锁表兜底给 ReleaseClient 解锁。
	log.Printf("[client] 账号 %s 客户端等待锁超时, 强制停止", pc.accountID)
	pc.client.Stop()
}

// stopManagedAsync 异步停止保活客户端(同 stopPooledAsync 语义)。
func stopManagedAsync(mc *managedClient) {
	// 崩溃类修复: 同 stopPooledAsync, 裸 go 调用必须自带 recover
	defer func() {
		if r := recover(); r != nil {
			log.Printf("[client] stopManagedAsync panic: %v", r)
		}
	}()
	if mc == nil || mc.client == nil {
		return
	}
	for i := 0; i < 30; i++ {
		if mc.mu.TryLock() {
			mc.client.Stop()
			mc.mu.Unlock()
			return
		}
		time.Sleep(time.Second)
	}
	log.Printf("[client] 保活账号 %s 客户端等待锁超时, 强制停止", mc.accountID)
	mc.client.Stop()
}

// DisconnectAccount removes an account from the connection pool and disconnects.
func (e *Engine) DisconnectAccount(ctx context.Context, tenantID, accountID uuid.UUID) error {
	key := accountKey(tenantID, accountID)

	// Remove from auth flow
	e.mu.Lock()
	if mc, ok := e.clients[key]; ok {
		mc.cancel()
		delete(e.clients, key)
	}
	e.mu.Unlock()

	// Remove from pool
	// R181: 锁内只摘除, 锁外异步停 —— 此前持全局 poolMu 裸 Stop: 秒级慢操作阻塞全部
	// GetClient, 且任务并发使用时损坏 mtgo 内部状态(同 cleanupIdleConnections 的修法)。
	e.poolMu.Lock()
	pc, hadPooled := e.poolClients[key]
	if hadPooled {
		delete(e.poolClients, key)
	}
	e.poolMu.Unlock()
	if hadPooled {
		go e.stopPooledAsync(pc)
	}

	return e.updateStatus(ctx, tenantID, accountID, domain.AccountStatusOffline)
}

// evictOldestIdleClientLocked removes the oldest idle client from the pool to make room.
// Caller must hold e.poolMu write lock.
func (e *Engine) evictOldestIdleClientLocked() bool {
	if len(e.poolClients) == 0 {
		return false
	}
	var oldestKey string
	var oldestTime time.Time
	for key, pc := range e.poolClients {
		if oldestKey == "" || pc.lastUsed.Before(oldestTime) {
			oldestKey = key
			oldestTime = pc.lastUsed
		}
	}
	if oldestKey != "" {
		pc := e.poolClients[oldestKey]
		log.Printf("[client] evicting oldest idle client %s to free pool space\n", pc.accountID)
		// 与 cleanupIdleConnections 一致：先拿 pc.mu 再操作，
		// 拿不到说明该 client 正在执行任务，跳过本次驱逐避免竞态。
		// 注意 TryLock 必须在 nil client 判断之外，否则无 client 的
		// 条目会绕过保护（本文件的 client_test.go 钉死此行为）。
		if !pc.mu.TryLock() {
			log.Printf("[client] 账号 %s 正在执行任务，跳过本次驱逐", pc.accountID)
			return false
		}
		defer pc.mu.Unlock()
		if pc.client != nil {
			pc.client.Stop()
		}
		delete(e.poolClients, oldestKey)
		return true
	}
	return false
}

// evictOldestIdleClient is the public wrapper that acquires the lock.
func (e *Engine) evictOldestIdleClient() bool {
	e.poolMu.Lock()
	defer e.poolMu.Unlock()
	return e.evictOldestIdleClientLocked()
}

// StartPoolCleaner starts a background goroutine that cleans up idle connections.

func (e *Engine) removeClient(key string) {
	e.mu.Lock()
	delete(e.clients, key)
	e.mu.Unlock()
}

func (e *Engine) GetClientByAccount(ctx context.Context, tenantID, accountID uuid.UUID) (*telegram.Client, interface{}, error) {
	client, err := e.GetClient(ctx, tenantID, accountID)
	if err != nil {
		return nil, nil, err
	}
	acc, err := e.repo.GetByID(ctx, tenantID, accountID)
	if err != nil {
		return client, nil, nil
	}
	return client, acc.Level, nil
}

// GetAllClients 返回租户下所有已连接客户端的快照。
// R修复(风险声明): 返回的客户端不持有 pc.mu 独占锁, 调用方绝不可对其发起
// RPC(mtgo 非线程安全)——仅适合被动接收 updates 等只读场景。
// 当前代码库已无 RPC 型调用方(monitor 走 GetClientByAccount 短借), 保留仅为接口兼容。
func (e *Engine) GetAllClients(tenantID uuid.UUID) []*telegram.Client {
	e.poolMu.RLock()
	defer e.poolMu.RUnlock()
	var clients []*telegram.Client
	for _, pc := range e.poolClients {
		if pc.tenantID == tenantID && pc.client != nil && pc.client.IsConnected() {
			clients = append(clients, pc.client)
		}
	}
	return clients
}

// GetClientByAccount returns a client and its level for a specific account (for monitor engine).

// WarmClient 预热账号连接：后台建好池化连接（不占锁、不阻塞调用方）。
// 任务创建后调用，等 worker 真正执行时 GetClient 直接命中热连接，零等待。
// 已有保活连接/池化连接/正在建连时静默返回。
func (e *Engine) WarmClient(ctx context.Context, tenantID, accountID uuid.UUID) {
	key := accountKey(tenantID, accountID)

	// 保活常驻连接无需预热
	e.mu.RLock()
	mc, isManaged := e.clients[key]
	e.mu.RUnlock()
	if isManaged && mc.client != nil && mc.client.IsConnected() {
		return
	}

	e.poolMu.RLock()
	_, inPool := e.poolClients[key]
	e.poolMu.RUnlock()
	if inPool {
		return
	}

	e.connectInflightMu.Lock()
	_, inflight := e.connectInflight[key]
	e.connectInflightMu.Unlock()
	if inflight {
		return
	}

	safego.Go(func() {
		wctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
		defer cancel()
		c, err := e.GetClient(wctx, tenantID, accountID)
		if err != nil {
			log.Printf("[warm] 账号 %s 预热失败: %v\n", accountID, err)
			return
		}
		// 放回池中保持热连接（不 Stop），执行时 GetClient 直接命中
		e.ReleaseClient(tenantID, accountID, c)
	})
}


// --- mtgo 断线自愈层（2026-09-21 源码交叉审计新增） ---

// isConnLoss: mtgo 连接类哨兵错误。语义是"连接没了"而非"账号死了"，
// 正确处置是剔除重拨，绝不许落 markAccountRestricted/handleDead。
// prepareRPC 对哨兵错误裸返回不包装（mtgo client.go），errors.Is 可靠。
func isConnLoss(err error) bool {
	if err == nil {
		return false
	}
	return errors.Is(err, telegram.ErrNotConnected) ||
		errors.Is(err, telegram.ErrClientClosed) ||
		errors.Is(err, telegram.ErrReconnectFailed) ||
		strings.Contains(err.Error(), "client: not connected")
}

// probeClient: 轻量探活。GetMe 第一行即 ensureConnectedContext（mtgo users.go），
// 死连接必返回 ErrNotConnected；me 有内存缓存，正常时成本一次 RPC 往返。
func (e *Engine) probeClient(ctx context.Context, client *telegram.Client) error {
	probeCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	_, err := client.GetMe(probeCtx)
	return err
}

// InvalidatePooledClient: 剔除池内死连接，下次 GetClient 走完整重拨。
// AutoConnect=false 的客户端断线不自愈，必须靠这里被动淘汰。
// 注意：调用方必须使用按实例身份匹配的 ReleaseClient（见该方法定义），
// 否则持有者会按 key 误解锁后续替换进来的新条目。
func (e *Engine) InvalidatePooledClient(tenantID, accountID uuid.UUID) {
	key := accountKey(tenantID, accountID)
	e.poolMu.Lock()
	pc, ok := e.poolClients[key]
	if ok {
		delete(e.poolClients, key)
		// P1-9 审计修复: registerOrphan 定义了却全仓零调用——entry 已摘除但
		// 持有者可能仍握锁在用, 不登记则 ReleaseClient 的兜底认领路径失效。
		e.registerOrphan(pc)
	}
	e.poolMu.Unlock()
	if ok && pc != nil && pc.client != nil {
		// 崩溃类修复: 裸 go 里 Stop() panic 会崩进程, 自带 recover
	go func() {
		defer func() {
			if r := recover(); r != nil {
				log.Printf("[client] InvalidatePooledClient stop panic: %v", r)
			}
		}()
		pc.mu.Lock()
		pc.client.Stop()
		pc.mu.Unlock()
	}()
	}
}
