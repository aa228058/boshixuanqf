package middleware

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"strings"
	"sync"
	"tg-sender/internal/safego"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/labstack/echo/v4"
	"tg-sender/internal/config"
	"tg-sender/internal/domain"
)

// TenantContextKey is the key for tenant ID in echo context.
const TenantContextKey = "tenant_id"

// JWTClaims represents JWT claims.
// F-03: 签发者/受众标识。所有 ParseWithClaims 均强制校验，配合 F-30 密钥轮换
// 可防止令牌被误用于其他环境/服务。
const (
	TokenIssuer   = "tg-sender"
	TokenAudience = "tg-sender-api"
)

// isSSETokenPath reports whether the path may receive a JWT via ?token= query
// param (EventSource cannot set Authorization headers).
func isSSETokenPath(path string) bool {
	return strings.HasPrefix(path, "/api/v1/events") ||
		(strings.HasPrefix(path, "/api/v1/tasks/") && strings.HasSuffix(path, "/progress/stream"))
}

type JWTClaims struct {
	TenantID  string `json:"tenant_id"`
	TokenType string `json:"token_type"` // access | refresh
	jwt.RegisteredClaims
}

type tokenBlacklistEntry struct {
	ExpiresAt time.Time
}

var (
	tokenBlacklist   = make(map[string]tokenBlacklistEntry)
	tokenBlacklistMu sync.RWMutex
)

// publicPaths defines routes that bypass JWT authentication.
var publicPaths = map[string]struct{}{
	"/api/v1/auth/login":    {},
	"/api/v1/auth/register": {},
	"/api/v1/auth/refresh":  {},
	"/api/v1/support":       {},
}

// InitTokenBlacklist loads existing revoked tokens from DB into memory cache.
func InitTokenBlacklist(db *pgxpool.Pool, stopCh <-chan struct{}) {
	if db == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	rows, err := db.Query(ctx, `SELECT jti, expires_at FROM token_blacklist WHERE expires_at > NOW()`)
	if err != nil {
		log.Printf("[auth] load token blacklist failed: %v", err)
		return
	}
	defer rows.Close()
	count := 0
	for rows.Next() {
		var jti string
		var expiresAt time.Time
		if err := rows.Scan(&jti, &expiresAt); err != nil {
			continue
		}
		tokenBlacklistMu.Lock()
		tokenBlacklist[jti] = tokenBlacklistEntry{ExpiresAt: expiresAt}
		tokenBlacklistMu.Unlock()
		count++
	}
	log.Printf("[auth] loaded %d revoked tokens into blacklist cache", count)
	safego.Go(func() {
		ticker := time.NewTicker(1 * time.Hour)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				cleanupTokenBlacklist(db)
			case <-stopCh:
				return
			}
		}
	})
}

func cleanupTokenBlacklist(db *pgxpool.Pool) {
	now := time.Now()
	tokenBlacklistMu.Lock()
	for jti, entry := range tokenBlacklist {
		if entry.ExpiresAt.Before(now) {
			delete(tokenBlacklist, jti)
		}
	}
	tokenBlacklistMu.Unlock()
	if db != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if _, err := db.Exec(ctx, `DELETE FROM token_blacklist WHERE expires_at < NOW()`); err != nil {
			log.Printf("[auth] cleanup token blacklist failed: %v", err)
		}
	}
}

// IsTokenRevoked checks if a token JTI is in the blacklist (memory first, then DB).
func IsTokenRevoked(db *pgxpool.Pool, jti string) bool {
	tokenBlacklistMu.RLock()
	entry, ok := tokenBlacklist[jti]
	tokenBlacklistMu.RUnlock()
	if ok {
		if entry.ExpiresAt.After(time.Now()) {
			return true
		}
		tokenBlacklistMu.Lock()
		delete(tokenBlacklist, jti)
		tokenBlacklistMu.Unlock()
		return false
	}
	if db != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		var count int
		err := db.QueryRow(ctx, `SELECT COUNT(*) FROM token_blacklist WHERE jti = $1`, jti).Scan(&count)
		if err == nil && count > 0 {
			return true
		}
	}
	return false
}

// RevokeToken adds a token JTI to the persistent blacklist.
func RevokeToken(db *pgxpool.Pool, jti string, tenantID uuid.UUID, expiresAt time.Time) error {
	tokenBlacklistMu.Lock()
	tokenBlacklist[jti] = tokenBlacklistEntry{ExpiresAt: expiresAt}
	tokenBlacklistMu.Unlock()
	if db != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_, err := db.Exec(ctx, `
			INSERT INTO token_blacklist (jti, tenant_id, expires_at) VALUES ($1, $2, $3)
			ON CONFLICT (jti) DO NOTHING`,
			jti, tenantID, expiresAt)
		if err != nil {
			return fmt.Errorf("revoke token: %w", err)
		}
	}
	return nil
}

// tenantCacheEntry 租户状态缓存条目
type tenantCacheEntry struct {
	status    string
	expiresAt *time.Time
	cachedAt  time.Time
}

// 租户状态缓存: JWTAuth 原本每个 API 请求都查库验租户, 前端 5s 轮询 + SSE 重连
// 使其成为全站最高频的 DB 查询。tenant 状态变更频率极低(激活码兑换/后台封禁/到期),
// 60 秒 TTL 是安全折中。封号等账号级操作不经过此缓存, 不受影响。
const tenantCacheTTL = 60 * time.Second

var (
	tenantCacheMu sync.RWMutex
	tenantCache   = map[uuid.UUID]tenantCacheEntry{}
)

// InvalidateTenantCache 在 tenant 状态被修改后调用(激活码兑换/后台封禁), 立即生效.
func InvalidateTenantCache(tenantID uuid.UUID) {
	tenantCacheMu.Lock()
	delete(tenantCache, tenantID)
	tenantCacheMu.Unlock()
}

// JWTAuth returns JWT authentication middleware.
func JWTAuth(cfg *config.Config, tenantRepo domain.TenantRepository, db *pgxpool.Pool) echo.MiddlewareFunc {
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			path := c.Request().URL.Path
			if _, ok := publicPaths[path]; ok {
				return next(c)
			}
			auth := c.Request().Header.Get("Authorization")
			if auth == "" {
				// 审计P0-03: 优先短期 ticket(JWT不进URL/日志); 兼容旧 ?token=
				if q := c.QueryParam("ticket"); q != "" {
					if tid, ok := ValidateEventTicket(q); ok {
						c.Set(TenantContextKey, tid.String())
						return next(c)
					}
					return echo.NewHTTPError(http.StatusUnauthorized, "invalid or expired ticket")
				}
				// F-08: ?token= 仅放行 SSE 流式端点（EventSource 无法设请求头），
				// 其余 GET 端点禁止查询参数传 token，防 JWT 进 URL 日志/浏览器历史/Referer。
				if c.Request().Method == http.MethodGet && isSSETokenPath(path) {
					if q := c.QueryParam("token"); q != "" {
						auth = "Bearer " + q
					}
				}
			}
			if auth == "" {
				return echo.NewHTTPError(http.StatusUnauthorized, "Unauthorized")
			}
			parts := strings.SplitN(auth, " ", 2)
			if len(parts) != 2 || strings.ToLower(parts[0]) != "bearer" {
				return echo.NewHTTPError(http.StatusUnauthorized, "Unauthorized")
			}
			token, err := jwt.ParseWithClaims(parts[1], &JWTClaims{}, func(token *jwt.Token) (interface{}, error) {
				return []byte(cfg.JWTSecret), nil
			}, jwt.WithValidMethods([]string{"HS256"}),
				jwt.WithIssuer(TokenIssuer), jwt.WithAudience(TokenAudience)) // F-03
			if err != nil || !token.Valid {
				return echo.NewHTTPError(http.StatusUnauthorized, "Unauthorized")
			}
			claims, ok := token.Claims.(*JWTClaims)
			if !ok {
				return echo.NewHTTPError(http.StatusUnauthorized, "Unauthorized")
			}
			if claims.TokenType == "refresh" {
				return echo.NewHTTPError(http.StatusUnauthorized, "Unauthorized")
			}
			if claims.ID != "" && IsTokenRevoked(db, claims.ID) {
				return echo.NewHTTPError(http.StatusUnauthorized, "token revoked")
			}
			if tenantRepo != nil {
				tenantID, err := uuid.Parse(claims.TenantID)
				if err != nil {
					return echo.NewHTTPError(http.StatusUnauthorized, "Unauthorized")
				}

				// 缓存读取: 命中且未过期则跳过 DB (全站最高频查询, 60s TTL)
				tenantCacheMu.RLock()
				entry, hit := tenantCache[tenantID]
				tenantCacheMu.RUnlock()

				if !hit || time.Since(entry.cachedAt) > tenantCacheTTL {
					tenant, err := tenantRepo.GetByID(c.Request().Context(), tenantID)
					if err != nil || tenant == nil {
						return echo.NewHTTPError(http.StatusUnauthorized, "tenant not found")
					}
					entry = tenantCacheEntry{
						status:    tenant.Status,
						expiresAt: tenant.ExpiresAt,
						cachedAt:  time.Now(),
					}
					tenantCacheMu.Lock()
					tenantCache[tenantID] = entry
					tenantCacheMu.Unlock()
				}

				if entry.status != "active" {
					return echo.NewHTTPError(http.StatusForbidden, "tenant account suspended")
				}
				if entry.expiresAt != nil && entry.expiresAt.Before(time.Now()) {
					return echo.NewHTTPError(http.StatusForbidden, "tenant subscription expired")
				}
			}
			c.Set(TenantContextKey, claims.TenantID)
			return next(c)
		}
	}
}

// GenerateToken generates an access JWT token for a tenant.
func GenerateToken(cfg *config.Config, tenantID string) (string, error) {
	claims := JWTClaims{
		TenantID:  tenantID,
		TokenType: "access",
		RegisteredClaims: jwt.RegisteredClaims{
			Issuer:    TokenIssuer,
			Audience:  jwt.ClaimStrings{TokenAudience},
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(cfg.JWTExpiry)),
			IssuedAt:  jwt.NewNumericDate(time.Now()),
			ID:        uuid.New().String(),
		},
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	return token.SignedString([]byte(cfg.JWTSecret))
}

// GenerateRefreshToken generates a refresh JWT token for a tenant and returns the token and its JTI.
func GenerateRefreshToken(cfg *config.Config, tenantID string) (string, string, error) {
	jti := uuid.New().String()
	claims := JWTClaims{
		TenantID:  tenantID,
		TokenType: "refresh",
		RegisteredClaims: jwt.RegisteredClaims{
			Issuer:    TokenIssuer,
			Audience:  jwt.ClaimStrings{TokenAudience},
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(cfg.JWTRefreshExp)),
			IssuedAt:  jwt.NewNumericDate(time.Now()),
			ID:        jti,
		},
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	tokenStr, err := token.SignedString([]byte(cfg.JWTSecret))
	if err != nil {
		return "", "", err
	}
	return tokenStr, jti, nil
}

// ParseRefreshToken validates a refresh token and returns the tenant ID and JTI.
func ParseRefreshToken(cfg *config.Config, tokenStr string) (string, string, error) {
	token, err := jwt.ParseWithClaims(tokenStr, &JWTClaims{}, func(token *jwt.Token) (interface{}, error) {
		return []byte(cfg.JWTSecret), nil
	}, jwt.WithValidMethods([]string{"HS256"}),
		jwt.WithIssuer(TokenIssuer), jwt.WithAudience(TokenAudience)) // F-03
	if err != nil || !token.Valid {
		return "", "", fmt.Errorf("invalid refresh token")
	}
	claims, ok := token.Claims.(*JWTClaims)
	if !ok || claims.TokenType != "refresh" {
		return "", "", fmt.Errorf("invalid refresh token claims")
	}
	return claims.TenantID, claims.ID, nil
}
