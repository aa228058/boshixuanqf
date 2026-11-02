package repository

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"math/big"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"tg-sender/internal/domain"
)

// ActivationCodeRepository handles activation code data access.
type ActivationCodeRepository struct {
	db *pgxpool.Pool
}

// NewActivationCodeRepository creates a new repository.
func NewActivationCodeRepository(db *pgxpool.Pool) *ActivationCodeRepository {
	return &ActivationCodeRepository{db: db}
}

const codeChars = "ABCDEFGHJKLMNPQRSTUVWXYZ23456789"

func generateCode() string {
	var sb strings.Builder
	max := big.NewInt(int64(len(codeChars)))
	for i := 0; i < 16; i++ {
		// 无横杠: 整串可一次双击/长按全选复制(带分隔符时双击只能选中一段)
		n, _ := rand.Int(rand.Reader, max)
		sb.WriteByte(codeChars[n.Int64()])
	}
	return sb.String()
}

// Generate creates batch activation codes.
func (r *ActivationCodeRepository) Generate(ctx context.Context, planID string, count int, timingType string, expiresDays *int, createdBy uuid.UUID, notes string) ([]domain.ActivationCode, error) {
	codes := make([]domain.ActivationCode, count)
	now := time.Now()

	tx, err := r.db.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)

	for i := 0; i < count; i++ {
		code := generateCode()
		var expiresAt *time.Time
		// 无论 immediate 还是 on_use，只要管理员填写了有效期就写入过期时间
		if expiresDays != nil {
			t := now.Add(time.Duration(*expiresDays) * 24 * time.Hour)
			expiresAt = &t
		}
		_, err := tx.Exec(ctx, `
	        INSERT INTO activation_codes (code, plan_id, timing_type, expires_at, created_by, notes)
	        VALUES ($1, $2, $3, $4, $5, $6)`,
			code, planID, timingType, expiresAt, createdBy, notes,
		)
		if err != nil {
			return nil, fmt.Errorf("generate code: %w", err)
		}
		codes[i] = domain.ActivationCode{
			Code:       code,
			PlanID:     planID,
			Status:     "unused",
			TimingType: timingType,
			ExpiresAt:  expiresAt,
			CreatedBy:  &createdBy,
			CreatedAt:  now,
			Notes:      notes,
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return codes, nil
}

// List returns activation codes with filters.
func (r *ActivationCodeRepository) List(ctx context.Context, status string, planID string, page, pageSize int) ([]domain.ActivationCode, int, error) {
	where := "WHERE 1=1"
	args := []any{}
	argIdx := 1
	if status != "" {
		where += fmt.Sprintf(" AND status = $%d", argIdx)
		args = append(args, status)
		argIdx++
	}
	if planID != "" {
		where += fmt.Sprintf(" AND plan_id = $%d", argIdx)
		args = append(args, planID)
		argIdx++
	}

	var total int
	err := r.db.QueryRow(ctx, "SELECT COUNT(*) FROM activation_codes "+where, args...).Scan(&total)
	if err != nil {
		return nil, 0, err
	}

	offset := (page - 1) * pageSize
	args = append(args, pageSize, offset)

	rows, err := r.db.Query(ctx, `
	    SELECT c.code_id, c.code, c.plan_id, c.status, c.used_by, c.used_at, c.expires_at, c.timing_type, c.created_by, c.created_at, c.notes, COALESCE(t.name, '') AS used_by_tenant_name
	    FROM activation_codes c
	    LEFT JOIN tenants t ON c.used_by = t.tenant_id `+where+`
	    ORDER BY c.created_at DESC LIMIT $`+fmt.Sprintf("%d", argIdx)+` OFFSET $`+fmt.Sprintf("%d", argIdx+1), args...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	var codes []domain.ActivationCode
	for rows.Next() {
		var c domain.ActivationCode
		if err := rows.Scan(&c.CodeID, &c.Code, &c.PlanID, &c.Status, &c.UsedBy, &c.UsedAt, &c.ExpiresAt, &c.TimingType, &c.CreatedBy, &c.CreatedAt, &c.Notes, &c.UsedByTenantName); err != nil {
			continue
		}
		codes = append(codes, c)
	}
	return codes, total, nil
}

// GetByCode retrieves a code by its string value.
func (r *ActivationCodeRepository) GetByCode(ctx context.Context, code string) (*domain.ActivationCode, error) {
	var c domain.ActivationCode
	err := r.db.QueryRow(ctx, `
	    SELECT code_id, code, plan_id, status, used_by, used_at, expires_at, timing_type, created_by, created_at, notes
	    -- P2-60 审计修复: 原来 WHERE REPLACE(code,'-','')=REPLACE($1,'-','') 无法走索引全表扫。
	    -- 先精确匹配(走唯一索引), ErrNoRows 时 fallback 去横线匹配(兼容用户输错横线)。
	    FROM activation_codes WHERE code = $1`, code,
	).Scan(&c.CodeID, &c.Code, &c.PlanID, &c.Status, &c.UsedBy, &c.UsedAt, &c.ExpiresAt, &c.TimingType, &c.CreatedBy, &c.CreatedAt, &c.Notes)
	if errors.Is(err, pgx.ErrNoRows) {
		err = r.db.QueryRow(ctx, `
	    SELECT code_id, code, plan_id, status, used_by, used_at, expires_at, timing_type, created_by, created_at, notes
	    FROM activation_codes WHERE REPLACE(code, '-', '') = REPLACE($1, '-', '')`, code,
		).Scan(&c.CodeID, &c.Code, &c.PlanID, &c.Status, &c.UsedBy, &c.UsedAt, &c.ExpiresAt, &c.TimingType, &c.CreatedBy, &c.CreatedAt, &c.Notes)
	}
	if err != nil {
		return nil, fmt.Errorf("get code: %w", err)
	}
	return &c, nil
}

// MarkUsed atomically marks a code as used by a tenant.
// Returns (true, nil) on success, (false, nil) if the code was already used/revoked/expired.
// The status='unused' guard closes the check-then-update race: two concurrent
// activations can both pass the pre-check, but only one UPDATE can win.
// UnmarkUsed 补偿恢复: 注册失败时把卡密退回未使用(仅当核销者匹配)。
func (r *ActivationCodeRepository) UnmarkUsed(ctx context.Context, codeID, tenantID uuid.UUID) (bool, error) {
	res, err := r.db.Exec(ctx,
		`UPDATE activation_codes SET status='unused', used_by=NULL, used_at=NULL
		 WHERE code_id=$1 AND used_by=$2`, codeID, tenantID)
	if err != nil {
		return false, err
	}
	return res.RowsAffected() > 0, nil
}

func (r *ActivationCodeRepository) MarkUsed(ctx context.Context, codeID, tenantID uuid.UUID) (bool, error) {
	res, err := r.db.Exec(ctx, `
	    UPDATE activation_codes SET status = 'used', used_by = $2, used_at = NOW()
	    WHERE code_id = $1 AND status = 'unused'`, codeID, tenantID)
	if err != nil {
		return false, err
	}
	rows := res.RowsAffected()
	return rows > 0, nil
}

// Revoke marks a code as revoked.
func (r *ActivationCodeRepository) Revoke(ctx context.Context, codeID uuid.UUID) error {
	_, err := r.db.Exec(ctx, `UPDATE activation_codes SET status = 'revoked' WHERE code_id = $1`, codeID)
	return err
}

// ExpireOldCodes marks expired codes.
func (r *ActivationCodeRepository) ExpireOldCodes(ctx context.Context) error {
	_, err := r.db.Exec(ctx, `
	    UPDATE activation_codes SET status = 'expired'
	    WHERE status = 'unused' AND expires_at IS NOT NULL AND expires_at < NOW()`)
	return err
}

// Export returns all unused activation codes as a plain text list.
func (r *ActivationCodeRepository) Export(ctx context.Context) ([]domain.ActivationCode, error) {
	rows, err := r.db.Query(ctx, `
	    SELECT code_id, code, plan_id, status, used_by, used_at, expires_at, timing_type, created_by, created_at, notes
	    FROM activation_codes WHERE status = 'unused'
	    ORDER BY created_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var codes []domain.ActivationCode
	for rows.Next() {
		var c domain.ActivationCode
		if err := rows.Scan(&c.CodeID, &c.Code, &c.PlanID, &c.Status, &c.UsedBy, &c.UsedAt, &c.ExpiresAt, &c.TimingType, &c.CreatedBy, &c.CreatedAt, &c.Notes); err != nil {
			continue
		}
		codes = append(codes, c)
	}
	return codes, rows.Err()
}

// GetByID retrieves a code by its UUID.
func (r *ActivationCodeRepository) GetByID(ctx context.Context, codeID uuid.UUID) (*domain.ActivationCode, error) {
	var c domain.ActivationCode
	err := r.db.QueryRow(ctx, `
	    SELECT code_id, code, plan_id, status, used_by, used_at, expires_at, timing_type, created_by, created_at, notes
	    FROM activation_codes WHERE code_id = $1`, codeID,
	).Scan(&c.CodeID, &c.Code, &c.PlanID, &c.Status, &c.UsedBy, &c.UsedAt, &c.ExpiresAt, &c.TimingType, &c.CreatedBy, &c.CreatedAt, &c.Notes)
	if err != nil {
		return nil, fmt.Errorf("get code: %w", err)
	}
	return &c, nil
}

// Delete hard-deletes an activation code.
func (r *ActivationCodeRepository) Delete(ctx context.Context, codeID uuid.UUID) error {
	_, err := r.db.Exec(ctx, `DELETE FROM activation_codes WHERE code_id = $1`, codeID)
	return err
}

// BatchRevoke revokes multiple codes.
func (r *ActivationCodeRepository) BatchRevoke(ctx context.Context, ids []uuid.UUID) error {
	if len(ids) == 0 {
		return nil
	}
	_, err := r.db.Exec(ctx, `UPDATE activation_codes SET status = 'revoked' WHERE code_id = ANY($1)`, ids)
	return err
}

// BatchDelete deletes multiple codes.
func (r *ActivationCodeRepository) BatchDelete(ctx context.Context, ids []uuid.UUID) error {
	if len(ids) == 0 {
		return nil
	}
	_, err := r.db.Exec(ctx, `DELETE FROM activation_codes WHERE code_id = ANY($1)`, ids)
	return err
}
