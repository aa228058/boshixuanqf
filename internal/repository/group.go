package repository

import (
	"context"
	"fmt"
	"log"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"tg-sender/internal/domain"
)

// GroupRepository handles group data access.
type GroupRepository struct {
	db *pgxpool.Pool
}

// NewGroupRepository creates a new group repository.
func NewGroupRepository(db *pgxpool.Pool) *GroupRepository {
	return &GroupRepository{db: db}
}

// List retrieves groups for a tenant.
func (r *GroupRepository) List(ctx context.Context, tenantID uuid.UUID, limit, offset int) ([]*domain.Group, error) {
	rows, err := r.db.Query(ctx, `
		SELECT group_id, tenant_id, tg_chat_id, title, username, type, member_count, is_joined, joined_at, last_scraped_at, access_hash, metadata
		-- P2-59 审计修复: 加 tiebreaker(group_id)——同秒创建的群分页不再重复/遗漏
		FROM groups WHERE tenant_id = $1 ORDER BY created_at DESC, group_id DESC LIMIT $2 OFFSET $3`, tenantID, limit, offset)
	if err != nil {
		return nil, fmt.Errorf("list groups: %w", err)
	}
	defer rows.Close()

	var groups []*domain.Group
	for rows.Next() {
		var g domain.Group
		err := rows.Scan(&g.GroupID, &g.TenantID, &g.TGChatID, &g.Title, &g.Username, &g.Type,
			&g.MemberCount, &g.IsJoined, &g.JoinedAt, &g.LastScrapedAt, &g.AccessHash, &g.Metadata)
		if err != nil {
			return nil, err
		}
		groups = append(groups, &g)
	}
	return groups, rows.Err()
}

// ListByAccount retrieves groups joined by a specific account.
func (r *GroupRepository) ListByAccount(ctx context.Context, tenantID, accountID uuid.UUID, limit, offset int) ([]*domain.Group, error) {
	log.Printf("[repo] ListByAccount 租户=%s 账号=%s 限制=%d 偏移=%d", tenantID, accountID, limit, offset)
	rows, err := r.db.Query(ctx, `
		SELECT g.group_id, g.tenant_id, g.tg_chat_id, g.title, g.username, g.type, g.member_count, g.is_joined, g.joined_at, g.last_scraped_at, g.access_hash, g.metadata
		FROM groups g
		JOIN account_groups ag ON g.group_id = ag.group_id
		WHERE g.tenant_id = $1 AND ag.account_id = $2 AND ag.account_id IS NOT NULL
		  AND ag.is_joined = true
		ORDER BY g.created_at DESC LIMIT $3 OFFSET $4`, tenantID, accountID, limit, offset)
	if err != nil {
		log.Printf("[repo] ListByAccount 查询错误: %v", err)
		return nil, fmt.Errorf("list groups by account: %w", err)
	}
	defer rows.Close()

	var groups []*domain.Group
	for rows.Next() {
		var g domain.Group
		err := rows.Scan(&g.GroupID, &g.TenantID, &g.TGChatID, &g.Title, &g.Username, &g.Type,
			&g.MemberCount, &g.IsJoined, &g.JoinedAt, &g.LastScrapedAt, &g.AccessHash, &g.Metadata)
		if err != nil {
			log.Printf("[repo] ListByAccount 扫描错误: %v", err)
			return nil, err
		}
		groups = append(groups, &g)
	}
	log.Printf("[repo] ListByAccount 返回了 %d 个群组", len(groups))
	return groups, rows.Err()
}

// UpsertAccountGroup creates or updates the account-group association.
func (r *GroupRepository) UpsertAccountGroup(ctx context.Context, accountID, groupID uuid.UUID, isJoined bool) error {
	_, err := r.db.Exec(ctx, `
		INSERT INTO account_groups (account_id, group_id, is_joined, joined_at, created_at)
		VALUES ($1, $2, $3, CASE WHEN $3 THEN NOW() ELSE NULL END, NOW())
		ON CONFLICT (account_id, group_id) DO UPDATE SET
			is_joined = EXCLUDED.is_joined,
			joined_at = EXCLUDED.joined_at`,
		accountID, groupID, isJoined)
	return err
}

// GetByID retrieves a group by ID.
func (r *GroupRepository) GetByID(ctx context.Context, tenantID, groupID uuid.UUID) (*domain.Group, error) {
	var g domain.Group
	err := r.db.QueryRow(ctx, `
		SELECT group_id, tenant_id, tg_chat_id, title, username, type, member_count, is_joined, joined_at, last_scraped_at, access_hash, metadata
		FROM groups WHERE tenant_id = $1 AND group_id = $2`, tenantID, groupID,
	).Scan(&g.GroupID, &g.TenantID, &g.TGChatID, &g.Title, &g.Username, &g.Type,
		&g.MemberCount, &g.IsJoined, &g.JoinedAt, &g.LastScrapedAt, &g.AccessHash, &g.Metadata)
	if err != nil {
		return nil, fmt.Errorf("get group: %w", err)
	}
	return &g, nil
}

// GetByTGChatID retrieves a group by Telegram chat ID.
func (r *GroupRepository) GetByTGChatID(ctx context.Context, tenantID uuid.UUID, tgChatID int64) (*domain.Group, error) {
	var g domain.Group
	err := r.db.QueryRow(ctx, `
		SELECT group_id, tenant_id, tg_chat_id, title, username, type, member_count, is_joined, joined_at, last_scraped_at, access_hash, metadata
		FROM groups WHERE tenant_id = $1 AND tg_chat_id = $2`, tenantID, tgChatID,
	).Scan(&g.GroupID, &g.TenantID, &g.TGChatID, &g.Title, &g.Username, &g.Type,
		&g.MemberCount, &g.IsJoined, &g.JoinedAt, &g.LastScrapedAt, &g.AccessHash, &g.Metadata)
	if err != nil {
		return nil, err
	}
	return &g, nil
}

// GetByUsername retrieves a group by username.
func (r *GroupRepository) GetByUsername(ctx context.Context, tenantID uuid.UUID, username string) (*domain.Group, error) {
	var g domain.Group
	err := r.db.QueryRow(ctx, `
		SELECT group_id, tenant_id, tg_chat_id, title, username, type, member_count, is_joined, joined_at, last_scraped_at, access_hash, metadata
		FROM groups WHERE tenant_id = $1 AND username = $2`, tenantID, username,
	).Scan(&g.GroupID, &g.TenantID, &g.TGChatID, &g.Title, &g.Username, &g.Type,
		&g.MemberCount, &g.IsJoined, &g.JoinedAt, &g.LastScrapedAt, &g.AccessHash, &g.Metadata)
	if err != nil {
		return nil, err
	}
	return &g, nil
}

// Create inserts a new group, or updates on conflict.
func (r *GroupRepository) Create(ctx context.Context, g *domain.Group) error {
	if g.Metadata == nil {
		g.Metadata = map[string]any{}
	}
	_, err := r.db.Exec(ctx, `
		INSERT INTO groups (group_id, tenant_id, tg_chat_id, title, username, type, member_count, is_joined, joined_at, access_hash, metadata)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)
		ON CONFLICT (tenant_id, tg_chat_id) DO UPDATE SET
			title = EXCLUDED.title,
			username = EXCLUDED.username,
			type = EXCLUDED.type,
			member_count = EXCLUDED.member_count,
			is_joined = EXCLUDED.is_joined,
			joined_at = EXCLUDED.joined_at,
			access_hash = EXCLUDED.access_hash,
			metadata = EXCLUDED.metadata`,
		g.GroupID, g.TenantID, g.TGChatID, g.Title, g.Username, g.Type, g.MemberCount, g.IsJoined, g.JoinedAt, g.AccessHash, g.Metadata)
	return err
}

// UpdateJoinStatus updates group join status.
func (r *GroupRepository) UpdateJoinStatus(ctx context.Context, tenantID, groupID uuid.UUID, isJoined bool) error {
	_, err := r.db.Exec(ctx, `
		UPDATE groups SET is_joined = $3, joined_at = CASE WHEN $3 THEN NOW() ELSE joined_at END
		WHERE tenant_id = $1 AND group_id = $2`, tenantID, groupID, isJoined)
	return err
}

// UpdateLastScraped updates last scraped time.
func (r *GroupRepository) UpdateLastScraped(ctx context.Context, tenantID, groupID uuid.UUID) error {
	_, err := r.db.Exec(ctx, `UPDATE groups SET last_scraped_at = NOW() WHERE tenant_id = $1 AND group_id = $2`, tenantID, groupID)
	return err
}

// Update updates a group.
func (r *GroupRepository) Update(ctx context.Context, g *domain.Group) error {
	_, err := r.db.Exec(ctx, `
		UPDATE groups SET title = $3, username = $4, type = $5, member_count = $6, is_joined = $7, access_hash = $8, metadata = $9
		WHERE tenant_id = $1 AND group_id = $2`,
		g.TenantID, g.GroupID, g.Title, g.Username, g.Type, g.MemberCount, g.IsJoined, g.AccessHash, g.Metadata)
	return err
}

// Delete removes a group.
func (r *GroupRepository) Delete(ctx context.Context, tenantID, groupID uuid.UUID) error {
	_, err := r.db.Exec(ctx, `DELETE FROM groups WHERE tenant_id = $1 AND group_id = $2`, tenantID, groupID)
	return err
}

// CountByTenant counts total groups for a tenant.
func (r *GroupRepository) CountByTenant(ctx context.Context, tenantID uuid.UUID) (int, error) {
	var count int
	err := r.db.QueryRow(ctx, `SELECT COUNT(*) FROM groups WHERE tenant_id = $1`, tenantID).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("count groups: %w", err)
	}
	return count, nil
}

// CountByAccount counts total groups joined by a specific account.
func (r *GroupRepository) CountByAccount(ctx context.Context, tenantID, accountID uuid.UUID) (int, error) {
	var count int
	err := r.db.QueryRow(ctx, `
		SELECT COUNT(*) FROM groups g
		JOIN account_groups ag ON g.group_id = ag.group_id
		-- P1-39 审计修复: 原来不查 is_joined——已退出的群也计入 total, 分页器虚高
		WHERE g.tenant_id = $1 AND ag.account_id = $2 AND ag.account_id IS NOT NULL AND ag.is_joined = true`, tenantID, accountID).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("count groups by account: %w", err)
	}
	return count, nil
}
