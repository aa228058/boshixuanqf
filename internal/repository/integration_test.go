//go:build integration

// T 工作流：裸 SQL 集成测试——D-00（campaigns 主键列名漂移）证明编译器/vet 均
// 覆盖不到裸 SQL 字符串，必须用真实 PostgreSQL 验证。
// 运行：go test -tags=integration ./internal/repository/  （需 Docker）
package repository

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"
	"github.com/testcontainers/testcontainers-go/modules/postgres"
)

// setupTestDB 启动容器化 PG，应用全部迁移，返回连接池。
func setupTestDB(t *testing.T) *pgxpool.Pool {
	t.Helper()
	ctx := context.Background()
	// postgres.Run 自带就绪等待，无需显式 WaitStrategy
	pg, err := postgres.Run(ctx, "postgres:16-alpine",
		postgres.WithDatabase("testdb"),
		postgres.WithUsername("test"),
		postgres.WithPassword("test"),
	)
	if err != nil {
		t.Fatalf("start postgres container: %v", err)
	}
	t.Cleanup(func() { _ = pg.Terminate(ctx) })

	dsn, err := pg.ConnectionString(ctx)
	if err != nil {
		t.Fatalf("connection string: %v", err)
	}
	pool, err := pgxpool.New(ctx, dsn+"&sslmode=disable")
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	t.Cleanup(pool.Close)

	// 应用全部迁移（与生产同路径）
	migDir, err := filepath.Abs("../../migrations")
	if err != nil {
		t.Fatalf("migrations path: %v", err)
	}
	if err := goose.SetDialect("postgres"); err != nil {
		t.Fatalf("goose dialect: %v", err)
	}
	if err := goose.Up(stdlib.OpenDBFromPool(pool), migDir); err != nil {
		t.Fatalf("apply migrations: %v", err)
	}
	return pool
}

// TestDeleteTenantCascadeQueries：验证 D-00 类列名漂移不回归——
// 软删除/PurgeTenant 的全部子查询必须在真实 Schema 上执行成功。
func TestTenantSoftDeleteAndPurgeQueries(t *testing.T) {
	pool := setupTestDB(t)
	ctx := context.Background()

	// 种子：租户+账号+活动+活动账号（验证 campaign 子查询的 id 列名）
	_, err := pool.Exec(ctx, `
		INSERT INTO tenants (tenant_id, username, password_hash, name, api_key_hash, plan_id)
		VALUES ('11111111-1111-1111-1111-111111111111', 'integtest', 'x', 'Integ', 'integ-test-key-hash', 'free');
		INSERT INTO accounts (account_id, tenant_id, phone, status)
		VALUES ('22222222-2222-2222-2222-222222222222', '11111111-1111-1111-1111-111111111111', '+10000000000', 'active');
		INSERT INTO campaigns (id, tenant_id, name, status)  -- 注意：主键是 id（D-00 教训）
		VALUES ('33333333-3333-3333-3333-333333333333', '11111111-1111-1111-1111-111111111111', 'c1', 'running');
		INSERT INTO campaign_accounts (campaign_id, account_id)
		VALUES ('33333333-3333-3333-3333-333333333333', '22222222-2222-2222-2222-222222222222');
	`)
	if err != nil {
		t.Fatalf("seed: %v", err)
	}

	// 软删除：deleted_at 置位，行保留
	var deletedAt *time.Time
	if err := pool.QueryRow(ctx,
		`UPDATE tenants SET status='suspended', deleted_at=NOW() WHERE tenant_id=$1 RETURNING deleted_at`,
		"11111111-1111-1111-1111-111111111111").Scan(&deletedAt); err != nil {
		t.Fatalf("soft delete: %v", err)
	}
	if deletedAt == nil {
		t.Fatal("deleted_at should be set")
	}

	// GetByID 过滤软删
	var n int
	if err := pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM tenants WHERE tenant_id=$1 AND deleted_at IS NULL`,
		"11111111-1111-1111-1111-111111111111").Scan(&n); err != nil || n != 0 {
		t.Fatalf("GetByID filter broken: n=%d err=%v", n, err)
	}

	// 模拟 PurgeTenant 的三个子查询（与 admin.go 中的裸 SQL 保持同构）
	if _, err := pool.Exec(ctx, `DELETE FROM campaign_accounts WHERE campaign_id IN (SELECT id FROM campaigns WHERE tenant_id = $1)`,
		"11111111-1111-1111-1111-111111111111"); err != nil {
		t.Fatalf("campaign_accounts subquery (D-00 regression): %v", err)
	}
	if _, err := pool.Exec(ctx, `DELETE FROM campaigns WHERE tenant_id = $1`,
		"11111111-1111-1111-1111-111111111111"); err != nil {
		t.Fatalf("campaigns delete: %v", err)
	}

	// ListRunning / GetPendingAll 的 JOIN 查询（G-03）在真实 Schema 上成立
	if _, err := pool.Exec(ctx, `
		SELECT campaigns.id FROM campaigns
		JOIN tenants t ON t.tenant_id = campaigns.tenant_id
		WHERE campaigns.status='running' AND t.status='active'
		  AND (t.expires_at IS NULL OR t.expires_at > NOW())`); err != nil {
		t.Fatalf("ListRunning join query (G-03): %v", err)
	}
	if _, err := pool.Exec(ctx, `
		SELECT pa.id FROM pending_approvals pa
		JOIN tenants t ON t.tenant_id = pa.tenant_id
		WHERE pa.status='pending' AND t.status='active'
		  AND (t.expires_at IS NULL OR t.expires_at > NOW())`); err != nil {
		t.Fatalf("GetPendingAll join query (G-03): %v", err)
	}
}

// TestUpdateStatusGuardMatrix：F-27 守卫在真实 DB 上的迁移矩阵。
func TestUpdateStatusGuardMatrix(t *testing.T) {
	pool := setupTestDB(t)
	ctx := context.Background()
	_, err := pool.Exec(ctx, `
		INSERT INTO tenants (tenant_id, username, password_hash, name, api_key_hash, plan_id)
		VALUES ('11111111-1111-1111-1111-111111111111', 'guardtest', 'x', 'G', 'guard-test-key-hash', 'free');
		INSERT INTO tasks (tenant_id, task_id, task_type, name, status, target_type)
		VALUES ('11111111-1111-1111-1111-111111111111', '44444444-4444-4444-4444-444444444444', 'broadcast', 't', 'running', 'group');
	`)
	if err != nil {
		t.Fatalf("seed: %v", err)
	}
	// 非法迁移 running->scheduled 应命中 0 行
	res, err := pool.Exec(ctx, `
		UPDATE tasks SET status='scheduled'
		WHERE tenant_id=$1 AND task_id=$2 AND (
			'scheduled' IN ('failed','cancelled','queued')
			OR (status='queued' AND 'scheduled' IN ('running','cancelled'))
			OR (status='running' AND 'scheduled' IN ('paused','completed','failed','cancelled')))`,
		"11111111-1111-1111-1111-111111111111", "44444444-4444-4444-4444-444444444444")
	if err != nil {
		t.Fatalf("guard query: %v", err)
	}
	if res.RowsAffected() != 0 {
		t.Fatal("illegal transition running->scheduled should affect 0 rows")
	}
	// 合法迁移 running->paused 应命中 1 行
	res, err = pool.Exec(ctx, `
		UPDATE tasks SET status='paused'
		WHERE tenant_id=$1 AND task_id=$2 AND (
			'paused' IN ('failed','cancelled','queued')
			OR (status='running' AND 'paused' IN ('paused','completed','failed','cancelled')))`,
		"11111111-1111-1111-1111-111111111111", "44444444-4444-4444-4444-444444444444")
	if err != nil {
		t.Fatalf("legal query: %v", err)
	}
	if res.RowsAffected() != 1 {
		t.Fatal("legal transition running->paused should affect 1 row")
	}
}
