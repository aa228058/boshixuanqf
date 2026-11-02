package repository

import (
	"context"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

// PlanLimitRepository 套餐日额度: 免费体验版限额, 付费档 0=不限。
type PlanLimitRepository struct {
	db *pgxpool.Pool
}

func NewPlanLimitRepository(db *pgxpool.Pool) *PlanLimitRepository {
	return &PlanLimitRepository{db: db}
}

// GetPlanLimits 返回租户套餐的日额度(0=不限)。
// 群发日限用既有列 max_daily_broadcasts(与 can_* 开关同期的老配额体系)。
func (r *PlanLimitRepository) GetPlanLimits(ctx context.Context, tenantID uuid.UUID) (dailyBroadcast, dailySearch int, err error) {
	err = r.db.QueryRow(ctx, `
		SELECT COALESCE(p.max_daily_broadcasts, 0), COALESCE(p.daily_search_limit, 0)
		FROM tenants t JOIN plans p ON p.plan_id = t.plan_id
		WHERE t.tenant_id = $1`, tenantID).Scan(&dailyBroadcast, &dailySearch)
	return
}

// AddDailyUsage 原子累加当日用量并返回最新值(INSERT ... ON CONFLICT 天然防并发超发)
func (r *PlanLimitRepository) AddDailyUsage(ctx context.Context, tenantID uuid.UUID, kind string, n int) (int, error) {
	var used int
	err := r.db.QueryRow(ctx, `
		INSERT INTO plan_daily_counters (tenant_id, day, kind, used)
		VALUES ($1, CURRENT_DATE, $2, $3)
		ON CONFLICT (tenant_id, day, kind) DO UPDATE SET used = plan_daily_counters.used + $3
		RETURNING used`, tenantID, kind, n).Scan(&used)
	return used, err
}

// GetDailyUsage 查当日已用量(P2-47 审计修复配套: 超限判断要先查后扣, 不能先扣后判)
func (r *PlanLimitRepository) GetDailyUsage(ctx context.Context, tenantID uuid.UUID, kind string) (int, error) {
	var used int
	err := r.db.QueryRow(ctx, `
		SELECT COALESCE(used, 0) FROM plan_daily_counters
		WHERE tenant_id = $1 AND day = CURRENT_DATE AND kind = $2`, tenantID, kind).Scan(&used)
	if err != nil && err.Error() == "no rows in result set" {
		return 0, nil // 今日还没用过
	}
	return used, err
}

// Kind 常量
const (
	KindBroadcastMsgs = "broadcast_msgs"
	KindGroupSearch   = "group_search"
)
