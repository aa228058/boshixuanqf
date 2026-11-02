package repository

import (
	"context"
	"fmt"
	"net"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"tg-sender/internal/domain"
)

// AuditRepository handles audit log data access.
type AuditRepository struct {
	db *pgxpool.Pool
}

// NewAuditRepository creates a new audit repository.
func NewAuditRepository(db *pgxpool.Pool) *AuditRepository {
	return &AuditRepository{db: db}
}

// Create inserts a new audit log.
// 调用方常不设 LogID：零值时自动生成 UUID，避免并发/多次写入撞 audit_logs_pkey。
func (r *AuditRepository) Create(ctx context.Context, al *domain.AuditLog) error {
	if al.LogID == uuid.Nil {
		al.LogID = uuid.New()
	}
	// ip 列是 Postgres inet 类型：非 IP 文本(如后台平面的 "admin-console")会直接报错。
	// 统一校验，非法值落 NULL——后台动作靠 action/target 列识别，不依赖 ip。
	ipVal := any(nil)
	if pip := net.ParseIP(strings.TrimSpace(al.IP)); pip != nil {
		ipVal = pip.String()
	}
	_, err := r.db.Exec(ctx, `
		INSERT INTO audit_logs (log_id, tenant_id, user_id, action, target, ip)
		VALUES ($1, $2, $3, $4, $5, $6)`,
		al.LogID, al.TenantID, al.UserID, al.Action, al.Target, ipVal)
	return err
}

// List retrieves audit logs for a tenant with optional time range filter.
func (r *AuditRepository) List(ctx context.Context, tenantID uuid.UUID, limit, offset int, startTime, endTime string) ([]*domain.AuditLog, error) {
	query := `SELECT log_id, tenant_id, user_id, action, target, COALESCE(ip::text, '') AS ip, created_at
		FROM audit_logs WHERE tenant_id = $1`
	args := []any{tenantID}
	argIdx := 2

	if startTime != "" {
		query += fmt.Sprintf(" AND created_at >= $%d", argIdx)
		args = append(args, startTime)
		argIdx++
	}
	if endTime != "" {
		query += fmt.Sprintf(" AND created_at <= $%d", argIdx)
		args = append(args, endTime)
		argIdx++
	}
	query += fmt.Sprintf(" ORDER BY created_at DESC LIMIT $%d OFFSET $%d", argIdx, argIdx+1)
	args = append(args, limit, offset)

	rows, err := r.db.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("list audit logs: %w", err)
	}
	defer rows.Close()

	var logs []*domain.AuditLog
	for rows.Next() {
		var al domain.AuditLog
		if err := rows.Scan(&al.LogID, &al.TenantID, &al.UserID, &al.Action, &al.Target, &al.IP, &al.CreatedAt); err != nil {
			return nil, err
		}
		logs = append(logs, &al)
	}
	return logs, rows.Err()
}
