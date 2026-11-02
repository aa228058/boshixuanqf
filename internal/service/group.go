package service

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"tg-sender/internal/domain"
	"tg-sender/internal/repository"
)

// GroupService handles group business logic.
type GroupService struct {
	db         *pgxpool.Pool
	repo       *repository.GroupRepository
	memberRepo *repository.MemberRepository
}

// NewGroupService creates a new group service.
func NewGroupService(db *pgxpool.Pool, repo *repository.GroupRepository, memberRepo *repository.MemberRepository) *GroupService {
	return &GroupService{db: db, repo: repo, memberRepo: memberRepo}
}

// List returns groups for a tenant.
func (s *GroupService) List(ctx context.Context, tenantID uuid.UUID, page, pageSize int) ([]*domain.Group, error) {
	if pageSize <= 0 {
		pageSize = 50
	}
	offset := (page - 1) * pageSize
	return s.repo.List(ctx, tenantID, pageSize, offset)
}

// ListByAccount returns groups joined by a specific account.
func (s *GroupService) ListByAccount(ctx context.Context, tenantID, accountID uuid.UUID, page, pageSize int) ([]*domain.Group, error) {
	if pageSize <= 0 {
		pageSize = 10000 // 默认返回全部，前端没有分页加载
	}
	offset := (page - 1) * pageSize
	return s.repo.ListByAccount(ctx, tenantID, accountID, pageSize, offset)
}

// CountByAccount returns total groups joined by a specific account.
func (s *GroupService) CountByAccount(ctx context.Context, tenantID, accountID uuid.UUID) (int, error) {
	return s.repo.CountByAccount(ctx, tenantID, accountID)
}

// Create creates a new group.
func (s *GroupService) Create(ctx context.Context, tenantID uuid.UUID, tgChatID int64, title, username, gtype string) (*domain.Group, error) {
	g := &domain.Group{
		GroupID:  uuid.New(),
		TenantID: tenantID,
		TGChatID: tgChatID,
		Title:    title,
		Username: username,
		Type:     gtype,
	}
	if err := s.repo.Create(ctx, g); err != nil {
		return nil, fmt.Errorf("create group: %w", err)
	}
	return g, nil
}

// GetMembers returns members for a group.
// ListGlobalMembers 私聊选择器数据源: 读取同步时写入 contacts 表的联系人缓存
// (之前误读 members 表——私聊对话在同步时已 UpsertContact 到 contacts 表, 读错表导致前端每次显示空/错数据)
func (s *GroupService) ListGlobalMembers(ctx context.Context, tenantID, accountID uuid.UUID) ([]map[string]any, error) {
	return s.memberRepo.ListContactRows(ctx, tenantID, accountID)
}

func (s *GroupService) GetMembers(ctx context.Context, tenantID, groupID uuid.UUID, page, pageSize int) ([]*domain.Member, int, error) {
	if pageSize <= 0 {
		pageSize = 100
	}
	offset := (page - 1) * pageSize
	members, err := s.memberRepo.ListByGroup(ctx, tenantID, groupID, pageSize, offset)
	if err != nil {
		return nil, 0, err
	}
	count, err := s.memberRepo.CountByGroup(ctx, tenantID, groupID)
	if err != nil {
		return nil, 0, err
	}
	return members, count, nil
}

// GetByID returns a group by ID.
func (s *GroupService) GetByID(ctx context.Context, tenantID, groupID uuid.UUID) (*domain.Group, error) {
	return s.repo.GetByID(ctx, tenantID, groupID)
}

// Update updates group fields.
func (s *GroupService) Update(ctx context.Context, tenantID, groupID uuid.UUID, title, username *string) error {
	g, err := s.repo.GetByID(ctx, tenantID, groupID)
	if err != nil {
		return err
	}
	if title != nil {
		g.Title = *title
	}
	if username != nil {
		g.Username = *username
	}
	return s.repo.Update(ctx, g)
}

// Delete removes a group and its members atomically.
func (s *GroupService) Delete(ctx context.Context, tenantID, groupID uuid.UUID) error {
	return pgx.BeginFunc(ctx, s.db, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `DELETE FROM members WHERE tenant_id = $1 AND group_id = $2`, tenantID, groupID); err != nil {
			return fmt.Errorf("delete members: %w", err)
		}
		if _, err := tx.Exec(ctx, `DELETE FROM groups WHERE tenant_id = $1 AND group_id = $2`, tenantID, groupID); err != nil {
			return fmt.Errorf("delete group: %w", err)
		}
		return nil
	})
}
