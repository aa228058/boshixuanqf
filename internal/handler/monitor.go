package handler

import (
	"context"
	"errors"
	"log"
	"net/http"
	"strconv"
	"tg-sender/internal/safego"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/labstack/echo/v4"
	"tg-sender/internal/domain"
	"tg-sender/internal/domain/monitor"
	"tg-sender/internal/middleware"
	"tg-sender/internal/service"
)

// MonitorHandler handles monitor rule APIs.
type MonitorHandler struct {
	svc        *service.MonitorService
	engine     *monitor.Engine
	accountSvc *service.AccountService
}

// NewMonitorHandler creates a new monitor handler.
func NewMonitorHandler(svc *service.MonitorService, engine *monitor.Engine, accountSvc *service.AccountService) *MonitorHandler {
	return &MonitorHandler{svc: svc, engine: engine, accountSvc: accountSvc}
}

// List godoc
// @Summary 获取监控规则列表
// @Description 获取租户下的所有监控规则
// @Tags 监控
// @Accept json
// @Produce json
// @Success 200 {array} domain.MonitorRule
// @Failure 500 {object} ErrorResponse
// @Security BearerAuth
// @Router /monitor-rules [get]
// List returns monitor rules for the tenant.
func (h *MonitorHandler) List(c echo.Context) error {
	tenantID, err := middleware.GetTenantID(c)
	if err != nil {
		return err
	}

	rules, err := h.svc.List(c.Request().Context(), tenantID)
	if err != nil {
		log.Printf("[handler] 获取监控规则失败: %v", err)
		return echo.NewHTTPError(http.StatusInternalServerError, "internal server error")
	}
	total := len(rules)
	return SuccessList(c, rules, total, 1, total)
}

// CreateRequest represents create monitor rule request.
type CreateMonitorRequest struct {
	Name          string   `json:"name" validate:"required"`
	Type          string   `json:"type" validate:"required,oneof=forward reply"`
	SourceChatIDs []string `json:"source_chat_ids" validate:"required"`
	TargetChatIDs []string `json:"target_chat_ids"`
	MatchType     string   `json:"match_type" validate:"required,oneof=contains regex exact from_user"`
	Keywords      []string `json:"keywords" validate:"required"`
	// 自动回复文本(type=reply 时使用), 为空则引擎用兜底文案
	ReplyContent string `json:"reply_content"`
	// 规则归属账号（2026-09-22 隔离改造）：空串/缺省 = 全部账号；否则只有该账号的监听应用此规则
	AccountID string `json:"account_id"`
}

// Create godoc
// @Summary 创建监控规则
// @Description 创建新的消息监控规则
// @Tags 监控
// @Accept json
// @Produce json
// @Param body body CreateMonitorRequest true "创建监控规则请求"
// @Success 201 {object} domain.MonitorRule
// @Failure 400 {object} ErrorResponse
// @Failure 500 {object} ErrorResponse
// @Security BearerAuth
// @Router /monitor-rules [post]
// Create creates a new monitor rule.
func (h *MonitorHandler) Create(c echo.Context) error {
	tenantID, err := middleware.GetTenantID(c)
	if err != nil {
		return err
	}

	var req CreateMonitorRequest
	if err := c.Bind(&req); err != nil {
		log.Printf("[handler] 解析创建监控规则请求失败: %v", err)
		return echo.NewHTTPError(http.StatusBadRequest, "请求参数错误")
	}

	// Validate keywords based on match_type
	if req.MatchType != "from_user" && len(req.Keywords) == 0 {
		return echo.NewHTTPError(http.StatusBadRequest, "关键词不能为空，匹配类型: "+req.MatchType)
	}
	if req.MatchType == "from_user" {
		for _, kw := range req.Keywords {
			if kw == "" {
				return echo.NewHTTPError(http.StatusBadRequest, "关键词不能包含空字符串")
			}
		}
	}
	if req.Type == "forward" && len(req.TargetChatIDs) == 0 {
		return echo.NewHTTPError(http.StatusBadRequest, "转发规则必须指定目标群组")
	}

	// 解析规则归属账号：空 = 全部账号（老行为）；非空必须解析成合法 UUID（2026-09-22 隔离）
	var ruleAccountID *uuid.UUID
	if req.AccountID != "" {
		parsed, perr := uuid.Parse(req.AccountID)
		if perr != nil {
			return echo.NewHTTPError(http.StatusBadRequest, "account_id 格式错误")
		}
		ruleAccountID = &parsed
	}

	ctx := c.Request().Context()
	rule, err := h.svc.Create(ctx, tenantID, req.Name, req.Type, req.SourceChatIDs, req.TargetChatIDs, req.Keywords, req.MatchType, req.ReplyContent, ruleAccountID)
	if err != nil {
		log.Printf("[handler] 创建监控规则失败: %v", err)
		return echo.NewHTTPError(http.StatusBadRequest, "创建监控规则失败")
	}
	log.Printf("[handler] 监控规则已创建: id=%s is_active=%v", rule.RuleID, rule.IsActive)
	// 新建即启用时自动开监控(异步, 不阻塞响应)
	if rule.IsActive {
		safego.Go(func() { h.autoManageMonitoring(context.Background(), tenantID, true) })
	}
	return Created(c, rule)
}

// UpdateRequest represents update monitor rule request.
type UpdateMonitorRequest struct {
	Name          string   `json:"name"`
	Type          string   `json:"type" validate:"omitempty,oneof=forward reply"`
	SourceChatIDs []string `json:"source_chat_ids"`
	TargetChatIDs []string `json:"target_chat_ids"`
	MatchType     string   `json:"match_type" validate:"omitempty,oneof=contains regex exact from_user"`
	Keywords      []string `json:"keywords"`
	IsActive      *bool    `json:"is_active"`
	// 自动回复文本; 传非空则更新, 不传保持原值
	ReplyContent *string `json:"reply_content,omitempty"`
}

// GetByID godoc
// @Summary 获取监控规则详情
// @Description 获取指定监控规则详情
// @Tags 监控
// @Accept json
// @Produce json
// @Param id path string true "规则ID"
// @Success 200 {object} domain.MonitorRule
// @Failure 400 {object} ErrorResponse
// @Failure 404 {object} ErrorResponse
// @Failure 500 {object} ErrorResponse
// @Security BearerAuth
// @Router /monitor-rules/{id} [get]
// GetByID returns a single monitor rule.
func (h *MonitorHandler) GetByID(c echo.Context) error {
	tenantID, err := middleware.GetTenantID(c)
	if err != nil {
		return err
	}
	ruleID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "规则ID格式错误")
	}
	rule, err := h.svc.GetByID(c.Request().Context(), tenantID, ruleID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return echo.NewHTTPError(http.StatusNotFound, "规则不存在")
		}
		log.Printf("[handler] 获取监控规则详情失败: %v", err)
		return echo.NewHTTPError(http.StatusInternalServerError, "internal error")
	}
	return Success(c, rule)
}

// Update godoc
// @Summary 更新监控规则
// @Description 更新监控规则(仅更新提供的字段)
// @Tags 监控
// @Accept json
// @Produce json
// @Param id path string true "规则ID"
// @Param body body UpdateMonitorRequest true "更新监控规则请求"
// @Success 200 {object} domain.MonitorRule
// @Failure 400 {object} ErrorResponse
// @Failure 404 {object} ErrorResponse
// @Failure 500 {object} ErrorResponse
// @Security BearerAuth
// @Router /monitor-rules/{id} [put]
// Update modifies a monitor rule (only updates provided fields).
func (h *MonitorHandler) Update(c echo.Context) error {
	tenantID, err := middleware.GetTenantID(c)
	if err != nil {
		return err
	}

	ruleID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "规则ID格式错误")
	}

	var req UpdateMonitorRequest
	if err := c.Bind(&req); err != nil {
		log.Printf("[handler] 解析更新监控规则请求失败: %v", err)
		return echo.NewHTTPError(http.StatusBadRequest, "请求参数错误")
	}

	// Get existing rule
	existing, err := h.svc.GetByID(c.Request().Context(), tenantID, ruleID)
	if err != nil {
		return echo.NewHTTPError(http.StatusNotFound, "规则不存在")
	}

	// Only update provided fields
	if req.Name != "" {
		existing.Name = req.Name
	}
	if req.Type != "" {
		existing.Type = req.Type
	}
	if req.SourceChatIDs != nil {
		existing.SourceChatIDs = req.SourceChatIDs
	}
	if req.TargetChatIDs != nil {
		existing.TargetChatIDs = req.TargetChatIDs
	}
	if req.MatchType != "" {
		existing.MatchType = req.MatchType
	}
	if req.Keywords != nil {
		existing.Keywords = req.Keywords
	}
	if req.ReplyContent != nil && *req.ReplyContent != "" {
		existing.ReplyContent = *req.ReplyContent
	}
	if req.IsActive != nil {
		existing.IsActive = *req.IsActive
	}

	if err := h.svc.Update(c.Request().Context(), existing); err != nil {
		log.Printf("[handler] 更新监控规则失败: %v", err)
		return echo.NewHTTPError(http.StatusBadRequest, "更新监控规则失败")
	}
	return Success(c, existing)
}

// ToggleRequest represents toggle request.
// P1-30 审计修复: IsActive 改 *bool——原来 bool 零值无法区分 "{} 空对象"(应取反)
// 和 "{is_active:false}"(显式关闭), 空 {} 会误关规则。nil=未传→取反, 非 nil=显式值。
type ToggleRequest struct {
	IsActive *bool `json:"is_active"`
}

// Toggle godoc
// @Summary 切换监控规则状态
// @Description 激活或停用监控规则
// @Tags 监控
// @Accept json
// @Produce json
// @Param id path string true "规则ID"
// @Param body body ToggleRequest true "切换请求"
// @Success 200 {object} map[string]bool
// @Failure 400 {object} ErrorResponse
// @Failure 500 {object} ErrorResponse
// @Security BearerAuth
// @Router /monitor-rules/{id}/toggle [post]
// Toggle activates/deactivates a monitor rule.
func (h *MonitorHandler) Toggle(c echo.Context) error {
	tenantID, err := middleware.GetTenantID(c)
	if err != nil {
		return err
	}
	ruleID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "规则ID格式错误")
	}
	var req ToggleRequest
	ctx := c.Request().Context()
	bodyHasField := false
	if c.Request().ContentLength > 0 {
		if err := c.Bind(&req); err != nil {
			log.Printf("[handler] 解析切换监控规则请求失败: %v", err)
			return echo.NewHTTPError(http.StatusBadRequest, "请求参数错误")
		}
		bodyHasField = req.IsActive != nil // P1-30: 显式带了 is_active 字段
	}
	if !bodyHasField {
		// 空请求体或 {} 空对象(2026-10-22 修复: 原来 {} 时 bool 零值 false 直接误关):
		// 未显式传值 → 按当前状态取反
		rules, lerr := h.svc.List(ctx, tenantID)
		if lerr == nil {
			for _, r := range rules {
				if r.RuleID == ruleID {
					inv := !r.IsActive
					req.IsActive = &inv
					break
				}
			}
		}
	}
	if req.IsActive == nil {
		return echo.NewHTTPError(http.StatusBadRequest, "规则不存在")
	}
	log.Printf("[handler] 规则开关: id=%s -> is_active=%v", ruleID, *req.IsActive)
	if err := h.svc.Toggle(ctx, tenantID, ruleID, *req.IsActive); err != nil {
		log.Printf("[handler] 切换监控规则失败: %v", err)
		return echo.NewHTTPError(http.StatusBadRequest, "切换规则状态失败")
	}
	// 异步执行监控启停, 不阻塞HTTP响应(StartMonitoring连接TG客户端可能耗时数秒)
	// P1-30 后续修正(2026-10-23 构建实证): IsActive 改 *bool 后这两处漏了解引用
	safego.Go(func() { h.autoManageMonitoring(context.Background(), tenantID, *req.IsActive) })
	return Success(c, map[string]bool{"is_active": *req.IsActive})
}

// autoManageMonitoring 规则即开关: 启用规则时自动为在线账号开监控, 全部停用时自动停。
func (h *MonitorHandler) autoManageMonitoring(ctx context.Context, tenantID uuid.UUID, justEnabled bool) {
	rules, err := h.svc.List(ctx, tenantID)
	if err != nil {
		log.Printf("[monitor] 自动管理: 获取规则列表失败: %v", err)
		return
	}
	var active []domain.MonitorRule
	for _, r := range rules {
		if r.IsActive {
			active = append(active, *r)
		}
	}
	accounts, err := h.accountSvc.List(ctx, tenantID, "")
	if err != nil {
		log.Printf("[monitor] 自动管理: 获取账号列表失败: %v", err)
		return
	}
	log.Printf("[monitor] 自动管理: 启用规则=%d 账号数=%d", len(active), len(accounts))
	if len(active) == 0 {
		for _, acc := range accounts {
			if h.engine.IsMonitoring(tenantID, acc.AccountID) {
				h.engine.StopMonitoring(tenantID, acc.AccountID)
			}
		}
		return
	}
	if !justEnabled {
		return
	}
	for _, acc := range accounts {
		// 不再按 status=="online" 硬门槛跳过: 授权但未入池的账号也尝试启动,
		// 真正的连通性由 StartMonitoring 内部的 GetClient 报错兜底。
		monitoring := h.engine.IsMonitoring(tenantID, acc.AccountID)
		log.Printf("[monitor] 账号检查: id=%s status=%q monitoring=%v", acc.AccountID, acc.Status, monitoring)
		if !monitoring {
			if err := h.engine.StartMonitoring(ctx, tenantID, acc.AccountID, active); err != nil {
				log.Printf("[monitor] auto-start failed for account %s: %v", acc.AccountID, err)
			}
		}
	}
}

// BootReconcile 重启对账: 为有启用规则的租户自动恢复监控。
// 在服务启动后延迟调用(等账号池完成首批连接)。
func (h *MonitorHandler) BootReconcile(ctx context.Context) {
	tenantIDs, err := h.svc.ListActiveTenantIDs(ctx)
	if err != nil {
		log.Printf("[monitor] 重启对账: 获取租户列表失败: %v", err)
		return
	}
	log.Printf("[monitor] 重启对账: %d 个租户存在启用规则", len(tenantIDs))
	for _, tenantID := range tenantIDs {
		h.autoManageMonitoring(ctx, tenantID, true)
	}
}

// Delete godoc
// @Summary 删除监控规则
// @Description 删除指定监控规则
// @Tags 监控
// @Accept json
// @Produce json
// @Param id path string true "规则ID"
// @Success 204
// @Failure 400 {object} ErrorResponse
// @Failure 500 {object} ErrorResponse
// @Security BearerAuth
// @Router /monitor-rules/{id} [delete]
// Delete removes a monitor rule.
func (h *MonitorHandler) Delete(c echo.Context) error {
	tenantID, err := middleware.GetTenantID(c)
	if err != nil {
		return err
	}

	ruleID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "规则ID格式错误")
	}

	if err := h.svc.Delete(c.Request().Context(), tenantID, ruleID); err != nil {
		log.Printf("[handler] 删除监控规则失败: %v", err)
		return echo.NewHTTPError(http.StatusInternalServerError, "删除规则失败")
	}
	return NoContent(c)
}

// GetMessages godoc
// @Summary 获取监控消息
// @Description 获取监控到的消息列表(占位)
// @Tags 监控
// @Accept json
// @Produce json
// @Success 200 {array} map[string]interface{}
// @Security BearerAuth
// @Router /monitor-rules/messages [get]
// GetMessages returns monitored messages.
// NOTE: This feature stores messages in memory for the last 24 hours.
func (h *MonitorHandler) GetMessages(c echo.Context) error {
	_, err := middleware.GetTenantID(c)
	if err != nil {
		return err
	}

	limit, err := strconv.Atoi(c.QueryParam("limit"))
	if err != nil || limit < 1 {
		limit = 20
	}
	if limit <= 0 || limit > 100 {
		limit = 20
	}
	offset, err := strconv.Atoi(c.QueryParam("offset"))
	if err != nil || offset < 0 {
		offset = 0
	}

	// Return empty list for now - messages are forwarded in real-time via SSE
	// tenantID reserved for future filtering
	return Success(c, map[string]any{
		"messages":  []any{},
		"total":     0,
		"page":      offset/limit + 1,
		"page_size": limit,
	})
}

// StartMonitoring godoc
// @Summary 开始监控
// @Description 为指定账号启动消息监控
// @Tags 监控
// @Accept json
// @Produce json
// @Param account_id path string true "账号ID"
// @Success 200 {object} map[string]string
// @Failure 400 {object} ErrorResponse
// @Security BearerAuth
// @Router /monitor/start/{account_id} [post]
// StartMonitoring starts monitoring for an account.
func (h *MonitorHandler) StartMonitoring(c echo.Context) error {
	tenantID, err := middleware.GetTenantID(c)
	if err != nil {
		return err
	}
	accountID, err := uuid.Parse(c.Param("account_id"))
	if err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "invalid account id")
	}

	// Verify account belongs to tenant and is online
	acc, err := h.accountSvc.GetByID(c.Request().Context(), tenantID, accountID)
	if err != nil {
		return echo.NewHTTPError(http.StatusForbidden, "账号不存在或无权限")
	}
	// 待机(离线)号也可开监控：监听引擎拿连接时会现场建连。只排除真正不可用的。
	if acc.Status == domain.AccountStatusBanned || acc.Status == domain.AccountStatusRestricted || acc.Status == domain.AccountStatusPendingAuth {
		return echo.NewHTTPError(http.StatusBadRequest, "账号不可用，当前状态: "+string(acc.Status))
	}

	rules, err := h.svc.List(c.Request().Context(), tenantID)
	if err != nil {
		log.Printf("[handler] 获取监控规则失败: %v", err)
		return echo.NewHTTPError(http.StatusInternalServerError, "获取监控规则失败")
	}

	ruleValues := make([]domain.MonitorRule, len(rules))
	for i, r := range rules {
		ruleValues[i] = *r
	}

	if err := h.engine.StartMonitoring(c.Request().Context(), tenantID, accountID, ruleValues); err != nil {
		log.Printf("[handler] 启动监控失败: %v", err)
		return echo.NewHTTPError(http.StatusInternalServerError, "启动监控失败")
	}
	return Success(c, map[string]string{"status": "monitoring_started"})
}

// StopMonitoring godoc
// @Summary 停止监控
// @Description 为指定账号停止消息监控
// @Tags 监控
// @Accept json
// @Produce json
// @Param account_id path string true "账号ID"
// @Success 200 {object} map[string]string
// @Failure 400 {object} ErrorResponse
// @Security BearerAuth
// @Router /monitor/stop/{account_id} [post]
// StopMonitoring stops monitoring for an account.
func (h *MonitorHandler) StopMonitoring(c echo.Context) error {
	tenantID, err := middleware.GetTenantID(c)
	if err != nil {
		return err
	}
	accountID, err := uuid.Parse(c.Param("account_id"))
	if err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "invalid account id")
	}

	// Verify account belongs to tenant
	if _, err := h.accountSvc.GetByID(c.Request().Context(), tenantID, accountID); err != nil {
		return echo.NewHTTPError(http.StatusForbidden, "账号不存在或无权限")
	}

	h.engine.StopMonitoring(tenantID, accountID)
	return Success(c, map[string]string{"status": "monitoring_stopped"})
}
