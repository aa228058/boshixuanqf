package middleware

import (
	"crypto/rand"
	"encoding/hex"
	"log"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/labstack/echo/v4"
	"golang.org/x/crypto/bcrypt"
	"tg-sender/internal/config"
)

// adminSecret returns the key used to sign/verify admin JWTs.
// Priority: ADMIN_JWT_SECRET env (independent random secret, recommended).
// Fallback: JWT_SECRET + "_admin" (kept for backward compatibility — if the
// tenant JWT secret leaks, this derivation is trivial, so migrate ASAP).
var adminSecretWarned bool

func adminSecret(cfg *config.Config) []byte {
	if s := os.Getenv("ADMIN_JWT_SECRET"); s != "" {
		// P2-71 审计修复: 原来 <32 只警告不拒绝——<16 的短密钥可被在线暴力枚举
		// (admin JWT 伪造=管理端全完)。<16 直接拒绝启动(不存在误伤: 生产不可能
		// 用 <16 的正经密钥); 16~32 保持警告。
		if len(s) < 16 {
			log.Fatal("[SECURITY] ADMIN_JWT_SECRET 过短(<16字符), 拒绝启动——请配置≥32字符的随机值")
		}
		if len(s) < 32 && !adminSecretWarned {
			adminSecretWarned = true
			log.Printf("[SECURITY] ADMIN_JWT_SECRET is shorter than 32 chars; use a random value of at least 32 chars")
		}
		return []byte(s)
	}
	// 审计P1-18: 生产环境拒绝回退派生(两个安全域不得共享密钥)
	if cfg.Env == "production" {
		log.Fatal("[SECURITY] 生产环境必须在 .env 配置 ADMIN_JWT_SECRET(独立随机值≥32字符, 不允许回退到 JWT_SECRET 派生)")
	}
	if !adminSecretWarned {
		adminSecretWarned = true
		log.Printf("[SECURITY] ADMIN_JWT_SECRET not set; admin tokens signed with JWT_SECRET+\"_admin\". " +
			"Set ADMIN_JWT_SECRET to an independent random value (>= 32 chars) to isolate admin tokens from tenant JWT secret compromise.")
	}
	return []byte(cfg.JWTSecret + "_admin")
}

// AdminAuth returns admin authentication middleware.
func AdminAuth(cfg *config.Config, db *pgxpool.Pool) echo.MiddlewareFunc {
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			auth := c.Request().Header.Get("Authorization")
			if auth == "" {
				return echo.NewHTTPError(http.StatusUnauthorized, "Unauthorized")
			}

			parts := strings.SplitN(auth, " ", 2)
			if len(parts) != 2 || strings.ToLower(parts[0]) != "bearer" {
				return echo.NewHTTPError(http.StatusUnauthorized, "Unauthorized")
			}

			token, err := jwt.Parse(parts[1], func(token *jwt.Token) (interface{}, error) {
				return adminSecret(cfg), nil
			}, jwt.WithValidMethods([]string{"HS256"}))
			if err != nil || !token.Valid {
				return echo.NewHTTPError(http.StatusUnauthorized, "Unauthorized")
			}

			claims, ok := token.Claims.(jwt.MapClaims)
			if !ok {
				return echo.NewHTTPError(http.StatusUnauthorized, "Unauthorized")
			}

			// F-07: JTI 撤销检查走持久化黑名单（token_blacklist 表，重启不失效）。
			// admin token 实际有效期 8h。
			if jti, _ := claims["jti"].(string); jti != "" && IsTokenRevoked(db, jti) {
				return echo.NewHTTPError(http.StatusUnauthorized, "token revoked")
			}

			adminIDStr, _ := claims["admin_id"].(string)
			adminID, err := uuid.Parse(adminIDStr)
			if err != nil {
				return echo.NewHTTPError(http.StatusUnauthorized, "Unauthorized")
			}

			c.Set("admin_id", adminID)
			c.Set("admin_role", claims["role"])
			return next(c)
		}
	}
}

// F-07: admin token 撤销不再使用独立的内存黑名单（重启即失效、多实例不一致），
// 复用租户侧持久化黑名单（token_blacklist 表 + 内存缓存，启动时全量加载）。
// admin 平面记录以 tenant_id = uuid.Nil 占位。
var adminBlacklistDB *pgxpool.Pool

// InitAdminBlacklist 注入持久化存储（在 InitTokenBlacklist 之后调用即可，
// 黑名单加载与查询直接复用共享实现）。
func InitAdminBlacklist(db *pgxpool.Pool) {
	adminBlacklistDB = db
}

// RevokeAdminToken marks an admin token JTI as revoked before its natural expiry.
func RevokeAdminToken(jti string, exp time.Time) {
	if err := RevokeToken(adminBlacklistDB, jti, uuid.Nil, exp); err != nil {
		log.Printf("[admin-auth] revoke admin token failed: %v", err)
	}
}

// GenerateAdminToken generates an admin JWT token.
func GenerateAdminToken(cfg *config.Config, adminID uuid.UUID, username, role string) (string, error) {
	claims := jwt.MapClaims{
		"admin_id": adminID.String(),
		"username": username,
		"role":     role,
		"jti":      uuid.New().String(), // 唯一 ID，支持撤销
		"exp":      time.Now().Add(8 * time.Hour).Unix(),
		"iat":      time.Now().Unix(),
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	return token.SignedString(adminSecret(cfg))
}

// GenerateAPIKey creates a new API key and its bcrypt hash.
func GenerateAPIKey() (string, string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", "", err
	}
	key := "tgs_" + hex.EncodeToString(b)
	hash, err := bcrypt.GenerateFromPassword([]byte(key), 12)
	if err != nil {
		return "", "", err
	}
	return key, string(hash), nil
}
