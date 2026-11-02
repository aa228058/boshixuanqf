package account

import (
	"log"
	"runtime/debug"
	"tg-sender/internal/safego"
	"time"
)

func (e *Engine) StartPoolCleaner() {
	e.wg.Add(1)
	safego.Go(func() {
		defer e.wg.Done()
		defer func() {
			if r := recover(); r != nil {
				log.Printf("连接池清理异常: %v\n%s\n", r, debug.Stack())
			}
		}()
		ticker := time.NewTicker(5 * time.Minute)
		defer ticker.Stop()
		for {
			select {
			case <-e.stopCh:
				return
			case <-ticker.C:
				e.cleanupIdleConnections()
			}
		}
	})
}

// Stop signals all background goroutines to exit and waits for them.

func (e *Engine) cleanupIdleConnections() {
	now := time.Now()
	idleTimeout := e.cfg.PoolIdleTimeout
	if idleTimeout <= 0 {
		idleTimeout = 5 * time.Minute
	}
	e.poolMu.Lock()
	// P1-10 审计修复: 原实现持全局 poolMu 直接 Stop(网络断开是秒级慢操作)——
	// 期间所有 GetClient 全局阻塞; 更糟的是 Stop 等待内部 goroutine 退出时,
	// 若回调路径需要 poolMu 即成真死锁。先在锁内收集, 锁外逐个 Stop。
	var toStop []*pooledClient
	for key, pc := range e.poolClients {
		if now.Sub(pc.lastUsed) > idleTimeout {
			// TryLock to avoid stopping a client currently in use by a task
			if !pc.mu.TryLock() {
				continue
			}
			log.Printf("断开空闲账号 %s（空闲 %v）\n", pc.accountID, now.Sub(pc.lastUsed))
			delete(e.poolClients, key)
			pc.mu.Unlock()
			toStop = append(toStop, pc)
		}
	}
	e.poolMu.Unlock()
	for _, pc := range toStop {
		if pc.client != nil {
			pc.client.Stop()
		}
	}
}

// GetAccountStatus returns the current status of an account.

func (e *Engine) Stop() {
	e.stopOnce.Do(func() { close(e.stopCh) })
	e.wg.Wait()
}

func (e *Engine) Close() {
	e.DisconnectAll()
	if e.pgStorage != nil {
		if err := e.pgStorage.Close(); err != nil {
			log.Printf("[ENGINE] 关闭pgStorage失败: %v\n", err)
		}
	}
	// DO NOT close e.pool here — it is a shared pool owned by the database package.
}

// DisconnectAll 断开全部账号。
// G/L修复: 与 DisconnectAccount 同款语义 —— 先从 map 批量摘除,
// 登记孤儿锁表, 再逐个异步 TryLock 停止。绝不在 poolMu 下裸 Stop
// (任务可能正持有 pc.mu 使用客户端, 裸停会损坏 mtgo 内部状态并泄漏锁)。
func (e *Engine) DisconnectAll() {
	e.mu.Lock()
	mcs := make([]*managedClient, 0, len(e.clients))
	for key, mc := range e.clients {
		mcs = append(mcs, mc)
		delete(e.clients, key)
	}
	e.mu.Unlock()
	for _, mc := range mcs {
		mc.cancel()
		mc.finish() // 关闭提交通道, 防 SubmitCode 向已关闭 channel 发送
		go stopManagedAsync(mc)
	}

	e.poolMu.Lock()
	pcs := make([]*pooledClient, 0, len(e.poolClients))
	for key, pc := range e.poolClients {
		pcs = append(pcs, pc)
		delete(e.poolClients, key)
	}
	e.poolMu.Unlock()
	for _, pc := range pcs {
		e.registerOrphan(pc)
		go e.stopPooledAsync(pc)
	}
}

// Close releases the shared PostgreSQL storage and disconnects all accounts.
// Should be called during server shutdown after DisconnectAll.
// NOTE: Does NOT close the shared database pool — that is managed by the caller.
