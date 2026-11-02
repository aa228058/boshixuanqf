package repository

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"
	"tg-sender/internal/domain"
)

// PlanRepository handles plan data access.
type PlanRepository struct {
	db *pgxpool.Pool
}

// NewPlanRepository creates a new plan repository.
func NewPlanRepository(db *pgxpool.Pool) *PlanRepository {
	return &PlanRepository{db: db}
}

// List returns all active plans.
func (r *PlanRepository) List(ctx context.Context) ([]domain.Plan, error) {
	rows, err := r.db.Query(ctx, `
		SELECT plan_id, name, price_monthly, max_accounts, max_concurrent, description, features, is_active, updated_at,
		       can_broadcast, can_scrape_members, can_scrape_links, can_join_groups, can_monitor, can_export, can_use_templates,
	       max_daily_broadcasts, max_daily_scrapes, max_monitor_rules, daily_search_limit
		FROM plans WHERE is_active = TRUE ORDER BY price_monthly ASC`)
	if err != nil {
		return nil, fmt.Errorf("list plans: %w", err)
	}
	defer rows.Close()

	var plans []domain.Plan
	for rows.Next() {
		var p domain.Plan
		var featuresJSON []byte
		err := rows.Scan(&p.PlanID, &p.Name, &p.PriceMonthly, &p.MaxAccounts, &p.MaxConcurrent, &p.Description, &featuresJSON, &p.IsActive, &p.UpdatedAt,
			&p.CanBroadcast, &p.CanScrapeMembers, &p.CanScrapeLinks, &p.CanJoinGroups, &p.CanMonitor, &p.CanExport, &p.CanUseTemplates,
			&p.MaxDailyBroadcasts, &p.MaxDailyScrapes, &p.MaxMonitorRules, &p.DailySearchLimit)
		if err != nil {
			continue
		}
		json.Unmarshal(featuresJSON, &p.Features)
		plans = append(plans, p)
	}
	return plans, rows.Err()
}

// ListAll 返回全部套餐(含已停用)——供管理端运营使用(P1-43 审计修复):
// List 过滤了 is_active=TRUE, 管理端拿不到停用套餐, 误删/停用后无法恢复。
func (r *PlanRepository) ListAll(ctx context.Context) ([]domain.Plan, error) {
	rows, err := r.db.Query(ctx, `
		SELECT plan_id, name, price_monthly, max_accounts, max_concurrent, description, features, is_active, updated_at,
		       can_broadcast, can_scrape_members, can_scrape_links, can_join_groups, can_monitor, can_export, can_use_templates,
	       max_daily_broadcasts, max_daily_scrapes, max_monitor_rules, daily_search_limit
		FROM plans ORDER BY price_monthly ASC`)
	if err != nil {
		return nil, fmt.Errorf("list all plans: %w", err)
	}
	defer rows.Close()
	var list []domain.Plan
	for rows.Next() {
		var p domain.Plan
		if err := rows.Scan(&p.PlanID, &p.Name, &p.PriceMonthly, &p.MaxAccounts, &p.MaxConcurrent, &p.Description, &p.Features, &p.IsActive, &p.UpdatedAt,
			&p.CanBroadcast, &p.CanScrapeMembers, &p.CanScrapeLinks, &p.CanJoinGroups, &p.CanMonitor, &p.CanExport, &p.CanUseTemplates,
			&p.MaxDailyBroadcasts, &p.MaxDailyScrapes, &p.MaxMonitorRules, &p.DailySearchLimit); err != nil {
			return nil, fmt.Errorf("scan plan: %w", err)
		}
		list = append(list, p)
	}
	return list, rows.Err()
}

// GetByID returns a plan by ID.
func (r *PlanRepository) GetByID(ctx context.Context, planID string) (*domain.Plan, error) {
	var p domain.Plan
	var featuresJSON []byte
	err := r.db.QueryRow(ctx, `
		SELECT plan_id, name, price_monthly, max_accounts, max_concurrent, description, features, is_active, updated_at,
		       can_broadcast, can_scrape_members, can_scrape_links, can_join_groups, can_monitor, can_export, can_use_templates,
	       max_daily_broadcasts, max_daily_scrapes, max_monitor_rules, daily_search_limit
		FROM plans WHERE plan_id = $1`, planID,
	).Scan(&p.PlanID, &p.Name, &p.PriceMonthly, &p.MaxAccounts, &p.MaxConcurrent, &p.Description, &featuresJSON, &p.IsActive, &p.UpdatedAt,
		&p.CanBroadcast, &p.CanScrapeMembers, &p.CanScrapeLinks, &p.CanJoinGroups, &p.CanMonitor, &p.CanExport, &p.CanUseTemplates,
			&p.MaxDailyBroadcasts, &p.MaxDailyScrapes, &p.MaxMonitorRules, &p.DailySearchLimit)
	if err != nil {
		return nil, fmt.Errorf("get plan: %w", err)
	}
	json.Unmarshal(featuresJSON, &p.Features)
	return &p, nil
}

// Update updates a plan.
func (r *PlanRepository) Update(ctx context.Context, planID string, updates map[string]any) error {
	allowedCols := map[string]bool{
		"name": true, "price_monthly": true, "max_accounts": true, "max_concurrent": true,
		"description": true, "features": true, "is_active": true,
		"can_broadcast": true, "can_scrape_members": true, "can_scrape_links": true,
		"can_join_groups": true, "can_monitor": true, "can_export": true, "can_use_templates": true,
		// 日配额列：此前白名单缺失，后台改套餐日限额被静默丢弃（同类 source bug）
		"max_daily_broadcasts": true, "max_daily_scrapes": true, "max_monitor_rules": true,
		// 051: 搜群日限(此前编辑被吞导致后台改此项仍被静默丢弃, 补上)
		"daily_search_limit": true,
	}
	setParts := []string{}
	args := []any{planID}
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
	query := "UPDATE plans SET " + setParts[0]
	for i := 1; i < len(setParts); i++ {
		query += ", " + setParts[i]
	}
	query += ", updated_at = NOW() WHERE plan_id = $1"
	_, err := r.db.Exec(ctx, query, args...)
	return err
}

// Create inserts a new plan (upsert on conflict).
func (r *PlanRepository) Create(ctx context.Context, p *domain.Plan) error {
	featuresJSON, _ := json.Marshal(p.Features)
	_, err := r.db.Exec(ctx, `
		INSERT INTO plans (
			plan_id, name, price_monthly, max_accounts, max_concurrent, description, features, is_active, updated_at,
			can_broadcast, can_scrape_members, can_scrape_links, can_join_groups, can_monitor, can_export, can_use_templates,
			max_daily_broadcasts, max_daily_scrapes, max_monitor_rules,
			daily_search_limit
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, NOW(),
			$9, $10, $11, $12, $13, $14, $15, $16, $17, $18, $19)
		ON CONFLICT (plan_id) DO UPDATE SET
			name = EXCLUDED.name,
			price_monthly = EXCLUDED.price_monthly,
			max_accounts = EXCLUDED.max_accounts,
			max_concurrent = EXCLUDED.max_concurrent,
			description = EXCLUDED.description,
			features = EXCLUDED.features,
			is_active = EXCLUDED.is_active,
			updated_at = NOW(),
			can_broadcast = EXCLUDED.can_broadcast,
			can_scrape_members = EXCLUDED.can_scrape_members,
			can_scrape_links = EXCLUDED.can_scrape_links,
			can_join_groups = EXCLUDED.can_join_groups,
			can_monitor = EXCLUDED.can_monitor,
			max_daily_broadcasts = EXCLUDED.max_daily_broadcasts,
			max_daily_scrapes = EXCLUDED.max_daily_scrapes,
			max_monitor_rules = EXCLUDED.max_monitor_rules,
			daily_search_limit = EXCLUDED.daily_search_limit,
			can_export = EXCLUDED.can_export,
			can_use_templates = EXCLUDED.can_use_templates`,
		p.PlanID, p.Name, p.PriceMonthly, p.MaxAccounts, p.MaxConcurrent, p.Description, featuresJSON, p.IsActive,
		p.CanBroadcast, p.CanScrapeMembers, p.CanScrapeLinks, p.CanJoinGroups, p.CanMonitor, p.CanExport, p.CanUseTemplates,
		p.MaxDailyBroadcasts, p.MaxDailyScrapes, p.MaxMonitorRules, p.DailySearchLimit,
	)
	if err != nil {
		return fmt.Errorf("create plan: %w", err)
	}
	return nil
}
