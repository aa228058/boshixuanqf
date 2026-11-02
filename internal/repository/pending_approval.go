package repository

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"tg-sender/internal/domain"
)

type PendingApprovalRepository struct {
	pool *pgxpool.Pool
}

func NewPendingApprovalRepository(pool *pgxpool.Pool) *PendingApprovalRepository {
	return &PendingApprovalRepository{pool: pool}
}

func (r *PendingApprovalRepository) Create(ctx context.Context, pa *domain.PendingApproval) error {
	// P2-86 审计修复: 原来 ON CONFLICT DO NOTHING 后不看 RowsAffected——首次插入
	// 失败(如字段超长/约束冲突)与"已存在跳过"无法区分, 失败被静默吞掉。
	// 检查 RowsAffected: 0 且非冲突场景应视为失败信号(至少记日志由调用方感知)。
	res, err := r.pool.Exec(ctx, `
		INSERT INTO pending_approvals (task_id, tenant_id, account_id, chat_id, access_hash, target_link, status, max_checks)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
		ON CONFLICT (task_id) DO NOTHING
	`, pa.TaskID, pa.TenantID, pa.AccountID, pa.ChatID, pa.AccessHash, pa.TargetLink, pa.Status, pa.MaxChecks)
	// P2-86: RowsAffected==0 说明唯一键冲突被 ON CONFLICT 跳过(幂等, 正常)或插入
	// 异常静默。err==nil 但 0 行时返回哨兵错误, 让调用方感知"这条审批没进去"。
	if err == nil && res.RowsAffected() == 0 {
		return fmt.Errorf("pending approval insert: 0 rows (conflict or silent failure)")
	}
	return err
}

func (r *PendingApprovalRepository) GetPending(ctx context.Context, tenantID uuid.UUID, limit int) ([]*domain.PendingApproval, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT id, task_id, tenant_id, account_id, chat_id, access_hash, target_link, status, check_count, max_checks, created_at, checked_at
		FROM pending_approvals
		WHERE status = 'pending' AND tenant_id = $1
		ORDER BY created_at
		LIMIT $2
	`, tenantID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var results []*domain.PendingApproval
	for rows.Next() {
		pa := &domain.PendingApproval{}
		if err := rows.Scan(&pa.ID, &pa.TaskID, &pa.TenantID, &pa.AccountID, &pa.ChatID, &pa.AccessHash, &pa.TargetLink, &pa.Status, &pa.CheckCount, &pa.MaxChecks, &pa.CreatedAt, &pa.CheckedAt); err != nil {
			return nil, fmt.Errorf("scan pending approval: %w", err)
		}
		results = append(results, pa)
	}
	return results, rows.Err()
}

// GetPendingAll retrieves pending approvals across all tenants (for background worker).
func (r *PendingApprovalRepository) GetPendingAll(ctx context.Context, limit int) ([]*domain.PendingApproval, error) {
	// G-03: 跳过非活跃/过期租户的待审批（其任务认领已被拦截，审批轮询同步跳过）。
	rows, err := r.pool.Query(ctx, `
		SELECT pa.id, pa.task_id, pa.tenant_id, pa.account_id, pa.chat_id, pa.access_hash, pa.target_link,
		       pa.status, pa.check_count, pa.max_checks, pa.created_at, pa.checked_at
		FROM pending_approvals pa
		JOIN tenants t ON t.tenant_id = pa.tenant_id
		WHERE pa.status = 'pending' AND t.status = 'active'
		  AND (t.expires_at IS NULL OR t.expires_at > NOW())
		ORDER BY pa.created_at
		LIMIT $1
	`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var results []*domain.PendingApproval
	for rows.Next() {
		pa := &domain.PendingApproval{}
		if err := rows.Scan(&pa.ID, &pa.TaskID, &pa.TenantID, &pa.AccountID, &pa.ChatID, &pa.AccessHash, &pa.TargetLink, &pa.Status, &pa.CheckCount, &pa.MaxChecks, &pa.CreatedAt, &pa.CheckedAt); err != nil {
			return nil, fmt.Errorf("scan pending approval: %w", err)
		}
		results = append(results, pa)
	}
	return results, rows.Err()
}

// GetByAccountAndLink checks if there's already a pending approval for the same account + link.
func (r *PendingApprovalRepository) GetByAccountAndLink(ctx context.Context, accountID uuid.UUID, targetLink string) (*domain.PendingApproval, error) {
	pa := &domain.PendingApproval{}
	err := r.pool.QueryRow(ctx, `
		SELECT id, task_id, tenant_id, account_id, chat_id, access_hash, target_link, status, check_count, max_checks, created_at, checked_at
		FROM pending_approvals
		WHERE account_id = $1 AND target_link = $2 AND status = 'pending'
		ORDER BY created_at DESC
		LIMIT 1
	`, accountID, targetLink).Scan(&pa.ID, &pa.TaskID, &pa.TenantID, &pa.AccountID, &pa.ChatID, &pa.AccessHash, &pa.TargetLink, &pa.Status, &pa.CheckCount, &pa.MaxChecks, &pa.CreatedAt, &pa.CheckedAt)
	if err != nil {
		return nil, err
	}
	return pa, nil
}

func (r *PendingApprovalRepository) UpdateStatus(ctx context.Context, tenantID, taskID uuid.UUID, status string) error {
	_, err := r.pool.Exec(ctx, `
		UPDATE pending_approvals SET status = $1, checked_at = NOW() WHERE tenant_id = $2 AND task_id = $3
	`, status, tenantID, taskID)
	return err
}

func (r *PendingApprovalRepository) IncrementCheck(ctx context.Context, tenantID, taskID uuid.UUID) error {
	_, err := r.pool.Exec(ctx, `
		UPDATE pending_approvals SET check_count = check_count + 1, checked_at = NOW() WHERE tenant_id = $1 AND task_id = $2
	`, tenantID, taskID)
	return err
}
