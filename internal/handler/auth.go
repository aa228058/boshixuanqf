package handler

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"log"
	"net/http"
	"strings"
	"sync"
	"tg-sender/internal/safego"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/labstack/echo/v4"
	"golang.org/x/crypto/bcrypt"
	"tg-sender/internal/config"
	"tg-sender/internal/domain"
	"tg-sender/internal/middleware"
	"tg-sender/internal/repository"
)

type RegisterRequest struct {
	Username string `json:"username" validate:"required,min=3,max=50"`
	Password string `json:"password" validate:"required,min=6"`
	Name     string `json:"name" validate:"required"`
	Code     string `json:"code" validate:"required"`
}

type LoginRequest struct {
	Username string `json:"username" validate:"required"`
	Password string `json:"password" validate:"required"`
}

type RefreshTokenRequest struct {
	RefreshToken string `json:"refresh_token" validate:"required"`
}

var (
	apiKeyMutex sync.Mutex
)

type AuthHandler struct {
	cfg         *config.Config
	db          *pgxpool.Pool
	tenantRepo  domain.TenantRepository
	codeRepo    domain.ActivationCodeRepository
	planRepo    domain.PlanRepository
	refreshRepo domain.RefreshTokenRepository
	auditRepo   *repository.AuditRepository
	taskEngine interface {
		RegisterTenantQuota(tenantID [16]byte, maxConcurrent int)
	}
}


func NewAuthHandler(
	cfg *config.Config,
	db *pgxpool.Pool,
	tenantRepo domain.TenantRepository,
	codeRepo domain.ActivationCodeRepository,
	planRepo domain.PlanRepository,
	refreshRepo domain.RefreshTokenRepository,
	auditRepo *repository.AuditRepository,
	taskEngine interface {
		RegisterTenantQuota(tenantID [16]byte, maxConcurrent int)
	},
) *AuthHandler {
	return &AuthHandler{
		cfg:         cfg,
		db:          db,
		tenantRepo:  tenantRepo,
		codeRepo:    codeRepo,
		planRepo:    planRepo,
		refreshRepo: refreshRepo,
		auditRepo:   auditRepo,
	}
}

func (h *AuthHandler) Register(c echo.Context) error {
	ctx := c.Request().Context()
	var req RegisterRequest
	if err := c.Bind(&req); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, err.Error())
	}
	if req.Username == "" || len(req.Username) < 3 || len(req.Username) > 50 {
		return echo.NewHTTPError(http.StatusBadRequest, "用户名长度为3-50个字符")
	}
	if req.Password == "" || len(req.Password) < 6 {
		return echo.NewHTTPError(http.StatusBadRequest, "密码至少6个字符")
	}
	// F-09: bcrypt 只取前 72 字节，超长密码不同后缀哈希相同，必须显式拒绝
	if len([]byte(req.Password)) > 72 {
		return echo.NewHTTPError(http.StatusBadRequest, "密码过长（最多72字节）")
	}
	if req.Name == "" {
		return echo.NewHTTPError(http.StatusBadRequest, "昵称不能为空")
	}
	if req.Code == "" {
		return echo.NewHTTPError(http.StatusBadRequest, "卡密不能为空")
	}

	// 验证卡密
	code, err := h.codeRepo.GetByCode(ctx, req.Code)
	if err != nil || code == nil {
		return echo.NewHTTPError(http.StatusBadRequest, "无效的卡密")
	}
	if code.Status != "unused" {
		return echo.NewHTTPError(http.StatusBadRequest, "卡密已被使用")
	}
	// 检查卡密是否过期（immediate 和 on_use 类型都可能有过期时间）
	if code.ExpiresAt != nil && code.ExpiresAt.Before(time.Now()) {
		return echo.NewHTTPError(http.StatusBadRequest, "卡密已过期")
	}

	// 获取套餐信息
	plan, err := h.planRepo.GetByID(ctx, code.PlanID)
	if err != nil || plan == nil {
		return echo.NewHTTPError(http.StatusBadRequest, "卡密对应的套餐不存在")
	}

	// F-09: cost 12（OWASP 建议），原 DefaultCost=10
	hashedPassword, err := bcrypt.GenerateFromPassword([]byte(req.Password), 12)
	if err != nil {
		log.Printf("[auth] 密码哈希失败: %v", err)
		return echo.NewHTTPError(http.StatusInternalServerError, "注册失败")
	}

	// P2-40 审计修复: API key 生成+哈希(成本12约250ms,无共享状态)移出全局锁,
	// 注册不再全局串行。MarkUsed 本身是 UPDATE...WHERE status='unused' 的原子
	// 操作, 双花由 DB 兜底, 不依赖此锁——锁只保留给卡密/租户创建的短临界区。
	apiKey := generateAPIKey()
	hashedAPIKey, err := bcrypt.GenerateFromPassword([]byte(apiKey), 12)
	if err != nil {
		log.Printf("[auth] API密钥哈希失败: %v", err)
		return echo.NewHTTPError(http.StatusInternalServerError, "注册失败")
	}

	apiKeyMutex.Lock()
	defer apiKeyMutex.Unlock()

	// 锁内重校卡密: 锁外排队期间卡密可能已被其他请求消耗(原实现靠 MarkUsed
	// 失败后回滚兜底, 会白建租户)。提前拦截省掉回滚路径。
	if fresh, ferr := h.codeRepo.GetByCode(ctx, req.Code); ferr != nil || fresh == nil || fresh.Status != "unused" {
		return echo.NewHTTPError(http.StatusConflict, "卡密刚被他人使用，请换一张")
	}

	// 计算过期时间
	var expiresAt *time.Time
	if code.TimingType == "on_use" {
		// 使用后才计时：统一30天（免费套餐已下线）
		exp := time.Now().Add(30 * 24 * time.Hour)
		expiresAt = &exp
	} else if code.ExpiresAt != nil {
		// 即时计时：使用卡密生成时设置的过期时间
		expiresAt = code.ExpiresAt
	}

	tenant := &domain.Tenant{
		TenantID:     uuid.New(),
		Name:         req.Name,
		Username:     req.Username,
		PasswordHash: string(hashedPassword),
		APIKeyHash:   string(hashedAPIKey),
		// 注意：api_key_prefix 由 repository.Create 用明文统一计算，
		// 不要在这里自行截断 apiKey[:8]（两种口径曾导致查询永远失配）
		MaxAccounts:        plan.MaxAccounts,
		MaxConcurrentTasks: plan.MaxConcurrent,
		PlanID:             code.PlanID,
		Status:             "active",
		ExpiresAt:          expiresAt,
	}

	// 先创建租户，再核销卡密。顺序不能反：
	// activation_codes.used_by 外键指向 tenants(tenant_id)，若先核销，
	// UPDATE 写入的 used_by 在 tenants 中尚不存在，必然违反外键，
	// 表现为注册报"卡密无效或已被使用"，而管理端看卡密仍是"未使用"。
	// 并发安全：同卡两个请求各自建租户(不同 tenant_id)，只有一个 MarkUsed
	// 能成功，失败者把刚建的租户标记删除；同用户名在 Create 阶段撞唯一
	// 约束直接 409，根本走不到核销，不会双花。
	if err := h.tenantRepo.Create(ctx, tenant, apiKey); err != nil {
		if isDuplicateError(err) {
			return echo.NewHTTPError(http.StatusConflict, "用户名已存在")
		}
		log.Printf("[auth] 创建租户失败: %v", err)
		return echo.NewHTTPError(http.StatusInternalServerError, "注册失败")
	}

	marked, err := h.codeRepo.MarkUsed(ctx, code.CodeID, tenant.TenantID)
	if err != nil || !marked {
		// 卡密无效/被并发抢占：回滚刚创建的租户，避免孤儿数据
		if rerr := h.tenantRepo.Update(context.Background(), tenant.TenantID, map[string]any{"status": "deleted"}); rerr != nil {
			log.Printf("[auth] 卡密核销失败后回滚租户失败 tenant=%s: %v", tenant.TenantID, rerr)
		}
		log.Printf("[auth] 卡密核销失败 code=%s err=%v", code.Code, err)
		return echo.NewHTTPError(http.StatusBadRequest, "卡密无效或已被使用")
	}

	// P1-02: 新租户注册后立即注册调度器配额(不用等重启)
	if h.taskEngine != nil {
		h.taskEngine.RegisterTenantQuota(tenant.TenantID, plan.MaxConcurrent)
	}

	return c.JSON(http.StatusCreated, map[string]interface{}{
		"message":   "注册成功",
		"tenant_id": tenant.TenantID,
		"api_key":   apiKey,
	})
}

// Login godoc
// @Summary 租户登录
// @Description 使用用户名/密码登录，返回JWT Token
// @Tags 认证
// @Accept json
// @Produce json
// @Param body body LoginRequest true "登录请求"
// @Success 200 {object} LoginResponse
// @Failure 400 {object} ErrorResponse
// @Failure 401 {object} ErrorResponse
// @Failure 403 {object} ErrorResponse
// @Failure 500 {object} ErrorResponse
// @Router /auth/login [post]
func (h *AuthHandler) Login(c echo.Context) error {
	var req LoginRequest
	if err := c.Bind(&req); err != nil {
		log.Printf("[handler] 登录请求解析失败: %v", err)
		return echo.NewHTTPError(http.StatusBadRequest, "请求参数错误")
	}
	if req.Username == "" || req.Password == "" {
		return echo.NewHTTPError(http.StatusBadRequest, "用户名和密码不能为空")
	}

	// Lookup tenant by username
	tenant, err := h.tenantRepo.GetByUsername(c.Request().Context(), req.Username)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return echo.NewHTTPError(http.StatusUnauthorized, "用户名或密码错误")
		}
		log.Printf("[handler] 登录查询用户失败: %v", err)
		return echo.NewHTTPError(http.StatusInternalServerError, "internal server error")
	}

	// Verify password only. API Key is NOT accepted as a password:
	// API keys typically carry broader permissions, and accepting them here
	// means a leaked key = full account takeover via password login.
	// API keys must be used via the dedicated Bearer middleware instead.
	if err := bcrypt.CompareHashAndPassword([]byte(tenant.PasswordHash), []byte(req.Password)); err != nil {
		return echo.NewHTTPError(http.StatusUnauthorized, "用户名或密码错误")
	}

	// Check tenant expiration
	if tenant.ExpiresAt != nil && tenant.ExpiresAt.Before(time.Now()) {
		return echo.NewHTTPError(http.StatusForbidden, "租户订阅已过期")
	}

	// Check tenant status
	if tenant.Status != "active" {
		return echo.NewHTTPError(http.StatusForbidden, "租户未激活")
	}

	token, err := middleware.GenerateToken(h.cfg, tenant.TenantID.String())
	if err != nil {
		log.Printf("[handler] Generate[REDACTED] failed: %v", err)
		return echo.NewHTTPError(http.StatusInternalServerError, "internal server error")
	}

	refreshToken, jti, err := middleware.GenerateRefreshToken(h.cfg, tenant.TenantID.String())
	if err != nil {
		log.Printf("[handler] GenerateRefresh[REDACTED] failed: %v", err)
		return echo.NewHTTPError(http.StatusInternalServerError, "internal server error")
	}

	// Store refresh token in DB
	parsedJTI, err := uuid.Parse(jti)
	if err != nil {
		log.Printf("[handler] JTI格式无效: %v", err)
		return echo.NewHTTPError(http.StatusInternalServerError, "internal server error")
	}
	if err := h.refreshRepo.Create(c.Request().Context(), parsedJTI, tenant.TenantID, time.Now().Add(h.cfg.JWTRefreshExp)); err != nil {
		log.Printf("[handler] StoreRefresh[REDACTED] failed: %v", err)
		return echo.NewHTTPError(http.StatusInternalServerError, "internal server error")
	}

	// Set refresh token in HTTP-only cookie
	// Secure 按请求 scheme：HTTP 部署下强 Secure 浏览器拒存 cookie，
	// F-20 内存 token 模式会每次刷新都被踢回登录页
	cookie := &http.Cookie{
		Name:     "refresh_token",
		Value:    refreshToken,
		Path:     "/",
		HttpOnly: true,
		Secure:   c.Scheme() == "https",
		SameSite: http.SameSiteStrictMode,
		MaxAge:   int(h.cfg.JWTRefreshExp.Seconds()),
	}
	c.SetCookie(cookie)

	// 审计: 记录租户登录 (合规底线, 谁/何时/从哪个IP)
	// 异步写: 审计失败绝不阻塞登录响应; 必须用独立 context —
	// 请求 context 在 handler 返回后即取消, goroutine 里再用来写库必失败
	if h.auditRepo != nil {
		auditTenantID := tenant.TenantID
		auditUsername := req.Username
		auditIP := c.RealIP()
		safego.Go(func() {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			_ = h.auditRepo.Create(ctx, &domain.AuditLog{
				LogID:     uuid.New(),
				TenantID:  auditTenantID,
				UserID:    auditTenantID.String(),
				Action:    "tenant_login",
				Target:    auditUsername,
				IP:        auditIP,
				CreatedAt: time.Now(),
			})
		})
	}

	// 获取套餐功能权限
	planFeatures := map[string]any{
		"broadcast": false, "scrape_members": false, "scrape_links": false,
		"join_groups": false, "monitor": false, "export": false,
		"templates": false, "scheduled_tasks": false,
	}
	if h.planRepo != nil {
		if plan, err := h.planRepo.GetByID(c.Request().Context(), tenant.PlanID); err == nil && plan != nil {
			// 权限单一数据源: 管理端套餐矩阵(features JSONB); 空时兜底老布尔列
			planFeatures = plan.FeatureMap()
		}
	}

	return Success(c, map[string]any{
		"token":                token,
		"tenant_id":            tenant.TenantID.String(),
		"tenant_name":          tenant.Name,
		"plan_id":              tenant.PlanID,
		"plan_features":        planFeatures,
		"status":               tenant.Status,
		"expires_at":           tenant.ExpiresAt,
		"max_accounts":         tenant.MaxAccounts,
		"max_concurrent_tasks": tenant.MaxConcurrentTasks,
	})
}

// RefreshRequest represents a token refresh request.
type RefreshRequest struct {
	RefreshToken string `json:"refresh_token" validate:"required"`
}

// RefreshResponse represents a token refresh response.
type RefreshResponse struct {
	Token        string `json:"token"`
	RefreshToken string `json:"refresh_token"`
}

// Refresh godoc
// @Summary 刷新Token
// @Description 使用refresh_token换取新的access_token和refresh_token
// @Tags 认证
// @Accept json
// @Produce json
// @Param body body RefreshRequest true "刷新请求(Cookie优先)"
// @Success 200 {object} RefreshResponse
// @Failure 400 {object} ErrorResponse
// @Failure 401 {object} ErrorResponse
// @Failure 500 {object} ErrorResponse
// @Router /auth/refresh [post]
func (h *AuthHandler) Refresh(c echo.Context) error {
	var refreshToken string

	// 1. Try HTTP-only cookie first
	cookie, err := c.Cookie("refresh_token")
	if err == nil && cookie.Value != "" {
		refreshToken = cookie.Value
	}

	// 2. Fallback to JSON body
	if refreshToken == "" {
		var req RefreshRequest
		if err := c.Bind(&req); err != nil {
			log.Printf("[handler] 刷新令牌请求解析失败: %v", err)
			return echo.NewHTTPError(http.StatusBadRequest, "请求参数错误")
		}
		refreshToken = req.RefreshToken
	}

	if refreshToken == "" {
		return echo.NewHTTPError(http.StatusBadRequest, "refresh_token required")
	}

	tenantID, jti, err := middleware.ParseRefreshToken(h.cfg, refreshToken)
	if err != nil {
		log.Printf("[handler] ParseRefresh[REDACTED] failed: %v", err)
		return echo.NewHTTPError(http.StatusUnauthorized, "invalid credentials")
	}

	parsedJTI, err := uuid.Parse(jti)
	if err != nil {
		log.Printf("[handler] JTI格式无效: %v", err)
		return echo.NewHTTPError(http.StatusUnauthorized, "invalid token format")
	}
	// V16 审计修复: 重排轮换顺序——原来是"先 Validate(原子删旧) 再签发新 token",
	// 中间任何一步失败(租户查询/状态检查/签发/入库)旧 token 都已删除, 用户被误登出。
	// 现改为: 先非消费预检(Peek) → 租户检查 → 签发并入库新 token → 最后 Validate 原子消费旧 token。
	// 失败安全性: Peek 前失败不动任何 token; 签发阶段失败旧 token 仍在(用户保持登录);
	// Validate 失败仅发生在并发竞争(另一请求已消费)或 token 已失效——此时返回 401 是正确行为。
	parsedTenantID, err := h.refreshRepo.Peek(c.Request().Context(), parsedJTI)
	if err != nil {
		log.Printf("[handler] Refresh [REDACTED] peek failed: %v", err)
		return echo.NewHTTPError(http.StatusUnauthorized, "invalid or expired refresh token")
	}

	// Check tenant status and expiration before issuing new tokens
	parsedUUID, err := uuid.Parse(tenantID)
	if err != nil {
		log.Printf("[handler] 租户ID格式无效: %v", err)
		return echo.NewHTTPError(http.StatusUnauthorized, "invalid token")
	}
	tenant, err := h.tenantRepo.GetByID(c.Request().Context(), parsedUUID)
	if err != nil {
		log.Printf("[handler] 刷新时查询租户失败: %v", err)
		return echo.NewHTTPError(http.StatusUnauthorized, "tenant not found")
	}
	if tenant.Status != "active" {
		return echo.NewHTTPError(http.StatusForbidden, "tenant account suspended")
	}
	if tenant.ExpiresAt != nil && tenant.ExpiresAt.Before(time.Now()) {
		return echo.NewHTTPError(http.StatusForbidden, "tenant subscription expired")
	}

	newToken, err := middleware.GenerateToken(h.cfg, tenantID)
	if err != nil {
		log.Printf("[handler] Generate[REDACTED] (refresh) failed: %v", err)
		return echo.NewHTTPError(http.StatusInternalServerError, "internal server error")
	}

	newRefreshToken, newJTI, err := middleware.GenerateRefreshToken(h.cfg, tenantID)
	if err != nil {
		log.Printf("[handler] GenerateRefresh[REDACTED] (refresh) failed: %v", err)
		return echo.NewHTTPError(http.StatusInternalServerError, "internal server error")
	}

	parsedNewJTI, err := uuid.Parse(newJTI)
	if err != nil {
		log.Printf("[handler] 新JTI格式无效: %v", err)
		return echo.NewHTTPError(http.StatusBadRequest, "invalid token")
	}
	if err := h.refreshRepo.Create(c.Request().Context(), parsedNewJTI, parsedUUID, time.Now().Add(h.cfg.JWTRefreshExp)); err != nil {
		log.Printf("[handler] Store new refresh [REDACTED] failed: %v", err)
		return echo.NewHTTPError(http.StatusInternalServerError, "internal server error")
	}

	// 最后一步才原子消费旧 token(原 Revoke 调用已删: Validate 是 DELETE 消费, 行已不存在,
	// 再 Revoke 只对已删除的行 UPDATE 0 行, 属死代码)。
	parsedTenantID, err = h.refreshRepo.Validate(c.Request().Context(), parsedJTI)
	if err != nil {
		log.Printf("[handler] Refresh [REDACTED] consume failed: %v", err)
		return echo.NewHTTPError(http.StatusUnauthorized, "invalid or expired refresh token")
	}
	if parsedTenantID.String() != tenantID {
		return echo.NewHTTPError(http.StatusUnauthorized, "token mismatch")
	}

	newCookie := &http.Cookie{
		Name:     "refresh_token",
		Value:    newRefreshToken,
		Path:     "/",
		HttpOnly: true,
		Secure:   c.Scheme() == "https",
		SameSite: http.SameSiteStrictMode,
		MaxAge:   int(h.cfg.JWTRefreshExp.Seconds()),
	}
	c.SetCookie(newCookie)

	return Success(c, map[string]any{
		"token":         newToken,
		"refresh_token": newRefreshToken,
	})
}

// Logout revokes the current access token and clears the refresh token cookie.
func (h *AuthHandler) Logout(c echo.Context) error {
	// F-02: 无论请求是否携带合法 token，都必须清掉 refresh cookie，
	// 否则前端调用 logout 后浏览器仍保留有效 refresh token。
	defer func() {
		c.SetCookie(&http.Cookie{
			Name:     "refresh_token",
			Value:    "",
			Path:     "/",
			HttpOnly: true,
			Secure:   c.Scheme() == "https",
			SameSite: http.SameSiteStrictMode,
			MaxAge:   -1,
		})
	}()

	auth := c.Request().Header.Get("Authorization")
	if auth == "" {
		return Success(c, map[string]string{"message": "logged out"})
	}
	parts := strings.SplitN(auth, " ", 2)
	if len(parts) != 2 || strings.ToLower(parts[0]) != "bearer" {
		return Success(c, map[string]string{"message": "logged out"})
	}

	token, err := jwt.ParseWithClaims(parts[1], &middleware.JWTClaims{}, func(token *jwt.Token) (interface{}, error) {
		return []byte(h.cfg.JWTSecret), nil
	}, jwt.WithValidMethods([]string{"HS256"}),
		jwt.WithIssuer(middleware.TokenIssuer), jwt.WithAudience(middleware.TokenAudience)) // F-03
	if err != nil || !token.Valid {
		return Success(c, map[string]string{"message": "logged out"})
	}

	claims, ok := token.Claims.(*middleware.JWTClaims)
	if !ok || claims.ID == "" {
		return Success(c, map[string]string{"message": "logged out"})
	}

	tenantID, _ := uuid.Parse(claims.TenantID)
	if err := middleware.RevokeToken(h.db, claims.ID, tenantID, claims.ExpiresAt.Time); err != nil {
		log.Printf("[handler] revoke token failed: %v", err)
	}

	// F-02: 登出必须同时撤销该租户全部 refresh token，否则 refresh 接口
	// 可在"登出"后继续签发 access token，登出形同虚设。
	if err := h.refreshRepo.RevokeAllForTenant(c.Request().Context(), tenantID); err != nil {
		log.Printf("[handler] revoke refresh tokens failed: %v", err)
	}

	return Success(c, map[string]string{"message": "logged out"})
}

// Me returns the current authenticated tenant info.
func (h *AuthHandler) Me(c echo.Context) error {
	// Extract tenant ID from JWT context (set by JWTAuth middleware)
	tenantID, ok := c.Get("tenant_id").(string)
	if !ok || tenantID == "" {
		return echo.NewHTTPError(http.StatusUnauthorized, "未登录")
	}

	id, err := uuid.Parse(tenantID)
	if err != nil {
		return echo.NewHTTPError(http.StatusUnauthorized, "无效的租户ID")
	}

	tenant, err := h.tenantRepo.GetByID(c.Request().Context(), id)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return echo.NewHTTPError(http.StatusUnauthorized, "租户不存在")
		}
		log.Printf("[auth] 查询租户失败: %v", err)
		return echo.NewHTTPError(http.StatusInternalServerError, "查询失败")
	}

	// 检查租户状态和过期时间
	if tenant.Status != "active" {
		return echo.NewHTTPError(http.StatusForbidden, "tenant account suspended")
	}
	if tenant.ExpiresAt != nil && tenant.ExpiresAt.Before(time.Now()) {
		return echo.NewHTTPError(http.StatusForbidden, "tenant subscription expired")
	}

	// 获取套餐功能权限
	planFeatures := map[string]any{
		"broadcast": false, "scrape_members": false, "scrape_links": false,
		"join_groups": false, "monitor": false, "export": false,
		"templates": false, "scheduled_tasks": false,
	}
	if h.planRepo != nil {
		if plan, err := h.planRepo.GetByID(c.Request().Context(), tenant.PlanID); err == nil && plan != nil {
			// 权限单一数据源: 管理端套餐矩阵(features JSONB); 空时兜底老布尔列
			planFeatures = plan.FeatureMap()
		}
	}

	return Success(c, map[string]any{
		"tenant_id":            tenant.TenantID.String(),
		"tenant_name":          tenant.Name,
		"username":             tenant.Username,
		"plan_id":              tenant.PlanID,
		"plan_features":        planFeatures,
		"status":               tenant.Status,
		"expires_at":           tenant.ExpiresAt,
		"max_accounts":         tenant.MaxAccounts,
		"max_concurrent_tasks": tenant.MaxConcurrentTasks,
	})
}

// generateAPIKey generates a random API key.
func generateAPIKey() string {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		// Fallback to UUID-based key
		return uuid.New().String() + uuid.New().String()
	}
	return hex.EncodeToString(b)
}

// isDuplicateError checks if a DB error is a unique constraint violation.
func isDuplicateError(err error) bool {
	if err == nil {
		return false
	}
	errStr := err.Error()
	return strings.Contains(errStr, "unique constraint") ||
		strings.Contains(errStr, "duplicate key") ||
		strings.Contains(errStr, "23505") ||
		strings.Contains(errStr, "UNIQUE constraint failed")
}

// RenewRequest 卡密续费请求
type RenewRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
	Code     string `json:"code"`
}

// Renew 租户自助续费：验证账号密码 + 卡密，延长订阅并同步套餐额度。
// 到期账号无需管理员介入，前端在登录页"订阅已过期"提示下提供续费入口。
func (h *AuthHandler) Renew(c echo.Context) error {
	ctx := c.Request().Context()
	var req RenewRequest
	if err := c.Bind(&req); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, err.Error())
	}
	if req.Username == "" || req.Password == "" || req.Code == "" {
		return echo.NewHTTPError(http.StatusBadRequest, "用户名、密码、卡密均不能为空")
	}

	// 1. 验证账号密码（不区分是否过期——过期账号正是续费对象）
	tenant, err := h.tenantRepo.GetByUsername(ctx, req.Username)
	if err != nil || tenant == nil {
		return echo.NewHTTPError(http.StatusUnauthorized, "用户名或密码错误")
	}
	if err := bcrypt.CompareHashAndPassword([]byte(tenant.PasswordHash), []byte(req.Password)); err != nil {
		return echo.NewHTTPError(http.StatusUnauthorized, "用户名或密码错误")
	}

	// 2. 验证卡密（与注册同一套规则）
	code, err := h.codeRepo.GetByCode(ctx, req.Code)
	if err != nil || code == nil {
		return echo.NewHTTPError(http.StatusBadRequest, "无效的卡密")
	}
	if code.ExpiresAt != nil && code.ExpiresAt.Before(time.Now()) {
		return echo.NewHTTPError(http.StatusBadRequest, "卡密已过期")
	}

	plan, err := h.planRepo.GetByID(ctx, code.PlanID)
	if err != nil || plan == nil {
		return echo.NewHTTPError(http.StatusBadRequest, "卡密对应的套餐不存在")
	}

	// 2.5 原子核销卡密(UPDATE ... WHERE status='unused', 抢占失败=并发被别人用了)
	// 必须先抢卡再续费——之前是"续费完才核销", 两张并发请求能同时通过检查, 一张卡密刷两次。
	marked, err := h.codeRepo.MarkUsed(ctx, code.CodeID, tenant.TenantID)
	if err != nil || !marked {
		return echo.NewHTTPError(http.StatusBadRequest, "卡密无效或已被使用")
	}

	// 3. 计算新过期时间：从"当前到期时间"续（没过期就从现在续），已过期则从现在起算
	base := time.Now()
	if tenant.ExpiresAt != nil && tenant.ExpiresAt.After(base) {
		base = *tenant.ExpiresAt
	}
	var newExpiry time.Time
	if code.TimingType == "on_use" {
		days := 5
		if code.PlanID != "free" {
			days = 30
		}
		newExpiry = base.Add(time.Duration(days) * 24 * time.Hour)
	} else if code.ExpiresAt != nil {
		// 即时计时：沿用卡密预设的到期时间（与注册逻辑一致）
		newExpiry = *code.ExpiresAt
	} else {
		newExpiry = base.AddDate(0, 1, 0)
	}

	// 4. 更新租户：过期时间 + 同步新套餐额度（续费即可能升级套餐）
	updates := map[string]any{
		"expires_at":           newExpiry,
		"plan_id":              code.PlanID,
		"max_accounts":         plan.MaxAccounts,
		"max_concurrent_tasks": plan.MaxConcurrent,
		"status":               "active",
	}
	if err := h.tenantRepo.Update(ctx, tenant.TenantID, updates); err != nil {
		// 修复：续费失败退还卡密（与注册失败补偿同一语义，防止"卡密已烧但租户未续"）
		if _, rerr := h.codeRepo.UnmarkUsed(ctx, code.CodeID, tenant.TenantID); rerr != nil {
			log.Printf("[auth] 续费失败退还卡密失败 code=%s: %v", code.Code, rerr)
		}
		log.Printf("[auth] 续费更新租户失败: %v", err)
		return echo.NewHTTPError(http.StatusInternalServerError, "续费失败，请稍后重试")
	}

	return c.JSON(http.StatusOK, map[string]any{
		"message":    "续费成功",
		"expires_at": newExpiry.Format(time.RFC3339),
		"plan_id":    code.PlanID,
	})
}
