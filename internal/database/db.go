package database

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"tg-sender/internal/config"
)

// DB wraps pgxpool for application use.
type DB struct {
	Pool *pgxpool.Pool
}

// New creates a new database connection pool.
func New(ctx context.Context, cfg *config.Config) (*DB, error) {
	poolCfg, err := pgxpool.ParseConfig(cfg.DSN())
	if err != nil {
		return nil, fmt.Errorf("parse db config: %w", err)
	}

	poolCfg.MaxConns = int32(cfg.DBMaxConns)
	poolCfg.MinConns = int32(cfg.DBMaxIdle)
	poolCfg.MaxConnLifetime = cfg.DBMaxLife
	poolCfg.MaxConnIdleTime = cfg.DBMaxIdleTime
	poolCfg.HealthCheckPeriod = cfg.DBHealthCheckPeriod

	// 全局查询超时：此前所有 SQL 都拿 context.Background() 跑，数据库卡住时
	// 连接会无限期傻等堆积。statement_timeout 在数据库侧兜底——任何单条
	// SQL 超过 15s 自动取消，比逐个调用点包 context 干净且无死角。
	// LISTEN/NOTIFY 的 WaitForNotification 不受 statement_timeout 影响，安全。
	if poolCfg.ConnConfig.RuntimeParams == nil {
		poolCfg.ConnConfig.RuntimeParams = map[string]string{}
	}
	poolCfg.ConnConfig.RuntimeParams["statement_timeout"] = "15000" // 毫秒
	poolCfg.ConnConfig.RuntimeParams["idle_in_transaction_session_timeout"] = "30000"

	pool, err := pgxpool.NewWithConfig(ctx, poolCfg)
	if err != nil {
		return nil, fmt.Errorf("create pool: %w", err)
	}

	if err := pool.Ping(ctx); err != nil {
		return nil, fmt.Errorf("ping db: %w", err)
	}

	return &DB{Pool: pool}, nil
}

// Close gracefully shuts down the pool.
func (db *DB) Close() {
	db.Pool.Close()
}

// ListenConn returns a dedicated connection for LISTEN/NOTIFY.
// Caller must call Release() when done.
func (db *DB) ListenConn(ctx context.Context) (*pgxpool.Conn, error) {
	return db.Pool.Acquire(ctx)
}

// RawConn returns the underlying pgx.Conn from a pool connection.
func RawConn(conn *pgxpool.Conn) *pgx.Conn {
	return conn.Conn()
}
