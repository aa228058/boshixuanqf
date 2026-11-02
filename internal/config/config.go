package config

import (
	"fmt"
	"log"
	"math"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/joho/godotenv"
)

// Config holds all application configuration.
type Config struct {
	// Server
	ServerPort     string
	ServerHost     string
	ReadTimeout    time.Duration
	WriteTimeout   time.Duration
	MaxHeaderBytes int
	AllowedOrigins string // comma-separated list of allowed CORS origins

	// Database
	DBHost        string
	DBPort        int
	DBUser        string
	DBPassword    string
	DBName        string
	DBSSLMode     string
	DBMaxConns    int
	DBMaxIdle     int
	DBMaxLife     time.Duration
	DBMaxIdleTime time.Duration

	// JWT
	JWTSecret     string
	JWTExpiry     time.Duration
	JWTRefreshExp time.Duration

	// Telegram
	TGAPIID   int
	TGAPIHash  string
	TGProxyURL string // 代理 socks5://user:pass@host:port，被墙/拉黑 IP 的服务器必配

	// 传输模式: auto(默认)=TCP→HTTP→MTProxy 逐级回退 | tcp | http | mtproxy
	// HTTP 传输走 80/5222 端口的 /api（机房到 TG 443 间歇抽风/被 QoS 时的生路）
	TGTransport      string
	TGMTPProxyAddr   string // MTProxy 地址 host:port（可选第三层回退）
	TGMTPProxySecret string // MTProxy 密钥（hex，dd/ee/simple 均可）

	// DC 自动迁移：保活连上后, 会话 DC 在黑名单里 → 后台自动迁到目标 DC。
	// TG_AUTO_MIGRATE_FROM=1 或 "1,4"（逗号分隔, 空=关闭）; TG_AUTO_MIGRATE_TO 默认 2。
	TGAutoMigrateFrom string
	DefaultProxy        string  // 批次2: 默认代理(env DEFAULT_PROXY)
	TGAutoMigrateTo   int32

	// R119: SMTP 通知邮件(SMTP_HOST 留空 = 通知功能静默关闭)
	SMTPHost string
	SMTPPort int
	SMTPUser string
	SMTPPass string
	SMTPFrom string

	// 协议号功能套餐门槛：租户套餐 PlanID 在此名单内才能使用（默认 pro,enterprise = 专业版/企业版）
	ProtocolPlanIDs string
	// P1-01: 全局同时执行任务数硬上限(原子信号量, 默认20)
	GlobalTaskConcurrency int
	// P2-05: 数据保留天数(可配置, 默认 7 天——见下方 getIntFallback 实际值)
	RiskRetentionDays     int
	AuditRetentionDays    int
	CampaignRetentionDays int

	// App
	Env           string // development | production
	LogLevel      string // debug | info | warn | error
	LogFile       string
	MaxAccounts   int // per tenant
	MaxConcurrent int // tasks per tenant
	MaxPoolSize   int // TG connection pool max size

	// Request timeout for all HTTP handlers (DB queries + TG API calls inherit this)
	RequestTimeout time.Duration
	// 任务调度参数（此前硬编码在 engine/repository 里）
	TaskWorkerCount    int // 并行 worker 数（默认 3）
	TaskClaimBatch     int // 每次认领任务数（默认 10）
	TaskTimeoutClaim   int // 每次认领超时锁任务数（默认 5）
	TaskLockTimeoutMin int // running 任务锁过期分钟数，超时后被 reclaim（默认 5）

	// Send intervals (randomized between min and max to avoid detection)
	// Tenant can set any value; system applies guard-rail offset if too low.
	MinSendInterval time.Duration
	MaxSendInterval time.Duration

	// Guard-rail floor intervals by account level (absolute minimums)
	FloorIntervalNew    time.Duration // default 8s
	FloorIntervalNormal time.Duration // default 3s
	FloorIntervalOld    time.Duration // default 1s

	// Adaptive guard-rail multipliers based on recent ban rate
	BanRateLowMultiplier    float64       // default 1.0 (no change)
	BanRateMediumMultiplier float64       // default 1.5 (>10% ban rate)
	BanRateHighMultiplier   float64       // default 2.0 (>30% ban rate)
	BanRateLookbackWindow   time.Duration // default 24h

	// Guard-rail jitter range when offsetting tenant config
	GuardMinJitter time.Duration // default 0s
	GuardMaxJitter time.Duration // default 2s
	GuardMinSpread time.Duration // default 2s
	GuardMaxSpread time.Duration // default 5s

	// Mode: auto | conservative | standard | aggressive
	// auto = adaptive mode, switches automatically based on system metrics
	Mode string

	// Version
	Version string // app version, e.g. v1.9.20

	// Adaptive thresholds (only used when Mode = "auto")
	AdaptiveCheckInterval time.Duration // how often to evaluate metrics (default 30s)
	MemoryLimitMB         int           // Go runtime memory limit in MiB, also the denominator for adaptive % (default 2800)
	MemHighThreshold      float64       // memory % to trigger downshift (default 80)
	MemLowThreshold       float64       // memory % to allow upshift (default 50)
	MemSafeThreshold      float64       // memory % for aggressive mode (default 30)
	FailRateThreshold     float64       // fail rate % to trigger downshift (default 20)
	FailRateSafe          float64       // fail rate % to allow upshift (default 5)
	QueueDeepThreshold    int           // queue depth to prevent upshift (default 200)
	QueueLightThreshold   int           // queue depth to allow upshift (default 50)

	// Auth & Session
	AuthTimeout           time.Duration // max time to wait for auth completion (default 5m)
	SessionBackupInterval time.Duration // interval to backup session files (default 1h)
	PoolIdleTimeout       time.Duration // how long to keep idle connections in pool (default 5m)

	// Task execution thresholds
	ScrapeLinksTimeout  time.Duration // timeout for scrape_links task (default 30m)
	TaskRetrySleep      time.Duration // sleep between task retries (default 5s)
	JoinIntervalMinutes int           // minutes between join attempts (default 2, jitter 0~50%)
	MediaDir            string        // uploaded media storage dir (default ./media)
	AvatarDir           string        // account avatar storage dir (default ./data/avatars)
	AlertBotToken       string        // TG bot token for admin alerts (optional)
	AlertChatID         string        // TG chat id for admin alerts (optional)
	DialogsLimit        int           // max dialogs to fetch per query (default 200)
	MemAlertThresholdMB int           // memory alert threshold in MB (default 3072)
	QueueAlertThreshold int           // queue depth alert threshold (default 500)

	// Monitor settings
	MonitorRuleReloadInterval time.Duration // rule reload ticker interval (default 30s)
	BroadcastWorkers          int           // broadcast concurrent account workers (default 5)

	// Auth polling
	AuthPollInterval time.Duration // auth code poll interval (default 5s)
	AuthIdleTimeout  time.Duration // auth idle timeout (default 30s)

	// Resource monitoring
	ResourceMonitorInterval time.Duration // system resource check interval (default 30s)

	// Approval checker
	ApprovalCheckInterval time.Duration // pending approval check interval (default 5m)
	ApprovalCheckLimit    int           // max pending approvals per check (default 50)

	// Server settings
	ShutdownTimeout time.Duration // graceful shutdown timeout (default 30s)

	// Quota reset
	QuotaResetInterval time.Duration // quota reset check interval (default 1h)

	// SSE settings
	SSEChannelBuffer int           // SSE channel buffer size (default 500)
	SSEHeartbeat     time.Duration // SSE heartbeat interval (default 30s)

	// Pagination defaults
	DefaultPageSize      int // default page size for lists (default 20)
	MaxPageSize          int // max page size for lists (default 200)
	GroupDefaultPageSize int // default page size for groups (default 50)

	// Broadcast limits
	TenantDailySoftLimit int // tenant daily broadcast soft limit (default 10000)
	MaxMsgsPerTask       int // max messages per broadcast task (default 500)
	MaxMappingsPerTask   int // max account-target mappings per task (default 500)

	// Database health check
	DBHealthCheckPeriod time.Duration // database health check period (default 30s)

	// Account connection
	ConnectRetrySleep time.Duration // account connection retry sleep (default 2s)

	// Log file permissions
	LogFileMode os.FileMode // log file permission mode (default 0644)

	// API key cache
	APIKeyCacheTTL time.Duration // API key cache TTL (default 5m)
}

// loadJWTSecret reads JWT secret from JWT_SECRET env var or JWT_SECRET_FILE.
func loadJWTSecret() string {
	secret := getEnv("JWT_SECRET", "")
	if secret != "" {
		return secret
	}
	filePath := getEnv("JWT_SECRET_FILE", "")
	if filePath != "" {
		// Convert relative path to absolute based on working directory
		if !filepath.IsAbs(filePath) {
			wd, err := os.Getwd()
			if err == nil {
				filePath = filepath.Join(wd, filePath)
			}
		}

		// Check file permissions (should be 600 or more restrictive)
		if info, err := os.Stat(filePath); err == nil {
			mode := info.Mode().Perm()
			if mode&0077 != 0 {
				log.Printf("[SECURITY WARNING] JWT_[REDACTED]_FILE %s 权限为 %04o，应为0600或更严格", filePath, mode)
			}
		}

		data, err := os.ReadFile(filePath) // #nosec G304 - file path from admin-controlled env var
		if err == nil {
			return strings.TrimSpace(string(data))
		}
		log.Printf("Warning: failed to read JWT_[REDACTED]_FILE %s: %v", filePath, err)
	}
	return ""
}

// parseDatabaseURL parses a postgres:// or postgresql:// connection string.
func parseDatabaseURL(url string) (host string, port int, user, password, dbname, sslmode string) {
	sslmode = "disable"
	// Strip prefix
	url = strings.TrimPrefix(url, "postgresql://")
	url = strings.TrimPrefix(url, "postgres://")

	// Extract sslmode from query
	if idx := strings.Index(url, "?"); idx != -1 {
		query := url[idx+1:]
		url = url[:idx]
		for _, part := range strings.Split(query, "&") {
			kv := strings.SplitN(part, "=", 2)
			if len(kv) == 2 && kv[0] == "sslmode" {
				sslmode = kv[1]
			}
		}
	}

	// user:password@host:port/dbname
	atIdx := strings.LastIndex(url, "@")
	if atIdx == -1 {
		return "localhost", 5432, "", "", url, sslmode
	}
	credPart := url[:atIdx]
	addrPart := url[atIdx+1:]

	// credentials
	colonIdx := strings.Index(credPart, ":")
	if colonIdx != -1 {
		user = credPart[:colonIdx]
		password = credPart[colonIdx+1:]
	} else {
		user = credPart
	}

	// address + dbname
	slashIdx := strings.Index(addrPart, "/")
	if slashIdx != -1 {
		dbname = addrPart[slashIdx+1:]
		addrPart = addrPart[:slashIdx]
	}

	// host:port
	portColonIdx := strings.LastIndex(addrPart, ":")
	if portColonIdx != -1 {
		host = addrPart[:portColonIdx]
		if p, err := strconv.Atoi(addrPart[portColonIdx+1:]); err == nil {
			port = p
		} else {
			port = 5432
		}
	} else {
		host = addrPart
		port = 5432
	}
	return
}

// Load reads configuration from environment variables with sensible defaults.
func Load() *Config {
	// Load .env file if it exists
	envFile := ".env"
	if err := godotenv.Load(envFile); err != nil {
		// .env file is optional, only log if it's not a "file not found" error
		if !os.IsNotExist(err) {
			log.Printf("警告：加载.env文件失败: %v", err)
		}
	} else {
		// Security check: .env file permissions should be 600 or more restrictive
		if info, err := os.Stat(envFile); err == nil {
			mode := info.Mode().Perm()
			if mode&0077 != 0 {
				log.Printf("[SECURITY WARNING] .env文件 权限为 %04o，应为0600或更严格", mode)
			}
		}
	}

	// Support legacy DATABASE_URL format
	dbHost := getEnv("DB_HOST", "localhost")
	dbPort := getInt("DB_PORT", 5432)
	dbUser := getEnv("DB_USER", "postgres")
	dbPassword := getEnv("DB_PASSWORD", "")
	dbName := getEnv("DB_NAME", "tg_sender")
	dbSSLMode := getEnv("DB_SSLMODE", "disable")

	if databaseURL := getEnv("DATABASE_URL", ""); databaseURL != "" {
		dbHost, dbPort, dbUser, dbPassword, dbName, dbSSLMode = parseDatabaseURL(databaseURL)
		log.Printf("[config] 使用DATABASE_URL连接数据库 (host=%s port=%d db=%s)", dbHost, dbPort, dbName)
	}

	cfg := &Config{
		Version:        getEnv("APP_VERSION", "v2.6.0"),
		ServerPort:     getEnv("SERVER_PORT", "8080"),
		ServerHost:     getEnv("SERVER_HOST", "0.0.0.0"),
		ReadTimeout:    getDuration("READ_TIMEOUT", 30*time.Second),
		WriteTimeout:   getDuration("WRITE_TIMEOUT", 30*time.Second),
		MaxHeaderBytes: getInt("MAX_HEADER_BYTES", 1<<20),
		AllowedOrigins: getEnv("ALLOWED_ORIGINS", "*"),

		DBHost:        dbHost,
		DBPort:        dbPort,
		DBUser:        dbUser,
		DBPassword:    dbPassword,
		DBName:        dbName,
		DBSSLMode:     dbSSLMode,
		DBMaxConns:    getInt("DB_MAX_CONNS", 20),
		DBMaxIdle:     getInt("DB_MAX_IDLE", 5),
		DBMaxLife:     getDuration("DB_MAX_LIFE", 30*time.Minute),
		DBMaxIdleTime: getDuration("DB_MAX_IDLE_TIME", 10*time.Minute),

		JWTSecret:     loadJWTSecret(),
		JWTExpiry:     getDuration("JWT_EXPIRY", 2*time.Hour), // F-21: 默认缩短至2h，refresh轮换兜底
		JWTRefreshExp: getDuration("JWT_REFRESH_EXP", 7*24*time.Hour),

		TGAPIID:   getIntFallback("TG_API_ID", "TELEGRAM_API_ID", 0),
		// R119: SMTP 通知邮件(PORT 默认 465 兼容多数服务商; Host 留空=通知关闭)
		SMTPHost: os.Getenv("SMTP_HOST"),
		SMTPPort: getIntFallback("SMTP_PORT", "SMTP_PORT", 465),
		SMTPUser: os.Getenv("SMTP_USER"),
		SMTPPass: os.Getenv("SMTP_PASS"),
		SMTPFrom: os.Getenv("SMTP_FROM"),
		TGAPIHash: getEnvFallback("TG_API_HASH", "TELEGRAM_API_HASH", ""),
		TGProxyURL: getEnv("TG_PROXY_URL", ""),

		TGTransport:      getEnv("TG_TRANSPORT", "auto"),
		TGMTPProxyAddr:   getEnv("TG_MTPROXY_ADDR", ""),
		TGMTPProxySecret: getEnv("TG_MTPROXY_SECRET", ""),

		TGAutoMigrateFrom: getEnv("TG_AUTO_MIGRATE_FROM", ""),
		TGAutoMigrateTo:   int32(getIntFallback("TG_AUTO_MIGRATE_TO", "", 2)),
	// 批次2(v1.19/1.27): 默认代理——新注册/导入的账号未指定代理时自动挂这个
	// 支持 socks5://user:pass@host:port / http://... 格式
	DefaultProxy: getEnv("DEFAULT_PROXY", ""),

		ProtocolPlanIDs: getEnv("PROTOCOL_PLAN_IDS", "pro,enterprise"),

		GlobalTaskConcurrency: getIntFallback("GLOBAL_TASK_CONCURRENCY", "", 20),
		RiskRetentionDays:     getIntFallback("RISK_RETENTION_DAYS", "", 7),
		// G-01: 审计保留期默认 90 天（原 7 天不足以支撑安全事件调查与合规要求）
		AuditRetentionDays:    getIntFallback("AUDIT_RETENTION_DAYS", "", 90),
		// G-02: 活动记录保留期默认 90 天（原 7 天静默硬删租户历史活动数据）
		CampaignRetentionDays: getIntFallback("CAMPAIGN_RETENTION_DAYS", "", 90),

		Env:           getEnv("ENV", "development"),
		LogLevel:      getEnv("LOG_LEVEL", "info"),
		LogFile:       getEnv("LOG_FILE", ""),
		MaxAccounts:   getInt("MAX_ACCOUNTS", 20),
		MaxConcurrent: getInt("MAX_CONCURRENT", 5),
		MaxPoolSize:   getInt("MAX_POOL_SIZE", 10),

		MinSendInterval: getDuration("MIN_SEND_INTERVAL", 4*time.Second),
		MaxSendInterval: getDuration("MAX_SEND_INTERVAL", 12*time.Second),

		Mode: getEnv("MODE", "auto"),

		AdaptiveCheckInterval: getDuration("ADAPTIVE_CHECK_INTERVAL", 30*time.Second),
		MemoryLimitMB:         getInt("MEMORY_LIMIT_MB", 2800),
		MemHighThreshold:      getFloat("MEM_HIGH_THRESHOLD", 80),
		MemLowThreshold:       getFloat("MEM_LOW_THRESHOLD", 50),
		MemSafeThreshold:      getFloat("MEM_SAFE_THRESHOLD", 30),
		FailRateThreshold:     getFloat("FAIL_RATE_THRESHOLD", 20),
		FailRateSafe:          getFloat("FAIL_RATE_SAFE", 5),
		QueueDeepThreshold:    getInt("QUEUE_DEEP_THRESHOLD", 200),
		QueueLightThreshold:   getInt("QUEUE_LIGHT_THRESHOLD", 50),

		AuthTimeout:           getDuration("AUTH_TIMEOUT", 5*time.Minute),
		SessionBackupInterval: getDuration("SESSION_BACKUP_INTERVAL", 1*time.Hour),
		PoolIdleTimeout:       getDuration("POOL_IDLE_TIMEOUT", 5*time.Minute),
		RequestTimeout:        getDuration("REQUEST_TIMEOUT", 30*time.Second),

		ScrapeLinksTimeout:        getDuration("SCRAPE_LINKS_TIMEOUT", 30*time.Minute),
		TaskRetrySleep:            getDuration("TASK_RETRY_SLEEP", 5*time.Second),
		DialogsLimit:              getInt("DIALOGS_LIMIT", 200),
		MediaDir:                  getEnv("MEDIA_DIR", "./media"),
		AvatarDir:                 getEnv("AVATAR_DIR", "./data/avatars"),
		AlertBotToken:             getEnv("TG_ALERT_BOT_TOKEN", ""),
		AlertChatID:               getEnv("TG_ALERT_CHAT_ID", ""),
		MemAlertThresholdMB:       getInt("MEM_ALERT_THRESHOLD_MB", 3072),
		QueueAlertThreshold:       getInt("QUEUE_ALERT_THRESHOLD", 500),
		MonitorRuleReloadInterval: getDuration("MONITOR_RULE_RELOAD_INTERVAL", 30*time.Second),
		BroadcastWorkers:          getInt("BROADCAST_WORKERS", 5),
		AuthPollInterval:          getDuration("AUTH_POLL_INTERVAL", 5*time.Second),
		AuthIdleTimeout:           getDuration("AUTH_IDLE_TIMEOUT", 30*time.Second),

		ResourceMonitorInterval: getDuration("RESOURCE_MONITOR_INTERVAL", 30*time.Second),
		ApprovalCheckInterval:   getDuration("APPROVAL_CHECK_INTERVAL", 5*time.Minute),
		ApprovalCheckLimit:      getInt("APPROVAL_CHECK_LIMIT", 50),
		ShutdownTimeout:         getDuration("SHUTDOWN_TIMEOUT", 30*time.Second),
		QuotaResetInterval:      getDuration("QUOTA_RESET_INTERVAL", 1*time.Hour),
		SSEChannelBuffer:        getInt("SSE_CHANNEL_BUFFER", 500),
		SSEHeartbeat:            getDuration("SSE_HEARTBEAT", 30*time.Second),
		DefaultPageSize:         getInt("DEFAULT_PAGE_SIZE", 20),
		MaxPageSize:             getInt("MAX_PAGE_SIZE", 200),
		GroupDefaultPageSize:    getInt("GROUP_DEFAULT_PAGE_SIZE", 50),
		TenantDailySoftLimit:    getInt("TENANT_DAILY_SOFT_LIMIT", 10000),
		MaxMsgsPerTask:          getInt("MAX_MSGS_PER_TASK", 500),
		MaxMappingsPerTask:      getInt("MAX_MAPPINGS_PER_TASK", 500),
		DBHealthCheckPeriod:     getDuration("DB_HEALTH_CHECK_PERIOD", 30*time.Second),
		ConnectRetrySleep:       getDuration("CONNECT_RETRY_SLEEP", 2*time.Second),
		LogFileMode:             os.FileMode(getInt("LOG_FILE_MODE", 0644)),
	}

	if err := cfg.Validate(); err != nil {
		log.Fatalf("config validation failed: %v", err)
	}
	return cfg
}

// Validate checks required configuration fields.
func (c *Config) Validate() error {
	if c.JWTSecret == "" {
		return fmt.Errorf("JWT_SECRET is required")
	}
	if len(c.JWTSecret) < 32 {
		return fmt.Errorf("JWT_SECRET must be at least 32 characters")
	}
	// Check entropy: reject low-entropy secrets (e.g., all same character)
	entropy := 0.0
	charCounts := make(map[rune]int)
	for _, ch := range c.JWTSecret {
		charCounts[ch]++
	}
	for _, count := range charCounts {
		p := float64(count) / float64(len(c.JWTSecret))
		if p > 0 {
			entropy -= p * math.Log2(p)
		}
	}
	if entropy < 2.0 {
		return fmt.Errorf("JWT_SECRET entropy too low, use a more random string")
	}
	if c.TGAPIID == 0 {
		return fmt.Errorf("TG_API_ID is required")
	}
	if c.TGAPIHash == "" {
		return fmt.Errorf("TG_API_HASH is required")
	}
	if c.DBPassword == "" {
		return fmt.Errorf("DB_PASSWORD is required")
	}
	if c.DBName == "" {
		return fmt.Errorf("DB_NAME is required")
	}
	if c.MaxAccounts <= 0 {
		c.MaxAccounts = 20
	}
	if c.MaxConcurrent <= 0 {
		c.MaxConcurrent = 5
	}
	if c.MaxPoolSize <= 0 {
		c.MaxPoolSize = 10
	}
	if c.MinSendInterval <= 0 {
		c.MinSendInterval = 4 * time.Second
	}
	if c.MaxSendInterval <= 0 {
		c.MaxSendInterval = 12 * time.Second
	}
	if c.MinSendInterval > c.MaxSendInterval {
		c.MinSendInterval, c.MaxSendInterval = c.MaxSendInterval, c.MinSendInterval
	}
	if c.AuthTimeout <= 0 {
		c.AuthTimeout = 5 * time.Minute
	}
	if c.SessionBackupInterval <= 0 {
		c.SessionBackupInterval = 1 * time.Hour
	}

	if c.Mode == "" {
		c.Mode = "auto"
	}
	if c.AdaptiveCheckInterval <= 0 {
		c.AdaptiveCheckInterval = 30 * time.Second
	}
	if c.MemHighThreshold <= 0 {
		c.MemHighThreshold = 80
	}
	if c.MemLowThreshold <= 0 {
		c.MemLowThreshold = 50
	}
	if c.MemSafeThreshold <= 0 {
		c.MemSafeThreshold = 30
	}
	if c.FailRateThreshold <= 0 {
		c.FailRateThreshold = 20
	}
	if c.FailRateSafe <= 0 {
		c.FailRateSafe = 5
	}
	if c.QueueDeepThreshold <= 0 {
		c.QueueDeepThreshold = 200
	}
	if c.QueueLightThreshold <= 0 {
		c.QueueLightThreshold = 50
	}
	if c.PoolIdleTimeout <= 0 {
		c.PoolIdleTimeout = 5 * time.Minute
	}
	if c.RequestTimeout <= 0 {
		c.RequestTimeout = 30 * time.Second
	}
	if c.TaskWorkerCount <= 0 {
		c.TaskWorkerCount = 3
	}
	if c.TaskClaimBatch <= 0 {
		c.TaskClaimBatch = 10
	}
	if c.TaskTimeoutClaim <= 0 {
		c.TaskTimeoutClaim = 5
	}
	if c.TaskLockTimeoutMin <= 0 {
		// P1-12 审计修复: 原来默认 5 分钟——但任务最长 30 分钟, 运行中的任务锁 5 分钟
		// 过期被 reclaim, 长任务被重复执行。改为 40 分钟 > 任务上限, 宁可晚回收不早重复。
		c.TaskLockTimeoutMin = 40
	}
	if c.ScrapeLinksTimeout <= 0 {
		c.ScrapeLinksTimeout = 30 * time.Minute
	}
	if c.TaskRetrySleep <= 0 {
		c.TaskRetrySleep = 5 * time.Second
	}
	if c.JoinIntervalMinutes <= 0 {
		c.JoinIntervalMinutes = 2
	}
	if c.MediaDir == "" {
		c.MediaDir = "./media"
	}
	if c.DialogsLimit <= 0 {
		c.DialogsLimit = 200
	}
	if c.MemAlertThresholdMB <= 0 {
		c.MemAlertThresholdMB = 3072
	}
	if c.QueueAlertThreshold <= 0 {
		c.QueueAlertThreshold = 500
	}
	if c.MonitorRuleReloadInterval <= 0 {
		c.MonitorRuleReloadInterval = 30 * time.Second
	}
	if c.BroadcastWorkers <= 0 {
		c.BroadcastWorkers = 5
	}
	if c.AuthPollInterval <= 0 {
		c.AuthPollInterval = 5 * time.Second
	}
	if c.AuthIdleTimeout <= 0 {
		c.AuthIdleTimeout = 30 * time.Second
	}
	if c.ResourceMonitorInterval <= 0 {
		c.ResourceMonitorInterval = 30 * time.Second
	}
	if c.ApprovalCheckInterval <= 0 {
		c.ApprovalCheckInterval = 5 * time.Minute
	}
	if c.ApprovalCheckLimit <= 0 {
		c.ApprovalCheckLimit = 50
	}
	if c.ShutdownTimeout <= 0 {
		c.ShutdownTimeout = 30 * time.Second
	}
	if c.QuotaResetInterval <= 0 {
		c.QuotaResetInterval = 1 * time.Hour
	}
	if c.SSEChannelBuffer <= 0 {
		c.SSEChannelBuffer = 500
	}
	if c.SSEHeartbeat <= 0 {
		c.SSEHeartbeat = 30 * time.Second
	}
	if c.DefaultPageSize <= 0 {
		c.DefaultPageSize = 20
	}
	if c.MaxPageSize <= 0 {
		c.MaxPageSize = 200
	}
	if c.GroupDefaultPageSize <= 0 {
		c.GroupDefaultPageSize = 50
	}
	if c.TenantDailySoftLimit <= 0 {
		c.TenantDailySoftLimit = 10000
	}
	if c.MaxMsgsPerTask <= 0 {
		c.MaxMsgsPerTask = 500
	}
	if c.MaxMappingsPerTask <= 0 {
		c.MaxMappingsPerTask = 500
	}
	if c.DBHealthCheckPeriod <= 0 {
		c.DBHealthCheckPeriod = 30 * time.Second
	}
	if c.ConnectRetrySleep <= 0 {
		c.ConnectRetrySleep = 2 * time.Second
	}
	if c.LogFileMode == 0 {
		c.LogFileMode = 0644
	}
	if c.APIKeyCacheTTL <= 0 {
		c.APIKeyCacheTTL = 5 * time.Minute
	}
	return nil
}

func (c *Config) DSN() string {
	return fmt.Sprintf("host=%s port=%d user=%s password=%s dbname=%s sslmode=%s",
		c.DBHost, c.DBPort, c.DBUser, c.DBPassword, c.DBName, c.DBSSLMode)
}

func getEnv(key, defaultVal string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return defaultVal
}

func getEnvFallback(key, fallbackKey, defaultVal string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	if v := os.Getenv(fallbackKey); v != "" {
		return v
	}
	return defaultVal
}

func getIntFallback(key, fallbackKey string, defaultVal int) int {
	if v := os.Getenv(key); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	// P2-65 审计修复: 原来空 fallbackKey 时 os.Getenv("") 无意义读取。跳过空键。
	if fallbackKey != "" {
		if v := os.Getenv(fallbackKey); v != "" {
			if n, err := strconv.Atoi(v); err == nil {
				return n
			}
		}
	}
	return defaultVal
}

func getInt(key string, defaultVal int) int {
	if v := os.Getenv(key); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	return defaultVal
}

func getDuration(key string, defaultVal time.Duration) time.Duration {
	if v := os.Getenv(key); v != "" {
		if d, err := time.ParseDuration(v); err == nil {
			return d
		}
		if n, err := strconv.Atoi(v); err == nil {
			return time.Duration(n) * time.Second
		}
		log.Printf("[config] 警告：%s=%q 无效，使用默认值 %v", key, v, defaultVal)
	}
	return defaultVal
}

func getFloat(key string, defaultVal float64) float64 {
	if v := os.Getenv(key); v != "" {
		if f, err := strconv.ParseFloat(v, 64); err == nil {
			return f
		}
	}
	return defaultVal
}

// 2026-10-22 审计清理: getStringSlice 已删除——全仓无调用点的死代码
