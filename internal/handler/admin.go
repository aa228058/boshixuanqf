package handler

import (
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	"github.com/labstack/echo/v4"
	"tg-sender/internal/domain"
	"tg-sender/internal/middleware"
	"tg-sender/internal/service"
)

// Admin login rate limiter
var (
	adminLoginLimiter = make(map[string][]time.Time)
	adminLoginLimitMu sync.RWMutex
)

// adminJTIRe: admin JWT 的 jti 格式白名单(P2-73)
var adminJTIRe = regexp.MustCompile(`^[A-Za-z0-9._-]+$`)

const (
	adminLoginMaxAttempts = 10          // max 10 login attempts per window
	adminLoginWindow      = time.Hour   // per hour per IP
	adminLoginCooldown    = time.Second // min 1 second between attempts from same IP
)

// adminLoginCleanEvery: 每 N 次登录尝试才做一次全 IP 清理(P2-61 审计修复:
// 原来每次登录都遍历所有 IP 的尝试记录做清理——IP 数量一多, 每次登录 O(总IP数) 越来越慢)
const adminLoginCleanEvery = 100

var adminLoginCleanCount int

func checkAdminLoginRateLimit(ip string) error {
	adminLoginLimitMu.Lock()
	defer adminLoginLimitMu.Unlock()

	now := time.Now()
	cutoff := now.Add(-adminLoginWindow)

	// 全量清理降频: 每 100 次登录清理一次, 其余只清理当前 IP
	adminLoginCleanCount++
	fullClean := adminLoginCleanCount%adminLoginCleanEvery == 0
	if fullClean {
		for k, times := range adminLoginLimiter {
			var recent []time.Time
			for _, t := range times {
				if t.After(cutoff) {
					recent = append(recent, t)
				}
			}
			if len(recent) == 0 {
				delete(adminLoginLimiter, k)
			} else {
				adminLoginLimiter[k] = recent
			}
		}
	}

	// Check current IP
	recent := adminLoginLimiter[ip]

	// Check max attempts in window
	if len(recent) >= adminLoginMaxAttempts {
		return echo.NewHTTPError(http.StatusTooManyRequests, "too many login attempts, please try again later")
	}

	// Check cooldown between attempts
	if len(recent) > 0 {
		last := recent[len(recent)-1]
		if now.Sub(last) < adminLoginCooldown {
			return echo.NewHTTPError(http.StatusTooManyRequests, "please wait a moment before trying again")
		}
	}

	adminLoginLimiter[ip] = append(recent, now)
	return nil
}

// AdminHandler handles admin routes.
type AdminHandler struct {
	adminService *service.AdminService
}

// NewAdminHandler creates a new admin handler.
func NewAdminHandler(as *service.AdminService) *AdminHandler {
	return &AdminHandler{adminService: as}
}

// AdminLogin handles admin authentication.
func (h *AdminHandler) AdminLogin(c echo.Context) error {
	// Rate limit by IP
	clientIP := c.RealIP()
	if clientIP == "" {
		clientIP = c.Request().RemoteAddr
	}
	if err := checkAdminLoginRateLimit(clientIP); err != nil {
		return err
	}

	var req struct {
		Username string `json:"username" validate:"required"`
		Password string `json:"password" validate:"required"`
	}
	if err := c.Bind(&req); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "invalid request")
	}
	token, err := h.adminService.AdminLogin(c.Request().Context(), req.Username, req.Password)
	if err != nil {
		return echo.NewHTTPError(http.StatusUnauthorized, "invalid username or password")
	}
	return Success(c, map[string]string{"token": token})
}

// AdminLogout revokes the current admin token (JTI blacklist) so a leaked
// session can be killed immediately instead of waiting out the 24h expiry.
func (h *AdminHandler) AdminLogout(c echo.Context) error {
	auth := c.Request().Header.Get("Authorization")
	const prefix = "Bearer "
	if !strings.HasPrefix(auth, prefix) {
		return echo.NewHTTPError(http.StatusBadRequest, "missing bearer token")
	}
	tokenStr := strings.TrimPrefix(auth, prefix)

	// 解析（不验签也可以拿到 jti——伪造的 jti 进了黑名单也无害，撤销本身幂等）
	parser := jwt.NewParser()
	tk, _, err := parser.ParseUnverified(tokenStr, jwt.MapClaims{})
	if err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "unparseable token")
	}
	claims, ok := tk.Claims.(jwt.MapClaims)
	if !ok {
		return echo.NewHTTPError(http.StatusBadRequest, "invalid claims")
	}
	jti, _ := claims["jti"].(string)
	// P2-73 审计修复: 原来只查非空——任意伪造 jti 进黑名单表(虽 TTL 到期清, 过期前
	// 是垃圾)。格式白名单: 字母数字横线点 8~64 位, 不合规直接拒绝。
	if len(jti) < 8 || len(jti) > 64 || !adminJTIRe.MatchString(jti) {
		return echo.NewHTTPError(http.StatusBadRequest, "invalid jti")
	}
	if jti == "" {
		return Success(c, map[string]bool{"revoked": false}) // 旧版无 JTI 的 token，无法撤销
	}
	exp := time.Now().Add(8 * time.Hour)
	if expF, ok := claims["exp"].(float64); ok {
		exp = time.Unix(int64(expF), 0)
	}
	middleware.RevokeAdminToken(jti, exp)
	return Success(c, map[string]bool{"revoked": true})
}

// GetStats returns platform statistics.
func (h *AdminHandler) GetStats(c echo.Context) error {
	stats, err := h.adminService.GetStats(c.Request().Context())
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, err.Error())
	}
	return Success(c, stats)
}

// ListTenants returns paginated tenant list.
func (h *AdminHandler) ListTenants(c echo.Context) error {
	status := c.QueryParam("status")
	planID := c.QueryParam("plan_id")
	page, err := strconv.Atoi(c.QueryParam("page"))
	if err != nil || page < 1 {
		page = 1
	}
	if page < 1 {
		page = 1
	}
	pageSize, err := strconv.Atoi(c.QueryParam("page_size"))
	if err != nil || pageSize < 1 {
		pageSize = 20
	}
	if pageSize < 1 {
		pageSize = 20
	}
	tenants, total, err := h.adminService.ListTenants(c.Request().Context(), page, pageSize, status, planID)
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, err.Error())
	}
	return SuccessList(c, tenants, total, page, pageSize)
}

// GetTenant returns a single tenant.
func (h *AdminHandler) GetTenant(c echo.Context) error {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "invalid tenant id")
	}
	tenant, err := h.adminService.GetTenant(c.Request().Context(), id)
	if err != nil {
		return echo.NewHTTPError(http.StatusNotFound, err.Error())
	}
	return Success(c, tenant)
}

// CreateTenant creates a new tenant.
func (h *AdminHandler) CreateTenant(c echo.Context) error {
	var req struct {
		Name     string `json:"name" validate:"required"`
		Username string `json:"username" validate:"required"`
		Password string `json:"password" validate:"required,min=6"`
		PlanID   string `json:"plan_id" validate:"required"`
	}
	if err := c.Bind(&req); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, err.Error())
	}
	tenant, apiKey, err := h.adminService.CreateTenant(c.Request().Context(), req.Name, req.Username, req.Password, req.PlanID)
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, err.Error())
	}
	return Created(c, map[string]any{"tenant": tenant, "api_key": apiKey})
}

// UpdateTenant updates tenant fields.
func (h *AdminHandler) UpdateTenant(c echo.Context) error {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "invalid tenant id")
	}
	var req struct {
		PlanID        string  `json:"plan_id"`
		Status        string  `json:"status"`
		MaxAccounts   *int    `json:"max_accounts"`
		MaxConcurrent *int    `json:"max_concurrent"`
		ExpiresAt     *string `json:"expires_at"`
	}
	if err := c.Bind(&req); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, err.Error())
	}
	if err := h.adminService.UpdateTenant(c.Request().Context(), id, req.PlanID, req.Status, req.MaxAccounts, req.MaxConcurrent, req.ExpiresAt); err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, err.Error())
	}
	return Success(c, map[string]string{"message": "updated"})
}

// ResetAPIKey resets a tenant's API key.
func (h *AdminHandler) ResetAPIKey(c echo.Context) error {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "invalid tenant id")
	}
	newKey, err := h.adminService.ResetAPIKey(c.Request().Context(), id)
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, err.Error())
	}
	return Success(c, map[string]string{"api_key": newKey})
}

// DeleteTenant hard-deletes a tenant.
func (h *AdminHandler) DeleteTenant(c echo.Context) error {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "invalid tenant id")
	}
	if err := h.adminService.DeleteTenant(c.Request().Context(), id); err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, err.Error())
	}
	return Success(c, map[string]string{"message": "deleted"})
}

// GenerateCodes generates activation codes.
func (h *AdminHandler) GenerateCodes(c echo.Context) error {
	adminID := c.Get("admin_id").(uuid.UUID)
	var req struct {
		PlanID      string `json:"plan_id" validate:"required"`
		Count       int    `json:"count" validate:"required,min=1,max=100"`
		TimingType  string `json:"timing_type" validate:"required,oneof=immediate on_use"`
		ExpiresDays *int   `json:"expires_days"`
		Notes       string `json:"notes"`
	}
	if err := c.Bind(&req); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, err.Error())
	}
	codes, err := h.adminService.GenerateCodes(c.Request().Context(), adminID, req.PlanID, req.Count, req.TimingType, req.ExpiresDays, req.Notes)
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, err.Error())
	}
	return Created(c, codes)
}

// ListCodes returns activation codes.
func (h *AdminHandler) ListCodes(c echo.Context) error {
	status := c.QueryParam("status")
	planID := c.QueryParam("plan_id")
	page, err := strconv.Atoi(c.QueryParam("page"))
	if err != nil || page < 1 {
		page = 1
	}
	if page < 1 {
		page = 1
	}
	pageSize, err := strconv.Atoi(c.QueryParam("page_size"))
	if err != nil || pageSize < 1 {
		pageSize = 20
	}
	if pageSize < 1 {
		pageSize = 20
	}
	codes, total, err := h.adminService.ListCodes(c.Request().Context(), status, planID, page, pageSize)
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, err.Error())
	}
	return SuccessList(c, codes, total, page, pageSize)
}

// RevokeCode revokes an activation code.
func (h *AdminHandler) RevokeCode(c echo.Context) error {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "invalid code id")
	}
	if err := h.adminService.RevokeCode(c.Request().Context(), id); err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, err.Error())
	}
	return Success(c, map[string]string{"message": "revoked"})
}

// ExportCodes exports all unused activation codes.
func (h *AdminHandler) ExportCodes(c echo.Context) error {
	codes, err := h.adminService.ExportCodes(c.Request().Context())
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, err.Error())
	}
	var lines []string
	for _, code := range codes {
		lines = append(lines, code.Code)
	}
	c.Response().Header().Set("Content-Type", "text/plain; charset=utf-8")
	c.Response().Header().Set("Content-Disposition", "attachment; filename=codes.txt")
	return c.String(http.StatusOK, strings.Join(lines, "\n"))
}

// GetCode returns a single activation code.
func (h *AdminHandler) GetCode(c echo.Context) error {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "invalid code id")
	}
	code, err := h.adminService.GetCode(c.Request().Context(), id)
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, err.Error())
	}
	return Success(c, code)
}

// DeleteCode hard-deletes an activation code.
// PurgeExpiredTenants 物理清除软删除超过保留期的租户（super）。
func (h *AdminHandler) PurgeExpiredTenants(c echo.Context) error {
	var req struct {
		RetentionDays int `json:"retention_days"`
	}
	_ = c.Bind(&req)
	purged, err := h.adminService.PurgeExpiredTenants(c.Request().Context(), req.RetentionDays)
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, err.Error())
	}
	return Success(c, map[string]int{"purged": purged})
}

// BatchRevokeCodes revokes multiple codes.
func (h *AdminHandler) BatchRevokeCodes(c echo.Context) error {
	var req struct {
		IDs []string `json:"ids"`
	}
	if err := c.Bind(&req); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, err.Error())
	}
	ids := make([]uuid.UUID, 0, len(req.IDs))
	for _, id := range req.IDs {
		uid, err := uuid.Parse(id)
		if err != nil {
			return echo.NewHTTPError(http.StatusBadRequest, "invalid id: "+id)
		}
		ids = append(ids, uid)
	}
	if err := h.adminService.BatchRevokeCodes(c.Request().Context(), ids); err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, err.Error())
	}
	return Success(c, map[string]string{"message": "revoked"})
}

// BatchDeleteCodes deletes multiple codes.
func (h *AdminHandler) BatchDeleteCodes(c echo.Context) error {
	var req struct {
		IDs []string `json:"ids"`
	}
	if err := c.Bind(&req); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, err.Error())
	}
	ids := make([]uuid.UUID, 0, len(req.IDs))
	for _, id := range req.IDs {
		uid, err := uuid.Parse(id)
		if err != nil {
			return echo.NewHTTPError(http.StatusBadRequest, "invalid id: "+id)
		}
		ids = append(ids, uid)
	}
	if err := h.adminService.BatchDeleteCodes(c.Request().Context(), ids); err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, err.Error())
	}
	return Success(c, map[string]string{"message": "deleted"})
}

// GetSupport returns support config.
func (h *AdminHandler) GetSupport(c echo.Context) error {
	config, err := h.adminService.GetSupport(c.Request().Context())
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, err.Error())
	}
	return Success(c, config)
}

// GetProbe R119: 账号能力探测配置(全局单行)
func (h *AdminHandler) GetProbe(c echo.Context) error {
	cfg, err := h.adminService.GetProbe(c.Request().Context())
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, err.Error())
	}
	if cfg == nil {
		cfg = &domain.ProbeConfig{}
	}
	return Success(c, cfg)
}

// UpdateProbe R119: 保存探测配置
func (h *AdminHandler) UpdateProbe(c echo.Context) error {
	var req domain.ProbeConfig
	if err := c.Bind(&req); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "参数错误")
	}
	if err := h.adminService.UpdateProbe(c.Request().Context(), &req); err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, err.Error())
	}
	return Success(c, map[string]string{"message": "探测配置已保存"})
}

// UpdateSupport updates support config.
func (h *AdminHandler) UpdateSupport(c echo.Context) error {
	var req domain.SupportConfig
	if err := c.Bind(&req); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, err.Error())
	}
	if err := h.adminService.UpdateSupport(c.Request().Context(), &req); err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, err.Error())
	}
	return Success(c, map[string]string{"message": "updated"})
}

// ListAnnouncements returns all announcements.
func (h *AdminHandler) ListAnnouncements(c echo.Context) error {
	announcements, err := h.adminService.ListAnnouncements(c.Request().Context())
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, err.Error())
	}
	return SuccessList(c, announcements, len(announcements), 1, len(announcements))
}

// CreateAnnouncement creates a new announcement.
func (h *AdminHandler) CreateAnnouncement(c echo.Context) error {
	var req domain.Announcement
	if err := c.Bind(&req); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, err.Error())
	}
	if err := h.adminService.CreateAnnouncement(c.Request().Context(), &req); err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, err.Error())
	}
	return Created(c, map[string]string{"message": "created"})
}

// UpdateAnnouncement updates an announcement.
func (h *AdminHandler) UpdateAnnouncement(c echo.Context) error {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "invalid announcement id")
	}
	var req domain.Announcement
	if err := c.Bind(&req); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, err.Error())
	}
	req.AnnouncementID = id
	if err := h.adminService.UpdateAnnouncement(c.Request().Context(), &req); err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, err.Error())
	}
	return Success(c, map[string]string{"message": "updated"})
}

// DeleteAnnouncement deletes an announcement.
func (h *AdminHandler) DeleteAnnouncement(c echo.Context) error {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "invalid announcement id")
	}
	if err := h.adminService.DeleteAnnouncement(c.Request().Context(), id); err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, err.Error())
	}
	return Success(c, map[string]string{"message": "deleted"})
}

// ListPlans returns all plans.
func (h *AdminHandler) ListPlans(c echo.Context) error {
	plans, err := h.adminService.ListPlans(c.Request().Context())
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, err.Error())
	}
	return SuccessList(c, plans, len(plans), 1, len(plans))
}

// UpdatePlan updates a plan.
func (h *AdminHandler) UpdatePlan(c echo.Context) error {
	planID := c.Param("id")
	var updates map[string]any
	if err := c.Bind(&updates); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, err.Error())
	}
	if err := h.adminService.UpdatePlan(c.Request().Context(), planID, updates); err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, err.Error())
	}
	return Success(c, map[string]string{"message": "updated"})
}

// GetSystemMetrics returns system metrics.
func (h *AdminHandler) GetSystemMetrics(c echo.Context) error {
	metrics, err := h.adminService.GetSystemMetrics(c.Request().Context())
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, err.Error())
	}
	return Success(c, metrics)
}

// GetAuditLogs returns audit logs.
func (h *AdminHandler) GetAuditLogs(c echo.Context) error {
	page, err := strconv.Atoi(c.QueryParam("page"))
	if err != nil || page < 1 {
		page = 1
	}
	if page < 1 {
		page = 1
	}
	pageSize, err := strconv.Atoi(c.QueryParam("page_size"))
	if err != nil || pageSize < 1 {
		pageSize = 20
	}
	if pageSize < 1 {
		pageSize = 20
	}
	startTime := c.QueryParam("start_time")
	endTime := c.QueryParam("end_time")

	logs, total, err := h.adminService.GetAuditLogs(c.Request().Context(), page, pageSize, startTime, endTime)
	if err != nil {
		if strings.Contains(err.Error(), "invalid time format") {
			return echo.NewHTTPError(http.StatusBadRequest, err.Error())
		}
		return echo.NewHTTPError(http.StatusInternalServerError, err.Error())
	}
	return SuccessList(c, logs, total, page, pageSize)
}
