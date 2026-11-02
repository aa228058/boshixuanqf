package repository

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"tg-sender/internal/domain"
)

// RiskRepository handles risk event data access.
type RiskRepository struct {
	db *pgxpool.Pool
}

// NewRiskRepository creates a new risk repository.
func NewRiskRepository(db *pgxpool.Pool) *RiskRepository {
	return &RiskRepository{db: db}
}

// Create inserts a new risk event.
func (r *RiskRepository) Create(ctx context.Context, re *domain.RiskEvent) error {
	_, err := r.db.Exec(ctx, `
		INSERT INTO risk_events (event_id, tenant_id, account_id, event_type, severity, details)
		VALUES ($1, $2, $3, $4, $5, $6)`,
		re.EventID, re.TenantID, re.AccountID, re.EventType, re.Severity, re.Details)
	return err
}

// ListByAccount retrieves risk events for an account.
func (r *RiskRepository) ListByAccount(ctx context.Context, tenantID, accountID uuid.UUID, limit int) ([]*domain.RiskEvent, error) {
	rows, err := r.db.Query(ctx, `
		SELECT event_id, tenant_id, account_id, event_type, severity, details, created_at
		FROM risk_events WHERE tenant_id = $1 AND account_id = $2
		ORDER BY created_at DESC LIMIT $3`, tenantID, accountID, limit)
	if err != nil {
		return nil, fmt.Errorf("list risk events: %w", err)
	}
	defer rows.Close()

	var events []*domain.RiskEvent
	for rows.Next() {
		var re domain.RiskEvent
		if err := rows.Scan(&re.EventID, &re.TenantID, &re.AccountID, &re.EventType, &re.Severity, &re.Details, &re.CreatedAt); err != nil {
			return nil, err
		}
		events = append(events, &re)
	}
	return events, rows.Err()
}

// ListByTenant retrieves risk events for a tenant with optional time range filter.
func (r *RiskRepository) ListByTenant(ctx context.Context, tenantID uuid.UUID, limit, offset int, startTime, endTime string) ([]*domain.RiskEvent, error) {
	query := `SELECT event_id, tenant_id, account_id, event_type, severity, details, created_at
		FROM risk_events WHERE tenant_id = $1`
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
		return nil, fmt.Errorf("list tenant risk events: %w", err)
	}
	defer rows.Close()

	var events []*domain.RiskEvent
	for rows.Next() {
		var re domain.RiskEvent
		if err := rows.Scan(&re.EventID, &re.TenantID, &re.AccountID, &re.EventType, &re.Severity, &re.Details, &re.CreatedAt); err != nil {
			return nil, err
		}
		events = append(events, &re)
	}
	return events, rows.Err()
}

// CountByAccount counts risk events for an account.
func (r *RiskRepository) CountByAccount(ctx context.Context, tenantID, accountID uuid.UUID) (map[string]int, error) {
	rows, err := r.db.Query(ctx, `
		SELECT event_type, COUNT(*) FROM risk_events
		WHERE tenant_id = $1 AND account_id = $2
		GROUP BY event_type`, tenantID, accountID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	counts := make(map[string]int)
	for rows.Next() {
		var et string
		var c int
		if err := rows.Scan(&et, &c); err != nil {
			return nil, err
		}
		counts[et] = c
	}
	return counts, rows.Err()
}

// CountByTenant counts total risk events for a tenant.
func (r *RiskRepository) CountByTenant(ctx context.Context, tenantID uuid.UUID) (int, error) {
	var count int
	err := r.db.QueryRow(ctx, `SELECT COUNT(*) FROM risk_events WHERE tenant_id = $1`, tenantID).Scan(&count)
	return count, err
}

// Count24h counts risk events in the last 24 hours for a tenant.
func (r *RiskRepository) Count24h(ctx context.Context, tenantID uuid.UUID) (int, error) {
	var count int
	err := r.db.QueryRow(ctx, `
		SELECT COUNT(*) FROM risk_events
		WHERE tenant_id = $1 AND created_at >= NOW() - INTERVAL '24 hours'`, tenantID).Scan(&count)
	return count, err
}

// Count24hBans 仅统计 24 小时内的封禁类事件(P1-18 审计修复)。
// updateBanRate 的语义是"封禁率"——原来用 Count24hAll(全部事件: flood_wait/
// session_revoked 都计入), 非封禁事件把 banRate 拉高, 发送间隔被误放大。
// 口径与 Count24hAll 一致(全平台, 只算 account_banned)。
func (r *RiskRepository) Count24hBans(ctx context.Context) (int, error) {
	var count int
	err := r.db.QueryRow(ctx, `
		SELECT COUNT(*) FROM risk_events
		WHERE created_at >= NOW() - INTERVAL '24 hours' AND event_type = 'account_banned'`).Scan(&count)
	return count, err
}

// Count24hYesterday counts risk events in the previous 24h period for a tenant.
func (r *RiskRepository) Count24hYesterday(ctx context.Context, tenantID uuid.UUID) (int, error) {
	var count int
	err := r.db.QueryRow(ctx, `
		SELECT COUNT(*) FROM risk_events
		WHERE tenant_id = $1 AND created_at >= NOW() - INTERVAL '48 hours'
		AND created_at < NOW() - INTERVAL '24 hours'`, tenantID).Scan(&count)
	return count, err
}
