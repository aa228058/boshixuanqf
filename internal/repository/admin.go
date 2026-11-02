package repository

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"golang.org/x/crypto/bcrypt"
	"tg-sender/internal/domain"
)

// AdminRepository handles admin data access.
type AdminRepository struct {
	db *pgxpool.Pool
}

// NewAdminRepository creates a new repository.
func NewAdminRepository(db *pgxpool.Pool) *AdminRepository {
	return &AdminRepository{db: db}
}

// GetByUsername retrieves an admin by username.
func (r *AdminRepository) GetByUsername(ctx context.Context, username string) (*domain.Admin, error) {
	var a domain.Admin
	err := r.db.QueryRow(ctx, `
	    SELECT admin_id, username, password_hash, role, created_at
	    FROM admins WHERE username = $1`, username,
	).Scan(&a.AdminID, &a.Username, &a.PasswordHash, &a.Role, &a.CreatedAt)
	if err != nil {
		return nil, fmt.Errorf("get admin: %w", err)
	}
	return &a, nil
}

// VerifyPassword checks if the password matches.
func (r *AdminRepository) VerifyPassword(hash, password string) bool {
	return bcrypt.CompareHashAndPassword([]byte(hash), []byte(password)) == nil
}

// ListTenants returns tenants with pagination and filters (admin).
func (r *AdminRepository) ListTenants(ctx context.Context, status string, page, pageSize int) ([]domain.Tenant, int, error) {
	where := "WHERE deleted_at IS NULL"
	args := []any{}
	argIdx := 1
	if status != "" {
		where += fmt.Sprintf(" AND status = $%d", argIdx)
		args = append(args, status)
		argIdx++
	}

	var total int
	err := r.db.QueryRow(ctx, "SELECT COUNT(*) FROM tenants "+where, args...).Scan(&total)
	if err != nil {
		return nil, 0, err
	}

	offset := (page - 1) * pageSize
	args = append(args, pageSize, offset)

	rows, err := r.db.Query(ctx, `
	    SELECT tenant_id, name, api_key_hash, api_key_prefix, max_accounts, max_concurrent_tasks, plan_id, status, expires_at, created_at
	    FROM tenants `+where+` ORDER BY created_at DESC LIMIT $`+fmt.Sprintf("%d", argIdx)+` OFFSET $`+fmt.Sprintf("%d", argIdx+1), args...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	var tenants []domain.Tenant
	for rows.Next() {
		var t domain.Tenant
		if err := rows.Scan(&t.TenantID, &t.Name, &t.APIKeyHash, &t.APIKeyPrefix, &t.MaxAccounts, &t.MaxConcurrentTasks, &t.PlanID, &t.Status, &t.ExpiresAt, &t.CreatedAt); err != nil {
			continue
		}
		tenants = append(tenants, t)
	}
	return tenants, total, nil
}

// UpdateTenant updates tenant info (admin).
func (r *AdminRepository) UpdateTenant(ctx context.Context, tenantID uuid.UUID, updates map[string]any) error {
	// Column whitelist to prevent SQL injection
	allowedCols := map[string]bool{"plan_id": true, "status": true, "max_accounts": true, "max_concurrent_tasks": true, "expires_at": true, "name": true}
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

// ResetAPIKey generates a new API key for a tenant.
func (r *AdminRepository) ResetAPIKey(ctx context.Context, tenantID uuid.UUID) (string, error) {
	newKey := uuid.New().String()
	hash, err := bcrypt.GenerateFromPassword([]byte(newKey), 12)
	if err != nil {
		return "", err
	}
	prefix := computeAPIKeyPrefix(newKey)
	_, err = r.db.Exec(ctx, `UPDATE tenants SET api_key_hash = $2, api_key_prefix = $3 WHERE tenant_id = $1`, tenantID, string(hash), prefix)
	if err != nil {
		return "", err
	}
	return newKey, nil
}

// Create inserts a new admin.
func (r *AdminRepository) Create(ctx context.Context, a *domain.Admin) error {
	_, err := r.db.Exec(ctx, `
	    INSERT INTO admins (admin_id, username, password_hash, role)
	    VALUES ($1, $2, $3, $4)`,
		a.AdminID, a.Username, a.PasswordHash, a.Role,
	)
	if err != nil {
		return fmt.Errorf("create admin: %w", err)
	}
	return nil
}
