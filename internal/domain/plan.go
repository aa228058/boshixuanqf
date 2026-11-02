package domain

import "time"

// FeatureMap 返回该套餐的功能开关表 —— 全系统唯一权限数据源。
// 优先用 features JSONB(管理端套餐矩阵勾选的英文 key);
// 老数据(features 为空)兜底用 can_* 布尔列(仅覆盖旧版 9 个功能,
// 不含 2026-10 新增的 discover/search/proxy/invite/verifier/ai_verify/checkup/dict/api)。
func (p *Plan) FeatureMap() map[string]any {
	m := map[string]any{}
	if p == nil {
		return m
	}
	if len(p.Features) > 0 {
		for _, k := range p.Features {
			m[k] = true
		}
		return m
	}
	m["broadcast"] = p.CanBroadcast
	m["scrape_members"] = p.CanScrapeMembers
	m["scrape_links"] = p.CanScrapeLinks
	m["join_groups"] = p.CanJoinGroups
	m["monitor"] = p.CanMonitor
	m["export"] = p.CanExport
	m["templates"] = p.CanUseTemplates
	m["scheduled_tasks"] = p.PlanID != "free"
	return m
}

// Plan represents a subscription plan.
type Plan struct {
	PlanID        string    `json:"plan_id"`
	Name          string    `json:"name"`
	PriceMonthly  int       `json:"price_monthly"`
	MaxAccounts   int       `json:"max_accounts"`
	MaxConcurrent int       `json:"max_concurrent"`
	Description   string    `json:"description"`
	Features      []string  `json:"features"`
	IsActive      bool      `json:"is_active"`
	UpdatedAt     time.Time `json:"updated_at"`

	// 功能开关
	CanBroadcast     bool `json:"can_broadcast" db:"can_broadcast"`
	CanScrapeMembers bool `json:"can_scrape_members" db:"can_scrape_members"`
	CanScrapeLinks   bool `json:"can_scrape_links" db:"can_scrape_links"`
	CanJoinGroups    bool `json:"can_join_groups" db:"can_join_groups"`
	CanMonitor       bool `json:"can_monitor" db:"can_monitor"`
	CanExport        bool `json:"can_export" db:"can_export"`
	CanUseTemplates  bool `json:"can_use_templates" db:"can_use_templates"`

	// 日配额（此前结构体缺失导致 repository 层无法写入，套餐日限额功能形同虚设）
	MaxDailyBroadcasts int `json:"max_daily_broadcasts" db:"max_daily_broadcasts"`
	MaxDailyScrapes    int `json:"max_daily_scrapes"    db:"max_daily_scrapes"`
	MaxMonitorRules    int `json:"max_monitor_rules"    db:"max_monitor_rules"`
	DailySearchLimit   int `json:"daily_search_limit"   db:"daily_search_limit"` // 051: 关键词搜群日限(0=不限)
}
