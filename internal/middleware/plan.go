package middleware

import (
	"net/http"
	"sync"
	"time"

	"tg-sender/internal/repository"

	"github.com/labstack/echo/v4"
)

// planCacheTTL: plan 查询结果缓存时长。plan 变化极慢，60s 内读旧值可接受。
const planCacheTTL = 60 * time.Second

type planCacheEntry struct {
	expiresAt *time.Time
	features  map[string]any
	fetchedAt time.Time
}

var (
	planCache   = map[string]planCacheEntry{} // tenantID|planID -> entry
	planCacheMu sync.RWMutex
)

// P2-3 审计清理: PlanAccess 死代码已删——全项目无调用点(用的是 RequirePlanFeature)。
// hasFeatureCompat 保留: RequirePlanFeature 在用它。

// Feature constants for plan checking.
const (
	FeatureBroadcast      = "broadcast"
	FeatureScrapeMembers  = "scrape_members"
	FeatureScrapeLinks    = "scrape_links"
	FeatureJoinGroups     = "join_groups"
	FeatureMonitor        = "monitor"
	FeatureTemplates      = "templates"
	FeatureExport         = "export"
	FeatureScheduledTasks = "scheduled_tasks"
	FeatureLoopBroadcast  = "loop_broadcast" // 循环群发(群发页双 tab: 只发一次/循环)
	// 2026-10 新增功能 key(套餐后台按 key 控制)
	FeatureDiscover  = "discover"    // 发现群组·群库
	FeatureSearch    = "search"      // 关键词搜群
	FeatureProxy     = "proxy"       // 代理设置
	FeatureInvite    = "invite"      // 邀请入群
	FeatureVerifier  = "verifier"    // 入群验证(按钮类)
	FeatureAIVerify  = "ai_verify"   // AI 图片验证
	FeatureDict      = "dict"        // 文案变量(高级语法)
	FeatureRaffle    = "raffle"      // 抽奖助手(三层控制: 套餐→租户→账号)
)

// PlanChecker provides manual plan feature checking (used by handlers).
type PlanChecker struct{}

// NewPlanChecker creates a new PlanChecker.
func NewPlanChecker(planRepo any, tenantRepo any) *PlanChecker {
	return &PlanChecker{}
}

// CheckFeature checks if a tenant has a specific feature enabled.
func (pc *PlanChecker) CheckFeature(c echo.Context, tenantID string, feature string) error {
	features, _ := c.Get("plan_features").(any)
	if !hasFeatureCompat(features, feature) {
		return echo.NewHTTPError(http.StatusForbidden, "plan feature not available")
	}
	return nil
}

// RequirePlanFeature returns middleware that checks if the tenant has the required feature.
func (pc *PlanChecker) RequirePlanFeature(feature string) echo.MiddlewareFunc {
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			features, _ := c.Get("plan_features").(any)
			if !hasFeatureCompat(features, feature) {
				return echo.NewHTTPError(http.StatusForbidden, "plan feature not available")
			}
			expiresAt, ok := c.Get("expires_at").(*time.Time)
			if ok && expiresAt != nil && expiresAt.Before(time.Now()) {
				return echo.NewHTTPError(http.StatusForbidden, "subscription expired, please renew")
			}
			return next(c)
		}
	}
}

// PlanContext loads tenant status, subscription expiration and plan features
// into the echo context. Must be registered AFTER JWTAuth and TenantIsolation.
// 缓存策略：只缓存 plan 查询结果（变化极慢，60s TTL），
// 租户状态（封号/过期）每次实时校验——管理员封禁必须立即生效，
// 不能为了省一次查询让被封用户继续用 60 秒。
func PlanContext(planRepo *repository.PlanRepository, tenantRepo *repository.TenantRepository) echo.MiddlewareFunc {
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			tenantID, err := GetTenantID(c)
			if err != nil {
				return err
			}

			// 租户状态实时校验（不缓存）
			tenant, err := tenantRepo.GetByID(c.Request().Context(), tenantID)
			if err != nil || tenant == nil {
				return echo.NewHTTPError(http.StatusUnauthorized, "tenant not found")
			}
			if tenant.Status != "active" {
				return echo.NewHTTPError(http.StatusForbidden, "tenant account suspended")
			}
			if tenant.ExpiresAt != nil && tenant.ExpiresAt.Before(time.Now()) {
				return echo.NewHTTPError(http.StatusForbidden, "tenant subscription expired")
			}
			c.Set("expires_at", tenant.ExpiresAt)

			// plan 查询走缓存
			now := time.Now()
			key := tenantID.String() + "|" + tenant.PlanID
			planCacheMu.RLock()
			entry, hit := planCache[key]
			planCacheMu.RUnlock()
			if hit && now.Sub(entry.fetchedAt) < planCacheTTL {
				c.Set("plan_features", entry.features)
				return next(c)
			}

			// Default fail-closed: no features until a plan confirms them.
			features := map[string]any{}
			if planRepo != nil && tenant.PlanID != "" {
				if plan, err := planRepo.GetByID(c.Request().Context(), tenant.PlanID); err == nil && plan != nil {
					// 权限单一数据源: 管理端套餐矩阵(features JSONB); 空时兜底老布尔列
					features = plan.FeatureMap()
				}
			}
			c.Set("plan_features", features)

			planCacheMu.Lock()
			planCache[key] = planCacheEntry{expiresAt: tenant.ExpiresAt, features: features, fetchedAt: now}
			planCacheMu.Unlock()

			return next(c)
		}
	}
}

// hasFeature checks if a feature is enabled in the plan features map.
// InvalidatePlanCache 清空 plan 缓存(P1-5 审计修复配套)。
// 管理端改套餐后调用——套餐变更极罕见, 全量清空成本可忽略;
// 原 60 秒 TTL 内所有租户仍按旧 features 校验, 必须立即清。
func InvalidatePlanCache() {
	planCacheMu.Lock()
	defer planCacheMu.Unlock()
	planCache = make(map[string]planCacheEntry)
}

func hasFeature(features map[string]any, key string) bool {
	if features == nil {
		return false
	}
	val, ok := features[key]
	if !ok {
		return false
	}
	switch v := val.(type) {
	case bool:
		return v
	case int:
		return v != 0
	case int64:
		return v != 0
	case float64:
		return v != 0
	case string:
		return v == "true" || v == "1" || v == "yes"
	default:
		return false
	}
}

// hasFeatureCompat: 兼容 features 存成数组的历史数据(管理端曾按 string[] 保存,
// 导致 hasFeature 全 false、套餐校验整体失效)。数组格式: ["raffle","broadcast",...]
func hasFeatureCompat(features any, key string) bool {
	switch f := features.(type) {
	case map[string]any:
		return hasFeature(f, key)
	case []any:
		for _, item := range f {
			if s, ok := item.(string); ok && s == key {
				return true
			}
		}
		return false
	case []string:
		for _, s := range f {
			if s == key {
				return true
			}
		}
		return false
	}
	return false
}
