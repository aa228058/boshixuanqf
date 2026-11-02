package repository

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"tg-sender/internal/domain"
)

// ProbeConfigRepository 探测配置(全局单行)
type ProbeConfigRepository struct {
	db *pgxpool.Pool
}

func NewProbeConfigRepository(db *pgxpool.Pool) *ProbeConfigRepository {
	return &ProbeConfigRepository{db: db}
}

// GetProbeConfig probe.ConfigProvider 接口适配(方法别名)
func (r *ProbeConfigRepository) GetProbeConfig(ctx context.Context) (*domain.ProbeConfig, error) {
	return r.Get(ctx)
}

// Get 读取探测配置(id=1, 一定存在由迁移保底插入)
func (r *ProbeConfigRepository) Get(ctx context.Context) (*domain.ProbeConfig, error) {
	var c domain.ProbeConfig
	err := r.db.QueryRow(ctx,
		`SELECT id, test_username, private_invite, public_link, updated_at FROM probe_configs WHERE id = 1`,
	).Scan(&c.ID, &c.TestUsername, &c.PrivateInvite, &c.PublicLink, &c.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &c, nil
}

// Update 更新三项配置(单行 upsert, 任何时刻只有 id=1)
func (r *ProbeConfigRepository) Update(ctx context.Context, testUsername, privateInvite, publicLink string) error {
	_, err := r.db.Exec(ctx,
		`INSERT INTO probe_configs (id, test_username, private_invite, public_link, updated_at)
		 VALUES (1, $1, $2, $3, now())
		 ON CONFLICT (id) DO UPDATE SET test_username = $1, private_invite = $2, public_link = $3, updated_at = now()`,
		testUsername, privateInvite, publicLink)
	return err
}
