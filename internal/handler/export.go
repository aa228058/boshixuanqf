package handler

import (
	"context"
	"log"
	"net/http"
	"time"

	"github.com/labstack/echo/v4"

	"tg-sender/internal/domain"
	"tg-sender/internal/middleware"
	"tg-sender/internal/repository"
	"tg-sender/internal/safego"
)

// ExportHandler 租户自助数据导出（G-05 合规：GDPR 式数据可携带权）。
// 聚合租户自有数据为 JSON 下载，明确排除会话/密钥等凭证类数据。
type ExportHandler struct {
	tenantRepo *repository.TenantRepository
	memberRepo *repository.MemberRepository
	groupRepo  *repository.GroupRepository
	taskRepo   *repository.TaskRepository
	auditRepo  *repository.AuditRepository
}

func NewExportHandler(
	tenantRepo *repository.TenantRepository,
	memberRepo *repository.MemberRepository,
	groupRepo *repository.GroupRepository,
	taskRepo *repository.TaskRepository,
	auditRepo *repository.AuditRepository,
) *ExportHandler {
	return &ExportHandler{
		tenantRepo: tenantRepo,
		memberRepo: memberRepo,
		groupRepo:  groupRepo,
		taskRepo:   taskRepo,
		auditRepo:  auditRepo,
	}
}

// ExportMyData 导出当前租户的全部自有数据（不含凭证）。
// GET /api/v1/tenant/export
func (h *ExportHandler) ExportMyData(c echo.Context) error {
	tenantID, err := middleware.GetTenantID(c)
	if err != nil {
		return err
	}
	ctx := c.Request().Context()

	tenant, err := h.tenantRepo.GetByID(ctx, tenantID)
	if err != nil {
		return echo.NewHTTPError(http.StatusNotFound, "tenant not found")
	}
	// 导出件不含凭证：profile 仅保留基本信息字段
	profile := map[string]any{
		"tenant_id":  tenant.TenantID,
		"username":   tenant.Username,
		"name":       tenant.Name,
		"status":     tenant.Status,
		"plan_id":    tenant.PlanID,
		"created_at": tenant.CreatedAt,
	}

	members, err := h.memberRepo.ListByTenant(ctx, tenantID, 10000)
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "export members failed")
	}
	groups, err := h.groupRepo.List(ctx, tenantID, 10000, 0)
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "export groups failed")
	}
	tasks, err := h.taskRepo.List(ctx, tenantID, nil, nil, nil, false, nil, 5000, 0)
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "export tasks failed")
	}

	// P1-48 审计修复: 三处硬编码上限(members/groups 10000、tasks 5000)原样超过后
	// 静默截断, 导出声称全量却缺数据。打满上限时标记 truncated——可能误报一行
	// (恰好整页), 绝不漏报。members repo 无 offset 参数, 无法分页拉全, 标记是
	// 当前架构下的诚实方案; 需要真全量时再给 repo 加分页。
	truncated := map[string]bool{
		"members": len(members) >= 10000,
		"groups":  len(groups) >= 10000,
		"tasks":   len(tasks) >= 5000,
	}

	export := map[string]any{
		"exported_at": time.Now().UTC(),
		"profile":     profile,
		"members":     members,
		"groups":      groups,
		"tasks":       tasks,
		"truncated":   truncated,
		"notice":      "credentials (sessions, api keys, passwords) are never included in exports",
	}

	// 合规要求：导出行为本身必须留痕
	ip := c.RealIP()
	safego.Go(func() {
		if err := h.auditRepo.Create(context.Background(), &domain.AuditLog{
			TenantID: tenantID, UserID: "tenant", Action: "tenant_data_export",
			Target: "full", IP: ip,
		}); err != nil {
			log.Printf("[audit] export audit write failed: %v", err)
		}
	})

	c.Response().Header().Set(echo.HeaderContentDisposition, `attachment; filename="my-data-export.json"`)
	return c.JSON(http.StatusOK, export)
}

