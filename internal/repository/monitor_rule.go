package repository

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"tg-sender/internal/domain"
)

// MonitorRuleRepository handles monitor rule data access.
type MonitorRuleRepository struct {
	db *pgxpool.Pool
}

// NewMonitorRuleRepository creates a new monitor rule repository.
func NewMonitorRuleRepository(db *pgxpool.Pool) *MonitorRuleRepository {
	return &MonitorRuleRepository{db: db}
}

// ListByTenant retrieves monitor rules for a tenant (implements monitor.RuleRepository).
func (r *MonitorRuleRepository) ListByTenant(ctx context.Context, tenantID uuid.UUID) ([]*domain.MonitorRule, error) {
	return r.List(ctx, tenantID)
}

// List retrieves monitor rules for a tenant.
// 2026-09-22 隔离改造: 查询返回全部规则(含各账号归属), 由引擎按 AccountID 过滤派发。
func (r *MonitorRuleRepository) List(ctx context.Context, tenantID uuid.UUID) ([]*domain.MonitorRule, error) {
	rows, err := r.db.Query(ctx, `
		SELECT rule_id, tenant_id, account_id, name, type, source_chat_ids, target_chat_ids, match_type, keywords, is_active, created_at, reply_content
		FROM monitor_rules WHERE tenant_id = $1 ORDER BY created_at DESC, rule_id DESC`, tenantID)
	if err != nil {
		return nil, fmt.Errorf("list monitor rules: %w", err)
	}
	defer rows.Close()

	var rules []*domain.MonitorRule
	for rows.Next() {
		var mr domain.MonitorRule
		err := rows.Scan(&mr.RuleID, &mr.TenantID, &mr.AccountID, &mr.Name, &mr.Type, &mr.SourceChatIDs, &mr.TargetChatIDs,
			&mr.MatchType, &mr.Keywords, &mr.IsActive, &mr.CreatedAt, &mr.ReplyContent)
		if err != nil {
			return nil, err
		}
		rules = append(rules, &mr)
	}
	return rules, rows.Err()
}

// ListActiveTenants 返回拥有启用规则的租户 ID 去重列表(重启对账用)。
func (r *MonitorRuleRepository) ListActiveTenants(ctx context.Context) ([]uuid.UUID, error) {
	// G-03: 过滤非活跃/过期租户——被封租户的监听规则不再轮询 TG。
	// resume 后 tenants.status 恢复 active，下轮周期查询自然重新纳入。
	rows, err := r.db.Query(ctx, `
		SELECT DISTINCT mr.tenant_id FROM monitor_rules mr
		JOIN tenants t ON t.tenant_id = mr.tenant_id
		WHERE mr.is_active = TRUE AND t.status = 'active'
		  AND (t.expires_at IS NULL OR t.expires_at > NOW())`)
	if err != nil {
		return nil, fmt.Errorf("list active tenants: %w", err)
	}
	defer rows.Close()

	var ids []uuid.UUID
	for rows.Next() {
		var id uuid.UUID
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

// GetByID retrieves a monitor rule by ID.
func (r *MonitorRuleRepository) GetByID(ctx context.Context, tenantID, ruleID uuid.UUID) (*domain.MonitorRule, error) {
	var mr domain.MonitorRule
	err := r.db.QueryRow(ctx, `
		SELECT rule_id, tenant_id, account_id, name, type, source_chat_ids, target_chat_ids, match_type, keywords, is_active, created_at, reply_content
		FROM monitor_rules WHERE tenant_id = $1 AND rule_id = $2`, tenantID, ruleID,
	).Scan(&mr.RuleID, &mr.TenantID, &mr.AccountID, &mr.Name, &mr.Type, &mr.SourceChatIDs, &mr.TargetChatIDs,
		&mr.MatchType, &mr.Keywords, &mr.IsActive, &mr.CreatedAt, &mr.ReplyContent)
	if err != nil {
		return nil, fmt.Errorf("get monitor rule: %w", err)
	}
	return &mr, nil
}

// Create inserts a new monitor rule.
func (r *MonitorRuleRepository) Create(ctx context.Context, mr *domain.MonitorRule) error {
	_, err := r.db.Exec(ctx, `
		INSERT INTO monitor_rules (rule_id, tenant_id, account_id, name, type, source_chat_ids, target_chat_ids, match_type, keywords, is_active, reply_content)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)`,
		mr.RuleID, mr.TenantID, mr.AccountID, mr.Name, mr.Type, mr.SourceChatIDs, mr.TargetChatIDs, mr.MatchType, mr.Keywords, mr.IsActive, mr.ReplyContent)
	return err
}

// Update modifies a monitor rule.
// account_id 可更新（$3），归属可以在编辑时调整；参数整体顺移一位。
func (r *MonitorRuleRepository) Update(ctx context.Context, mr *domain.MonitorRule) error {
	_, err := r.db.Exec(ctx, `
		UPDATE monitor_rules SET account_id = $3, name = $4, type = $5, source_chat_ids = $6, target_chat_ids = $7,
		                       match_type = $8, keywords = $9, is_active = $10, reply_content = $11
		WHERE tenant_id = $1 AND rule_id = $2`,
		mr.TenantID, mr.RuleID, mr.AccountID, mr.Name, mr.Type, mr.SourceChatIDs, mr.TargetChatIDs, mr.MatchType, mr.Keywords, mr.IsActive, mr.ReplyContent)
	return err
}

// Delete removes a monitor rule.
func (r *MonitorRuleRepository) Delete(ctx context.Context, tenantID, ruleID uuid.UUID) error {
	_, err := r.db.Exec(ctx, `DELETE FROM monitor_rules WHERE tenant_id = $1 AND rule_id = $2`, tenantID, ruleID)
	return err
}
