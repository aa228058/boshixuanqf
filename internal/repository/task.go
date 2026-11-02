package repository

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"tg-sender/internal/domain"
)

// TaskRepository handles task data access.
type TaskRepository struct {
	db *pgxpool.Pool
}

// NewTaskRepository creates a new task repository.
func NewTaskRepository(db *pgxpool.Pool) *TaskRepository {
	return &TaskRepository{db: db}
}

// List retrieves tasks for a tenant, optionally filtered by status and account_id.
// When accountID is provided, only tasks that include this account in account_ids are returned.
func (r *TaskRepository) List(ctx context.Context, tenantID uuid.UUID, status *domain.TaskStatus, accountID *uuid.UUID, taskType *domain.TaskType, hideLoops bool, since *time.Time, limit, offset int) ([]*domain.Task, error) {
	// 动态拼装 WHERE, 所有过滤条件(状态/账号/类型)都在 SQL 层生效, 避免翻页后再过滤导致"筛选没反应"
	query := `
		SELECT task_id, tenant_id, type, status, priority, account_ids, target_ids, payload, progress,
		       locked_at, worker_id, retry_count, scheduled_at, started_at, completed_at, created_at,
		       is_loop, loop_interval_minutes, min_interval_seconds, max_interval_seconds, next_run_at
		FROM tasks WHERE tenant_id = $1`
	args := []interface{}{tenantID}
	idx := 2
	if status != nil {
		query += fmt.Sprintf(" AND status = $%d", idx)
		args = append(args, *status)
		idx++
	}
	if accountID != nil {
		query += fmt.Sprintf(" AND account_ids @> $%d::jsonb", idx)
		args = append(args, fmt.Sprintf(`["%s"]`, accountID.String()))
		idx++
	}
	if taskType != nil && *taskType != "" {
		query += fmt.Sprintf(" AND type = $%d", idx)
		args = append(args, *taskType)
		idx++
	}
	if hideLoops {
		// 任务列表页隐藏循环包装任务, 只显示实际执行记录
		query += " AND is_loop = FALSE"
	}
	if since != nil {
		// 今日/近7日筛选(任务列表页时间范围Tab)
		query += fmt.Sprintf(" AND created_at >= $%d", idx)
		args = append(args, *since)
		idx++
	}
	query += fmt.Sprintf(" ORDER BY created_at DESC LIMIT $%d OFFSET $%d", idx, idx+1)
	args = append(args, limit, offset)

	rows, err := r.db.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("list tasks: %w", err)
	}
	defer rows.Close()

	return r.scanTasks(rows)
}

// GetByID retrieves a task by ID.
// GetByTaskIDAnyTenant 仅用于取消落库等系统级操作(普通业务必须用带tenant的GetByID)。
func (r *TaskRepository) GetByTaskIDAnyTenant(ctx context.Context, taskID uuid.UUID) (*domain.Task, error) {
	var t domain.Task
	var lockedAt pgtype.Timestamptz
	var workerID pgtype.Text
	var scheduledAt, startedAt, completedAt, nextRunAt pgtype.Timestamptz
	err := r.db.QueryRow(ctx, `
		SELECT task_id, tenant_id, type, status, priority, account_ids, target_ids, payload, progress,
		       locked_at, worker_id, retry_count, scheduled_at, started_at, completed_at, created_at,
		       is_loop, loop_interval_minutes, min_interval_seconds, max_interval_seconds, next_run_at
		FROM tasks WHERE task_id = $1`, taskID,
	).Scan(&t.TaskID, &t.TenantID, &t.Type, &t.Status, &t.Priority, &t.AccountIDs, &t.TargetIDs, &t.Payload, &t.Progress,
		&lockedAt, &workerID, &t.RetryCount, &scheduledAt, &startedAt, &completedAt, &t.CreatedAt,
		&t.IsLoop, &t.LoopIntervalMinutes, &t.MinIntervalSeconds, &t.MaxIntervalSeconds, &nextRunAt)
	if err != nil {
		return nil, fmt.Errorf("get task: %w", err)
	}
	// P3修复(精读发现): 查了 next_run_at 但漏赋值, 与 GetByID/scanTasks 行为不一致
	if nextRunAt.Valid {
		tt := nextRunAt.Time
		t.NextRunAt = &tt
	}
	if lockedAt.Valid {
		tt := lockedAt.Time
		t.LockedAt = &tt
	}
	if workerID.Valid {
		t.WorkerID = &workerID.String
	}
	if scheduledAt.Valid {
		tt := scheduledAt.Time
		t.ScheduledAt = &tt
	}
	if startedAt.Valid {
		tt := startedAt.Time
		t.StartedAt = &tt
	}
	if completedAt.Valid {
		tt := completedAt.Time
		t.CompletedAt = &tt
	}
	return &t, nil
}

func (r *TaskRepository) GetByID(ctx context.Context, tenantID, taskID uuid.UUID) (*domain.Task, error) {
	var t domain.Task
	var lockedAt pgtype.Timestamptz
	var workerID pgtype.Text
	var scheduledAt pgtype.Timestamptz
	var startedAt pgtype.Timestamptz
	var completedAt pgtype.Timestamptz
	var nextRunAt pgtype.Timestamptz

	err := r.db.QueryRow(ctx, `
		SELECT task_id, tenant_id, type, status, priority, account_ids, target_ids, payload, progress,
		       locked_at, worker_id, retry_count, scheduled_at, started_at, completed_at, created_at,
					       is_loop, loop_interval_minutes, min_interval_seconds, max_interval_seconds, next_run_at
		FROM tasks WHERE tenant_id = $1 AND task_id = $2`, tenantID, taskID,
	).Scan(&t.TaskID, &t.TenantID, &t.Type, &t.Status, &t.Priority, &t.AccountIDs, &t.TargetIDs, &t.Payload, &t.Progress,
		&lockedAt, &workerID, &t.RetryCount, &scheduledAt, &startedAt, &completedAt, &t.CreatedAt,
		&t.IsLoop, &t.LoopIntervalMinutes, &t.MinIntervalSeconds, &t.MaxIntervalSeconds, &nextRunAt)
	if err != nil {
		return nil, fmt.Errorf("get task: %w", err)
	}
	if lockedAt.Valid {
		tt := lockedAt.Time
		t.LockedAt = &tt
	}
	if workerID.Valid {
		t.WorkerID = &workerID.String
	}
	if scheduledAt.Valid {
		tt := scheduledAt.Time
		t.ScheduledAt = &tt
	}
	if startedAt.Valid {
		tt := startedAt.Time
		t.StartedAt = &tt
	}
	if completedAt.Valid {
		tt := completedAt.Time
		t.CompletedAt = &tt
	}
	if nextRunAt.Valid {
		tt := nextRunAt.Time
		t.NextRunAt = &tt
	}
	return &t, nil
}

// Create inserts a new task and notifies workers.
func (r *TaskRepository) Create(ctx context.Context, t *domain.Task) error {
	err := pgx.BeginFunc(ctx, r.db, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `
			INSERT INTO tasks (task_id, tenant_id, type, status, priority, account_ids, target_ids, payload, progress, scheduled_at,
			                   is_loop, loop_interval_minutes, min_interval_seconds, max_interval_seconds, next_run_at)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15)`,
			t.TaskID, t.TenantID, t.Type, t.Status, t.Priority, t.AccountIDs, t.TargetIDs, t.Payload, t.Progress, t.ScheduledAt,
			t.IsLoop, t.LoopIntervalMinutes, t.MinIntervalSeconds, t.MaxIntervalSeconds, t.NextRunAt)
		return err
	})
	if err != nil {
		return err
	}
	// Notify workers that a new task is available
	if _, notifyErr := r.db.Exec(ctx, "NOTIFY new_task"); notifyErr != nil {
		// Non-fatal: workers will still poll
		log.Printf("[task] failed to notify new task: %v", notifyErr)
	}
	return nil
}

// ClaimTasks atomically claims pending/queued tasks for a worker using row-level locking.
// lockTimeoutMin 与 running 锁过期一致：超过该分钟数未更新的锁视为 worker 崩溃可回收。
func (r *TaskRepository) ClaimTasks(ctx context.Context, workerID string, limit int, lockTimeoutMin int) ([]*domain.Task, error) {
	return r.claimTasksWithStatus(ctx, workerID, limit, "queued", lockTimeoutMin)
}

// ClaimTimeoutTasks claims tasks whose lock has expired.
func (r *TaskRepository) ClaimTimeoutTasks(ctx context.Context, workerID string, limit int, lockTimeoutMin int) ([]*domain.Task, error) {
	rows, err := r.db.Query(ctx, `
		UPDATE tasks SET status = 'running', locked_at = NOW(), worker_id = $1, started_at = COALESCE(started_at, NOW())
		WHERE task_id IN (
			SELECT task_id FROM tasks
			WHERE status = 'running'
			  AND locked_at < NOW() - ($3 * INTERVAL '1 minute')
			ORDER BY priority DESC, created_at ASC
			FOR UPDATE SKIP LOCKED
			LIMIT $2
		)
		RETURNING task_id, tenant_id, type, status, priority, account_ids, target_ids, payload, progress,
		          locked_at, worker_id, retry_count, scheduled_at, started_at, completed_at, created_at,
					       is_loop, loop_interval_minutes, min_interval_seconds, max_interval_seconds, next_run_at`,
		workerID, limit, lockTimeoutMin)
	if err != nil {
		return nil, fmt.Errorf("claim timeout tasks: %w", err)
	}
	defer rows.Close()
	return r.scanTasks(rows)
}

// CountRunningGlobal 全局 running 任务总数(内存自适应的全局并发上限检查用)
func (r *TaskRepository) CountRunningGlobal(ctx context.Context) (int, error) {
	var n int
	err := r.db.QueryRow(ctx, `SELECT count(*) FROM tasks WHERE status='running'`).Scan(&n)
	return n, err
}

func (r *TaskRepository) claimTasksWithStatus(ctx context.Context, workerID string, limit int, status domain.TaskStatus, lockTimeoutMin int) ([]*domain.Task, error) {
	rows, err := r.db.Query(ctx, `
		UPDATE tasks SET status = 'running', locked_at = NOW(), worker_id = $1, started_at = COALESCE(started_at, NOW())
		WHERE task_id IN (
			SELECT task_id FROM tasks
			WHERE status = $2
			  AND (locked_at IS NULL OR locked_at < NOW() - ($4 * INTERVAL '1 minute'))
			  AND (scheduled_at IS NULL OR scheduled_at <= NOW())
			ORDER BY priority DESC, created_at ASC
			FOR UPDATE SKIP LOCKED
			LIMIT $3
		)
		RETURNING task_id, tenant_id, type, status, priority, account_ids, target_ids, payload, progress,
		          locked_at, worker_id, retry_count, scheduled_at, started_at, completed_at, created_at,
					       is_loop, loop_interval_minutes, min_interval_seconds, max_interval_seconds, next_run_at`,
		workerID, status, limit, lockTimeoutMin)
	if err != nil {
		return nil, fmt.Errorf("claim tasks: %w", err)
	}
	defer rows.Close()
	return r.scanTasks(rows)
}

// ActivatePendingTasks converts pending tasks whose scheduled_at has passed to queued.
func (r *TaskRepository) ActivatePendingTasks(ctx context.Context) error {
	_, err := r.db.Exec(ctx, `
		UPDATE tasks SET status = 'queued', updated_at = NOW()
		WHERE status = 'pending' AND scheduled_at IS NOT NULL AND scheduled_at <= NOW()`)
	return err
}

// GetNextPendingTaskTime returns the scheduled_at of the earliest pending task.
// Returns nil if no pending tasks exist.
func (r *TaskRepository) GetNextPendingTaskTime(ctx context.Context) (*time.Time, error) {
	var scheduledAt pgtype.Timestamptz
	err := r.db.QueryRow(ctx, `
		SELECT scheduled_at FROM tasks
		WHERE status = 'pending' AND scheduled_at IS NOT NULL AND scheduled_at > NOW()
		ORDER BY scheduled_at ASC LIMIT 1`).Scan(&scheduledAt)
	if err == pgx.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get next pending task: %w", err)
	}
	if scheduledAt.Valid {
		t := scheduledAt.Time
		return &t, nil
	}
	return nil, nil
}

// CountOwnedAccounts 校验这些账号都属于该租户(M-02: 防任务Update写入跨租户账号)。
func (r *TaskRepository) CountOwnedAccounts(ctx context.Context, tenantID uuid.UUID, accountIDs []uuid.UUID) (int, error) {
	if len(accountIDs) == 0 {
		return 0, nil
	}
	rows, err := r.db.Query(ctx,
		`SELECT COUNT(*)::int FROM accounts WHERE tenant_id=$1 AND account_id = ANY($2)`, tenantID, accountIDs)
	if err != nil {
		return 0, err
	}
	defer rows.Close()
	var n int
	if rows.Next() {
		_ = rows.Scan(&n)
	}
	return n, rows.Err()
}

// UpdateProgress atomically increments task progress counters.
// Uses row-level locking to prevent concurrent updates from overwriting each other.
func (r *TaskRepository) UpdateProgress(ctx context.Context, tenantID, taskID uuid.UUID, progress domain.TaskProgress) error {
	_, err := r.db.Exec(ctx, `
		UPDATE tasks
		SET progress = jsonb_set(
			jsonb_set(
				jsonb_set(progress, '{done}', to_jsonb(COALESCE((progress->>'done')::int, 0) + $3)),
				'{failed}', to_jsonb(COALESCE((progress->>'failed')::int, 0) + $4)
			),
			'{current_target}', to_jsonb($5::text)
		)
		WHERE tenant_id = $1 AND task_id = $2`,
		tenantID, taskID, progress.Done, progress.Failed, progress.CurrentTarget)
	return err
}


// UpdateProgressExact sets progress to exact values (use only for final status).
func (r *TaskRepository) UpdateProgressExact(ctx context.Context, tenantID, taskID uuid.UUID, progress domain.TaskProgress) error {
	_, err := r.db.Exec(ctx, `
		UPDATE tasks SET progress = $3 WHERE tenant_id = $1 AND task_id = $2`,
		tenantID, taskID, progress)
	return err
}

// UpdateStatus updates task status.
func (r *TaskRepository) UpdateStatus(ctx context.Context, tenantID, taskID uuid.UUID, status domain.TaskStatus) error {
	var completedAt interface{}
	if status == domain.TaskStatusCompleted || status == domain.TaskStatusFailed || status == domain.TaskStatusCancelled {
		completedAt = time.Now()
	}
	// F-27: 迁移守卫——WHERE 内嵌合法迁移矩阵，非法迁移命中 0 行即报错。
	// 终态（failed/cancelled）与恢复（queued）为全来源白名单（引擎 crash 恢复、
	// 管理操作、任务失败路径都需要）；其余按状态机矩阵校验。
	res, err := r.db.Exec(ctx, `
		UPDATE tasks SET status = $3, completed_at = $4, locked_at = NULL, worker_id = NULL
		WHERE tenant_id = $1 AND task_id = $2 AND (
			$3 IN ('failed', 'cancelled', 'queued')
			OR (status = 'scheduled' AND $3 IN ('queued', 'cancelled'))
			OR (status = 'queued'   AND $3 IN ('running', 'cancelled'))
			OR (status = 'running'  AND $3 IN ('paused', 'completed', 'failed', 'cancelled'))
			OR (status = 'paused'   AND $3 IN ('running', 'cancelled'))
		)`,
		tenantID, taskID, status, completedAt)
	if err != nil {
		return err
	}
	if n := res.RowsAffected(); n == 0 {
		return fmt.Errorf("invalid task transition to %q (or task not found)", status)
	}
	return nil
}

// ResetToQueued resets a failed task to queued for retry.
func (r *TaskRepository) ResetToQueued(ctx context.Context, tenantID, taskID uuid.UUID) error {
	_, err := r.db.Exec(ctx, `
		UPDATE tasks SET status = 'queued', locked_at = NULL, worker_id = NULL,
		                 retry_count = retry_count + 1, completed_at = NULL
		WHERE tenant_id = $1 AND task_id = $2`, tenantID, taskID)
	return err
}

// Delete removes a task.
func (r *TaskRepository) Delete(ctx context.Context, tenantID, taskID uuid.UUID) error {
	_, err := r.db.Exec(ctx, `DELETE FROM tasks WHERE tenant_id = $1 AND task_id = $2`, tenantID, taskID)
	return err
}

// CountStats returns task statistics for a tenant.
func (r *TaskRepository) CountStats(ctx context.Context, tenantID uuid.UUID) (total, running, failed int, err error) {
	err = r.db.QueryRow(ctx, `
		SELECT COUNT(*),
		       COUNT(*) FILTER (WHERE status = 'running'),
		       COUNT(*) FILTER (WHERE status = 'failed')
		FROM tasks WHERE tenant_id = $1`, tenantID).Scan(&total, &running, &failed)
	return
}

// Count returns total tasks for a tenant (optionally filtered by status and account_id).
func (r *TaskRepository) Count(ctx context.Context, tenantID uuid.UUID, status *domain.TaskStatus, accountID *uuid.UUID, taskType *domain.TaskType, since *time.Time) (int, error) {
	query := `SELECT COUNT(*) FROM tasks WHERE tenant_id = $1`
	args := []interface{}{tenantID}
	idx := 2
	if status != nil {
		query += fmt.Sprintf(" AND status = $%d", idx)
		args = append(args, *status)
		idx++
	}
	if accountID != nil {
		// Use parameterised JSONB array to prevent injection
		args = append(args, []string{accountID.String()})
		query += fmt.Sprintf(" AND account_ids @> $%d::jsonb", idx)
		idx++
	}
	if taskType != nil && *taskType != "" {
		query += fmt.Sprintf(" AND type = $%d", idx)
		args = append(args, *taskType)
		idx++
	}
	if since != nil {
		query += fmt.Sprintf(" AND created_at >= $%d", idx)
		args = append(args, *since)
		idx++
	}
	var count int
	if err := r.db.QueryRow(ctx, query, args...).Scan(&count); err != nil {
		return 0, fmt.Errorf("count tasks: %w", err)
	}
	return count, nil
}

func (r *TaskRepository) scanTasks(rows pgx.Rows) ([]*domain.Task, error) {
	var tasks []*domain.Task
	for rows.Next() {
		var t domain.Task
		var lockedAt pgtype.Timestamptz
		var workerID pgtype.Text
		var scheduledAt pgtype.Timestamptz
		var startedAt pgtype.Timestamptz
		var completedAt pgtype.Timestamptz
		var nextRunAt pgtype.Timestamptz

		err := rows.Scan(&t.TaskID, &t.TenantID, &t.Type, &t.Status, &t.Priority, &t.AccountIDs, &t.TargetIDs, &t.Payload, &t.Progress,
			&lockedAt, &workerID, &t.RetryCount, &scheduledAt, &startedAt, &completedAt, &t.CreatedAt,
			&t.IsLoop, &t.LoopIntervalMinutes, &t.MinIntervalSeconds, &t.MaxIntervalSeconds, &nextRunAt)
		if err != nil {
			return nil, err
		}
		if nextRunAt.Valid {
			tt := nextRunAt.Time
			t.NextRunAt = &tt
		}
		if lockedAt.Valid {
			tt := lockedAt.Time
			t.LockedAt = &tt
		}
		if workerID.Valid {
			t.WorkerID = &workerID.String
		}
		if scheduledAt.Valid {
			tt := scheduledAt.Time
			t.ScheduledAt = &tt
		}
		if startedAt.Valid {
			tt := startedAt.Time
			t.StartedAt = &tt
		}
		if completedAt.Valid {
			tt := completedAt.Time
			t.CompletedAt = &tt
		}
		tasks = append(tasks, &t)
	}
	return tasks, rows.Err()
}

// CountByStatus counts tasks with a specific status for a tenant.
// P1-46 审计修复: 原来 tenantID 为 Nil 时报错返回 0——container 注入的 getQueueDepth
// 正是传 uuid.Nil(想要全平台队列深度), 结果恒 0, 自适应升档永不触发。
// 现在 Nil = 全租户统计。
func (r *TaskRepository) CountByStatus(ctx context.Context, tenantID uuid.UUID, status string) (int, error) {
	var count int
	var err error
	if tenantID == uuid.Nil {
		err = r.db.QueryRow(ctx, `SELECT COUNT(*) FROM tasks WHERE status = $1`, status).Scan(&count)
	} else {
		err = r.db.QueryRow(ctx, `SELECT COUNT(*) FROM tasks WHERE tenant_id = $1 AND status = $2`, tenantID, status).Scan(&count)
	}
	if err != nil {
		return 0, fmt.Errorf("count tasks by status: %w", err)
	}
	return count, nil
}

// CountCompletedToday counts tasks completed today for a tenant.
func (r *TaskRepository) CountCompletedToday(ctx context.Context, tenantID uuid.UUID) (int, error) {
	var count int
	err := r.db.QueryRow(ctx, `
		SELECT COUNT(*) FROM tasks
		WHERE tenant_id = $1 AND status = 'completed'
		AND completed_at >= CURRENT_DATE`, tenantID).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("count completed tasks today: %w", err)
	}
	return count, nil
}

// CountCompletedYesterday counts tasks completed yesterday for a tenant.
func (r *TaskRepository) CountCompletedYesterday(ctx context.Context, tenantID uuid.UUID) (int, error) {
	var count int
	err := r.db.QueryRow(ctx, `
		SELECT COUNT(*) FROM tasks
		WHERE tenant_id = $1 AND status = 'completed'
		AND completed_at >= CURRENT_DATE - INTERVAL '1 day'
		AND completed_at < CURRENT_DATE`, tenantID).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("count completed tasks yesterday: %w", err)
	}
	return count, nil
}

// Update updates task fields (only allowed for pending/paused tasks).
func (r *TaskRepository) Update(ctx context.Context, tenantID, taskID uuid.UUID, accountIDs []uuid.UUID, targetIDs []string, payload map[string]any) error {
	cmd, err := r.db.Exec(ctx, `
		UPDATE tasks
		SET account_ids = $1, target_ids = $2, payload = $3, updated_at = NOW()
		WHERE tenant_id = $4 AND task_id = $5 AND status IN ('pending', 'paused')`,
		accountIDs, targetIDs, payload, tenantID, taskID)
	if err != nil {
		return fmt.Errorf("update task: %w", err)
	}
	if cmd.RowsAffected() == 0 {
		return fmt.Errorf("task not found or cannot be updated (only pending/paused tasks can be updated)")
	}
	return nil
}

// CountCompletedLast7Days returns completed task counts for the last 7 days (oldest first).
func (r *TaskRepository) CountCompletedLast7Days(ctx context.Context, tenantID uuid.UUID) ([]int, error) {
	rows, err := r.db.Query(ctx, `
		SELECT 
			COALESCE(COUNT(*) FILTER (WHERE completed_at >= CURRENT_DATE - INTERVAL '6 days' AND completed_at < CURRENT_DATE - INTERVAL '5 days'), 0) as d6,
			COALESCE(COUNT(*) FILTER (WHERE completed_at >= CURRENT_DATE - INTERVAL '5 days' AND completed_at < CURRENT_DATE - INTERVAL '4 days'), 0) as d5,
			COALESCE(COUNT(*) FILTER (WHERE completed_at >= CURRENT_DATE - INTERVAL '4 days' AND completed_at < CURRENT_DATE - INTERVAL '3 days'), 0) as d4,
			COALESCE(COUNT(*) FILTER (WHERE completed_at >= CURRENT_DATE - INTERVAL '3 days' AND completed_at < CURRENT_DATE - INTERVAL '2 days'), 0) as d3,
			COALESCE(COUNT(*) FILTER (WHERE completed_at >= CURRENT_DATE - INTERVAL '2 days' AND completed_at < CURRENT_DATE - INTERVAL '1 day'), 0) as d2,
			COALESCE(COUNT(*) FILTER (WHERE completed_at >= CURRENT_DATE - INTERVAL '1 day' AND completed_at < CURRENT_DATE), 0) as d1,
			COALESCE(COUNT(*) FILTER (WHERE completed_at >= CURRENT_DATE), 0) as d0
		FROM tasks
		WHERE tenant_id = $1 AND status = 'completed'
		  AND completed_at >= CURRENT_DATE - INTERVAL '6 days'`, tenantID)
	if err != nil {
		return nil, fmt.Errorf("count completed last 7 days: %w", err)
	}
	defer rows.Close()

	var result []int
	if rows.Next() {
		var d0, d1, d2, d3, d4, d5, d6 int
		if err := rows.Scan(&d6, &d5, &d4, &d3, &d2, &d1, &d0); err != nil {
			return nil, fmt.Errorf("scan task trend: %w", err)
		}
		result = []int{d6, d5, d4, d3, d2, d1, d0}
	}
	return result, nil
}

// SetLoopConfig marks a task as a scheduled broadcast loop task.
func (r *TaskRepository) SetLoopConfig(ctx context.Context, tenantID, taskID uuid.UUID, isLoop bool, loopInterval, minInt, maxInt int, taskType domain.TaskType) error {
	_, err := r.db.Exec(ctx, `
		UPDATE tasks SET is_loop = $3, loop_interval_minutes = $4, min_interval_seconds = $5,
		                 max_interval_seconds = $6, type = $7, next_run_at = NOW()
		WHERE tenant_id = $1 AND task_id = $2`,
		tenantID, taskID, isLoop, loopInterval, minInt, maxInt, taskType)
	return err
}

// StopLoop 停止循环任务: 关闭循环标记并置为暂停, 调度器不再激活它。
func (r *TaskRepository) StopLoop(ctx context.Context, tenantID, taskID uuid.UUID) error {
	_, err := r.db.Exec(ctx, `UPDATE tasks SET is_loop = FALSE, status = 'paused' WHERE tenant_id = $1 AND task_id = $2`, tenantID, taskID)
	return err
}

// RescheduleLoopTask resets a finished loop task back to scheduled with a new run time.
func (r *TaskRepository) RescheduleLoopTask(ctx context.Context, tenantID, taskID uuid.UUID, nextRunAt time.Time) error {
	_, err := r.db.Exec(ctx, `
		-- 状态必须重置为 'pending': 调度器的 ActivatePendingTasks 每500ms扫描
		-- 'pending' 且 scheduled_at 到期的任务激活为 queued。
		-- 原值 'scheduled' 没有任何调度路径处理, 会导致循环任务执行一次后永远停摆。
		UPDATE tasks SET status = 'pending', next_run_at = $3, scheduled_at = $3,
		                 completed_at = NULL, locked_at = NULL, worker_id = NULL,
		                 progress = '{"total":0,"done":0,"failed":0}'
		WHERE tenant_id = $1 AND task_id = $2`, tenantID, taskID, nextRunAt)
	return err
}

// AccountStats 返回单个账号维度的仪表盘统计(账号切换后只看该账号的数据)。
// 查询都走 account_ids @> 账号 的索引条件。
func (r *TaskRepository) AccountStats(ctx context.Context, tenantID, accountID uuid.UUID) (map[string]any, error) {
	// 近 7 天每天完成的任务数
	rows, err := r.db.Query(ctx, `
		SELECT to_char(d, 'MM-DD') AS day, COUNT(t.task_id) AS cnt
		FROM generate_series(CURRENT_DATE - INTERVAL '6 days', CURRENT_DATE, INTERVAL '1 day') d
		LEFT JOIN tasks t ON t.tenant_id = $1 AND t.account_ids @> $2::jsonb
			AND t.status = 'completed' AND date(t.completed_at) = d
		GROUP BY d ORDER BY d`, tenantID, fmt.Sprintf(`["%s"]`, accountID.String()))
	if err != nil {
		return nil, fmt.Errorf("account task trend: %w", err)
	}
	defer rows.Close()
	trend := make([]int, 0, 7)
	for rows.Next() {
		var day string
		var cnt int
		if err := rows.Scan(&day, &cnt); err != nil {
			return nil, err
		}
		trend = append(trend, cnt)
	}
	if len(trend) == 0 {
		trend = []int{0, 0, 0, 0, 0, 0, 0}
	}

	out := map[string]any{"task_trend": trend}
	// 注意: pgx 严格校验参数个数, 每条 SQL 按实际占位符数量传参
	counts := []struct {
		key  string
		sql  string
		args []interface{}
	}{
		{"completed_today", `SELECT COUNT(*) FROM tasks WHERE tenant_id=$1 AND account_ids @> $2::jsonb AND status='completed' AND completed_at::date = CURRENT_DATE`,
			[]interface{}{tenantID, fmt.Sprintf(`["%s"]`, accountID.String())}},
		{"completed_yesterday", `SELECT COUNT(*) FROM tasks WHERE tenant_id=$1 AND account_ids @> $2::jsonb AND status='completed' AND completed_at::date = CURRENT_DATE - 1`,
			[]interface{}{tenantID, fmt.Sprintf(`["%s"]`, accountID.String())}},
		// V32 审计修复: 原来不查 is_joined——已退出的群也计入"总群数", 仪表盘数字虚高
		{"total_groups", `SELECT COUNT(*) FROM groups WHERE tenant_id=$1 AND group_id IN (SELECT group_id FROM account_groups WHERE account_id=$2 AND is_joined=true)`,
			[]interface{}{tenantID, accountID}},
		{"risks_24h", `SELECT COUNT(*) FROM risk_events WHERE tenant_id=$1 AND account_id=$2 AND created_at >= NOW() - INTERVAL '24 hours'`,
			[]interface{}{tenantID, accountID}},
	}
	for _, cq := range counts {
		var n int
		if err := r.db.QueryRow(ctx, cq.sql, cq.args...).Scan(&n); err != nil {
			return nil, fmt.Errorf("account stats %s: %w", cq.key, err)
		}
		out[cq.key] = n
	}
	return out, nil
}
