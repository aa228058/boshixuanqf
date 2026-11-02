package account

import (
	"context"
	"fmt"
	"log"
	"os"
	"sync"
	"tg-sender/internal/safego"
	"tg-sender/internal/sse"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/mtgo-labs/mtgo/telegram"
	"github.com/mtgo-labs/storage"
	"tg-sender/internal/config"
	"tg-sender/internal/domain"
)

// Engine manages Telegram account connections with lazy loading and connection pooling.
type Engine struct {
	cfg       *config.Config
	repo      domain.AccountRepository
	riskRepo  domain.RiskEventRepository
	quotaRepo domain.QuotaRepository

	// sseBroker 状态变更实时推送（账号在线/离线/受限等 → 前端即时刷新，无需轮询）
	sseBroker *sse.Broker

	// banRate is the global 24h ban rate, refreshed every 5 minutes by a background goroutine.
	banRateMu   sync.RWMutex
	banRate     float64
	clients     map[string]*managedClient // accounts in auth flow
	mu          sync.RWMutex
	poolClients map[string]*pooledClient // active connections for task execution
	poolMu      sync.RWMutex
	maxPoolSize int

	// DC 自动迁移冷却(每账号 10 分钟最多试一次, 防失败死循环)
	migrateMu       sync.Mutex
	migrateAttempts map[string]time.Time

	// 自动迁移判定缓存(治每30s每号2次DB查询): key → 会话名+DC, 5分钟TTL
	autoMigrateMu    sync.Mutex
	autoMigrateCache map[string]autoMigrateInfo

	// 孤儿锁表: 池内 entry 被断开/清理摘除后, 持有者仍握着 pc.mu。
	// 按 client 指针登记, ReleaseClient 兜底认领解锁 —— 否则锁永久泄漏
	// (表现: 该账号之后所有 GetClient 永远阻塞, 只能重启进程)。
	orphanMu sync.Mutex
	orphans  map[*telegram.Client]*pooledClient

	// 传输方式缓存: dc → 上次成功的传输(tcp/http/mtproxy)。
	// 命中后直接用它, 跳过已证死亡的传输(治"每次连客户端白等30秒TCP")。
	transportCacheMu sync.RWMutex
	transportCache   map[int32]string

	// 同账号并发建连去重: key → 建连完成的信号通道。
	// 并发 GetClient 共享一次建连, 防止双连接竞争/客户端泄漏。
	connectInflightMu sync.Mutex
	connectInflight   map[string]chan struct{}

	// R181: 死会话密钥熔断表(AUTH_KEY_UNREGISTERED 的账号 TTL 内 GetClient 快速失败)
	deadKeyMu sync.Mutex
	deadKey   map[uuid.UUID]time.Time
	// R182.1: 认证成功时间, markDeadKey 对 5 分钟内的新登录宽限(防 gotd flush 时序竞态误熔断)
	authGrace map[uuid.UUID]time.Time

	// Session backup
	backupDir string // directory to backup session files

	// Adaptive profile (set by adaptive engine)
	adaptiveProfile *struct {
		MinSendInterval time.Duration
		MaxSendInterval time.Duration
	}

	// Shared PostgreSQL storage for session persistence (created once in NewEngine, reused everywhere)
	pgStorage storage.Storage
	pool      *pgxpool.Pool

	// Background goroutine lifecycle
	stopCh   chan struct{}
	stopOnce sync.Once
	wg       sync.WaitGroup
}

// NOTE: statuses cache removed — GetAccountStatus reads directly from DB for accuracy.
// If caching is reintroduced, use a TTL-backed map with proper locking.

type managedClient struct {
	keepOnline bool // 导入协议号时置 true: 认证成功后不断开, 保活常驻(用于连通性验证)
	client        *telegram.Client
	accountID     uuid.UUID
	tenantID      uuid.UUID
	codeCh        chan string
	passwordCh    chan string
	cancel        context.CancelFunc
	deviceProfile map[string]any
	deviceType    string
	authPhase     string
	passwordHint  string
	mu            sync.Mutex
	// R182.2: 登录会话名(rename 前暂存), 统一空闲退出时用于二次搬运 gotd 最终 flush 的密钥
	finalMoveSession string
	closeOnce     sync.Once
	// 错误重试计数: gotd 在码/密码被 TG 拒绝后重新回调 CodeFunc/PasswordFunc,
	// 通过 submitted 标志区分"首次等待"与"错误后重试", 驱动前端错误提示.
	codeSubmitted bool
	codeRetries   int
	// R158: 传输重试时自动复用上次的验证码
	lastCode       string
	codeAutoResend int
	loginSessionName string // R160
	probeFails         int    // R162/R163: 探活连续失败计数(≥2 才摘除): 登录流程用临时会话名, 成功后改名接管(防覆盖正式会话)
	pwdSubmitted  bool
	pwdRetries    int
	// finished: 认证流程已结束(成功/失败/断开)。置位后 SubmitCode/SubmitPassword
	// 拒绝提交, 防止向已关闭的 channel 发送(panic)。
	finished bool
}

// finish 幂等结束认证流程: 标记 finished + 关闭提交通道。
// 所有原本直接 closeOnce.Do(close(ch)) 的位置必须改走这里。
func (mc *managedClient) finish() {
	mc.closeOnce.Do(func() {
		mc.mu.Lock()
		mc.finished = true
		mc.mu.Unlock()
		close(mc.codeCh)
		close(mc.passwordCh)
	})
}

type pooledClient struct {
	client    *telegram.Client
	accountID uuid.UUID
	tenantID  uuid.UUID
	lastUsed  time.Time
	mu        sync.Mutex // protects client from concurrent use (mtgo client is not thread-safe)

	probeFails int // R163: 探活连续失败计数(≥2 才重拨, 抗抖动)
}

// NewEngine creates a new account engine using the shared database pool.
func NewEngine(cfg *config.Config, pool *pgxpool.Pool, repo domain.AccountRepository, riskRepo domain.RiskEventRepository, quotaRepo domain.QuotaRepository) (*Engine, error) {
	backupDir := os.Getenv("SESSION_BACKUP_DIR")
	if backupDir == "" {
		backupDir = "./session_backups"
	}
	if err := os.MkdirAll(backupDir, 0755); err != nil {
		log.Printf("[engine] 创建目录失败: %v", err)
	}

	// R181: 头像目录启动自检——不可写时一次性给出可操作修复提示。
	// 此前每次账号上线事件都会刷一条 mkdir permission denied，日志被刷屏用户也看不到根因。
	if cfg.AvatarDir != "" {
		if err := os.MkdirAll(cfg.AvatarDir, 0755); err != nil {
			log.Printf("[engine] 头像目录 %s 不可写: %v | 修复: chown -R 10001:10001 ./data && chmod -R u+rwX ./data", cfg.AvatarDir, err)
		} else if probe, err := os.CreateTemp(cfg.AvatarDir, ".write_test_*"); err != nil {
			log.Printf("[engine] 头像目录 %s 不可写: %v | 修复: chown -R 10001:10001 ./data && chmod -R u+rwX ./data", cfg.AvatarDir, err)
		} else {
			probe.Close()
			os.Remove(probe.Name())
		}
	}

	// Use shared database pool for session persistence (avoid duplicate connection overhead)
	pgStorage := storage.NewAdapter(NewPgxPoolAdapter(pool))

	e := &Engine{
		cfg:         cfg,
		repo:        repo,
		riskRepo:    riskRepo,
		quotaRepo:   quotaRepo,
		clients:     make(map[string]*managedClient),
		poolClients: make(map[string]*pooledClient),
		maxPoolSize: cfg.MaxPoolSize,

		// 并发建连去重/传输缓存/迁移冷却: 构造时显式初始化(nil map 写入会 panic)
		connectInflight: make(map[string]chan struct{}),
		transportCache:  make(map[int32]string),
		migrateAttempts: make(map[string]time.Time),
		deadKey:         make(map[uuid.UUID]time.Time),
		authGrace:       make(map[uuid.UUID]time.Time),
		backupDir:   backupDir,
		pgStorage:   pgStorage,
		pool:        pool,
		stopCh:      make(chan struct{}),
	}
	e.wg.Add(1)
	safego.Go(func() { e.refreshBanRateLoop() })
	e.wg.Add(1)
	safego.Go(func() { e.cleanupLoop() })
	return e, nil
}

// cleanupLoop 每日清理: 风险事件/审计日志只保留7天。
// 这两张表只增不减, 不清理的话几个月后仪表盘查询会越来越慢。
// SetSSEBroker 注入 SSE broker，用于账号状态变更实时推送。
func (e *Engine) SetSSEBroker(broker *sse.Broker) {
	e.sseBroker = broker
}

// updateStatus 统一状态变更出口：落库 + SSE 推送。
func (e *Engine) updateStatus(ctx context.Context, tenantID, accountID uuid.UUID, status domain.AccountStatus) error {
	if err := e.repo.UpdateStatus(ctx, tenantID, accountID, status); err != nil {
		return err
	}
	e.notifyAccountStatus(tenantID, accountID, status)
	// 头像补拉: 账号上线且缺头像时异步补齐(存量号自动恢复, 有头像立即返回)
	if status == domain.AccountStatusOnline {
		e.ensureAvatarAsync(tenantID, accountID)
		// R130 手机号回读: 协议号(phone 空)上线时异步回读 self.Phone 存库
		e.ensurePhoneAsync(tenantID, accountID)
	}
	return nil
}

// notifyAccountStatus 向租户广播账号状态事件（BroadcastToTenant 非阻塞）。
func (e *Engine) notifyAccountStatus(tenantID, accountID uuid.UUID, status domain.AccountStatus) {
	if e.sseBroker == nil {
		return
	}
	e.sseBroker.BroadcastToTenant(tenantID, "events", map[string]any{
		"type":       "account_status",
		"account_id": accountID.String(),
		"status":     string(status),
	})
}

func (e *Engine) cleanupLoop() {
	defer e.wg.Done()
	e.cleanupOnce()
	ticker := time.NewTicker(24 * time.Hour)
	defer ticker.Stop()
	for {
		select {
		case <-e.stopCh:
			return
		case <-ticker.C:
			e.cleanupOnce()
		}
	}
}

func (e *Engine) cleanupOnce() {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	// P2-05: 保留天数可配置(.env RISK/AUDIT/CAMPAIGN_RETENTION_DAYS, 默认90)
	fmtSprintf := func(days int) string { return fmt.Sprintf("INTERVAL '%d days'", days) }
	if _, err := e.pool.Exec(ctx, fmt.Sprintf(`DELETE FROM risk_events WHERE created_at < NOW() - %s`, fmtSprintf(e.cfg.RiskRetentionDays))); err != nil {
		log.Printf("[cleanup] risk_events: %v", err)
	}
	if _, err := e.pool.Exec(ctx, fmt.Sprintf(`DELETE FROM audit_logs WHERE created_at < NOW() - %s`, fmtSprintf(e.cfg.AuditRetentionDays))); err != nil {
		log.Printf("[cleanup] audit_logs: %v", err)
	}
	if _, err := e.pool.Exec(ctx, fmt.Sprintf(`DELETE FROM campaigns WHERE created_at < NOW() - %s`, fmtSprintf(e.cfg.CampaignRetentionDays))); err != nil {
		log.Printf("[cleanup] campaigns: %v", err)
	}
	// 删号残留: 参战记录里账号已不存在 → 标阵亡(显示准确)
	if _, err := e.pool.Exec(ctx,
		`UPDATE campaign_accounts SET status='dead'
		 WHERE status != 'dead' AND account_id NOT IN (SELECT account_id FROM accounts)`); err != nil {
		log.Printf("[cleanup] campaign orphan: %v", err)
	}
}

// refreshBanRateLoop refreshes the global 24h ban rate every 5 minutes.
// Stops automatically when the engine stops (stopCh closed).
func (e *Engine) refreshBanRateLoop() {
	defer e.wg.Done()
	e.updateBanRate()
	ticker := time.NewTicker(5 * time.Minute)
	defer ticker.Stop()
	for {
		select {
		case <-e.stopCh:
			return
		case <-ticker.C:
			e.updateBanRate()
		}
	}
}

// updateBanRate computes bans in last 24h / total accounts and stores it atomically.
func (e *Engine) updateBanRate() {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	// P1-18 审计修复: 原来 Count24hAll 统计全部风控事件——flood_wait/session_revoked
	// 等非封禁事件把 banRate 拉高, 发送间隔被误放大。改只统计 account_banned。
	bans, err := e.riskRepo.Count24hBans(ctx)
	if err != nil {
		log.Printf("[engine] ban rate refresh failed (risk count): %v", err)
		return
	}
	total, err := e.repo.CountAll(ctx)
	if err != nil || total == 0 {
		log.Printf("[engine] ban rate refresh failed (account count): %v", err)
		return
	}
	e.banRateMu.Lock()
	e.banRate = float64(bans) / float64(total)
	e.banRateMu.Unlock()
}

// ConnectAccount starts the authentication flow for an account.
// It creates a client, starts the auth goroutine, and returns immediately.
// The client does NOT keep a long-running connection; session is saved after auth success.

func accountKey(tenantID, accountID uuid.UUID) string {
	return tenantID.String() + ":" + accountID.String()
}

// generateDeviceConfig creates a realistic Telegram device profile using device-manager.
// Supports iOS, macOS. The profile is deterministic: same sessionName + deviceType always yields the same device.

// GetByID implements AccountEngine.
func (e *Engine) GetByID(ctx context.Context, tenantID, accountID uuid.UUID) (*domain.Account, error) {
	return e.repo.GetByID(ctx, tenantID, accountID)
}
