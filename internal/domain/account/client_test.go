package account

import (
	"testing"
	"time"

	"github.com/google/uuid"
)

// 构造一个空 Engine（nil 依赖），只初始化和驱逐相关的字段
func newEvictionTestEngine() *Engine {
	return &Engine{
		poolClients: make(map[string]*pooledClient),
		maxPoolSize: 3,
	}
}

func insertPooled(e *Engine, tenantID, accountID uuid.UUID, lastUsed time.Time) *pooledClient {
	pc := &pooledClient{
		accountID: accountID,
		tenantID:  tenantID,
		lastUsed:  lastUsed,
	}
	e.poolClients[accountKey(tenantID, accountID)] = pc
	return pc
}

func TestEvictOldest_EvictsLeastRecentlyUsed(t *testing.T) {
	e := newEvictionTestEngine()
	tenant := uuid.New()

	old := insertPooled(e, tenant, uuid.New(), time.Now().Add(-30*time.Minute))
	insertPooled(e, tenant, uuid.New(), time.Now().Add(-5*time.Minute))
	newest := insertPooled(e, tenant, uuid.New(), time.Now().Add(-1*time.Minute))

	e.poolMu.Lock()
	evicted := e.evictOldestIdleClientLocked()
	e.poolMu.Unlock()

	if !evicted {
		t.Fatal("expected eviction to succeed")
	}
	if _, stillThere := e.poolClients[accountKey(tenant, old.accountID)]; stillThere {
		t.Error("oldest client was not evicted")
	}
	if _, gone := e.poolClients[accountKey(tenant, newest.accountID)]; !gone {
		t.Error("newest client was wrongly evicted")
	}
	if len(e.poolClients) != 2 {
		t.Errorf("pool size = %d, want 2", len(e.poolClients))
	}
}

func TestEvictOldest_SkipsWhenMuHeld(t *testing.T) {
	e := newEvictionTestEngine()
	tenant := uuid.New()

	// 只有这一个连接，且它的 mu 被占用（模拟正在执行任务）
	pc := insertPooled(e, tenant, uuid.New(), time.Now().Add(-30*time.Minute))
	pc.mu.Lock()
	defer pc.mu.Unlock()

	e.poolMu.Lock()
	evicted := e.evictOldestIdleClientLocked()
	e.poolMu.Unlock()

	if evicted {
		t.Error("must NOT evict a client whose mu is held (in-flight task); TryLock guard failed")
	}
	if _, gone := e.poolClients[accountKey(tenant, pc.accountID)]; !gone {
		t.Error("client should still be in pool")
	}
}

func TestEvictOldest_EmptyPool(t *testing.T) {
	e := newEvictionTestEngine()
	e.poolMu.Lock()
	evicted := e.evictOldestIdleClientLocked()
	e.poolMu.Unlock()
	if evicted {
		t.Error("empty pool must report no eviction")
	}
}

func TestGetClient_PoolFullReturnsErrorWhenAllBusy(t *testing.T) {
	e := newEvictionTestEngine()
	tenant := uuid.New()

	// 填满池，且全部 mu 被占用 → 驱逐全部跳过 → GetClient 应报错而不是杀在途连接
	var pcs []*pooledClient
	for i := 0; i < e.maxPoolSize; i++ {
		pc := insertPooled(e, tenant, uuid.New(), time.Now().Add(-time.Duration(i)*time.Minute))
		pc.mu.Lock()
		pcs = append(pcs, pc)
	}
	defer func() {
		for _, pc := range pcs {
			pc.mu.Unlock()
		}
	}()

	_, err := e.GetClient(t.Context(), tenant, uuid.New())
	if err == nil {
		t.Fatal("expected pool-full error when all clients busy, got nil")
	}
	// 所有连接必须原封不动
	if len(e.poolClients) != e.maxPoolSize {
		t.Errorf("pool size = %d, want %d (no client may be killed mid-task)", len(e.poolClients), e.maxPoolSize)
	}
}
