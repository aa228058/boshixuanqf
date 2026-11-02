package handler

import (
	"archive/zip"
	"bytes"
	"context"
	"errors"
	"fmt"
	"log"
	"net/http"
	"strings"
	"tg-sender/internal/safego"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/labstack/echo/v4"
	"tg-sender/internal/domain"
	"tg-sender/internal/middleware"
	"tg-sender/internal/repository"
	"tg-sender/internal/service"

	"io"
	"os"
	"os/exec"
	"path/filepath"
)

// AccountHandler handles account APIs.
type AccountHandler struct {
	svc        *service.AccountService
	taskSvc    *service.TaskService // R181 群转移: 导出链接后用目标账号创建 join_groups 任务
	memberRepo domain.MemberRepository
	auditRepo  *repository.AuditRepository
}

// AccountListResponse represents a list of accounts response.
type AccountListResponse struct {
	Data []*domain.Account `json:"data"`
}

// AccountResponse represents a single account response.
type AccountResponse struct {
	Data *domain.Account `json:"data"`
}

// ErrorResponse represents an error response.
type ErrorResponse struct {
	Error string `json:"error"`
}

// NewAccountHandler creates a new account handler.
func NewAccountHandler(svc *service.AccountService, memberRepo domain.MemberRepository, auditRepo *repository.AuditRepository, taskSvc ...*service.TaskService) *AccountHandler {
	h := &AccountHandler{svc: svc, memberRepo: memberRepo, auditRepo: auditRepo}
	if len(taskSvc) > 0 {
		h.taskSvc = taskSvc[0]
	}
	return h
}

// List godoc
// @Summary 获取账号列表
// @Description 获取租户下的所有TG账号
// @Tags 账号
// @Accept json
// @Produce json
// @Success 200 {object} AccountListResponse
// @Failure 500 {object} ErrorResponse
// @Security BearerAuth
// @Router /accounts [get]
// List returns all accounts for the tenant.
func (h *AccountHandler) List(c echo.Context) error {
	tenantID, err := middleware.GetTenantID(c)
	if err != nil {
		return err
	}

	accounts, err := h.svc.List(c.Request().Context(), tenantID, c.QueryParam("source"))
	if err != nil {
		log.Printf("[handler] 获取账号列表失败: %v", err)
		return echo.NewHTTPError(http.StatusInternalServerError, "internal server error")
	}
	total := len(accounts)
	return SuccessList(c, accounts, total, 1, total)
}

// CreateRequest represents create account request.
type CreateAccountRequest struct {
	Phone  string `json:"phone" validate:"required"`
	Remark string `json:"remark"`
	Level  string `json:"level" validate:"required,oneof=new normal old"`
}

// Create godoc
// @Summary 创建账号
// @Description 创建新的TG账号配置
// @Tags 账号
// @Accept json
// @Produce json
// @Param body body CreateAccountRequest true "创建账号请求"
// @Success 201 {object} AccountResponse
// @Failure 400 {object} ErrorResponse
// @Failure 500 {object} ErrorResponse
// @Security BearerAuth
// @Router /accounts [post]
// Create creates a new account.
func (h *AccountHandler) Create(c echo.Context) error {
	tenantID, err := middleware.GetTenantID(c)
	if err != nil {
		return err
	}

	var req CreateAccountRequest
	if err := c.Bind(&req); err != nil {
		log.Printf("[handler] 解析创建账号请求失败: %v", err)
		return echo.NewHTTPError(http.StatusBadRequest, "请求参数错误")
	}
	if req.Phone == "" {
		return echo.NewHTTPError(http.StatusBadRequest, "手机号不能为空")
	}

	level := domain.AccountLevel(req.Level)
	if level == "" {
		level = domain.AccountLevelNormal
	}

	acc, err := h.svc.Create(c.Request().Context(), tenantID, req.Phone, req.Remark, level)
	if err != nil {
		log.Printf("[handler] 创建账号失败: %v", err)
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			return echo.NewHTTPError(http.StatusBadRequest, "该手机号已存在")
		}
		return echo.NewHTTPError(http.StatusBadRequest, "创建账号失败")
	}
	return Created(c, acc)
}

// Delete godoc
// @Summary 删除账号
// @Description 删除指定TG账号
// @Tags 账号
// @Accept json
// @Produce json
// @Param id path string true "账号ID"
// @Success 204
// @Failure 400 {object} ErrorResponse
// @Failure 500 {object} ErrorResponse
// @Security BearerAuth
// @Router /accounts/{id} [delete]
// Delete removes an account.
func (h *AccountHandler) Delete(c echo.Context) error {
	tenantID, err := middleware.GetTenantID(c)
	if err != nil {
		return err
	}

	accountID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "invalid account id")
	}

	if err := h.svc.Delete(c.Request().Context(), tenantID, accountID); err != nil {
		log.Printf("[handler] 删除账号失败: %v", err)
		return echo.NewHTTPError(http.StatusInternalServerError, "failed to delete account")
	}
	// F-31: 敏感操作审计（异步，不阻塞响应）
	safego.Go(func() {
		if err := h.auditRepo.Create(context.Background(), &domain.AuditLog{
			TenantID: tenantID, UserID: "tenant", Action: "account_delete",
			Target: accountID.String(), IP: c.RealIP(),
		}); err != nil {
			log.Printf("[audit] write failed: %v", err)
		}
	})
	return NoContent(c)
}

// Connect godoc
// @Summary 连接账号
// @Description 连接指定TG账号到Telegram
// @Tags 账号
// @Accept json
// @Produce json
// @Param id path string true "账号ID"
// @Success 200 {object} map[string]string
// @Failure 400 {object} ErrorResponse
// @Failure 500 {object} ErrorResponse
// @Security BearerAuth
// @Router /accounts/{id}/connect [post]
// Connect connects an account.
func (h *AccountHandler) Connect(c echo.Context) error {
	tenantID, err := middleware.GetTenantID(c)
	if err != nil {
		return err
	}

	accountID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "invalid account id")
	}

	if err := h.svc.Connect(c.Request().Context(), tenantID, accountID); err != nil {
		log.Printf("[handler] 连接账号失败: %v", err)
		return echo.NewHTTPError(http.StatusInternalServerError, "failed to connect account")
	}
	return Success(c, map[string]string{"status": "connecting"})
}

// Disconnect godoc
// @Summary 断开账号
// @Description 断开指定TG账号连接
// @Tags 账号
// @Accept json
// @Produce json
// @Param id path string true "账号ID"
// @Success 200 {object} map[string]string
// @Failure 400 {object} ErrorResponse
// @Failure 500 {object} ErrorResponse
// @Security BearerAuth
// @Router /accounts/{id}/disconnect [post]
// Disconnect disconnects an account.
func (h *AccountHandler) Disconnect(c echo.Context) error {
	tenantID, err := middleware.GetTenantID(c)
	if err != nil {
		return err
	}

	accountID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "invalid account id")
	}

	if err := h.svc.Disconnect(c.Request().Context(), tenantID, accountID); err != nil {
		log.Printf("[handler] 断开账号失败: %v", err)
		return echo.NewHTTPError(http.StatusInternalServerError, "failed to disconnect account")
	}
	return Success(c, map[string]string{"status": "disconnected"})
}

// VerifyCodeRequest represents verify code request.
type VerifyCodeRequest struct {
	Code string `json:"code" validate:"required"`
}

// SubmitCode godoc
// @Summary 提交验证码
// @Description 提交TG登录验证码
// @Tags 账号
// @Accept json
// @Produce json
// @Param id path string true "账号ID"
// @Param body body VerifyCodeRequest true "验证码请求"
// @Success 200 {object} map[string]string
// @Failure 400 {object} ErrorResponse
// @Failure 500 {object} ErrorResponse
// @Security BearerAuth
// @Router /accounts/{id}/code [post]
// SubmitCode is an alias for VerifyCode.
func (h *AccountHandler) SubmitCode(c echo.Context) error {
	return h.VerifyCode(c)
}

// VerifyCode godoc
// @Summary 验证验证码
// @Description 验证TG登录验证码
// @Tags 账号
// @Accept json
// @Produce json
// @Param id path string true "账号ID"
// @Param body body VerifyCodeRequest true "验证码请求"
// @Success 200 {object} map[string]string
// @Failure 400 {object} ErrorResponse
// @Failure 500 {object} ErrorResponse
// @Security BearerAuth
// @Router /accounts/{id}/verify [post]
// VerifyCode submits the phone verification code.
func (h *AccountHandler) VerifyCode(c echo.Context) error {
	tenantID, err := middleware.GetTenantID(c)
	if err != nil {
		return err
	}

	accountID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "invalid account id")
	}

	var req VerifyCodeRequest
	if err := c.Bind(&req); err != nil {
		log.Printf("[handler] 解析验证码请求失败: %v", err)
		return echo.NewHTTPError(http.StatusBadRequest, "请求参数错误")
	}
	if req.Code == "" {
		return echo.NewHTTPError(http.StatusBadRequest, "验证码不能为空")
	}

	if err := h.svc.VerifyCode(c.Request().Context(), tenantID, accountID, req.Code); err != nil {
		log.Printf("[handler] 验证验证码失败: %v", err)
		return echo.NewHTTPError(http.StatusInternalServerError, "failed to verify code")
	}
	return Success(c, map[string]string{"status": "code_submitted"})
}

// Submit2FARequest represents 2FA password request.
type Submit2FARequest struct {
	Password string `json:"password" validate:"required"`
}

// Submit2FA godoc
// @Summary 提交2FA密码
// @Description 提交TG两步验证密码
// @Tags 账号
// @Accept json
// @Produce json
// @Param id path string true "账号ID"
// @Param body body Submit2FARequest true "2FA密码请求"
// @Success 200 {object} map[string]string
// @Failure 400 {object} ErrorResponse
// @Failure 500 {object} ErrorResponse
// @Security BearerAuth
// @Router /accounts/{id}/password [post]
// Submit2FA submits the 2FA password.
func (h *AccountHandler) Submit2FA(c echo.Context) error {
	tenantID, err := middleware.GetTenantID(c)
	if err != nil {
		return err
	}

	accountID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "invalid account id")
	}

	var req Submit2FARequest
	if err := c.Bind(&req); err != nil {
		log.Printf("[handler] 解析2FA请求失败: %v", err)
		return echo.NewHTTPError(http.StatusBadRequest, "请求参数错误")
	}
	if req.Password == "" {
		return echo.NewHTTPError(http.StatusBadRequest, "密码不能为空")
	}

	if err := h.svc.Submit2FA(c.Request().Context(), tenantID, accountID, req.Password); err != nil {
		log.Printf("[handler] 提交2FA失败: %v", err)
		return echo.NewHTTPError(http.StatusInternalServerError, "failed to submit 2FA")
	}
	return Success(c, map[string]string{"status": "2fa_submitted"})
}

// GetAuthState godoc
// @Summary 获取账号认证状态
// @Description 获取当前账号的认证阶段（waiting_code/waiting_password/connected/none）
// @Tags 账号
// @Accept json
// @Produce json
// @Param id path string true "账号ID"
// @Success 200 {object} account.AuthState
// @Failure 400 {object} ErrorResponse
// @Security BearerAuth
// @Router /accounts/{id}/auth-state [get]
// GetAuthState returns the current authentication state.
func (h *AccountHandler) GetAuthState(c echo.Context) error {
	tenantID, err := middleware.GetTenantID(c)
	if err != nil {
		return err
	}

	accountID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "invalid account id")
	}

	state := h.svc.GetAuthState(tenantID, accountID)
	return Success(c, state)
}

// GetByID godoc
// @Summary 获取账号详情
// @Description 获取指定TG账号详情
// @Tags 账号
// @Accept json
// @Produce json
// @Param id path string true "账号ID"
// @Success 200 {object} domain.Account
// @Failure 400 {object} ErrorResponse
// @Failure 404 {object} ErrorResponse
// @Failure 500 {object} ErrorResponse
// @Security BearerAuth
// @Router /accounts/{id} [get]
// GetByID returns a single account.
func (h *AccountHandler) GetByID(c echo.Context) error {
	tenantID, err := middleware.GetTenantID(c)
	if err != nil {
		return err
	}
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "invalid account id")
	}
	acc, err := h.svc.GetByID(c.Request().Context(), tenantID, id)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return echo.NewHTTPError(http.StatusNotFound, "账号不存在")
		}
		log.Printf("[handler] 获取详情失败: %v", err)
		return echo.NewHTTPError(http.StatusInternalServerError, "internal error")
	}
	return Success(c, acc)
}

// UpdateRequest represents update account request.
type UpdateAccountRequest struct {
	Level       *string `json:"level,omitempty"`
	HealthScore *int    `json:"health_score,omitempty"`
	Remark      *string `json:"remark,omitempty"`
}

// Update godoc
// @Summary 更新账号
// @Description 更新TG账号配置
// @Tags 账号
// @Accept json
// @Produce json
// @Param id path string true "账号ID"
// @Param body body UpdateAccountRequest true "更新账号请求"
// @Success 204
// @Failure 400 {object} ErrorResponse
// @Failure 500 {object} ErrorResponse
// @Security BearerAuth
// @Router /accounts/{id} [put]
// Update updates account fields.
func (h *AccountHandler) Update(c echo.Context) error {
	tenantID, err := middleware.GetTenantID(c)
	if err != nil {
		return err
	}
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "invalid account id")
	}
	var req UpdateAccountRequest
	if err := c.Bind(&req); err != nil {
		log.Printf("[handler] 解析更新账号请求失败: %v", err)
		return echo.NewHTTPError(http.StatusBadRequest, "请求参数错误")
	}
	var level *domain.AccountLevel
	if req.Level != nil {
		l := domain.AccountLevel(*req.Level)
		level = &l
	}
	if err := h.svc.Update(c.Request().Context(), tenantID, id, level, req.HealthScore, req.Remark); err != nil {
		log.Printf("[handler] 更新账号失败: %v", err)
		return echo.NewHTTPError(http.StatusInternalServerError, "internal server error")
	}
	return NoContent(c)
}

// GetQuotas godoc
// @Summary 获取账号配额
// @Description 获取指定TG账号的配额信息
// @Tags 账号
// @Accept json
// @Produce json
// @Param id path string true "账号ID"
// @Success 200 {object} domain.AccountQuota
// @Failure 400 {object} ErrorResponse
// @Failure 500 {object} ErrorResponse
// @Security BearerAuth
// @Router /accounts/{id}/quotas [get]
// GetQuotas returns account quotas.
func (h *AccountHandler) GetQuotas(c echo.Context) error {
	tenantID, err := middleware.GetTenantID(c)
	if err != nil {
		return err
	}

	accountID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "invalid account id")
	}

	// Verify account belongs to tenant and return quotas
	quotas, err := h.svc.GetQuotas(c.Request().Context(), tenantID, accountID)
	if err != nil {
		log.Printf("[handler] 获取配额失败: %v", err)
		return echo.NewHTTPError(http.StatusInternalServerError, "internal server error")
	}
	return Success(c, quotas)
}

// GetTelegramDialogs godoc
// @Summary 获取Telegram对话列表
// @Description 获取账号的Telegram对话列表(群组、频道、用户)
// @Tags 账号
// @Accept json
// @Produce json
// @Param id path string true "账号ID"
// @Success 200 {array} map[string]interface{}
// @Failure 400 {object} ErrorResponse
// @Failure 500 {object} ErrorResponse
// @Security BearerAuth
// @Router /accounts/{id}/telegram-dialogs [get]
// GetTelegramDialogs returns all Telegram dialogs for an account (groups, channels, users).
func (h *AccountHandler) GetTelegramDialogs(c echo.Context) error {
	tenantID, err := middleware.GetTenantID(c)
	if err != nil {
		return err
	}
	accountID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "invalid account id")
	}
	dialogs, err := h.svc.GetTelegramDialogs(c.Request().Context(), tenantID, accountID)
	if err != nil {
		log.Printf("[handler] 获取对话列表失败: %v", err)
		return echo.NewHTTPError(http.StatusInternalServerError, "internal server error")
	}
	// 私聊用户写入联系人缓存(异步, 不影响返回): 之后打开选择器直接读库秒开
	if h.memberRepo != nil {
		safego.Go(func() {
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			for _, m := range dialogs {
				if m["type"] != "user" {
					continue
				}
				// id 在 Go map 中是 int64, 兼容数字多种类型
				var id int64
				switch v := m["id"].(type) {
				case int64:
					id = v
				case float64:
					id = int64(v)
				case int:
					id = int64(v)
				default:
					continue
				}
				uname, _ := m["username"].(string)
				title, _ := m["title"].(string)
				if uname == "" {
					uname = strings.TrimPrefix(title, "@")
				}
				if err := h.memberRepo.UpsertContact(ctx, tenantID, accountID, id, uname, title, "", 0); err != nil {
					log.Printf("[handler] 写入联系人失败: %v", err)
				}
			}
		})
	}
	return Success(c, dialogs)
}

// GetTelegramContacts godoc
// @Summary 获取Telegram联系人
// @Description 获取账号的Telegram联系人列表
// @Tags 账号
// @Accept json
// @Produce json
// @Param id path string true "账号ID"
// @Success 200 {array} map[string]interface{}
// @Failure 400 {object} ErrorResponse
// @Failure 500 {object} ErrorResponse
// @Security BearerAuth
// @Router /accounts/{id}/telegram-contacts [get]
// GetTelegramContacts returns the account's Telegram contact list.
func (h *AccountHandler) GetTelegramContacts(c echo.Context) error {
	tenantID, err := middleware.GetTenantID(c)
	if err != nil {
		return err
	}
	accountID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "invalid account id")
	}
	contacts, err := h.svc.GetTelegramContacts(c.Request().Context(), tenantID, accountID)
	if err != nil {
		log.Printf("[handler] 获取联系人失败: %v", err)
		return echo.NewHTTPError(http.StatusInternalServerError, "internal server error")
	}
	return Success(c, contacts)
}

// ResolvePeerRequest represents peer resolution request.
type ResolvePeerRequest struct {
	Identifier string `json:"identifier" validate:"required"` // username or invite link
}

// ResolvePeer godoc
// @Summary 解析Telegram用户/群组
// @Description 解析Telegram用户名或邀请链接为聊天信息
// @Tags 账号
// @Accept json
// @Produce json
// @Param id path string true "账号ID"
// @Param body body ResolvePeerRequest true "解析请求"
// @Success 200 {object} map[string]interface{}
// @Failure 400 {object} ErrorResponse
// @Failure 500 {object} ErrorResponse
// @Security BearerAuth
// @Router /accounts/{id}/resolve-peer [post]
// ResolvePeer resolves a Telegram username or link to chat info.
func (h *AccountHandler) ResolvePeer(c echo.Context) error {
	tenantID, err := middleware.GetTenantID(c)
	if err != nil {
		return err
	}
	accountID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "invalid account id")
	}
	var req ResolvePeerRequest
	if err := c.Bind(&req); err != nil {
		log.Printf("[handler] 解析目标请求失败: %v", err)
		return echo.NewHTTPError(http.StatusBadRequest, "请求参数错误")
	}
	if req.Identifier == "" {
		return echo.NewHTTPError(http.StatusBadRequest, "标识符不能为空")
	}
	peer, err := h.svc.ResolvePeer(c.Request().Context(), tenantID, accountID, req.Identifier)
	if err != nil {
		log.Printf("[handler] 解析目标失败: %v", err)
		return echo.NewHTTPError(http.StatusInternalServerError, "failed to resolve peer")
	}
	return Success(c, peer)
}

// ============ 协议号导入与资料管理 ============

// ImportSessions 批量导入协议号（每行一个 session 字符串）
func (h *AccountHandler) ImportSessions(c echo.Context) error {
	tenantID, err := middleware.GetTenantID(c)
	if err != nil {
		return err
	}
	var req struct {
		Sessions []string `json:"sessions"`
		Remark   string   `json:"remark"`
	}
	if err := c.Bind(&req); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "invalid request")
	}
	if len(req.Sessions) == 0 {
		return echo.NewHTTPError(http.StatusBadRequest, "sessions 不能为空")
	}
	if len(req.Sessions) > 50 {
		return echo.NewHTTPError(http.StatusBadRequest, "单次最多导入 50 个")
	}
	result, err := h.svc.ImportSessions(c.Request().Context(), tenantID, req.Sessions, req.Remark)
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, err.Error())
	}
	return c.JSON(http.StatusOK, result)
}

// UpdateProfile 修改账号资料（TG 名字/签名/用户名 + 本地备注）
// RunProbe R119: 执行账号能力探测(8 项矩阵, 24h 冷却 —— 冷却中返回缓存结果)
func (h *AccountHandler) RunProbe(c echo.Context) error {
	tenantID, err := middleware.GetTenantID(c)
	if err != nil {
		return err
	}
	accountID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "invalid account id")
	}
	res, err := h.svc.Probe(c.Request().Context(), tenantID, accountID)
	if err != nil {
		return echo.NewHTTPError(http.StatusTooManyRequests, err.Error())
	}
	return Success(c, res)
}

// GetProbe R119: 读取最近一次探测结果(不触发检测, 弹窗打开先展示上次结果)
func (h *AccountHandler) GetProbe(c echo.Context) error {
	tenantID, err := middleware.GetTenantID(c)
	if err != nil {
		return err
	}
	accountID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "invalid account id")
	}
	res, err := h.svc.GetProbeResult(c.Request().Context(), tenantID, accountID)
	if err != nil || res == nil {
		return Success(c, nil)
	}
	return Success(c, res)
}

func (h *AccountHandler) UpdateProfile(c echo.Context) error {
	tenantID, err := middleware.GetTenantID(c)
	if err != nil {
		return err
	}
	accountID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "invalid account id")
	}
	var req struct {
		FirstName string `json:"first_name"`
		LastName  string `json:"last_name"`
		About     string `json:"about"`
		Username  string `json:"username"`
		Remark    string `json:"remark"`
		TwoFA     string `json:"twofa"` // 二步验证密码(存device_profile, 掉线重登用)
	}
	if err := c.Bind(&req); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "invalid request")
	}
	if err := h.svc.UpdateAccountProfile(c.Request().Context(), tenantID, accountID, req.FirstName, req.LastName, req.About, req.Username, req.Remark, req.TwoFA); err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, err.Error())
	}
	return c.JSON(http.StatusOK, map[string]bool{"ok": true})
}

// UploadAvatar 上传账号头像（multipart，字段名 photo）
func (h *AccountHandler) UploadAvatar(c echo.Context) error {
	tenantID, err := middleware.GetTenantID(c)
	if err != nil {
		return err
	}
	accountID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "invalid account id")
	}
	file, err := c.FormFile("photo")
	if err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "missing photo field")
	}
	src, err := file.Open()
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "open file failed")
	}
	defer src.Close()
	photo, err := io.ReadAll(io.LimitReader(src, 5<<20)) // 5MB 上限
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "read file failed")
	}
	// F-26: 魔数校验（与 media.go 同口径），仅允许 JPEG/PNG，与声明的 Content-Type 无关
	if len(photo) < 3 || !(photo[0] == 0xFF && photo[1] == 0xD8 && photo[2] == 0xFF) &&
		!(len(photo) >= 4 && photo[0] == 0x89 && photo[1] == 0x50 && photo[2] == 0x4E && photo[3] == 0x47) {
		return echo.NewHTTPError(http.StatusBadRequest, "invalid image format (jpeg/png only)")
	}
	if err := h.svc.UploadAccountAvatar(c.Request().Context(), tenantID, accountID, photo); err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, err.Error())
	}
	return c.JSON(http.StatusOK, map[string]bool{"ok": true})
}

// sanitizeStem 提取文件名主体(只保留安全字符防路径注入, 截 40 字符与 CreateTemp 前缀规则一致)。
// R182: 同时服务三处——临时文件前缀(手机号兜底解析)、sidecar json 命名(.json 同 stem 配对)、rar/7z 侧车拷贝。
func sanitizeStem(name string) string {
	base := filepath.Base(strings.ReplaceAll(name, "\\", "/"))
	stem := strings.TrimSuffix(base, filepath.Ext(base))
	var b strings.Builder
	for _, r := range stem {
		if (r >= '0' && r <= '9') || r == '+' || (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || r == '-' || r == '_' {
			b.WriteRune(r)
		}
	}
	s := b.String()
	if len(s) > 40 {
		s = s[:40]
	}
	return s
}

// tempPrefixFromName 提取文件名主体作为 CreateTemp 前缀(R182: 原始文件名在解压落盘时丢失,
// service 端手机号兜底解析(extractPhoneFromFilename)依赖它)。
func tempPrefixFromName(name string) string {
	if s := sanitizeStem(name); s != "" {
		return s + "-"
	}
	return "import-"
}

// extractArchiveCLI 用系统 7z 解压 rar/7z(p7zip 的 7z 内置 RAR 解码, 两种格式一条命令, Dockerfile 已装 p7zip)。
func extractArchiveCLI(archivePath, ext string) (string, error) {
	dir, err := os.MkdirTemp("", "archive-out-")
	if err != nil {
		return "", err
	}
	cmd := exec.Command("7z", "x", "-y", "-o"+dir, archivePath)
	out, err := cmd.CombinedOutput()
	if err != nil {
		os.RemoveAll(dir)
		return "", fmt.Errorf("%v: %s", err, out)
	}
	return dir, nil
}

// ImportSessionFiles 上传 .session 文件批量导入（multipart，字段名 files，可多选）
func (h *AccountHandler) ImportSessionFiles(c echo.Context) error {
	tenantID, err := middleware.GetTenantID(c)
	if err != nil {
		return err
	}
	form, err := c.MultipartForm()
	if err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "需要 multipart 表单，字段名 files")
	}
	files := form.File["files"]
	if len(files) == 0 {
		return echo.NewHTTPError(http.StatusBadRequest, "未收到文件（字段名 files）")
	}
	if len(files) > 50 {
		return echo.NewHTTPError(http.StatusBadRequest, "单次最多导入 50 个文件")
	}
	remark := c.FormValue("remark")

	// 落盘临时文件（SQLite 驱动需要可寻址路径）
	// (2026-10-20) 支持 .zip 压缩包: 自动解压包内全部 .session 逐个导入
	var paths []string
	for _, fh := range files {
		src, err := fh.Open()
		if err != nil {
			return echo.NewHTTPError(http.StatusInternalServerError, "读取上传文件失败")
		}
		if strings.HasSuffix(strings.ToLower(fh.Filename), ".zip") {
			data, err := io.ReadAll(io.LimitReader(src, 50<<20)) // zip 包 50MB 上限
			src.Close()
			if err != nil {
				return echo.NewHTTPError(http.StatusBadRequest, "读取压缩包失败")
			}
			zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
			if err != nil {
				return echo.NewHTTPError(http.StatusBadRequest, "压缩包解析失败(需标准 zip 格式): "+err.Error())
			}
			zipCount := 0
			// R182 两遍式: 第一遍收集同 stem 的 .json(常含 2FA 密码/手机号, 号商导出惯例),
			// 第二遍落 session 时按同 stem 写出 sidecar json, service 端 readSidecarJSON 读取。
			sidecars := map[string][]byte{}
			var sessionFiles []*zip.File
			for _, zf := range zr.File {
				if zf.FileInfo().IsDir() {
					continue
				}
				lowerName := strings.ToLower(zf.Name)
				if strings.HasSuffix(lowerName, ".json") {
					if rc, err := zf.Open(); err == nil {
						data, _ := io.ReadAll(io.LimitReader(rc, 1<<20))
						rc.Close()
						if stem := sanitizeStem(zf.Name); stem != "" {
							sidecars[stem] = data
						}
					}
					continue
				}
				if strings.HasSuffix(lowerName, ".session") {
					sessionFiles = append(sessionFiles, zf)
				}
			}
			for _, zf := range sessionFiles {
				if len(paths) >= 50 {
					return echo.NewHTTPError(http.StatusBadRequest, "解压后文件超 50 个上限, 请分包导入")
				}
				rc, err := zf.Open()
				if err != nil {
					continue
				}
				// R182: 原始文件名放前缀, service 端据此兜底解析手机号
				tmp, err := os.CreateTemp("", tempPrefixFromName(zf.Name)+"*.session")
				if err != nil {
					rc.Close()
					continue
				}
				// P2-50 审计修复: 原来 LimitReader 到限返回 EOF——Copy 视为成功, 超 10MB
				// 的文件被静默截断当完整 session 导入。+1 探测: 超限时拒绝。
				written, err := io.Copy(tmp, io.LimitReader(rc, 10<<20+1))
				if err != nil || written > 10<<20 {
					rc.Close()
					tmp.Close()
					os.Remove(tmp.Name())
					continue
				}
				rc.Close()
				tmp.Close()
				// R182: 同 stem 的 sidecar json 写出(2FA 密码/手机号), service 端 readSidecarJSON 读取
				if stem := sanitizeStem(zf.Name); stem != "" {
					if data, ok := sidecars[stem]; ok {
						_ = os.WriteFile(strings.TrimSuffix(tmp.Name(), ".session")+".json", data, 0600)
					}
				}
				paths = append(paths, tmp.Name())
				zipCount++
			}
			if zipCount == 0 {
				return echo.NewHTTPError(http.StatusBadRequest, "压缩包内未找到 .session 文件")
			}
			continue
		}
		// R182: rar/7z 支持(exec 系统解压工具, 与 zip 同规则: 包 50MB/单文件 10MB/总量 50 个上限)
		if strings.HasSuffix(strings.ToLower(fh.Filename), ".rar") || strings.HasSuffix(strings.ToLower(fh.Filename), ".7z") {
			lowerName := strings.ToLower(fh.Filename)
			ext := filepath.Ext(lowerName)
			data, err := io.ReadAll(io.LimitReader(src, 50<<20))
			src.Close()
			if err != nil {
				return echo.NewHTTPError(http.StatusBadRequest, "读取压缩包失败")
			}
			archiveTmp, err := os.CreateTemp("", "import-archive-*"+ext)
			if err != nil {
				return echo.NewHTTPError(http.StatusInternalServerError, "创建临时文件失败")
			}
			if _, err := archiveTmp.Write(data); err != nil {
				archiveTmp.Close()
				os.Remove(archiveTmp.Name())
				return echo.NewHTTPError(http.StatusInternalServerError, "写入临时包失败")
			}
			archiveTmp.Close()
			outDir, err := extractArchiveCLI(archiveTmp.Name(), ext)
			os.Remove(archiveTmp.Name())
			if err != nil {
				return echo.NewHTTPError(http.StatusBadRequest, ext+" 解压失败: "+err.Error())
			}
			defer os.RemoveAll(outDir)
			arcCount := 0
			walkErr := filepath.Walk(outDir, func(p string, info os.FileInfo, werr error) error {
				if werr != nil || info.IsDir() || !strings.HasSuffix(strings.ToLower(info.Name()), ".session") {
					return nil
				}
				if len(paths) >= 50 {
					return echo.NewHTTPError(http.StatusBadRequest, "解压后文件超 50 个上限, 请分包导入")
				}
				dest, derr := os.CreateTemp("", tempPrefixFromName(info.Name())+"*.session")
				if derr != nil {
					return nil
				}
				in, ierr := os.Open(p)
				if ierr != nil {
					dest.Close()
					os.Remove(dest.Name())
					return nil
				}
				written, cerr := io.Copy(dest, io.LimitReader(in, 10<<20+1))
				in.Close()
				dest.Close()
				if cerr != nil || written > 10<<20 {
					os.Remove(dest.Name())
					return nil
				}
				// R182: 同 stem .json 跟随拷贝(rar/7z 包内 sidecar, 同 zip 规则)
				if jdata, jerr := os.ReadFile(strings.TrimSuffix(p, ".session") + ".json"); jerr == nil {
					_ = os.WriteFile(strings.TrimSuffix(dest.Name(), ".session")+".json", jdata, 0600)
				}
				paths = append(paths, dest.Name())
				arcCount++
				return nil
			})
			if walkErr != nil {
				return walkErr
			}
			if arcCount == 0 {
				return echo.NewHTTPError(http.StatusBadRequest, "压缩包内未找到 .session 文件")
			}
			continue
		}
		// R182.5: 前端 accept 放行了 .tdata, 但后端不支持 TDesktop 格式(与 Telethon session 完全不同),
		// 直接解析只会报"不是有效的 .session 文件"让人摸不着头脑——给明确提示。
		if strings.HasSuffix(strings.ToLower(fh.Filename), ".tdata") {
			return echo.NewHTTPError(http.StatusBadRequest, ".tdata 是 Telegram 桌面版数据目录格式, 暂不支持导入; 请联系号商提供 .session(Telethon 格式)文件")
		}
		// R182: 原始文件名放前缀, service 端据此兜底解析手机号
		tmp, err := os.CreateTemp("", tempPrefixFromName(fh.Filename)+"*.session")
		if err != nil {
			src.Close()
			return echo.NewHTTPError(http.StatusInternalServerError, "创建临时文件失败")
		}
		// P2-50 审计修复: 同 zip 分支——LimitReader 到限返回 EOF 被当成功, 超 10MB
		// 文件被静默截断。+1 探测: 超限拒绝。
		written, err := io.Copy(tmp, io.LimitReader(src, 10<<20+1)) // 单文件 10MB 上限
		if err != nil || written > 10<<20 {
			src.Close()
			tmp.Close()
			os.Remove(tmp.Name())
			return echo.NewHTTPError(http.StatusBadRequest, "文件过大（单文件限 10MB）")
		}
		src.Close()
		tmp.Close()
		paths = append(paths, tmp.Name())
	}
	defer func() {
		for _, p := range paths {
			os.Remove(p)
		}
	}()

	result, err := h.svc.ImportSessionFiles(c.Request().Context(), tenantID, paths, remark)
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, err.Error())
	}
	// F-31: 会话文件导入属高敏感操作（TG 会话进入系统），必须留痕
	safego.Go(func() {
		if err := h.auditRepo.Create(context.Background(), &domain.AuditLog{
			TenantID: tenantID, UserID: "tenant", Action: "account_import",
			Target: fmt.Sprintf("files=%d", len(paths)), IP: c.RealIP(),
		}); err != nil {
			log.Printf("[audit] write failed: %v", err)
		}
	})
	return c.JSON(http.StatusOK, result)
}

type migrateReq struct {
	TargetDC int32 `json:"target_dc"`
}

// MigrateDC POST /api/v1/accounts/:id/migrate — DC迁移(也可由保活自动触发)
func (h *AccountHandler) MigrateDC(c echo.Context) error {
	tenantID, err := middleware.GetTenantID(c)
	if err != nil {
		return err
	}
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "invalid id")
	}
	var req migrateReq
	if err := c.Bind(&req); err != nil || req.TargetDC < 1 || req.TargetDC > 5 {
		return echo.NewHTTPError(http.StatusBadRequest, "target_dc 必须是 1-5")
	}
	if err := h.svc.MigrateAccountDC(c.Request().Context(), tenantID, id, req.TargetDC); err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, err.Error())
	}
	return c.JSON(http.StatusOK, map[string]bool{"ok": true})
}

// CloneProfile handles POST /accounts/clone-profile {"source":"@username","target_ids":[...]}
// 批量克隆他人资料(名/姓/签名)到勾选账号; 用户名不克隆(全局唯一)
func (h *AccountHandler) CloneProfile(c echo.Context) error {
	tenantID, err := middleware.GetTenantID(c)
	if err != nil {
		return err
	}
	var req struct {
		Source    string   `json:"source"`
		TargetIDs []string `json:"target_ids"`
	}
	if err := c.Bind(&req); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "invalid body")
	}
	if req.Source == "" || len(req.TargetIDs) == 0 {
		return echo.NewHTTPError(http.StatusBadRequest, "源用户名和目标账号不能为空")
	}
	ids := make([]uuid.UUID, 0, len(req.TargetIDs))
	for _, s := range req.TargetIDs {
		id, err := uuid.Parse(s)
		if err != nil {
			return echo.NewHTTPError(http.StatusBadRequest, "目标账号 ID 格式错误")
		}
		ids = append(ids, id)
	}
	ok, failed, err := h.svc.CloneProfile(c.Request().Context(), tenantID, req.Source, ids)
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, err.Error())
	}
	return c.JSON(http.StatusOK, map[string]interface{}{"ok": true, "applied": ok, "failed": failed})
}

// SetProxy handles PUT /accounts/:id/proxy {"proxy_url": "socks5://..."} 空串=直连
func (h *AccountHandler) SetProxy(c echo.Context) error {
	tenantID, err := middleware.GetTenantID(c)
	if err != nil {
		return err
	}
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "invalid account id")
	}
	var req struct {
		ProxyURL string `json:"proxy_url"`
	}
	if err := c.Bind(&req); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "invalid body")
	}
	if err := h.svc.SetProxy(c.Request().Context(), tenantID, id, req.ProxyURL); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, err.Error())
	}
	return c.JSON(http.StatusOK, map[string]bool{"ok": true})
}

// TestProxy handles POST /accounts/:id/proxy/test
func (h *AccountHandler) TestProxy(c echo.Context) error {
	tenantID, err := middleware.GetTenantID(c)
	if err != nil {
		return err
	}
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "invalid account id")
	}
	res, err := h.svc.TestProxy(c.Request().Context(), tenantID, id)
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, err.Error())
	}
	return c.JSON(http.StatusOK, res)
}

// GetAvatar 返回账号自己的 TG 头像(需登录, 仅本租户, 未拉取过返回 404)。
// <img> 发不了 Authorization 头, 前端走 axios blob 加载。
// RefreshAvatar 手动刷新头像: POST /accounts/:id/avatar/refresh
func (h *AccountHandler) RefreshAvatar(c echo.Context) error {
	tenantID, terr := middleware.GetTenantID(c)
	if terr != nil {
		return echo.NewHTTPError(http.StatusUnauthorized, "未登录")
	}
	accountID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "无效的账号ID")
	}
	if err := h.svc.RefreshAvatar(c.Request().Context(), tenantID, accountID); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, err.Error())
	}
	return c.JSON(http.StatusOK, map[string]interface{}{"refreshing": true})
}

// Stats 每账号成功/失败/跳过聚合: GET /accounts/stats
func (h *AccountHandler) Stats(c echo.Context) error {
	tenantID, terr := middleware.GetTenantID(c)
	if terr != nil {
		return echo.NewHTTPError(http.StatusUnauthorized, "未登录")
	}
	data, err := h.svc.StatsByAccount(c.Request().Context(), tenantID)
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "统计失败")
	}
	return c.JSON(http.StatusOK, data)
}

// Update2FA 平台内修改二次验证密码: PUT /accounts/:id/2fa
func (h *AccountHandler) Update2FA(c echo.Context) error {
	tenantID, terr := middleware.GetTenantID(c)
	if terr != nil {
		return echo.NewHTTPError(http.StatusUnauthorized, "未登录")
	}
	accountID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "无效的账号ID")
	}
	var req struct {
		Current string `json:"current"`
		Next    string `json:"next"`
	}
	if err := c.Bind(&req); err != nil || req.Next == "" {
		return echo.NewHTTPError(http.StatusBadRequest, "参数错误: 需 current(可为空) 和 next")
	}
	if err := h.svc.Update2FA(c.Request().Context(), tenantID, accountID, req.Current, req.Next); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, err.Error())
	}
	return c.JSON(http.StatusOK, map[string]interface{}{"updated": true})
}

// BatchUpdateProfile 批量改资料: POST /accounts/profile-batch
func (h *AccountHandler) BatchUpdateProfile(c echo.Context) error {
	tenantID, terr := middleware.GetTenantID(c)
	if terr != nil {
		return echo.NewHTTPError(http.StatusUnauthorized, "未登录")
	}
	var req struct {
		AccountIDs      []string `json:"account_ids"`
		FirstName       string   `json:"first_name"`
		LastName        string   `json:"last_name"`
		About           string   `json:"about"`
		Username        string   `json:"username"`
		Remark          string   `json:"remark"`
		RandomUsername  bool     `json:"random_username"`
	}
	if err := c.Bind(&req); err != nil || len(req.AccountIDs) == 0 {
		return echo.NewHTTPError(http.StatusBadRequest, "参数错误")
	}
	ids := make([]uuid.UUID, 0, len(req.AccountIDs))
	for _, s := range req.AccountIDs {
		id, err := uuid.Parse(s)
		if err != nil {
			return echo.NewHTTPError(http.StatusBadRequest, "无效的账号ID: "+s)
		}
		ids = append(ids, id)
	}
	res, err := h.svc.BatchUpdateProfile(c.Request().Context(), tenantID, ids,
		req.FirstName, req.LastName, req.About, req.Username, req.Remark, req.RandomUsername)
	if err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, err.Error())
	}
	return c.JSON(http.StatusOK, res)
}

// BatchSetPool 批量切换启用/备用: POST /accounts/pool-batch
func (h *AccountHandler) BatchSetPool(c echo.Context) error {
	tenantID, terr := middleware.GetTenantID(c)
	if terr != nil {
		return echo.NewHTTPError(http.StatusUnauthorized, "未登录")
	}
	var req struct {
		AccountIDs []string `json:"account_ids"`
		Status     string   `json:"status"`
	}
	if err := c.Bind(&req); err != nil || len(req.AccountIDs) == 0 {
		return echo.NewHTTPError(http.StatusBadRequest, "参数错误")
	}
	ids := make([]uuid.UUID, 0, len(req.AccountIDs))
	for _, s := range req.AccountIDs {
		id, err := uuid.Parse(s)
		if err != nil {
			return echo.NewHTTPError(http.StatusBadRequest, "无效的账号ID: "+s)
		}
		ids = append(ids, id)
	}
	res, err := h.svc.BatchSetPool(c.Request().Context(), tenantID, ids, req.Status)
	if err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, err.Error())
	}
	return c.JSON(http.StatusOK, res)
}

// SetPool 切换账号 启用/备用: PUT /accounts/:id/pool {"status": "active"|"standby"}
func (h *AccountHandler) SetPool(c echo.Context) error {
	tenantID, terr := middleware.GetTenantID(c)
	if terr != nil {
		return echo.NewHTTPError(http.StatusUnauthorized, "未登录")
	}
	accountID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "无效的账号ID")
	}
	var req struct {
		Status string `json:"status"`
	}
	if err := c.Bind(&req); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "参数错误")
	}
	if err := h.svc.SetPoolStatus(c.Request().Context(), tenantID, accountID, req.Status); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, err.Error())
	}
	return c.JSON(http.StatusOK, map[string]interface{}{"status": req.Status})
}

// Reconnect 死号重连(存 session 直连, 不用验证码): POST /accounts/:id/reconnect
func (h *AccountHandler) Reconnect(c echo.Context) error {
	tenantID, terr := middleware.GetTenantID(c)
	if terr != nil {
		return echo.NewHTTPError(http.StatusUnauthorized, "未登录")
	}
	accountID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "无效的账号ID")
	}
	if err := h.svc.Reconnect(c.Request().Context(), tenantID, accountID); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, err.Error())
	}
	return c.JSON(http.StatusOK, map[string]interface{}{"reconnecting": true})
}

// SetRaffle 切换单账号抽奖参与开关(改进#1): PUT /accounts/:id/raffle {"enabled": bool}
func (h *AccountHandler) SetRaffle(c echo.Context) error {
	tenantID, terr := middleware.GetTenantID(c)
	if terr != nil {
		return echo.NewHTTPError(http.StatusUnauthorized, "未登录")
	}
	accountID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "无效的账号ID")
	}
	var req struct {
		Enabled bool `json:"enabled"`
	}
	if err := c.Bind(&req); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "参数错误")
	}
	if err := h.svc.SetRaffleEnabled(c.Request().Context(), tenantID, accountID, req.Enabled); err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "设置失败")
	}
	return c.JSON(http.StatusOK, map[string]interface{}{"enabled": req.Enabled})
}

// TransferGroups R181 群转移: 把源账号已加入的群导出邀请链接,
// 用目标账号创建标准 join_groups 任务(间隔/抖动/Flood熔断/任务面板全复用)。
// POST /accounts/:id/transfer-groups {"target_account_id": "..."}
func (h *AccountHandler) TransferGroups(c echo.Context) error {
	tenantID, terr := middleware.GetTenantID(c)
	if terr != nil {
		return echo.NewHTTPError(http.StatusUnauthorized, "未登录")
	}
	sourceID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "无效的账号ID")
	}
	if h.taskSvc == nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "任务服务未就绪")
	}

	var req struct {
		TargetAccountID string `json:"target_account_id"`
		// 随机间隔(分钟): 每加一群在 [min,max] 内随机取值, 行为不可预测, 反风控。
		// 缺省 2~3(沿用全局默认值); 新注册/低权重号建议 5~8。
		IntervalMinMinutes int `json:"interval_min_minutes"`
		IntervalMaxMinutes int `json:"interval_max_minutes"`
	}
	if err := c.Bind(&req); err != nil || req.TargetAccountID == "" {
		return echo.NewHTTPError(http.StatusBadRequest, "参数错误: 需 target_account_id")
	}
	// 默认间隔 2~3 分钟; 只传一个值时退化为固定间隔
	intervalMin, intervalMax := req.IntervalMinMinutes, req.IntervalMaxMinutes
	if intervalMin <= 0 && intervalMax <= 0 {
		intervalMin, intervalMax = 2, 3
	} else if intervalMax <= 0 {
		intervalMax = intervalMin
	} else if intervalMin <= 0 {
		intervalMin = intervalMax
	}
	targetID, err := uuid.Parse(req.TargetAccountID)
	if err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "目标账号ID格式错误")
	}
	if sourceID == targetID {
		return echo.NewHTTPError(http.StatusBadRequest, "源账号和目标账号不能是同一个")
	}

	ctx := c.Request().Context()

	// 目标账号校验: 必须属于本租户且未被封/受限(否则任务必死)
	target, err := h.svc.GetByID(ctx, tenantID, targetID)
	if err != nil || target == nil {
		return echo.NewHTTPError(http.StatusBadRequest, "目标账号不存在或不属于当前租户")
	}
	if target.Status == domain.AccountStatusBanned {
		return echo.NewHTTPError(http.StatusBadRequest, "目标账号已被封禁, 不能作为转移目标")
	}

	// 第一步: 源账号导出邀请链接
	export, err := h.svc.ExportTransferableGroups(ctx, tenantID, sourceID)
	if err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, err.Error())
	}
	if len(export.Links) == 0 {
		return c.JSON(http.StatusOK, map[string]interface{}{
			"task":        nil,
			"links_total": 0,
			"skipped":     export.Skipped,
			"message":     "没有可转移的群(全部在跳过清单里)",
		})
	}

	// 第二步: 目标账号创建标准加群任务(租户可指定随机间隔, 默认2~3分钟)
	task, err := h.taskSvc.CreateJoinGroupsWithInterval(ctx, tenantID, targetID, export.Links, intervalMin, intervalMax)
	if err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "创建转移任务失败: "+err.Error())
	}

	if h.auditRepo != nil {
		_ = h.auditRepo.Create(ctx, &domain.AuditLog{
			LogID:     uuid.New(),
			TenantID:  tenantID,
			UserID:    tenantID.String(),
			Action:    "transfer_groups",
			Target:    fmt.Sprintf("source=%s target=%s groups=%d", sourceID, targetID, len(export.Links)),
			IP:        c.RealIP(),
			CreatedAt: time.Now(),
		})
	}

	return c.JSON(http.StatusOK, map[string]interface{}{
		"task":        task,
		"links_total": len(export.Links),
		"skipped":     export.Skipped,
	})
}

func (h *AccountHandler) GetAvatar(c echo.Context) error {
	tenantID, err := middleware.GetTenantID(c)
	if err != nil {
		return err
	}
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "invalid account id")
	}
	p, err := h.svc.GetAvatarPath(c.Request().Context(), tenantID, id)
	if err != nil {
		return echo.NewHTTPError(http.StatusNotFound)
	}
	c.Response().Header().Set(echo.HeaderCacheControl, "private, max-age=3600")
	return c.File(p)
}
