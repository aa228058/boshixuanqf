package repository

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"tg-sender/internal/domain"
)

type AccountGroupRepository struct {
	db *pgxpool.Pool
}

func NewAccountGroupRepository(db *pgxpool.Pool) *AccountGroupRepository {
	return &AccountGroupRepository{db: db}
}

func (r *AccountGroupRepository) List(ctx context.Context, tenantID uuid.UUID) ([]*domain.AccountGroup, error) {
	rows, err := r.db.Query(ctx, `
		SELECT category_id, tenant_id, name, color, account_ids, created_at
		FROM account_group_categories WHERE tenant_id = $1 ORDER BY created_at DESC`, tenantID)
	if err != nil {
		return nil, fmt.Errorf("list account groups: %w", err)
	}
	defer rows.Close()
	var list []*domain.AccountGroup
	for rows.Next() {
		var g domain.AccountGroup
		if err := rows.Scan(&g.GroupID, &g.TenantID, &g.Name, &g.Color, &g.AccountIDs, &g.CreatedAt); err != nil {
			return nil, err
		}
		list = append(list, &g)
	}
	return list, rows.Err()
}

func (r *AccountGroupRepository) Create(ctx context.Context, g *domain.AccountGroup) error {
	// P1-2 审计修复: 原来 account_ids 直接入库不校验归属——可以把他人账号 ID 塞进分组。
	if err := r.validateAccountOwnership(ctx, g.TenantID, g.AccountIDs); err != nil {
		return err
	}
	_, err := r.db.Exec(ctx, `
		INSERT INTO account_group_categories (category_id, tenant_id, name, color, account_ids)
		VALUES ($1, $2, $3, $4, $5)`,
		g.GroupID, g.TenantID, g.Name, g.Color, g.AccountIDs)
	return err
}

// validateAccountOwnership 校验账号列表全部属于指定租户(P1-2)
func (r *AccountGroupRepository) validateAccountOwnership(ctx context.Context, tenantID uuid.UUID, accountIDs []uuid.UUID) error {
	if len(accountIDs) == 0 {
		return nil
	}
	var cnt int
	err := r.db.QueryRow(ctx,
		`SELECT COUNT(*) FROM accounts WHERE tenant_id = $1 AND account_id = ANY($2)`,
		tenantID, accountIDs).Scan(&cnt)
	if err != nil {
		return fmt.Errorf("校验账号归属失败: %w", err)
	}
	if cnt != len(accountIDs) {
		return fmt.Errorf("分组包含不属于本租户的账号")
	}
	return nil
}

func (r *AccountGroupRepository) Update(ctx context.Context, tenantID, groupID uuid.UUID, name, color string, accountIDs []uuid.UUID) error {
	// P1-2 审计修复: 同 Create——更新分组时也要校验账号归属
	if err := r.validateAccountOwnership(ctx, tenantID, accountIDs); err != nil {
		return err
	}
	_, err := r.db.Exec(ctx, `
		UPDATE account_group_categories SET name = $3, color = $4, account_ids = $5
		WHERE category_id = $1 AND tenant_id = $2`,
		groupID, tenantID, name, color, accountIDs)
	return err
}

func (r *AccountGroupRepository) Delete(ctx context.Context, tenantID, groupID uuid.UUID) error {
	_, err := r.db.Exec(ctx, `DELETE FROM account_group_categories WHERE category_id = $1 AND tenant_id = $2`, groupID, tenantID)
	return err
}
