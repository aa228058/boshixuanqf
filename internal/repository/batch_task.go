package repository

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"tg-sender/internal/domain"
)

// BatchTaskRepository implements domain.BatchTaskRepository.
type BatchTaskRepository struct {
	db *pgxpool.Pool
}

// NewBatchTaskRepository creates a new batch task repository.
func NewBatchTaskRepository(db *pgxpool.Pool) *BatchTaskRepository {
	return &BatchTaskRepository{db: db}
}

// Create inserts a new batch task.
func (r *BatchTaskRepository) Create(ctx context.Context, batch *domain.BatchTask) error {
	_, err := r.db.Exec(ctx, `
		INSERT INTO batch_tasks (batch_id, tenant_id, message, total_msgs, sub_tasks, created_at)
		VALUES ($1, $2, $3, $4, $5, $6)`,
		batch.BatchID, batch.TenantID, batch.Message, batch.TotalMsgs, batch.SubTasks, batch.CreatedAt)
	return err
}

// GetByID retrieves a batch task by ID.
func (r *BatchTaskRepository) GetByID(ctx context.Context, tenantID, batchID uuid.UUID) (*domain.BatchTask, error) {
	batch := &domain.BatchTask{}
	var subTasks []uuid.UUID
	err := r.db.QueryRow(ctx, `
		SELECT batch_id, tenant_id, message, total_msgs, sub_tasks, created_at
		FROM batch_tasks
		WHERE tenant_id = $1 AND batch_id = $2`,
		tenantID, batchID).Scan(
		&batch.BatchID, &batch.TenantID, &batch.Message, &batch.TotalMsgs, &subTasks, &batch.CreatedAt)
	if err != nil {
		return nil, fmt.Errorf("get batch task: %w", err)
	}
	batch.SubTasks = subTasks
	return batch, nil
}

// UpdateSubTask appends a sub-task ID to the batch.
// UpdateSubTask appends a sub-task ID to the batch.
// P2-39 审计修复: 原来裸 array_append 无幂等——重试/重复调用会把同一 subTaskID
// 追加两次。条件更新: 数组已含该 ID 时不动。
func (r *BatchTaskRepository) UpdateSubTask(ctx context.Context, tenantID, batchID uuid.UUID, subTaskID uuid.UUID) error {
	_, err := r.db.Exec(ctx, `
		UPDATE batch_tasks
		SET sub_tasks = array_append(sub_tasks, $3)
		WHERE tenant_id = $1 AND batch_id = $2
		  AND NOT (sub_tasks @> ARRAY[$3]::uuid[])`,
		tenantID, batchID, subTaskID)
	return err
}
