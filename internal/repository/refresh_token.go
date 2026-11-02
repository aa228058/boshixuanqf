package repository

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// RefreshTokenRepository handles refresh token data access.
type RefreshTokenRepository struct {
	db *pgxpool.Pool
}

// NewRefreshTokenRepository creates a new refresh token repository.
func NewRefreshTokenRepository(db *pgxpool.Pool) *RefreshTokenRepository {
	return &RefreshTokenRepository{db: db}
}

// Create stores a new refresh token.
func (r *RefreshTokenRepository) Create(ctx context.Context, jti uuid.UUID, tenantID uuid.UUID, expiresAt time.Time) error {
	_, err := r.db.Exec(ctx, `
		INSERT INTO refresh_tokens (jti, tenant_id, expires_at)
		VALUES ($1, $2, $3)`,
		jti, tenantID, expiresAt)
	if err != nil {
		return fmt.Errorf("store refresh token: %w", err)
	}
	return nil
}

// Peek performs a non-consuming existence check (V16 审计修复的配套):
// 轮换流程先把新 token 签发入库、最后才由 Validate 原子消费旧 token。
// 若无 Peek 预检, 无效 token 的请求也会在签发阶段往库里插入孤儿新 token
// (靠 CleanupExpired 兜底太慢)。Peek 让无效请求在签发前就被 401 挡掉。
func (r *RefreshTokenRepository) Peek(ctx context.Context, jti uuid.UUID) (uuid.UUID, error) {
	var tenantID uuid.UUID
	err := r.db.QueryRow(ctx, `
		SELECT tenant_id FROM refresh_tokens
		WHERE jti = $1 AND expires_at > NOW() AND revoked_at IS NULL`,
		jti).Scan(&tenantID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return uuid.Nil, fmt.Errorf("invalid or expired refresh token")
		}
		return uuid.Nil, fmt.Errorf("refresh peek db error: %w", err)
	}
	return tenantID, nil
}

// Validate checks if a refresh token exists, is not expired, and not revoked.
func (r *RefreshTokenRepository) Validate(ctx context.Context, jti uuid.UUID) (uuid.UUID, error) {
	// L-01: 原子消费(DELETE...RETURNING)——原 SELECT 校验存在并发复用窗口:
	// 同一 refresh_token 并发请求 N 次可同时通过校验签发 N 个新 token。
	// 消费式删除保证任何 refresh_token 在其生命周期内最多成功刷新一次。
	var tenantID uuid.UUID
	err := r.db.QueryRow(ctx, `
		DELETE FROM refresh_tokens
		WHERE jti = $1 AND expires_at > NOW() AND revoked_at IS NULL
		RETURNING tenant_id`,
		jti).Scan(&tenantID)
	if err != nil {
		// V15 审计修复: 原来 err != nil 统一报 invalid——DB 抖动(连接断开/超时)和
		// "token 不存在"是同一错误, 用户被 DB 故障误登出。区分 ErrNoRows(真无效)与其他
		// 错误(基础设施故障, 向上传递让调用方报 503 而不是清会话)。
		if errors.Is(err, pgx.ErrNoRows) {
			return uuid.Nil, fmt.Errorf("invalid or expired refresh token")
		}
		return uuid.Nil, fmt.Errorf("refresh validate db error: %w", err)
	}
	return tenantID, nil
}

// Revoke marks a refresh token as revoked.
func (r *RefreshTokenRepository) Revoke(ctx context.Context, jti uuid.UUID) error {
	_, err := r.db.Exec(ctx, `
		UPDATE refresh_tokens SET revoked_at = NOW() WHERE jti = $1`, jti)
	return err
}

// RevokeAllForTenant revokes all refresh tokens for a tenant.
func (r *RefreshTokenRepository) RevokeAllForTenant(ctx context.Context, tenantID uuid.UUID) error {
	_, err := r.db.Exec(ctx, `
		UPDATE refresh_tokens SET revoked_at = NOW()
		WHERE tenant_id = $1 AND revoked_at IS NULL`, tenantID)
	return err
}

// CleanupExpired removes expired tokens older than 7 days.
func (r *RefreshTokenRepository) CleanupExpired(ctx context.Context) error {
	_, err := r.db.Exec(ctx, `
		DELETE FROM refresh_tokens
		WHERE expires_at < NOW() - INTERVAL '7 days' OR revoked_at < NOW() - INTERVAL '7 days'`)
	return err
}
