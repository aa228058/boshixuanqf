package repository

import (
	"context"
	"fmt"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"tg-sender/internal/domain"
)

// connectTestDB 从环境变量连接测试数据库。未设置 DB_HOST 时 skip，
// 使本地无库也能跑单测；CI 里有 Postgres 服务会真实执行。
func connectTestDB(t *testing.T) *pgxpool.Pool {
	t.Helper()
	host := os.Getenv("DB_HOST")
	if host == "" {
		t.Skip("DB_HOST not set; skipping integration test")
	}
	port := os.Getenv("DB_PORT")
	if port == "" {
		port = "5432"
	}
	dsn := fmt.Sprintf("postgres://%s:%s@%s:%s/%s?sslmode=disable",
		os.Getenv("DB_USER"), os.Getenv("DB_PASSWORD"), host, port, os.Getenv("DB_NAME"))
	pool, err := pgxpool.New(context.Background(), dsn)
	if err != nil {
		t.Fatalf("connect test db: %v", err)
	}
	t.Cleanup(pool.Close)
	return pool
}

// ensureTasksTable 建最小 tasks 表（仅含 ClaimTasks 触碰的列），
// 与正式迁移无关，测试自包含。
func ensureTasksTable(t *testing.T, pool *pgxpool.Pool) {
	t.Helper()
	_, err := pool.Exec(context.Background(), `
		CREATE TABLE IF NOT EXISTS tasks (
			task_id UUID PRIMARY KEY,
			tenant_id UUID NOT NULL,
			type TEXT NOT NULL,
			status TEXT NOT NULL DEFAULT 'queued',
			priority INT DEFAULT 5,
			account_ids JSONB DEFAULT '[]',
			target_ids JSONB DEFAULT '[]',
			payload JSONB DEFAULT '{}',
			progress JSONB DEFAULT '{}',
			locked_at TIMESTAMPTZ,
			worker_id TEXT,
			retry_count INT DEFAULT 0,
			scheduled_at TIMESTAMPTZ,
			started_at TIMESTAMPTZ,
			completed_at TIMESTAMPTZ,
			created_at TIMESTAMPTZ DEFAULT NOW(),
			is_loop BOOLEAN DEFAULT FALSE,
			loop_interval_minutes INT DEFAULT 60,
			min_interval_seconds INT DEFAULT 5,
			max_interval_seconds INT DEFAULT 15,
			next_run_at TIMESTAMPTZ
		)`)
	if err != nil {
		t.Fatalf("create tasks table: %v", err)
	}
	if _, err := pool.Exec(context.Background(), `TRUNCATE tasks`); err != nil {
		t.Fatalf("truncate tasks: %v", err)
	}
}

func seedTask(t *testing.T, pool *pgxpool.Pool, tenantID uuid.UUID, taskType string, priority int) uuid.UUID {
	t.Helper()
	id := uuid.New()
	_, err := pool.Exec(context.Background(),
		`INSERT INTO tasks (task_id, tenant_id, type, status, priority) VALUES ($1,$2,$3,'queued',$4)`,
		id, tenantID, taskType, priority)
	if err != nil {
		t.Fatalf("seed task: %v", err)
	}
	return id
}

func TestClaimTasks_ClaimsQueuedInPriorityOrder(t *testing.T) {
	pool := connectTestDB(t)
	ensureTasksTable(t, pool)
	repo := NewTaskRepository(pool)
	tenant := uuid.New()

	low := seedTask(t, pool, tenant, string(domain.TaskTypeBroadcast), 1)
	mid := seedTask(t, pool, tenant, string(domain.TaskTypeBroadcast), 5)
	high := seedTask(t, pool, tenant, string(domain.TaskTypeBroadcast), 9)

	claimed, err := repo.ClaimTasks(context.Background(), "worker-A", 10, 5)
	if err != nil {
		t.Fatalf("ClaimTasks: %v", err)
	}
	if len(claimed) != 3 {
		t.Fatalf("claimed %d tasks, want 3", len(claimed))
	}
	// 优先级降序：high → mid → low
	if claimed[0].TaskID != high || claimed[1].TaskID != mid || claimed[2].TaskID != low {
		t.Errorf("claim order wrong: got %v,%v,%v want %v,%v,%v",
			claimed[0].TaskID, claimed[1].TaskID, claimed[2].TaskID, high, mid, low)
	}
	// 认领后状态必须 running 且带 worker_id（WorkerID 是 *string）
	for _, task := range claimed {
		if task.Status != "running" {
			t.Errorf("task %s status = %q, want running", task.TaskID, task.Status)
		}
		if task.WorkerID == nil || *task.WorkerID != "worker-A" {
			t.Errorf("task %s worker = %v, want worker-A", task.TaskID, task.WorkerID)
		}
	}
	// 再认领应为空（无 queued 剩余）
	again, err := repo.ClaimTasks(context.Background(), "worker-B", 10, 5)
	if err != nil {
		t.Fatalf("second ClaimTasks: %v", err)
	}
	if len(again) != 0 {
		t.Errorf("second claim got %d tasks, want 0 (all already running)", len(again))
	}
}

func TestClaimTasks_ConcurrentClaimersGetDisjointSets(t *testing.T) {
	pool := connectTestDB(t)
	ensureTasksTable(t, pool)
	repo := NewTaskRepository(pool)
	tenant := uuid.New()

	const total = 20
	ids := make([]uuid.UUID, total)
	for i := 0; i < total; i++ {
		ids[i] = seedTask(t, pool, tenant, string(domain.TaskTypeBroadcast), 5)
	}

	// 两个并发 worker 各认领 12（> total/2，必然竞争同一批行）
	var wg sync.WaitGroup
	results := make([][]*domain.Task, 2)
	errs := make([]error, 2)
	for w := 0; w < 2; w++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			// 轻微错峰让两个事务真正重叠在 SKIP LOCKED 上
			time.Sleep(time.Duration(idx) * 50 * time.Millisecond)
			tasks, err := repo.ClaimTasks(context.Background(), fmt.Sprintf("worker-%d", idx), 12, 5)
			results[idx] = tasks
			errs[idx] = err
		}(w)
	}
	wg.Wait()
	for i, err := range errs {
		if err != nil {
			t.Fatalf("worker-%d claim: %v", i, err)
		}
	}

	got := len(results[0]) + len(results[1])
	if got != total {
		t.Errorf("total claimed = %d, want %d (double-claim or lost task!)", got, total)
	}

	// 两批必须不相交（SKIP LOCKED 核心保证）
	seen := map[uuid.UUID]int{}
	for _, batch := range results {
		for _, task := range batch {
			seen[task.TaskID]++
		}
	}
	for id, n := range seen {
		if n > 1 {
			t.Errorf("task %s claimed %d times by different workers", id, n)
		}
	}
}

func TestClaimTimeoutTasks_ReclaimsExpiredLock(t *testing.T) {
	pool := connectTestDB(t)
	ensureTasksTable(t, pool)
	repo := NewTaskRepository(pool)
	tenant := uuid.New()

	id := seedTask(t, pool, tenant, string(domain.TaskTypeBroadcast), 5)
	// 模拟崩溃的 worker：状态 running 但 locked_at 已过期 6 分钟
	_, err := pool.Exec(context.Background(),
		`UPDATE tasks SET status='running', worker_id='crashed-worker', locked_at = NOW() - INTERVAL '6 minutes' WHERE task_id=$1`, id)
	if err != nil {
		t.Fatalf("simulate crashed worker: %v", err)
	}

	claimed, err := repo.ClaimTimeoutTasks(context.Background(), "rescue-worker", 5, 5)
	if err != nil {
		t.Fatalf("ClaimTimeoutTasks: %v", err)
	}
	if len(claimed) != 1 || claimed[0].TaskID != id {
		t.Fatalf("reclaimed %d tasks, want the crashed one", len(claimed))
	}
	if claimed[0].WorkerID == nil || *claimed[0].WorkerID != "rescue-worker" {
		t.Errorf("new worker = %v, want rescue-worker", claimed[0].WorkerID)
	}
}
