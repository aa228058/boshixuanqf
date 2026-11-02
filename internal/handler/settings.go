package handler

import (
	"log"
	"encoding/json"
	"net/http"

	"github.com/google/uuid"
	"github.com/labstack/echo/v4"
	"tg-sender/internal/middleware"
	"tg-sender/internal/notify"
	"tg-sender/internal/service"
)

// SettingsHandler handles settings, activation, templates, groups, exports, and notification center.
type SettingsHandler struct {
	svc    *service.SettingsService
	mailer *notify.Mailer // R119: 通知中心(可为 nil)
}

// NewSettingsHandler creates a new settings handler.
func NewSettingsHandler(svc *service.SettingsService, mailer *notify.Mailer) *SettingsHandler {
	return &SettingsHandler{svc: svc, mailer: mailer}
}

// GetNotify R119: 通知中心 —— 读取当前通知设置
func (h *SettingsHandler) GetNotify(c echo.Context) error {
	tenantID, err := middleware.GetTenantID(c)
	if err != nil {
		return err
	}
	tenant, err := h.svc.GetProfile(c.Request().Context(), tenantID)
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, err.Error())
	}
	settings := map[string]bool{}
	if len(tenant.NotifySettings) > 0 {
		_ = json.Unmarshal(tenant.NotifySettings, &settings)
	}
	// R120: 返回租户自己的 SMTP 配置(脱敏: 授权码不回传, 仅回显是否已设置)
	var smtpCfg map[string]any
	_ = json.Unmarshal(tenant.NotifySMTP, &smtpCfg)
	smtpConfigured := tenant.SMTPConfig() != nil
	if smtpCfg == nil {
		smtpCfg = map[string]any{}
	}
	if smtpConfigured {
		smtpCfg["pass_set"] = true // 前端据此显示"已设置授权码"
		delete(smtpCfg, "pass")    // 授权码绝不下发
	}
	return Success(c, map[string]any{
		"email":           tenant.NotifyEmail,
		"settings":        settings,
		"events":          notify.EventLabels, // 事件清单(键→中文标签)
		"smtp":            smtpCfg,            // 租户 SMTP 配置(脱敏)
		"smtp_configured": smtpConfigured,
	})
}

// UpdateNotify R119: 通知中心 —— 保存通知设置
func (h *SettingsHandler) UpdateNotify(c echo.Context) error {
	tenantID, err := middleware.GetTenantID(c)
	if err != nil {
		return err
	}
	var req struct {
		Email    string          `json:"email"`
		Settings map[string]bool `json:"settings"`
		SMTP     map[string]any  `json:"smtp"` // R120: 租户发件配置(provider/host/port/user/pass/from)
	}
	if err := c.Bind(&req); err != nil || req.Email == "" {
		return echo.NewHTTPError(http.StatusBadRequest, "参数错误: 需要合法的邮箱地址")
	}
	raw, _ := json.Marshal(req.Settings)
	// SMTP 配置: 若前端未传 pass 且租户已配置过, 保留旧授权码(前端不回传已存的密码)
	smtpRaw, _ := json.Marshal(req.SMTP)
	if req.SMTP != nil {
		if _, hasPass := req.SMTP["pass"]; !hasPass {
			tenant, terr := h.svc.GetProfile(c.Request().Context(), tenantID)
			if terr == nil && tenant != nil && tenant.SMTPConfig() != nil {
				var old map[string]any
				if json.Unmarshal(tenant.NotifySMTP, &old) == nil {
					if p, ok := old["pass"]; ok {
						req.SMTP["pass"] = p
						smtpRaw, _ = json.Marshal(req.SMTP)
					}
				}
			}
		}
	}
	if err := h.svc.UpdateNotify(c.Request().Context(), tenantID, req.Email, raw, smtpRaw); err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, err.Error())
	}
	return Success(c, map[string]string{"message": "通知设置已保存"})
}

// TestNotify R119: 通知中心 —— 发送测试邮件
func (h *SettingsHandler) TestNotify(c echo.Context) error {
	tenantID, err := middleware.GetTenantID(c)
	if err != nil {
		return err
	}
	tenant, err := h.svc.GetProfile(c.Request().Context(), tenantID)
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, err.Error())
	}
	// R120: 用租户自己的 SMTP 配置发测试邮件
	cfg := tenant.SMTPConfig()
	if !notify.Configured(cfg) {
		return echo.NewHTTPError(http.StatusBadRequest, "请先在下方「发件邮箱」中选择服务商并填写配置")
	}
	if tenant.NotifyEmail == "" {
		return echo.NewHTTPError(http.StatusBadRequest, "请先填写接收通知的邮箱")
	}
	subject := "【博士轩群发器】测试邮件"
	body := "这是一封测试邮件。\n\n如果您收到它, 说明通知邮箱配置成功——中奖/任务等事件将推送到此邮箱。\n\n博士轩群发器"
	if err := h.mailer.Send(cfg, tenant.NotifyEmail, subject, body); err != nil {
		// R147: 失败落日志(SMTP 原始错误), 不再只回前端
		log.Printf("[notify] 测试邮件发送失败 tenant=%s smtp=%s:%d err=%v", tenantID, cfg.Host, cfg.Port, err)
		return echo.NewHTTPError(http.StatusInternalServerError, "发送失败: "+err.Error())
	}
	log.Printf("[notify] 测试邮件发送成功 tenant=%s → %s", tenantID, tenant.NotifyEmail)
	return Success(c, map[string]string{"message": "测试邮件已发送, 请查收(含垃圾邮件文件夹)"})
}

// GetProfile returns tenant profile.
func (h *SettingsHandler) GetProfile(c echo.Context) error {
	tenantID, err := middleware.GetTenantID(c)
	if err != nil {
		return err
	}
	tenant, err := h.svc.GetProfile(c.Request().Context(), tenantID)
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, err.Error())
	}
	return Success(c, tenant)
}

// ActivateCode redeems an activation code.
func (h *SettingsHandler) ActivateCode(c echo.Context) error {
	tenantID, err := middleware.GetTenantID(c)
	if err != nil {
		return err
	}
	var req struct {
		Code string `json:"code"`
	}
	if err := c.Bind(&req); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "invalid request")
	}
	tenant, err := h.svc.ActivateCode(c.Request().Context(), tenantID, req.Code)
	if err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, err.Error())
	}
	return Success(c, tenant)
}

// GetPlans returns available plans.
func (h *SettingsHandler) GetPlans(c echo.Context) error {
	plans, err := h.svc.GetPlans(c.Request().Context())
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, err.Error())
	}
	return Success(c, plans)
}

// GetSupport returns support config (public, no auth).
func (h *SettingsHandler) GetSupport(c echo.Context) error {
	config, err := h.svc.GetSupport(c.Request().Context())
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, err.Error())
	}
	return Success(c, config)
}

// GetAnnouncements returns announcements for tenant.
func (h *SettingsHandler) GetAnnouncements(c echo.Context) error {
	tenantID, err := middleware.GetTenantID(c)
	if err != nil {
		return err
	}
	tenant, err := h.svc.GetProfile(c.Request().Context(), tenantID)
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, err.Error())
	}
	planID := "free"
	if tenant.PlanID != "" {
		planID = tenant.PlanID
	}
	announcements, err := h.svc.GetAnnouncements(c.Request().Context(), planID)
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, err.Error())
	}
	return Success(c, announcements)
}

// === Message Templates ===

func (h *SettingsHandler) ListTemplates(c echo.Context) error {
	tenantID, err := middleware.GetTenantID(c)
	if err != nil {
		return err
	}
	list, err := h.svc.ListTemplates(c.Request().Context(), tenantID)
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, err.Error())
	}
	return SuccessList(c, list, len(list), 1, len(list))
}

func (h *SettingsHandler) CreateTemplate(c echo.Context) error {
	tenantID, err := middleware.GetTenantID(c)
	if err != nil {
		return err
	}
	var req struct {
		Name    string `json:"name"`
		Content string `json:"content"`
	}
	if err := c.Bind(&req); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "invalid request")
	}
	t, err := h.svc.CreateTemplate(c.Request().Context(), tenantID, req.Name, req.Content)
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, err.Error())
	}
	return Success(c, t)
}

func (h *SettingsHandler) UpdateTemplate(c echo.Context) error {
	tenantID, err := middleware.GetTenantID(c)
	if err != nil {
		return err
	}
	templateID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "invalid template id")
	}
	var req struct {
		Name    string `json:"name"`
		Content string `json:"content"`
	}
	if err := c.Bind(&req); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "invalid request")
	}
	if err := h.svc.UpdateTemplate(c.Request().Context(), tenantID, templateID, req.Name, req.Content); err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, err.Error())
	}
	return NoContent(c)
}

func (h *SettingsHandler) DeleteTemplate(c echo.Context) error {
	tenantID, err := middleware.GetTenantID(c)
	if err != nil {
		return err
	}
	templateID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "invalid template id")
	}
	if err := h.svc.DeleteTemplate(c.Request().Context(), tenantID, templateID); err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, err.Error())
	}
	return NoContent(c)
}

// === Account Groups ===

func (h *SettingsHandler) ListAccountGroups(c echo.Context) error {
	tenantID, err := middleware.GetTenantID(c)
	if err != nil {
		return err
	}
	list, err := h.svc.ListAccountGroups(c.Request().Context(), tenantID)
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, err.Error())
	}
	return SuccessList(c, list, len(list), 1, len(list))
}

func (h *SettingsHandler) CreateAccountGroup(c echo.Context) error {
	tenantID, err := middleware.GetTenantID(c)
	if err != nil {
		return err
	}
	var req struct {
		Name       string      `json:"name"`
		Color      string      `json:"color"`
		AccountIDs []uuid.UUID `json:"account_ids"`
	}
	if err := c.Bind(&req); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "invalid request")
	}
	g, err := h.svc.CreateAccountGroup(c.Request().Context(), tenantID, req.Name, req.Color, req.AccountIDs)
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, err.Error())
	}
	return Success(c, g)
}

func (h *SettingsHandler) UpdateAccountGroup(c echo.Context) error {
	tenantID, err := middleware.GetTenantID(c)
	if err != nil {
		return err
	}
	groupID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "invalid group id")
	}
	var req struct {
		Name       string      `json:"name"`
		Color      string      `json:"color"`
		AccountIDs []uuid.UUID `json:"account_ids"`
	}
	if err := c.Bind(&req); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "invalid request")
	}
	if err := h.svc.UpdateAccountGroup(c.Request().Context(), tenantID, groupID, req.Name, req.Color, req.AccountIDs); err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, err.Error())
	}
	return NoContent(c)
}

func (h *SettingsHandler) DeleteAccountGroup(c echo.Context) error {
	tenantID, err := middleware.GetTenantID(c)
	if err != nil {
		return err
	}
	groupID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "invalid group id")
	}
	if err := h.svc.DeleteAccountGroup(c.Request().Context(), tenantID, groupID); err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, err.Error())
	}
	return NoContent(c)
}

// === Export ===

func (h *SettingsHandler) ExportScrape(c echo.Context) error {
	tenantID, err := middleware.GetTenantID(c)
	if err != nil {
		return err
	}
	format := c.QueryParam("format")
	if format == "" {
		format = "csv"
	}
	var groupID *uuid.UUID
	if gid := c.QueryParam("group_id"); gid != "" {
		id, err := uuid.Parse(gid)
		if err != nil {
			// P2-48 审计修复: 原来 Parse 失败静默置 nil 走全量导出——用户手滑传错
			// group_id 会把全库成员导出去(数据泄露+超大响应)。显式报错。
			return echo.NewHTTPError(http.StatusBadRequest, "group_id 格式错误(应为 UUID)")
		}
		groupID = &id
	}

	var data []byte
	var filename string
	var contentType string

	switch format {
	case "json":
		data, err = h.svc.ExportMembersJSON(c.Request().Context(), tenantID, groupID)
		filename = "members.json"
		contentType = "application/json"
	default:
		data, err = h.svc.ExportMembersCSV(c.Request().Context(), tenantID, groupID)
		filename = "members.csv"
		contentType = "text/csv"
	}

	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, err.Error())
	}

	c.Response().Header().Set("Content-Type", contentType)
	c.Response().Header().Set("Content-Disposition", "attachment; filename="+filename)
	return c.Blob(http.StatusOK, contentType, data)
}

// MarkAnnouncementRead marks an announcement as read for the tenant.
func (h *SettingsHandler) MarkAnnouncementRead(c echo.Context) error {
	// No-op or store read status if needed; for now just return 200
	return NoContent(c)
}
