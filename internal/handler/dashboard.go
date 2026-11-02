package handler

import (
	"github.com/google/uuid"
	"log"
	"net/http"
	"strconv"
	"strings"

	"github.com/labstack/echo/v4"
	"tg-sender/internal/middleware"
	"tg-sender/internal/service"
)

// DashboardHandler handles dashboard APIs.
type DashboardHandler struct {
	svc *service.DashboardService
}

// NewDashboardHandler creates a new dashboard handler.
func NewDashboardHandler(svc *service.DashboardService) *DashboardHandler {
	return &DashboardHandler{svc: svc}
}

// Stats godoc
// @Summary 仪表盘统计
// @Description 获取仪表盘统计数据(账号、任务、风险数量)
// @Tags 仪表盘
// @Accept json
// @Produce json
// @Success 200 {object} map[string]interface{}
// @Failure 500 {object} ErrorResponse
// @Security BearerAuth
// @Router /dashboard/stats [get]
// Stats returns dashboard statistics (aggregated, no full arrays).
func (h *DashboardHandler) Stats(c echo.Context) error {
	tenantID, err := middleware.GetTenantID(c)
	if err != nil {
		return err
	}

	ctx := c.Request().Context()

	// account_id 传入时仪表盘只统计该账号的数据(跟随侧边栏账号切换)
	if aid := c.QueryParam("account_id"); aid != "" {
		if accountID, perr := uuid.Parse(aid); perr == nil {
			stats, err := h.svc.StatsForAccount(ctx, tenantID, accountID)
			if err != nil {
				log.Printf("[dashboard] 账号统计失败 account=%s: %v", accountID, err)
				return c.JSON(http.StatusInternalServerError, map[string]string{"error": err.Error()})
			}
			return c.JSON(http.StatusOK, stats)
		}
	}

	// Use COUNT queries instead of loading all data
	stats, err := h.svc.Stats(ctx, tenantID)
	if err != nil {
		log.Printf("[handler] 获取统计失败: %v", err)
		return echo.NewHTTPError(http.StatusInternalServerError, "internal server error")
	}

	return Success(c, stats)
}

// SendCalendar 发送日历: GET /dashboard/send-calendar?months=6
// 返回 { "2026-10-20": 123, ... } 日期→当日成功发送量
func (h *DashboardHandler) SendCalendar(c echo.Context) error {
	tenantID, err := middleware.GetTenantID(c)
	if err != nil {
		return err
	}
	months, _ := strconv.Atoi(c.QueryParam("months"))
	data, err := h.svc.SendCalendar(c.Request().Context(), tenantID, months)
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "统计失败")
	}
	return c.JSON(http.StatusOK, data)
}

// RiskEvents godoc
// @Summary 风险事件列表
// @Description 获取租户的风险事件列表
// @Tags 仪表盘
// @Accept json
// @Produce json
// @Success 200 {array} domain.RiskEvent
// @Failure 500 {object} ErrorResponse
// @Security BearerAuth
// @Router /dashboard/risk-events [get]
// RiskEvents returns risk events for the tenant with optional time range filter.
func (h *DashboardHandler) RiskEvents(c echo.Context) error {
	tenantID, err := middleware.GetTenantID(c)
	if err != nil {
		return err
	}

	startTime := c.QueryParam("start_time")
	endTime := c.QueryParam("end_time")
	limit := 50
	if l := c.QueryParam("limit"); l != "" {
		if parsed, err := strconv.Atoi(l); err == nil && parsed > 0 && parsed <= 200 {
			limit = parsed
		}
	}
	offset := 0
	if o := c.QueryParam("offset"); o != "" {
		// V33 审计修复: offset 上限 clamp, 防 OFFSET 超大扫全表
		if parsed, err := strconv.Atoi(o); err == nil && parsed >= 0 {
			if parsed > 100000 {
				parsed = 100000
			}
			offset = parsed
		}
	}

	risks, err := h.svc.RiskEvents(c.Request().Context(), tenantID, limit, offset, startTime, endTime)
	if err != nil {
		// V20/P1-42: 时间参数非法由 service 层校验拒绝, 映射 400 而非 500
		if strings.Contains(err.Error(), "invalid time format") {
			return echo.NewHTTPError(http.StatusBadRequest, err.Error())
		}
		log.Printf("[handler] 获取风险列表失败: %v", err)
		return echo.NewHTTPError(http.StatusInternalServerError, "internal server error")
	}
	return SuccessList(c, risks, len(risks), 1, len(risks))
}

// AuditLogs godoc
// @Summary 审计日志
// @Description 获取租户的审计日志列表
// @Tags 仪表盘
// @Accept json
// @Produce json
// @Success 200 {array} domain.AuditLog
// @Failure 500 {object} ErrorResponse
// @Security BearerAuth
// @Router /dashboard/activities [get]
// AuditLogs returns audit logs for the tenant with optional time range filter.
func (h *DashboardHandler) AuditLogs(c echo.Context) error {
	tenantID, err := middleware.GetTenantID(c)
	if err != nil {
		return err
	}

	startTime := c.QueryParam("start_time")
	endTime := c.QueryParam("end_time")
	limit := 50
	if l := c.QueryParam("limit"); l != "" {
		if parsed, err := strconv.Atoi(l); err == nil && parsed > 0 && parsed <= 200 {
			limit = parsed
		}
	}
	offset := 0
	if o := c.QueryParam("offset"); o != "" {
		// V33/V34 审计修复: 原来只校验非负——传 999999999 会 OFFSET 超大扫全表拖垮库。
		// 加上限 clamp。
		if parsed, err := strconv.Atoi(o); err == nil && parsed >= 0 {
			if parsed > 100000 {
				parsed = 100000
			}
			offset = parsed
		}
	}

	logs, err := h.svc.AuditLogs(c.Request().Context(), tenantID, limit, offset, startTime, endTime)
	if err != nil {
		// V20/P1-42: 时间参数非法由 service 层校验拒绝, 映射 400 而非 500
		if strings.Contains(err.Error(), "invalid time format") {
			return echo.NewHTTPError(http.StatusBadRequest, err.Error())
		}
		log.Printf("[handler] 获取审计日志失败: %v", err)
		return echo.NewHTTPError(http.StatusInternalServerError, "internal server error")
	}
	return SuccessList(c, logs, len(logs), 1, len(logs))
}
