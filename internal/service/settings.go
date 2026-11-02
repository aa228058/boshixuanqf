package service

import (
	"context"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/google/uuid"
	"tg-sender/internal/domain"
	"tg-sender/internal/middleware"
	"tg-sender/internal/repository"
)

// SettingsService handles tenant settings, activation, templates, groups, and exports.
type SettingsService struct {
	tenantRepo   *repository.TenantRepository
	planRepo     *repository.PlanRepository
	codeRepo     *repository.ActivationCodeRepository
	supportRepo  *repository.SupportConfigRepository
	announceRepo *repository.AnnouncementRepository
	templateRepo *repository.MessageTemplateRepository
	groupRepo    *repository.AccountGroupRepository
	memberRepo   *repository.MemberRepository
}

// NewSettingsService creates a new settings service.
func NewSettingsService(
	tr *repository.TenantRepository,
	pr *repository.PlanRepository,
	cr *repository.ActivationCodeRepository,
	sr *repository.SupportConfigRepository,
	ar *repository.AnnouncementRepository,
	tmr *repository.MessageTemplateRepository,
	agr *repository.AccountGroupRepository,
	mr *repository.MemberRepository,
) *SettingsService {
	return &SettingsService{
		tenantRepo:   tr,
		planRepo:     pr,
		codeRepo:     cr,
		supportRepo:  sr,
		announceRepo: ar,
		templateRepo: tmr,
		groupRepo:    agr,
		memberRepo:   mr,
	}
}

// GetProfile returns tenant profile with plan info.
// UpdateNotify R119+R120: 保存租户通知设置(邮箱+订阅+SMTP 发件配置)
func (s *SettingsService) UpdateNotify(ctx context.Context, tenantID uuid.UUID, email string, settings, smtp json.RawMessage) error {
	return s.tenantRepo.UpdateNotify(ctx, tenantID, email, settings, smtp)
}

func (s *SettingsService) GetProfile(ctx context.Context, tenantID uuid.UUID) (*domain.Tenant, error) {
	return s.tenantRepo.GetByID(ctx, tenantID)
}

// GetPlans returns all available plans.
func (s *SettingsService) GetPlans(ctx context.Context) ([]domain.Plan, error) {
	return s.planRepo.List(ctx)
}

// ActivateCode redeems an activation code for a tenant.
func (s *SettingsService) ActivateCode(ctx context.Context, tenantID uuid.UUID, code string) (*domain.Tenant, error) {
	ac, err := s.codeRepo.GetByCode(ctx, code)
	if err != nil {
		return nil, fmt.Errorf("invalid code")
	}
	if ac.Status != "unused" {
		return nil, fmt.Errorf("code already used or expired")
	}
	if ac.ExpiresAt != nil && ac.ExpiresAt.Before(time.Now()) {
		return nil, fmt.Errorf("code expired")
	}

	plan, err := s.planRepo.GetByID(ctx, ac.PlanID)
	if err != nil {
		return nil, fmt.Errorf("plan not found")
	}

	updates := map[string]any{
		"plan_id":              ac.PlanID,
		"max_accounts":         plan.MaxAccounts,
		"max_concurrent_tasks": plan.MaxConcurrent,
		"status":               "active",
	}
	if plan.PriceMonthly > 0 {
		expires := time.Now().Add(30 * 24 * time.Hour)
		updates["expires_at"] = expires
	} else {
		// 免费套餐不过期: 清除旧过期时间, 避免老租户(此前 expires_at 已过期的)
		// 激活免费码后仍被 middleware 的过期判断踢出
		updates["expires_at"] = nil
	}

	// Atomically claim the code FIRST. The UPDATE ... WHERE status='unused'
	// makes this race-safe: if another request wins the race, ok=false here
	// and the tenant is never upgraded. Updating the tenant before claiming
	// would grant the plan even when the code was already consumed.
	ok, err := s.codeRepo.MarkUsed(ctx, ac.CodeID, tenantID)
	if err != nil {
		return nil, fmt.Errorf("mark code used: %w", err)
	}
	if !ok {
		return nil, fmt.Errorf("code already used or expired")
	}
	if err := s.tenantRepo.Update(ctx, tenantID, updates); err != nil {
		// P1-13 审计修复: MarkUsed(烧卡)在 Update 之前——Update 失败时卡密已烧但
		// 租户没升级。补偿: 回滚已烧的卡密, 用户可以重试兑换。
		// 修正(2026-10-23 构建实证): code 是卡密字符串, CodeID 在 ac 结构体上。
		if _, unmarkErr := s.codeRepo.UnmarkUsed(ctx, ac.CodeID, tenantID); unmarkErr != nil {
			log.Printf("[activate] 卡密补偿失败 code=%s tenant=%s: %v", code, tenantID, unmarkErr)
		}
		return nil, fmt.Errorf("update tenant: %w", err)
	}
	// P1-4 审计修复: 兑换成功后立即失效租户缓存——原缓存 60 秒 TTL 内仍按旧套餐 403。
	middleware.InvalidateTenantCache(tenantID)
	return s.tenantRepo.GetByID(ctx, tenantID)
}

// GetSupport returns support config.
func (s *SettingsService) GetSupport(ctx context.Context) (*domain.SupportConfig, error) {
	return s.supportRepo.Get(ctx)
}

// GetAnnouncements returns active announcements for tenant.
func (s *SettingsService) GetAnnouncements(ctx context.Context, planID string) ([]domain.Announcement, error) {
	return s.announceRepo.List(ctx, planID)
}

// === Message Templates ===

func (s *SettingsService) ListTemplates(ctx context.Context, tenantID uuid.UUID) ([]*domain.MessageTemplate, error) {
	return s.templateRepo.List(ctx, tenantID)
}

func (s *SettingsService) CreateTemplate(ctx context.Context, tenantID uuid.UUID, name, content string) (*domain.MessageTemplate, error) {
	vars := extractVariables(content)
	// R87-M3 修复: 内容无 {变量} 时 extractVariables 返回 nil → pgx 存 NULL
	// 而 message_templates.variables 是 NOT NULL → 创建模板 500。
	// nil 归一化为空数组, 存 PostgreSQL 空数组 '{}'。
	if vars == nil {
		vars = []string{}
	}
	t := &domain.MessageTemplate{
		TemplateID: uuid.New(),
		TenantID:   tenantID,
		Name:       name,
		Content:    content,
		Variables:  vars,
		CreatedAt:  time.Now(),
	}
	if err := s.templateRepo.Create(ctx, t); err != nil {
		return nil, err
	}
	return t, nil
}

func (s *SettingsService) UpdateTemplate(ctx context.Context, tenantID, templateID uuid.UUID, name, content string) error {
	vars := extractVariables(content)
	return s.templateRepo.Update(ctx, tenantID, templateID, name, content, vars)
}

func (s *SettingsService) DeleteTemplate(ctx context.Context, tenantID, templateID uuid.UUID) error {
	return s.templateRepo.Delete(ctx, tenantID, templateID)
}

func extractVariables(content string) []string {
	seen := make(map[string]bool)
	var vars []string
	// Simple regex-like extraction for {{var_name}}
	for {
		start := strings.Index(content, "{{")
		if start == -1 {
			break
		}
		end := strings.Index(content[start:], "}}")
		if end == -1 {
			break
		}
		v := strings.TrimSpace(content[start+2 : start+end])
		if v != "" && !seen[v] {
			seen[v] = true
			vars = append(vars, v)
		}
		content = content[start+end+2:]
	}
	return vars
}

// === Account Groups ===

func (s *SettingsService) ListAccountGroups(ctx context.Context, tenantID uuid.UUID) ([]*domain.AccountGroup, error) {
	return s.groupRepo.List(ctx, tenantID)
}

func (s *SettingsService) CreateAccountGroup(ctx context.Context, tenantID uuid.UUID, name, color string, accountIDs []uuid.UUID) (*domain.AccountGroup, error) {
	g := &domain.AccountGroup{
		GroupID:    uuid.New(),
		TenantID:   tenantID,
		Name:       name,
		Color:      color,
		AccountIDs: accountIDs,
		CreatedAt:  time.Now(),
	}
	if err := s.groupRepo.Create(ctx, g); err != nil {
		return nil, err
	}
	return g, nil
}

func (s *SettingsService) UpdateAccountGroup(ctx context.Context, tenantID, groupID uuid.UUID, name, color string, accountIDs []uuid.UUID) error {
	return s.groupRepo.Update(ctx, tenantID, groupID, name, color, accountIDs)
}

func (s *SettingsService) DeleteAccountGroup(ctx context.Context, tenantID, groupID uuid.UUID) error {
	return s.groupRepo.Delete(ctx, tenantID, groupID)
}

// === Export ===

func (s *SettingsService) ExportMembersCSV(ctx context.Context, tenantID uuid.UUID, groupId *uuid.UUID) ([]byte, error) {
	var members []*domain.Member
	var err error
	if groupId != nil {
		members, err = s.memberRepo.ListByGroup(ctx, tenantID, *groupId, 10000, 0)
	} else {
		members, err = s.memberRepo.ListByTenant(ctx, tenantID, 10000)
	}
	if err != nil {
		return nil, err
	}

	var buf strings.Builder
	w := csv.NewWriter(&buf)
	_ = w.Write([]string{"username", "first_name", "last_name", "phone", "bio", "last_seen", "group_id"})
	for _, m := range members {
		var lastSeenStr string
		if m.LastSeen != nil {
			lastSeenStr = m.LastSeen.Format(time.RFC3339)
		}
		_ = w.Write([]string{
			m.Username,
			m.FirstName,
			m.LastName,
			m.Phone,
			m.Bio,
			lastSeenStr,
			m.GroupID.String(),
		})
	}
	w.Flush()
	return []byte(buf.String()), nil
}

func (s *SettingsService) ExportMembersJSON(ctx context.Context, tenantID uuid.UUID, groupId *uuid.UUID) ([]byte, error) {
	var members []*domain.Member
	var err error
	if groupId != nil {
		members, err = s.memberRepo.ListByGroup(ctx, tenantID, *groupId, 10000, 0)
	} else {
		members, err = s.memberRepo.ListByTenant(ctx, tenantID, 10000)
	}
	if err != nil {
		return nil, err
	}
	return json.MarshalIndent(members, "", "  ")
}
