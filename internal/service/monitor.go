package service

import (
	"context"
	"fmt"
	"log"

	"github.com/google/uuid"
	"tg-sender/internal/domain"
	"tg-sender/internal/repository"
)

// MonitorService handles monitor rule business logic.
type MonitorService struct {
	repo   *repository.MonitorRuleRepository
	engine MonitorEngine
}

// MonitorEngine interface for monitor rule operations.
type MonitorEngine interface {
	ReloadRules(ctx context.Context, tenantID uuid.UUID) error
}

// NewMonitorService creates a new monitor service.
func NewMonitorService(repo *repository.MonitorRuleRepository, engine MonitorEngine) *MonitorService {
	return &MonitorService{repo: repo, engine: engine}
}

// List returns monitor rules for a tenant.
func (s *MonitorService) List(ctx context.Context, tenantID uuid.UUID) ([]*domain.MonitorRule, error) {
	return s.repo.List(ctx, tenantID)
}

// ListActiveTenantIDs 返回存在启用规则的租户 ID 列表(用于重启对账)。
func (s *MonitorService) ListActiveTenantIDs(ctx context.Context) ([]uuid.UUID, error) {
	return s.repo.ListActiveTenants(ctx)
}

// Create creates a new monitor rule.
// accountID 为 nil 表示规则属于全部账号（老数据兼容）；非 nil 则仅该账号的监听应用（隔离）。
func (s *MonitorService) Create(ctx context.Context, tenantID uuid.UUID, name, ruleType string, sourceChatIDs, targetChatIDs, keywords []string, matchType, replyContent string, accountID *uuid.UUID) (*domain.MonitorRule, error) {
	mr := &domain.MonitorRule{
		RuleID:        uuid.New(),
		TenantID:      tenantID,
		AccountID:     accountID,
		Name:          name,
		Type:          ruleType,
		SourceChatIDs: sourceChatIDs,
		TargetChatIDs: targetChatIDs,
		MatchType:     matchType,
		Keywords:      keywords,
		ReplyContent:  replyContent,
		IsActive:      true,
	}
	if err := s.repo.Create(ctx, mr); err != nil {
		return nil, fmt.Errorf("create monitor rule: %w", err)
	}
	if err := s.engine.ReloadRules(ctx, tenantID); err != nil {
		log.Printf("[monitor] 重载规则失败: %v", err)
	}
	return mr, nil
}

// Update modifies a monitor rule.
func (s *MonitorService) Update(ctx context.Context, mr *domain.MonitorRule) error {
	if err := s.repo.Update(ctx, mr); err != nil {
		return fmt.Errorf("update monitor rule: %w", err)
	}
	if err := s.engine.ReloadRules(ctx, mr.TenantID); err != nil {
		log.Printf("[monitor] 重载规则失败: %v", err)
	}
	return nil
}

// GetByID returns a monitor rule by ID.
func (s *MonitorService) GetByID(ctx context.Context, tenantID, ruleID uuid.UUID) (*domain.MonitorRule, error) {
	return s.repo.GetByID(ctx, tenantID, ruleID)
}

// Toggle activates/deactivates a monitor rule.
func (s *MonitorService) Toggle(ctx context.Context, tenantID, ruleID uuid.UUID, isActive bool) error {
	mr, err := s.repo.GetByID(ctx, tenantID, ruleID)
	if err != nil {
		return err
	}
	mr.IsActive = isActive
	if err := s.repo.Update(ctx, mr); err != nil {
		return fmt.Errorf("toggle monitor rule: %w", err)
	}
	return s.engine.ReloadRules(ctx, tenantID)
}

// Delete removes a monitor rule.
func (s *MonitorService) Delete(ctx context.Context, tenantID, ruleID uuid.UUID) error {
	if err := s.repo.Delete(ctx, tenantID, ruleID); err != nil {
		return fmt.Errorf("delete monitor rule: %w", err)
	}
	if err := s.engine.ReloadRules(ctx, tenantID); err != nil {
		log.Printf("[monitor] 重载规则失败: %v", err)
	}
	return nil
}
