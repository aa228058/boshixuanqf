package domain

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
)

// AccountLevel represents the manual account tier selected by user.
// SystemGroup 平台级群组库条目(找群功能), 跨租户共享, 不含来源租户身份。
type SystemGroup struct {
	SGID        uuid.UUID `json:"sg_id"`
	Source      string    `json:"source"` // scraped | joined | bio
	Title       string    `json:"title"`
	Username    string    `json:"username,omitempty"`
	TmeLink     string    `json:"tme_link,omitempty"`
	GType       string    `json:"gtype"` // public | private | channel | unknown
	MemberCount int       `json:"member_count"`
	Status      string    `json:"status"` // active | joined | invalid
	RefCount    int       `json:"ref_count"`
	LastSeenAt  time.Time `json:"last_seen_at"`
}

// DedupKey 归并键: 有 username 用 username, 否则用 tme_link 原值。
func (g SystemGroup) DedupKey() string {
	if g.Username != "" {
		return "u:" + strings.ToLower(g.Username)
	}
	return "l:" + g.TmeLink
}

// TextDict 文案变量(数据字典): 群发消息里写 {变量名}, 发送时随机取词条替换。
type TextDict struct {
	DictID    uuid.UUID `json:"dict_id"`
	TenantID  uuid.UUID `json:"tenant_id"`
	Name      string    `json:"name"`
	WordCount int       `json:"word_count"`
	CreatedAt time.Time `json:"created_at"`
}

// TextDictEntry 词条
type TextDictEntry struct {
	EntryID uuid.UUID `json:"entry_id"`
	DictID  uuid.UUID `json:"dict_id"`
	Word    string    `json:"word"`
	Weight  int       `json:"weight"`
}

// AIConfig 租户级 AI 配置(入群验证的图片识别兜底)。api_key 加密存储。
type AIConfig struct {
	TenantID  uuid.UUID `json:"tenant_id"`
	Provider  string    `json:"provider"` // glm | qwen | custom
	APIKey    string    `json:"-"`        // 脱敏, 永不返回明文
	HasKey    bool      `json:"has_key"`
	Model     string    `json:"model"`
	UpdatedAt time.Time `json:"updated_at"`
}

// VerificationLog 入群验证记录
type VerificationLog struct {
	VLID        uuid.UUID `json:"vl_id"`
	TenantID    uuid.UUID `json:"tenant_id"`
	AccountID   uuid.UUID `json:"account_id"`
	GroupChatID int64     `json:"group_chat_id"`
	GroupTitle  string    `json:"group_title"`
	Status      string    `json:"status"` // pending | pass | fail | stopped
	Reason      string    `json:"reason"`
	Steps       []any     `json:"steps"`
	CreatedAt   time.Time `json:"created_at"`
}

type AccountLevel string

const (
	AccountLevelNew    AccountLevel = "new"
	AccountLevelNormal AccountLevel = "normal"
	AccountLevelOld    AccountLevel = "old"
)

// AccountStatus represents the connection status of an account.
type AccountStatus string

const (
	AccountStatusOnline        AccountStatus = "online"        // Currently connected (in pool)
	AccountStatusOffline       AccountStatus = "offline"       // Not authenticated or auth failed
	AccountStatusPendingAuth   AccountStatus = "pending_auth"  // Waiting for code/2FA
	AccountStatusAuthenticated AccountStatus = "authenticated" // Auth success, session saved, not connected
	AccountStatusRestricted    AccountStatus = "restricted"    // Flood wait or rate limited
	AccountStatusBanned        AccountStatus = "banned"        // Account banned by Telegram
	AccountStatusSessionRevoked AccountStatus = "session_revoked" // R157: 会话被 TG 吊销(AUTH_KEY_UNREGISTERED), 需重新登录
)

// TaskType represents the type of task.
type TaskType string

const (
	TaskTypeBroadcast          TaskType = "broadcast"
	TaskTypeScrapeMembers      TaskType = "scrape_members"
	TaskTypeScrapeLinks        TaskType = "scrape_links"
	TaskTypeJoinGroups         TaskType = "join_groups"
	// P2-1 审计清理: TaskTypeMonitorForward/MonitorReply 死类型已删——
	// 从未有创建/执行入口(监控任务走 monitor_rules 表, 不走 tasks 表)
	TaskTypeScheduledBroadcast TaskType = "scheduled_broadcast"
	TaskTypeMonitorScrape      TaskType = "monitor_scrape"
	TaskTypeInviteMembers      TaskType = "invite_members" // 邀请入群: 把来源群成员邀请进自己的群
)

// TaskStatus represents the status of a task.
type TaskStatus string

const (
	TaskStatusPending   TaskStatus = "pending"
	TaskStatusScheduled TaskStatus = "scheduled"
	TaskStatusQueued    TaskStatus = "queued"
	TaskStatusRunning   TaskStatus = "running"
	TaskStatusPaused    TaskStatus = "paused"
	TaskStatusCompleted TaskStatus = "completed"
	TaskStatusFailed    TaskStatus = "failed"
	TaskStatusCancelled TaskStatus = "cancelled"
)

// Tenant represents a tenant (customer).
type Tenant struct {
	TenantID           uuid.UUID  `json:"tenant_id" db:"tenant_id"`
	Name               string     `json:"name" db:"name"`
	Username           string     `json:"username" db:"username"`
	PasswordHash       string     `json:"-" db:"password_hash"`
	APIKeyHash         string     `json:"-" db:"api_key_hash"`
	APIKeyPrefix       string     `json:"-" db:"api_key_prefix"`
	MaxAccounts        int        `json:"max_accounts" db:"max_accounts"`
	MaxConcurrentTasks int        `json:"max_concurrent_tasks" db:"max_concurrent_tasks"`
	PlanID             string     `json:"plan_id" db:"plan_id"`
	Status             string     `json:"status" db:"status"`
	ExpiresAt          *time.Time `json:"expires_at,omitempty" db:"expires_at"`
	CreatedAt          time.Time  `json:"created_at" db:"created_at"`
	// R119: 通知中心 —— 通知邮箱 + 事件订阅(JSONB, map[string]bool)
	NotifyEmail    string          `json:"notify_email" db:"notify_email"`
	NotifySettings json.RawMessage `json:"notify_settings" db:"notify_settings"`
	// R120: 租户级 SMTP 发件配置(服务商/主机/端口/账号/授权码/发件人)
	NotifySMTP json.RawMessage `json:"-" db:"notify_smtp"` // SMTP 授权码敏感字段，仅通过脱敏的通知配置接口返回
}

// NotifySMTPConfig 租户 SMTP 配置结构
type NotifySMTPConfig struct {
	Provider string `json:"provider"` // qq / 163 / gmail / outlook / tinkmail / custom
	Host     string `json:"host"`
	Port     int    `json:"port"`
	User     string `json:"user"`
	Pass     string `json:"pass"` // 授权码/应用专用密码
	From     string `json:"from"`
}

// SMTPConfig 解析租户 SMTP 配置(未配置返回 nil)
func (t *Tenant) SMTPConfig() *NotifySMTPConfig {
	if t == nil || len(t.NotifySMTP) < 3 {
		return nil
	}
	var cfg NotifySMTPConfig
	if json.Unmarshal(t.NotifySMTP, &cfg) != nil || cfg.Host == "" || cfg.From == "" {
		return nil
	}
	if cfg.Port == 0 {
		cfg.Port = 465
	}
	return &cfg
}

// NotifyEnabled 判定某事件是否订阅(键缺失=false, 后端兜底)
func (t *Tenant) NotifyEnabled(event string) bool {
	if t == nil || len(t.NotifySettings) == 0 {
		return false
	}
	var m map[string]bool
	if err := json.Unmarshal(t.NotifySettings, &m); err != nil {
		return false
	}
	return m[event]
}

// Account represents a Telegram account.
type Account struct {
	AccountID     uuid.UUID      `json:"account_id" db:"account_id"`
	TenantID      uuid.UUID      `json:"tenant_id" db:"tenant_id"`
	Phone         string         `json:"phone" db:"phone"`
	Source        string         `json:"source" db:"source"` // phone=正常号 | protocol=协议号

	SessionName   string         `json:"session_name" db:"session_name"`
	Status        AccountStatus  `json:"status" db:"status"`
	Level         AccountLevel   `json:"level" db:"level"`
	HealthScore   int            `json:"health_score" db:"health_score"`
	DeviceProfile map[string]any `json:"-" db:"device_profile"`
	LastOnline    *time.Time     `json:"last_online" db:"last_online"`
	Remark        string         `json:"remark" db:"remark"`
	// Account runtime statistics for adaptive guard-rail
	RecentBanRate  float64    `json:"recent_ban_rate,omitempty" db:"recent_ban_rate"`
	RecentFailRate float64    `json:"recent_fail_rate,omitempty" db:"recent_fail_rate"`
	MsgsSent24h    int        `json:"msgs_sent_24h,omitempty" db:"msgs_sent_24h"`
	LastBanAt      *time.Time `json:"last_ban_at,omitempty" db:"last_ban_at"`
	// FloodWait auto-recovery: account is blocked until this time
	BlockedUntil *time.Time `json:"blocked_until,omitempty" db:"blocked_until"`
	// Tenant-configurable send intervals (0 = use system default)
	MinInterval time.Duration `json:"min_interval" db:"min_interval"`
	MaxInterval time.Duration `json:"max_interval" db:"max_interval"`
	CreatedAt   time.Time     `json:"created_at" db:"created_at"`
	// 展示字段：来自 sessions 表（连接后 TG 回填），列表接口填充，非 accounts 表列
	FirstName string `json:"first_name,omitempty" db:"-"`
	LastName  string `json:"last_name,omitempty" db:"-"`
	Username  string `json:"username,omitempty" db:"-"`
	// 头像：avatar_path 为 accounts 表列(相对 AvatarDir 的文件名, 空=未拉取)；
	// avatar_url 为派生展示字段(MarshalJSON 时按 avatar_path 生成, 不入库)
	AvatarPath string `json:"-" db:"avatar_path"`
	AvatarURL  string `json:"avatar_url,omitempty" db:"-"`
	// 能力体检(047): capabilities 入库; dc/dc_line 来自 sessions 表 join(线路列)
	Capabilities map[string]interface{} `json:"capabilities,omitempty" db:"capabilities"`
	DC           int32                  `json:"dc" db:"-"`
	DCLine       string                 `json:"dc_line,omitempty" db:"-"`
	// 代理(049): socks5://user:pass@host:port, 空=直连
	ProxyURL string `json:"proxy_url,omitempty" db:"proxy_url"`
	// 抽奖助手(055): 该账号是否参与自动抽奖, 默认 true
	RaffleEnabled bool `json:"raffle_enabled" db:"raffle_enabled"`
	// 备用池(056): active=启用(占套餐额度) / standby=备用(不占额度, 停业务)
	PoolStatus string `json:"pool_status" db:"pool_status"`
}

var dcRegionNames = map[int32]string{1: "美国东部", 2: "美国东部", 3: "欧洲", 4: "欧洲", 5: "新加坡"}

func (a *Account) fillDerived() {
	if a.AvatarPath != "" {
		a.AvatarURL = "/accounts/" + a.AccountID.String() + "/avatar"
	}
	if a.DC > 0 {
		if r, ok := dcRegionNames[a.DC]; ok {
			a.DCLine = fmt.Sprintf("%s DC%d", r, a.DC)
		} else {
			a.DCLine = fmt.Sprintf("DC%d", a.DC)
		}
	}
}

// avatarURLPath 由 MarshalJSON 生成, 便于前端 <img> 直接引用(需带认证, 前端走 axios blob)。
func (a *Account) fillAvatarURL() {
	if a.AvatarPath != "" {
		a.AvatarURL = "/accounts/" + a.AccountID.String() + "/avatar"
	}
}

func (a Account) MarshalJSON() ([]byte, error) {
	a.fillDerived()
	type Alias Account
	return json.Marshal(struct {
		Alias
	}{Alias: (Alias)(a)})
}

// Task represents a task to be executed.
type Task struct {
	TaskID      uuid.UUID      `json:"task_id" db:"task_id"`
	TenantID    uuid.UUID      `json:"tenant_id" db:"tenant_id"`
	Type        TaskType       `json:"type" db:"type"`
	Status      TaskStatus     `json:"status" db:"status"`
	Priority    int            `json:"priority" db:"priority"`
	AccountIDs  []uuid.UUID    `json:"account_ids" db:"account_ids"`
	TargetIDs   []string       `json:"target_ids" db:"target_ids"`
	Payload     map[string]any `json:"payload" db:"payload"`
	Progress    TaskProgress   `json:"progress" db:"progress"`
	LockedAt    *time.Time     `json:"locked_at" db:"locked_at"`
	WorkerID    *string        `json:"worker_id" db:"worker_id"`
	RetryCount  int            `json:"retry_count" db:"retry_count"`
	ScheduledAt *time.Time     `json:"scheduled_at" db:"scheduled_at"`
	StartedAt   *time.Time     `json:"started_at" db:"started_at"`
	CompletedAt *time.Time     `json:"completed_at" db:"completed_at"`
	CreatedAt   time.Time      `json:"created_at" db:"created_at"`
	// 循环发送配置
	IsLoop              bool       `json:"is_loop" db:"is_loop"`
	LoopIntervalMinutes int        `json:"loop_interval_minutes" db:"loop_interval_minutes"`
	MinIntervalSeconds  int        `json:"min_interval_seconds" db:"min_interval_seconds"`
	MaxIntervalSeconds  int        `json:"max_interval_seconds" db:"max_interval_seconds"`
	NextRunAt           *time.Time `json:"next_run_at" db:"next_run_at"`
}

// AccountProgress tracks per-account progress within a task.
type AccountProgress struct {
	AccountID uuid.UUID `json:"account_id"`
	Done      int       `json:"done"`
	Failed    int       `json:"failed"`
	Total     int       `json:"total"`
}

// BatchTask represents a group of sub-tasks created by auto-splitting a large broadcast.
type BatchTask struct {
	BatchID   uuid.UUID   `json:"batch_id"`
	TenantID  uuid.UUID   `json:"tenant_id"`
	Message   string      `json:"message"`
	TotalMsgs int         `json:"total_msgs"`
	SubTasks  []uuid.UUID `json:"sub_tasks"`
	CreatedAt time.Time   `json:"created_at"`
}

// BatchTaskRepository defines storage for batch tasks.
type BatchTaskRepository interface {
	Create(ctx context.Context, batch *BatchTask) error
	GetByID(ctx context.Context, tenantID, batchID uuid.UUID) (*BatchTask, error)
	UpdateSubTask(ctx context.Context, tenantID, batchID uuid.UUID, subTaskID uuid.UUID) error
}

// TaskProgress tracks task execution progress.
type TaskProgress struct {
	Total           int               `json:"total"`
	Done            int               `json:"done"`
	Failed          int               `json:"failed"`
	CurrentTarget   string            `json:"current_target,omitempty"`
	AccountProgress []AccountProgress `json:"account_progress,omitempty"`
}

// AccountTargetMapping defines which targets each account should send to.
// Used in broadcast tasks for precise control instead of cartesian product.
type AccountTargetMapping struct {
	AccountID uuid.UUID `json:"account_id"`
	Targets   []string  `json:"targets"`
}

// BroadcastMode defines how a broadcast task distributes messages.
type BroadcastMode string

const (
	BroadcastModeCartesian BroadcastMode = "cartesian" // all accounts × all targets (legacy)
	BroadcastModeMapping   BroadcastMode = "mapping"   // account-target mapping (precise)
)

// TaskLog represents a single execution log entry.
type TaskLog struct {
	LogID       uuid.UUID `json:"log_id" db:"log_id"`
	TaskID      uuid.UUID `json:"task_id" db:"task_id"`
	TenantID    uuid.UUID `json:"tenant_id" db:"tenant_id"`
	AccountID   uuid.UUID `json:"account_id" db:"account_id"`
	TargetID    string    `json:"target_id" db:"target_id"`
	TargetName  string    `json:"target_name,omitempty" db:"target_name"`
	Action      string    `json:"action" db:"action"`
	Status      string    `json:"status" db:"status"`
	TGResponse  string    `json:"tg_response,omitempty" db:"tg_response"`
	ErrorCode   string    `json:"error_code,omitempty" db:"error_code"`
	Message     string    `json:"message,omitempty" db:"message"`
	TGMessageID int64     `json:"tg_message_id,omitempty" db:"tg_message_id"`
	ExecutedAt  time.Time `json:"executed_at" db:"executed_at"`
}

// Group represents a Telegram group/channel.
type Group struct {
	GroupID       uuid.UUID      `json:"group_id" db:"group_id"`
	TenantID      uuid.UUID      `json:"tenant_id" db:"tenant_id"`
	TGChatID      int64          `json:"tg_chat_id" db:"tg_chat_id"`
	Title         string         `json:"title" db:"title"`
	Username      string         `json:"username" db:"username"`
	Type          string         `json:"type" db:"type"`
	MemberCount   int            `json:"member_count" db:"member_count"`
	IsJoined      bool           `json:"is_joined" db:"is_joined"`
	JoinedAt      *time.Time     `json:"joined_at" db:"joined_at"`
	LastScrapedAt *time.Time     `json:"last_scraped_at" db:"last_scraped_at"`
	AccessHash    int64          `json:"access_hash" db:"access_hash"`
	Metadata      map[string]any `json:"metadata,omitempty" db:"metadata"`
}

// Member represents a group member.
type Member struct {
	MemberID  uuid.UUID `json:"member_id" db:"member_id"`
	TenantID  uuid.UUID `json:"tenant_id" db:"tenant_id"`
	GroupID   uuid.UUID `json:"group_id" db:"group_id"`
	TGUserID  int64     `json:"tg_user_id" db:"tg_user_id"`
	Username  string    `json:"username" db:"username"`
	FirstName string    `json:"first_name" db:"first_name"`
	LastName  string    `json:"last_name" db:"last_name"`
	Phone     string    `json:"phone" db:"phone"`
	Bio       string    `json:"bio" db:"bio"`
	// AccessHash 历史消息采集时从 tg.User 提取, 私聊直发必需(migration 031 加列)
	AccessHash int64      `json:"access_hash" db:"access_hash"`
	LastSeen   *time.Time `json:"last_seen" db:"last_seen"`
	HasPhoto   bool       `json:"has_photo" db:"has_photo"`
	ScrapedAt  time.Time  `json:"scraped_at" db:"scraped_at"`
}

// MonitorRule represents a monitoring rule.
// AccountID 为 nil 表示规则属于"全部账号"（老数据兼容）；
// 非 nil 时只有该账号的监听会话应用此规则（2026-09-22 修复 A 号规则触发 B 号的问题）。
type MonitorRule struct {
	RuleID        uuid.UUID `json:"rule_id" db:"rule_id"`
	TenantID      uuid.UUID `json:"tenant_id" db:"tenant_id"`
	AccountID     *uuid.UUID `json:"account_id,omitempty" db:"account_id"`
	Name          string    `json:"name" db:"name"`
	Type          string    `json:"type" db:"type"` // forward | reply
	SourceChatIDs []string  `json:"source_chat_ids" db:"source_chat_ids"`
	TargetChatIDs []string  `json:"target_chat_ids" db:"target_chat_ids"`
	MatchType     string    `json:"match_type" db:"match_type"` // contains | regex | exact | from_user
	Keywords      []string  `json:"keywords" db:"keywords"`
	ReplyContent  string    `json:"reply_content" db:"reply_content"`
	IsActive      bool      `json:"is_active" db:"is_active"`
	CreatedAt     time.Time `json:"created_at" db:"created_at"`
}

// AccountQuota tracks usage quotas per account.
type AccountQuota struct {
	AccountID      uuid.UUID `json:"account_id" db:"account_id"`
	QuotaType      string    `json:"quota_type" db:"quota_type"` // broadcast | join | scrape
	DailyLimit     int       `json:"daily_limit" db:"daily_limit"`
	UsedToday      int       `json:"used_today" db:"used_today"`
	OverLimitCount int       `json:"over_limit_count" db:"over_limit_count"`
	ResetAt        time.Time `json:"reset_at" db:"reset_at"`
}

// RiskEvent represents a risk control event.
type RiskEvent struct {
	EventID   uuid.UUID      `json:"event_id" db:"event_id"`
	TenantID  uuid.UUID      `json:"tenant_id" db:"tenant_id"`
	AccountID uuid.UUID      `json:"account_id" db:"account_id"`
	EventType string         `json:"event_type" db:"event_type"`
	Severity  string         `json:"severity" db:"severity"`
	Details   map[string]any `json:"details" db:"details"`
	CreatedAt time.Time      `json:"created_at" db:"created_at"`
}

// AuditLog represents an audit log entry.
type AuditLog struct {
	LogID     uuid.UUID `json:"log_id" db:"log_id"`
	TenantID  uuid.UUID `json:"tenant_id" db:"tenant_id"`
	UserID    string    `json:"user_id" db:"user_id"`
	Action    string    `json:"action" db:"action"`
	Target    string    `json:"target" db:"target"`
	IP        string    `json:"ip" db:"ip"`
	CreatedAt time.Time `json:"created_at" db:"created_at"`
}

// QuotaConfig defines quota limits per account level.
// 注：账号日限额 enforcement 已按产品决策移除（AddUsage 记账已删，见 service/task.go），
// 此处保留仅为 QuotaRepository.InitAccountQuotas 初始化配额行所用；额度永远为 0，不再拦截。
var (
	quotaConfigOnce sync.Once
	quotaConfig     map[AccountLevel]map[string]int
)

func initQuotaConfig() {
	quotaConfigOnce.Do(func() {
		quotaConfig = map[AccountLevel]map[string]int{
			AccountLevelNew:    {"broadcast_daily": 0, "join_daily": 0, "scrape_daily": 0},
			AccountLevelNormal: {"broadcast_daily": 0, "join_daily": 0, "scrape_daily": 0},
			AccountLevelOld:    {"broadcast_daily": 0, "join_daily": 0, "scrape_daily": 0},
		}
	})
}

// GetQuotaConfig returns the quota config for a given account level.
// Safe for concurrent use.
func GetQuotaConfig(level AccountLevel) (map[string]int, bool) {
	initQuotaConfig()
	cfg, ok := quotaConfig[level]
	return cfg, ok
}

// AccountRepository defines account data access interface.
type AccountRepository interface {
	List(ctx context.Context, tenantID uuid.UUID, source string) ([]*Account, error)
	ListByIDs(ctx context.Context, tenantID uuid.UUID, accountIDs []uuid.UUID) ([]*Account, error)
	GetByID(ctx context.Context, tenantID, accountID uuid.UUID) (*Account, error)
	Create(ctx context.Context, a *Account) error
	CountAndCreate(ctx context.Context, a *Account, maxAccounts int) (*Account, int, error)
	UpdateStatus(ctx context.Context, tenantID, accountID uuid.UUID, status AccountStatus) error
	// P1-20: 原子更新状态+封禁截止时间(risk.go 调用——上个会话加了 repo 实现但漏了接口声明)
	UpdateStatusAndBlockedUntil(ctx context.Context, tenantID, accountID uuid.UUID, status AccountStatus, blockedUntil time.Time) error
	UpdateHealthScore(ctx context.Context, tenantID, accountID uuid.UUID, score int) error
	Delete(ctx context.Context, tenantID, accountID uuid.UUID) error
	CountByTenant(ctx context.Context, tenantID uuid.UUID) (int, error)
	CountStats(ctx context.Context, tenantID uuid.UUID) (total, online int, err error)
	CountAll(ctx context.Context) (int, error)
	Update(ctx context.Context, a *Account) error
	UpdateAvatarPath(ctx context.Context, tenantID, accountID uuid.UUID, avatarPath string) error
	// R130: 协议号手机号回读写库(phone.go ensurePhoneAsync)
	UpdatePhone(ctx context.Context, tenantID, accountID uuid.UUID, phone string) error
	// R160: 登录成功后临时会话改名接管(防覆盖正式会话密钥)
	RenameSessionStorage(ctx context.Context, from, to string) error
	// R182: 会话存档存在性检测(读档优先连接路径的依据)
	SessionExists(ctx context.Context, sessionID string) (bool, error)
	// R164: 登录临时会话清理(sessions 表防膨胀 + 旧密钥残留安全)
	CleanupLoginSessions(ctx context.Context) (int64, error)
	CleanupLoginSessionsFor(ctx context.Context, base string) (int64, error)
	UpdateCapabilities(ctx context.Context, tenantID, accountID uuid.UUID, caps map[string]interface{}) error
	UpdateProxyURL(ctx context.Context, tenantID, accountID uuid.UUID, proxyURL string) error
	UpdateSessionName(ctx context.Context, tenantID, accountID uuid.UUID, sessionName string) error
	UpdateBlockedUntil(ctx context.Context, tenantID, accountID uuid.UUID, blockedUntil time.Time) error
}

// RiskEventRepository defines risk event data access interface.
type RiskEventRepository interface {
	Create(ctx context.Context, re *RiskEvent) error
	ListByAccount(ctx context.Context, tenantID, accountID uuid.UUID, limit int) ([]*RiskEvent, error)
	ListByTenant(ctx context.Context, tenantID uuid.UUID, limit, offset int, startTime, endTime string) ([]*RiskEvent, error)
	CountByAccount(ctx context.Context, tenantID, accountID uuid.UUID) (map[string]int, error)
	CountByTenant(ctx context.Context, tenantID uuid.UUID) (int, error)
	Count24hBans(ctx context.Context) (int, error)
}

// QuotaRepository defines quota data access interface.
type QuotaRepository interface {
	Get(ctx context.Context, accountID uuid.UUID, quotaType string) (*AccountQuota, error)
	GetAll(ctx context.Context, accountID uuid.UUID) ([]*AccountQuota, error)
	Upsert(ctx context.Context, q *AccountQuota) error
	IncrementUsage(ctx context.Context, accountID uuid.UUID, quotaType string) (*UsageResult, error)
	ResetDaily(ctx context.Context) error
	InitAccountQuotas(ctx context.Context, accountID uuid.UUID, level AccountLevel) error
}

// UsageResult holds the result of an increment operation.
type UsageResult struct {
	DailyLimit     int
	UsedToday      int
	OverLimit      bool
	OverLimitCount int
}

// MemberRepository defines member data access interface.
type MemberRepository interface {
	ListByGroup(ctx context.Context, tenantID, groupID uuid.UUID, limit, offset int) ([]*Member, error)
	CountByGroup(ctx context.Context, tenantID, groupID uuid.UUID) (int, error)
	BatchCreate(ctx context.Context, members []*Member) error
	DeleteByGroup(ctx context.Context, tenantID, groupID uuid.UUID) error
	UpsertContact(ctx context.Context, tenantID, accountID uuid.UUID, tgUserID int64, username, firstName, lastName string, accessHash int64) error
	ListContactsByAccount(ctx context.Context, tenantID, accountID uuid.UUID) (map[int64]string, error)
	ListContactRows(ctx context.Context, tenantID, accountID uuid.UUID) ([]map[string]any, error)
	GetAccessHashByTGUserID(ctx context.Context, tenantID uuid.UUID, tgUserID int64) (int64, error)
}

// PendingApproval tracks join requests waiting for admin approval.
type PendingApproval struct {
	ID         int        `json:"id" db:"id"`
	TaskID     uuid.UUID  `json:"task_id" db:"task_id"`
	TenantID   uuid.UUID  `json:"tenant_id" db:"tenant_id"`
	AccountID  uuid.UUID  `json:"account_id" db:"account_id"`
	ChatID     int64      `json:"chat_id" db:"chat_id"`
	AccessHash int64      `json:"access_hash" db:"access_hash"`
	TargetLink string     `json:"target_link" db:"target_link"`
	Status     string     `json:"status" db:"status"`
	CheckCount int        `json:"check_count" db:"check_count"`
	MaxChecks  int        `json:"max_checks" db:"max_checks"`
	CreatedAt  time.Time  `json:"created_at" db:"created_at"`
	CheckedAt  *time.Time `json:"checked_at,omitempty" db:"checked_at"`
}

// PendingApprovalRepository defines pending approval data access interface.
type PendingApprovalRepository interface {
	Create(ctx context.Context, pa *PendingApproval) error
	GetPending(ctx context.Context, tenantID uuid.UUID, limit int) ([]*PendingApproval, error)
	GetPendingAll(ctx context.Context, limit int) ([]*PendingApproval, error)
	GetByAccountAndLink(ctx context.Context, accountID uuid.UUID, targetLink string) (*PendingApproval, error)
	UpdateStatus(ctx context.Context, tenantID, taskID uuid.UUID, status string) error
	IncrementCheck(ctx context.Context, tenantID, taskID uuid.UUID) error
}

// RefreshTokenRepository defines refresh token data access interface.
type RefreshTokenRepository interface {
	Create(ctx context.Context, jti uuid.UUID, tenantID uuid.UUID, expiresAt time.Time) error
	// V16: Peek 是非消费预检, 轮换流程签发新 token 前先确认旧 token 有效, 避免无效请求灌孤儿
	Peek(ctx context.Context, jti uuid.UUID) (uuid.UUID, error)
	Validate(ctx context.Context, jti uuid.UUID) (uuid.UUID, error)
	Revoke(ctx context.Context, jti uuid.UUID) error
	// F-02: 登出时按租户撤销全部 refresh token
	RevokeAllForTenant(ctx context.Context, tenantID uuid.UUID) error
}

// TenantRepository defines tenant data access interface.
type TenantRepository interface {
	GetByID(ctx context.Context, tenantID uuid.UUID) (*Tenant, error)
	GetByUsername(ctx context.Context, username string) (*Tenant, error)
	Create(ctx context.Context, t *Tenant, apiKeyPlaintext string) error
	Update(ctx context.Context, tenantID uuid.UUID, updates map[string]any) error
	List(ctx context.Context, page, pageSize int, status, planID string) ([]*Tenant, int, error)
}

// ActivationCodeRepository defines activation code data access interface.
type ActivationCodeRepository interface {
	GetByCode(ctx context.Context, code string) (*ActivationCode, error)
	MarkUsed(ctx context.Context, codeID, tenantID uuid.UUID) (bool, error)
	UnmarkUsed(ctx context.Context, codeID, tenantID uuid.UUID) (bool, error)
}

// PlanRepository defines plan data access interface.
type PlanRepository interface {
	GetByID(ctx context.Context, planID string) (*Plan, error)
	Create(ctx context.Context, p *Plan) error
}

// ScrapedLink represents a link scraped from Telegram messages.
type ScrapedLink struct {
	LinkID          uuid.UUID  `json:"link_id" db:"link_id"`
	TenantID        uuid.UUID  `json:"tenant_id" db:"tenant_id"`
	AccountID       uuid.UUID  `json:"account_id" db:"account_id"`
	LinkType        string     `json:"link_type" db:"link_type"` // private | public
	LinkHash        string     `json:"link_hash" db:"link_hash"`
	FullURL         string     `json:"full_url" db:"full_url"`
	SourceChatID    int64      `json:"source_chat_id" db:"source_chat_id"`
	SourceChatTitle string     `json:"source_chat_title" db:"source_chat_title"`
	ResolvedChatID  int64      `json:"resolved_chat_id" db:"resolved_chat_id"`
	ResolvedTitle   string     `json:"resolved_title" db:"resolved_title"`
	MemberCount     int        `json:"member_count" db:"member_count"`
	Status          string     `json:"status" db:"status"` // new | resolved | joined | ignored
	CreatedAt       time.Time  `json:"created_at" db:"created_at"`
	ResolvedAt      *time.Time `json:"resolved_at,omitempty" db:"resolved_at"`
}

// ScrapedLinkRepository defines scraped link data access interface.
type ScrapedLinkRepository interface {
	Create(ctx context.Context, link *ScrapedLink) error
	CreateBatch(ctx context.Context, links []*ScrapedLink) error
	GetByHash(ctx context.Context, tenantID uuid.UUID, linkHash string) (*ScrapedLink, error)
	List(ctx context.Context, tenantID uuid.UUID, status string, limit, offset int) ([]*ScrapedLink, int, error)
	UpdateResolved(ctx context.Context, linkID uuid.UUID, resolvedChatID int64, resolvedTitle string, memberCount int) error
	UpdateStatus(ctx context.Context, linkID uuid.UUID, status string) error
	CountByTenant(ctx context.Context, tenantID uuid.UUID) (int, error)
	CountByTenantSince(ctx context.Context, tenantID uuid.UUID, since time.Time) (int, error)
	ListByAccount(ctx context.Context, tenantID, accountID uuid.UUID, since time.Time, limit, offset int) ([]*ScrapedLink, int, error)
	CountByTenantAndStatus(ctx context.Context, tenantID uuid.UUID, status string) (int, error)
}
