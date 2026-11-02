package repository

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"tg-sender/internal/config"
	"tg-sender/internal/domain"
)

// TenantRepository handles tenant data access.
type TenantRepository struct {
	db  *pgxpool.Pool
	cfg *config.Config
}

// NewTenantRepository creates a new tenant repository.
func NewTenantRepository(db *pgxpool.Pool, cfg *config.Config) *TenantRepository {
	return &TenantRepository{db: db, cfg: cfg}
}

// UpdateNotify R119+R120: 保存租户通知设置(接收邮箱+事件订阅+SMTP 发件配置)
func (r *TenantRepository) UpdateNotify(ctx context.Context, tenantID uuid.UUID, email string, settings, smtp json.RawMessage) error {
	_, err := r.db.Exec(ctx,
		`UPDATE tenants SET notify_email = $2, notify_settings = $3, notify_smtp = $4 WHERE tenant_id = $1`,
		tenantID, email, settings, smtp)
	return err
}

// GetByID retrieves a tenant by ID.
func (r *TenantRepository) GetByID(ctx context.Context, tenantID uuid.UUID) (*domain.Tenant, error) {
	var t domain.Tenant
	var expiresAt *time.Time
	err := r.db.QueryRow(ctx, `
		SELECT tenant_id, name, username, password_hash, api_key_hash, max_accounts, max_concurrent_tasks, plan_id, status, expires_at, created_at, notify_email, notify_settings, notify_smtp
		FROM tenants WHERE tenant_id = $1 AND deleted_at IS NULL`, tenantID,
	).Scan(&t.TenantID, &t.Name, &t.Username, &t.PasswordHash, &t.APIKeyHash, &t.MaxAccounts, &t.MaxConcurrentTasks, &t.PlanID, &t.Status, &expiresAt, &t.CreatedAt, &t.NotifyEmail, &t.NotifySettings, &t.NotifySMTP)
	if err != nil {
		return nil, fmt.Errorf("get tenant: %w", err)
	}
	t.ExpiresAt = expiresAt
	return &t, nil
}

// computeAPIKeyPrefix returns the first 8 hex chars of SHA256(apiKey) for fast index lookup.
func computeAPIKeyPrefix(apiKey string) string {
	h := sha256.Sum256([]byte(apiKey))
	return hex.EncodeToString(h[:])[:8]
}

// Create inserts a new tenant.
// apiKeyPlaintext 用于计算 api_key_prefix 索引。F-29: API key 认证原语已删除
// （从未接线且前缀索引有历史性失配缺陷），本列仅作数据保留，供未来重新设计。
func (r *TenantRepository) Create(ctx context.Context, t *domain.Tenant, apiKeyPlaintext string) error {
	prefix := computeAPIKeyPrefix(apiKeyPlaintext)
	_, err := r.db.Exec(ctx, `
		INSERT INTO tenants (tenant_id, name, username, password_hash, api_key_hash, api_key_prefix, max_accounts, max_concurrent_tasks, plan_id, status, expires_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)`,
		t.TenantID, t.Name, t.Username, t.PasswordHash, t.APIKeyHash, prefix, t.MaxAccounts, t.MaxConcurrentTasks, t.PlanID, t.Status, t.ExpiresAt,
	)
	return err
}

// Update updates tenant fields.
func (r *TenantRepository) Update(ctx context.Context, tenantID uuid.UUID, updates map[string]any) error {
	// Column whitelist to prevent SQL injection
	allowedCols := map[string]bool{"plan_id": true, "status": true, "max_accounts": true, "max_concurrent_tasks": true, "expires_at": true, "name": true, "username": true, "password_hash": true, "api_key_hash": true, "api_key_prefix": true}
	setParts := []string{}
	args := []any{tenantID}
	idx := 2
	for col, val := range updates {
		if !allowedCols[col] {
			continue
		}
		setParts = append(setParts, fmt.Sprintf("%s = $%d", col, idx))
		args = append(args, val)
		idx++
	}
	if len(setParts) == 0 {
		return nil
	}
	query := "UPDATE tenants SET " + setParts[0]
	for i := 1; i < len(setParts); i++ {
		query += ", " + setParts[i]
	}
	query += " WHERE tenant_id = $1"
	_, err := r.db.Exec(ctx, query, args...)
	return err
}

// UpdateAPIKeyHash updates the API key hash and prefix for a tenant.
// apiKeyPlaintext 用于计算前缀索引（F-29: 查询侧原语已删除，仅保留列写入）。
func (r *TenantRepository) UpdateAPIKeyHash(ctx context.Context, tenantID uuid.UUID, hash string, apiKeyPlaintext string) error {
	prefix := computeAPIKeyPrefix(apiKeyPlaintext)
	_, err := r.db.Exec(ctx, `UPDATE tenants SET api_key_hash = $2, api_key_prefix = $3 WHERE tenant_id = $1`, tenantID, hash, prefix)
	if err != nil {
		return fmt.Errorf("update api key hash: %w", err)
	}
	return nil
}

// List returns paginated tenants with filters.
func (r *TenantRepository) List(ctx context.Context, page, pageSize int, status, planID string) ([]*domain.Tenant, int, error) {
	where := "WHERE deleted_at IS NULL"
	args := []any{}
	idx := 1
	if status != "" {
		where += fmt.Sprintf(" AND status = $%d", idx)
		args = append(args, status)
		idx++
	}
	if planID != "" {
		where += fmt.Sprintf(" AND plan_id = $%d", idx)
		args = append(args, planID)
		idx++
	}

	var total int
	err := r.db.QueryRow(ctx, "SELECT COUNT(*) FROM tenants "+where, args...).Scan(&total)
	if err != nil {
		return nil, 0, err
	}

	offset := (page - 1) * pageSize
	args = append(args, pageSize, offset)

	// P2-74 审计修复: 原 SELECT 不含 username——管理端租户列表该字段恒空。
	rows, err := r.db.Query(ctx, `
		SELECT tenant_id, username, name, api_key_hash, max_accounts, max_concurrent_tasks, plan_id, status, expires_at, created_at
		FROM tenants `+where+` ORDER BY created_at DESC LIMIT $`+fmt.Sprintf("%d", idx)+` OFFSET $`+fmt.Sprintf("%d", idx+1), args...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	var tenants []*domain.Tenant
	for rows.Next() {
		var t domain.Tenant
		var expiresAt *time.Time
		if err := rows.Scan(&t.TenantID, &t.Username, &t.Name, &t.APIKeyHash, &t.MaxAccounts, &t.MaxConcurrentTasks, &t.PlanID, &t.Status, &expiresAt, &t.CreatedAt); err != nil {
			continue
		}
		t.ExpiresAt = expiresAt
		tenants = append(tenants, &t)
	}
	return tenants, total, nil
}

// GetByUsername finds a tenant by username.
func (r *TenantRepository) GetByUsername(ctx context.Context, username string) (*domain.Tenant, error) {
	var t domain.Tenant
	var expiresAt *time.Time
	err := r.db.QueryRow(ctx, `
		SELECT tenant_id, name, username, password_hash, api_key_hash, max_accounts, max_concurrent_tasks, plan_id, status, expires_at, created_at
		FROM tenants WHERE username = $1 AND deleted_at IS NULL`, username).Scan(
		&t.TenantID, &t.Name, &t.Username, &t.PasswordHash, &t.APIKeyHash, &t.MaxAccounts, &t.MaxConcurrentTasks, &t.PlanID, &t.Status, &expiresAt, &t.CreatedAt,
	)
	if err != nil {
		return nil, fmt.Errorf("get tenant by username: %w", err)
	}
	t.ExpiresAt = expiresAt
	return &t, nil
}
