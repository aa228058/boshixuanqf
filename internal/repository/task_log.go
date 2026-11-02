package repository

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"tg-sender/internal/domain"
)

// TaskLogRepository handles task l data access.
type TaskLogRepository struct {
	db *pgxpool.Pool
}

// NewTaskLogRepository creates a new task l repository.
func NewTaskLogRepository(db *pgxpool.Pool) *TaskLogRepository {
	return &TaskLogRepository{db: db}
}

// nilAccount 把零值 UUID 转成 SQL NULL：account_id 列可空且外键到 accounts，
// 零值 UUID 既非 NULL 又不存在于 accounts，直接插入会撞 FK（2026-09-22 日志实证）。
func nilAccount(id uuid.UUID) any {
	if id == uuid.Nil {
		return nil
	}
	return id
}

// Create inserts a new task l.
func (r *TaskLogRepository) Create(ctx context.Context, l *domain.TaskLog) error {
	_, err := r.db.Exec(ctx, `
		INSERT INTO task_logs (log_id, task_id, tenant_id, account_id, target_id, target_name, action, status, tg_response, error_code, message, tg_message_id, executed_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13)`,
		l.LogID, l.TaskID, l.TenantID, nilAccount(l.AccountID), l.TargetID, l.TargetName, l.Action, l.Status, l.TGResponse, l.ErrorCode, l.Message, l.TGMessageID, l.ExecutedAt)
	return err
}

// CountSuccessByDay 按天统计成功发送量(发送日历数据源): 返回 日期→成功条数 映射
func (r *TaskLogRepository) CountSuccessByDay(ctx context.Context, tenantID uuid.UUID, from, to time.Time) (map[string]int, error) {
	rows, err := r.db.Query(ctx, `
		SELECT to_char(executed_at AT TIME ZONE 'Asia/Shanghai', 'YYYY-MM-DD') AS day, COUNT(*)
		FROM task_logs
		WHERE tenant_id = $1 AND status = 'success' AND executed_at >= $2 AND executed_at < $3
		GROUP BY day ORDER BY day`, tenantID, from, to)
	if err != nil {
		return nil, fmt.Errorf("count success by day: %w", err)
	}
	defer rows.Close()
	result := map[string]int{}
	for rows.Next() {
		var day string
		var n int
		if err := rows.Scan(&day, &n); err != nil {
			return nil, err
		}
		result[day] = n
	}
	return result, nil
}

// StatsByAccount 按账号聚合成功/失败/跳过总数(账号卡片记分牌 + 任务展开报表共用)
// 返回 account_id → {success, failed, skipped}
func (r *TaskLogRepository) StatsByAccount(ctx context.Context, tenantID uuid.UUID) (map[string]map[string]int, error) {
	rows, err := r.db.Query(ctx, `
		SELECT account_id::text,
		       COUNT(*) FILTER (WHERE status = 'success'),
		       COUNT(*) FILTER (WHERE status IN ('failed','error')),
		       COUNT(*) FILTER (WHERE status IN ('skipped','banned','restricted'))
		FROM task_logs
		WHERE tenant_id = $1 AND account_id IS NOT NULL
		GROUP BY account_id`, tenantID)
	if err != nil {
		return nil, fmt.Errorf("stats by account: %w", err)
	}
	defer rows.Close()
	result := map[string]map[string]int{}
	for rows.Next() {
		var id string
		var s, f, k int
		if err := rows.Scan(&id, &s, &f, &k); err != nil {
			return nil, err
		}
		result[id] = map[string]int{"success": s, "failed": f, "skipped": k}
	}
	return result, nil
}

// StatsByAccountForTask 单任务内按账号聚合(任务展开报表用)
func (r *TaskLogRepository) StatsByAccountForTask(ctx context.Context, tenantID, taskID uuid.UUID) (map[string]map[string]int, error) {
	rows, err := r.db.Query(ctx, `
		SELECT account_id::text,
		       COUNT(*) FILTER (WHERE status = 'success'),
		       COUNT(*) FILTER (WHERE status IN ('failed','error')),
		       COUNT(*) FILTER (WHERE status IN ('skipped','banned','restricted'))
		FROM task_logs
		WHERE tenant_id = $1 AND task_id = $2 AND account_id IS NOT NULL
		GROUP BY account_id`, tenantID, taskID)
	if err != nil {
		return nil, fmt.Errorf("task stats by account: %w", err)
	}
	defer rows.Close()
	result := map[string]map[string]int{}
	for rows.Next() {
		var id string
		var s, f, k int
		if err := rows.Scan(&id, &s, &f, &k); err != nil {
			return nil, err
		}
		result[id] = map[string]int{"success": s, "failed": f, "skipped": k}
	}
	return result, nil
}

// ListByTask retrieves logs for a task.
func (r *TaskLogRepository) ListByTask(ctx context.Context, tenantID, taskID uuid.UUID, limit, offset int) ([]*domain.TaskLog, error) {
	rows, err := r.db.Query(ctx, `
		SELECT log_id, task_id, tenant_id, account_id, target_id, target_name, action, status, tg_response, error_code, message, executed_at
		FROM task_logs WHERE tenant_id = $1 AND task_id = $2
		ORDER BY executed_at DESC LIMIT $3 OFFSET $4`, tenantID, taskID, limit, offset)
	if err != nil {
		return nil, fmt.Errorf("list task logs: %w", err)
	}
	defer rows.Close()

	var logs []*domain.TaskLog
	for rows.Next() {
		var l domain.TaskLog
		// 2026-10-22 审计修复(P0-17): account_id 可空(批量日志无账号), Scan 到 uuid.UUID
		// 遇 NULL 直接报错 500——改 Scan 到 *uuid.UUID, NULL 落 nil
		var accountID *uuid.UUID
		err := rows.Scan(&l.LogID, &l.TaskID, &l.TenantID, &accountID, &l.TargetID, &l.TargetName, &l.Action, &l.Status, &l.TGResponse, &l.ErrorCode, &l.Message, &l.ExecutedAt)
		if err != nil {
			return nil, err
		}
		if accountID != nil {
			l.AccountID = *accountID
		}
		logs = append(logs, &l)
	}
	return logs, rows.Err()
}

// CountByTask counts logs for a task.
func (r *TaskLogRepository) CountByTask(ctx context.Context, tenantID, taskID uuid.UUID) (int, error) {
	var count int
	err := r.db.QueryRow(ctx, `SELECT COUNT(*) FROM task_logs WHERE tenant_id = $1 AND task_id = $2`, tenantID, taskID).Scan(&count)
	return count, err
}

// ListByAccount gets task logs for a specific account with pagination and status filter.
func (r *TaskLogRepository) ListByAccount(ctx context.Context, tenantID, accountID uuid.UUID, status string, page, pageSize int) ([]*domain.TaskLog, int, error) {
	offset := (page - 1) * pageSize

	var count int
	countQuery := `SELECT COUNT(*) FROM task_logs WHERE tenant_id = $1 AND account_id = $2`
	countArgs := []any{tenantID, accountID}
	query := `SELECT log_id, task_id, tenant_id, account_id, target_id, target_name, action, status, error_code, message, executed_at FROM task_logs WHERE tenant_id = $1 AND account_id = $2`
	args := []any{tenantID, accountID}
	argIdx := 3

	if status != "" {
		countQuery += fmt.Sprintf(" AND status = $%d", argIdx)
		query += fmt.Sprintf(" AND status = $%d", argIdx)
		countArgs = append(countArgs, status)
		args = append(args, status)
		argIdx++
	}

	err := r.db.QueryRow(ctx, countQuery, countArgs...).Scan(&count)
	if err != nil {
		return nil, 0, err
	}

	query += fmt.Sprintf(" ORDER BY executed_at DESC LIMIT $%d OFFSET $%d", argIdx, argIdx+1)
	args = append(args, pageSize, offset)

	rows, err := r.db.Query(ctx, query, args...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	var logs []*domain.TaskLog
	for rows.Next() {
		l := &domain.TaskLog{}
		if err := rows.Scan(&l.LogID, &l.TaskID, &l.TenantID, &l.AccountID, &l.TargetID, &l.TargetName, &l.Action, &l.Status, &l.ErrorCode, &l.Message, &l.ExecutedAt); err != nil {
			return nil, 0, err
		}
		logs = append(logs, l)
	}
	return logs, count, rows.Err()
}

// CreateBatch inserts multiple task logs in a single transaction for better performance.
// DeleteByTask 清空某任务的全部日志。
func (r *TaskLogRepository) DeleteByTask(ctx context.Context, tenantID, taskID uuid.UUID) error {
	_, err := r.db.Exec(ctx, `DELETE FROM task_logs WHERE tenant_id = $1 AND task_id = $2`, tenantID, taskID)
	return err
}

// R116: DeleteOlderThan 租户版已删(随 /logs/cleanup 端点删除, 零调用;
// 系统级清扫用 DeleteOlderThanGlobal, engine 仍在调用)

// DeleteOlderThanGlobal 系统级定时清扫(无租户上下文), 勿暴露给租户接口。
func (r *TaskLogRepository) DeleteOlderThanGlobal(ctx context.Context, days int) (int64, error) {
	tag, err := r.db.Exec(ctx, `DELETE FROM task_logs WHERE executed_at < NOW() - ($1 || ' days')::interval`, days)
	if err != nil {
		return 0, err
	}
	return tag.RowsAffected(), nil
}

func (r *TaskLogRepository) CreateBatch(ctx context.Context, logs []*domain.TaskLog) error {
	if len(logs) == 0 {
		return nil
	}
	// Use COPY for efficient bulk insert
	conn, err := r.db.Acquire(ctx)
	if err != nil {
		return fmt.Errorf("acquire conn for batch insert: %w", err)
	}
	defer conn.Release()

	// 2026-10-21 修正: 原列清单(10)与 Values(11)数量不匹配, COPY 每次都报错走 fallback;
	// 且缺 target_name/message —— 群名和耗时从未写进过库。补齐 13 列与 Values 严格对齐
	_, err = conn.CopyFrom(ctx, []string{"task_logs"},
		[]string{"log_id", "task_id", "tenant_id", "account_id", "target_id", "target_name", "action", "status", "tg_response", "error_code", "message", "tg_message_id", "executed_at"},
		(*copyFromLogs)(&logs))
	if err != nil {
		// Fallback to individual inserts if COPY fails
		for _, l := range logs {
			if err := r.Create(ctx, l); err != nil {
				return fmt.Errorf("batch fallback insert failed: %w", err)
			}
		}
	}
	return nil
}

type copyFromLogs []*domain.TaskLog

func (c *copyFromLogs) Next() bool { return len(*c) > 0 }
func (c *copyFromLogs) Values() ([]any, error) {
	if len(*c) == 0 {
		return nil, nil
	}
	l := (*c)[0]
	*c = (*c)[1:]
	return []any{l.LogID, l.TaskID, l.TenantID, nilAccount(l.AccountID), l.TargetID, l.TargetName, l.Action, l.Status, l.TGResponse, l.ErrorCode, l.Message, l.TGMessageID, l.ExecutedAt}, nil
}
func (c *copyFromLogs) Err() error { return nil }

// HasSuccessForTarget 检查某目标是否已成功发送(续传防重: 重启后继续任务时跳过).
func (r *TaskLogRepository) HasSuccessForTarget(ctx context.Context, tenantID, taskID uuid.UUID, targetID string) (bool, error) {
	var n int
	err := r.db.QueryRow(ctx, `
	    SELECT count(*) FROM task_logs
	    WHERE tenant_id=$1 AND task_id=$2 AND target_id=$3 AND status='success'`,
		tenantID, taskID, targetID).Scan(&n)
	return n > 0, err
}

// HasSuccessForTargets 批量返回 (账号,目标) 维度已成功发送的复合键集合:
// P1-47: 断点续传防重原是每目标一次 count(*)(N+1), 循环前一次性查回。
// P0-12: 键升级为 accountID+"|"+targetID——原按目标全局去重会把 cartesian
// 模式(所有账号×所有目标全发)压成单账号发送; mapping 模式账号目标不重叠,
// 升级后行为不变。
func (r *TaskLogRepository) HasSuccessForTargets(ctx context.Context, tenantID, taskID uuid.UUID, targetIDs []string) (map[string]bool, error) {
	result := make(map[string]bool)
	if len(targetIDs) == 0 {
		return result, nil
	}
	rows, err := r.db.Query(ctx, `
	    SELECT account_id, target_id FROM task_logs
	    WHERE tenant_id=$1 AND task_id=$2 AND status='success' AND target_id = ANY($3::text[])`,
		tenantID, taskID, targetIDs)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var accID uuid.UUID
		var t string
		if err := rows.Scan(&accID, &t); err != nil {
			return nil, err
		}
		result[accID.String()+"|"+t] = true
	}
	return result, rows.Err()
}

// ListFailedTargets 返回任务日志中标记为失败的去重目标列表, 用于断点续发(只重试失败目标)
func (r *TaskLogRepository) ListFailedTargets(ctx context.Context, tenantID, taskID uuid.UUID) ([]string, error) {
	rows, err := r.db.Query(ctx, `
		SELECT DISTINCT target_id
		FROM task_logs WHERE tenant_id = $1 AND task_id = $2 AND status = 'failed' AND target_id <> ''
		ORDER BY target_id`, tenantID, taskID)
	if err != nil {
		return nil, fmt.Errorf("list failed targets: %w", err)
	}
	defer rows.Close()
	var targets []string
	for rows.Next() {
		var t string
		if err := rows.Scan(&t); err != nil {
			return nil, err
		}
		targets = append(targets, t)
	}
	return targets, rows.Err()
}
