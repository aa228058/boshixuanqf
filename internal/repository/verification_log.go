package repository

import (
	"context"
	"encoding/json"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"tg-sender/internal/domain"
)

// VerificationLogRepository 入群验证记录
type VerificationLogRepository struct {
	db *pgxpool.Pool
}

func NewVerificationLogRepository(db *pgxpool.Pool) *VerificationLogRepository {
	return &VerificationLogRepository{db: db}
}

func (r *VerificationLogRepository) Create(ctx context.Context, log *domain.VerificationLog) error {
	steps, _ := json.Marshal(log.Steps)
	_, err := r.db.Exec(ctx, `
		INSERT INTO verification_logs (vl_id, tenant_id, account_id, group_chat_id, group_title, status, reason, steps)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8)`,
		log.VLID, log.TenantID, log.AccountID, log.GroupChatID, log.GroupTitle, log.Status, log.Reason, steps)
	return err
}

func (r *VerificationLogRepository) UpdateStatus(ctx context.Context, vlID uuid.UUID, status, reason string, steps []any) error {
	raw, _ := json.Marshal(steps)
	_, err := r.db.Exec(ctx,
		`UPDATE verification_logs SET status = $2, reason = $3, steps = $4 WHERE vl_id = $1`,
		vlID, status, reason, raw)
	return err
}

// List 租户验证记录(最近 N 条)
func (r *VerificationLogRepository) List(ctx context.Context, tenantID uuid.UUID, limit int) ([]domain.VerificationLog, error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	rows, err := r.db.Query(ctx, `
		SELECT vl_id, account_id, group_chat_id, group_title, status, reason, steps, created_at
		FROM verification_logs WHERE tenant_id = $1
		ORDER BY created_at DESC LIMIT $2`, tenantID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.VerificationLog
	for rows.Next() {
		var v domain.VerificationLog
		var steps []byte
		if err := rows.Scan(&v.VLID, &v.AccountID, &v.GroupChatID, &v.GroupTitle, &v.Status, &v.Reason, &steps, &v.CreatedAt); err != nil {
			return nil, err
		}
		v.TenantID = tenantID
		_ = json.Unmarshal(steps, &v.Steps)
		out = append(out, v)
	}
	return out, rows.Err()
}
