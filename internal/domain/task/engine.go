package task

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"regexp"
	"runtime/debug"
	"strings"
	"sync"
	"sync/atomic"
	"tg-sender/internal/safego"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/mtgo-labs/mtgo/telegram"
	"github.com/mtgo-labs/mtgo/telegram/types"
	"tg-sender/internal/config"
	"tg-sender/internal/database"
	"tg-sender/internal/domain"
	"tg-sender/internal/logger"
	"tg-sender/internal/repository"
	"tg-sender/internal/sse"
)

// Pre-compiled regex patterns for link scraping (package-level to avoid recompilation)
var (
	tmeLinkRe     = regexp.MustCompile(`(?i)(https?://)?(t\.me|telegram\.me)/(joinchat/)?(@?[+a-zA-Z0-9_-]+)`)
	privateLinkRe = regexp.MustCompile(`(?i)(https?://)?(t\.me|telegram\.me)/(joinchat/|\+)([a-zA-Z0-9_-]+)`)
)

// scrapeHandlerRegistry tracks active scrape-link handlers to prevent memory leaks.
// Key: handlerID (uuid), Value: *scrapeHandlerEntry
type scrapeHandlerEntry struct {
	active int32 // atomic flag
}

var scrapeHandlerRegistry sync.Map

// taskLog is the structured logger for the task engine.
var taskLog = logger.New("task-engine")

// Engine manages task queue and execution.
type Engine struct {
	cfg          *config.Config
	db           *database.DB
	repo         *repository.TaskRepository
	logRepo      *repository.TaskLogRepository
	quotaRepo    *repository.QuotaRepository
	accEngine    AccountEngine
	riskRepo     *repository.RiskRepository
	memberRepo   domain.MemberRepository
	approvalRepo domain.PendingApprovalRepository
	groupRepo    *repository.GroupRepository
	sseBroker    *sse.Broker
	// R119: 任务结束邮件通知回调(由 container 注入, nil=不通知)
	finishNotify func(tenantID uuid.UUID, taskName string, taskType string, status string, done, failed int)

	workerID string
	stopCh   chan struct{}
	stopOnce sync.Once
	// GlobalLimitFn 返回当前全局并发上限(由 adaptive 内存调度注入, 按服务器内存动态调整).
	// nil 表示不限. 领取任务前检查全局 running 数, 达到上限则本轮不领.
	GlobalLimitFn func() int
	wg            sync.WaitGroup
	listenConn    *pgxpool.Conn
	listenMu      sync.Mutex

	// taskCtxs holds cancel functions for active tasks (taskID -> cancel)
	taskCtxs sync.Map
	// runningTasks tracks in-flight task IDs to prevent duplicate execution
	runningTasks sync.Map
	// progressCache 缓存各任务最新进度（TaskProgress），StreamProgress
	// 读这里而不是每秒查库——N 个前端同时看进度时 DB QPS 归零。
	// 任务结束时由 goroutine defer 删除。
	progressCache sync.Map
	// scheduler provides resource-aware, plan-based dynamic concurrency control
	scheduler *AutoScheduler

	// linkScraper is the persistent background link scraping service
	linkScraper *LinkScraper

	// scrapedLinkRepo provides access to persistent scraped links storage
	scrapedLinkRepo domain.ScrapedLinkRepository

	// dictRepo 文案变量(数据字典), 群发时 {{词典名}} 随机替换
	dictRepo *repository.TextDictRepository

	// systemGroupRepo 群库大厅(发现群组页), 加入失败时回写失效状态
	systemGroupRepo *repository.SystemGroupRepository

	// scrapeRegMu protects scrapeRegistered to prevent duplicate OnMessage registration
	scrapeRegMu sync.Mutex
	// scrapeRegistered tracks which clients have active OnMessage handlers for monitor scrape
	scrapeRegistered map[string]bool
	globalSem chan struct{} // P1-01: 全局任务并发原子锁(容量=cfg.GlobalTaskConcurrency)
}

// SetFinishNotifier R119: 注入任务结束通知回调(container 接线用)
func (e *Engine) SetFinishNotifier(fn func(tenantID uuid.UUID, taskName string, taskType string, status string, done, failed int)) {
	e.finishNotify = fn
}

// AccountEngine interface for task execution.
type AccountEngine interface {
	// SendInterval returns a randomized send interval for an account level.
	// Deprecated: use SendIntervalWithGuard for tenant-configurable intervals.
	SendInterval(level domain.AccountLevel) time.Duration
	// SendIntervalWithGuard returns a send interval with guard-rail offset applied.
	// Tenant can set any min/max values (including 0); the system automatically offsets
	// to a safe range based on account level and recent ban rate.
	SendIntervalWithGuard(level domain.AccountLevel, tenantMin, tenantMax time.Duration) time.Duration
	GetClient(ctx context.Context, tenantID, accountID uuid.UUID) (*telegram.Client, error)
	ReleaseClient(tenantID, accountID uuid.UUID, client *telegram.Client)
	GetAccountLevel(ctx context.Context, tenantID, accountID uuid.UUID) domain.AccountLevel
	UpdateAccountStatus(ctx context.Context, tenantID, accountID uuid.UUID, status domain.AccountStatus) error
	GetByID(ctx context.Context, tenantID, accountID uuid.UUID) (*domain.Account, error)
	WarmClient(ctx context.Context, tenantID, accountID uuid.UUID)
}

// WarmAccounts 后台并行预热一批账号的连接（任务创建时调用，执行零等待）。
func (e *Engine) WarmAccounts(ctx context.Context, tenantID uuid.UUID, accountIDs []uuid.UUID) {
	for _, id := range accountIDs {
		e.accEngine.WarmClient(ctx, tenantID, id)
	}
}

// NewEngine creates a new task engine.
func NewEngine(cfg *config.Config, db *database.DB, repo *repository.TaskRepository, logRepo *repository.TaskLogRepository, quotaRepo *repository.QuotaRepository, riskRepo *repository.RiskRepository) *Engine {
	e := &Engine{
		cfg:              cfg,
		db:               db,
		repo:             repo,
		logRepo:          logRepo,
		quotaRepo:        quotaRepo,
		riskRepo:         riskRepo,
		workerID:         fmt.Sprintf("worker-%s", uuid.New().String()[:8]),
		stopCh:           make(chan struct{}),
		scheduler:        NewAutoScheduler(int64(cfg.MemoryLimitMB)),
		scrapeRegistered: make(map[string]bool),
	}
	if cfg.GlobalTaskConcurrency <= 0 {
		cfg.GlobalTaskConcurrency = 20
	}
	e.globalSem = make(chan struct{}, cfg.GlobalTaskConcurrency)
	return e
}

func (e *Engine) SetAccountEngine(ae AccountEngine) {
	e.accEngine = ae
}

// SetMemberRepo injects the member repository dependency.

func (e *Engine) SetMemberRepo(mr domain.MemberRepository) {
	e.memberRepo = mr
}

// SetApprovalRepo injects the pending approval repository dependency.

func (e *Engine) SetApprovalRepo(ar domain.PendingApprovalRepository) {
	e.approvalRepo = ar
}

// SetSSEBroker injects the SSE broker for task progress推送.

func (e *Engine) SetSSEBroker(broker *sse.Broker) {
	e.sseBroker = broker
}

// SetGroupRepo injects the group repository dependency.
func (e *Engine) SetGroupRepo(repo *repository.GroupRepository) {
	e.groupRepo = repo
}

// SetLinkScraper injects the persistent link scraper.
func (e *Engine) SetLinkScraper(scraper *LinkScraper) {
	e.linkScraper = scraper
}

// SetTextDictRepo injects the text-dict repository for {{dict}} variable substitution.
func (e *Engine) SetTextDictRepo(r *repository.TextDictRepository) {
	e.dictRepo = r
}

// SetSystemGroupRepo injects the system-group repository (群库大厅).
// R87-M10: 加入失败时回写群失效状态("已失效"筛选的数据源)。
func (e *Engine) SetSystemGroupRepo(r *repository.SystemGroupRepository) {
	e.systemGroupRepo = r
}

// SetScrapedLinkRepo injects the scraped link repository.
func (e *Engine) SetScrapedLinkRepo(repo domain.ScrapedLinkRepository) {
	e.scrapedLinkRepo = repo
}

// PlanDemandLimit 返回所有在册租户套餐并发之和（由 container 的全局上限函数取用）。
// 0 表示调度器未初始化；调用方应与硬件档位取 min 并设下限。
func (e *Engine) PlanDemandLimit() int {
	if e.scheduler == nil {
		return 0
	}
	return e.scheduler.TotalMaxConcurrent()
}

// loadTenantQuotas loads all active tenants and registers their plan quotas with the scheduler.
func (e *Engine) loadTenantQuotas(ctx context.Context) error {
	rows, err := e.db.Pool.Query(ctx, `
		SELECT tenant_id, max_concurrent_tasks FROM tenants WHERE status = 'active' AND (expires_at IS NULL OR expires_at > NOW())
	`)
	if err != nil {
		return fmt.Errorf("query tenants: %w", err)
	}
	defer rows.Close()
	count := 0
	for rows.Next() {
		var tenantID uuid.UUID
		var maxConcurrent int
		if err := rows.Scan(&tenantID, &maxConcurrent); err != nil {
			continue
		}
		if maxConcurrent <= 0 {
			maxConcurrent = 1
		}
		e.scheduler.RegisterTenant(tenantID, maxConcurrent)
		count++
	}
	log.Printf("[engine] 已加载 %d 个租户配额到调度器", count)
	return nil
}

// publishProgress sends task progress update via SSE if broker is available.
// Uses "events" topic to match frontend subscription via Handler("events").
// 同时写入 progressCache，供 StreamProgress 免库读取。
func (e *Engine) publishProgress(tenantID uuid.UUID, taskID uuid.UUID, progress domain.TaskProgress) {
	e.progressCache.Store(taskID, progress)
	if e.sseBroker != nil {
		e.sseBroker.BroadcastToTenant(tenantID, "events", map[string]any{
			"task_id":  taskID,
			"progress": progress,
			"time":     time.Now().UTC(),
		})
	}
}

// GetProgress 返回任务最新进度（内存缓存）。第二返回值 false = 无缓存
// （任务未在执行或刚结束），调用方可回退查库。
func (e *Engine) GetProgress(taskID uuid.UUID) (domain.TaskProgress, bool) {
	v, ok := e.progressCache.Load(taskID)
	if !ok {
		return domain.TaskProgress{}, false
	}
	p, ok := v.(domain.TaskProgress)
	return p, ok
}

// publishWaitFrame 发送等待倒计时帧(R118): 等待窗口内每秒一帧, 仅走 SSE 不落库。
// 前端用它显示"账号X 还剩N秒 → 下一目标"。target 是下一目标的 ID(群/私聊),
// 群名由前端群缓存映射。
func (e *Engine) publishWaitFrame(tenantID uuid.UUID, taskID uuid.UUID, accountID, targetID string, remaining, nextIndex, total int) {
	if e.sseBroker != nil {
		e.sseBroker.BroadcastToTenant(tenantID, "events", map[string]any{
			"type":       "task_wait",
			"task_id":    taskID,
			"account_id": accountID,
			"target_id":  targetID,
			"remaining":  remaining,
			"next_index": nextIndex,
			"total":      total,
			"time":       time.Now().UTC(),
		})
	}
}

// publishLog sends a single task log entry via SSE for real-time display.
func (e *Engine) publishLog(tenantID uuid.UUID, taskID uuid.UUID, log *domain.TaskLog) {
	log.Message = FormatLogMessage(log)
	if e.sseBroker != nil {
		e.sseBroker.BroadcastToTenant(tenantID, "events", map[string]any{
			"type":    "task_log",
			"task_id": taskID,
			"log":     log,
			"time":    time.Now().UTC(),
		})
	}
}
// startLogCleanup 每 24 小时自动删除 7 天前的历史日志, 防表膨胀。
func (e *Engine) startLogCleanup(ctx context.Context) {
	safego.Go(func() {
		ticker := time.NewTicker(24 * time.Hour)
		defer ticker.Stop()
		// 启动时先清一次(系统级清扫, 无租户上下文 → Global 版本)
		if n, err := e.logRepo.DeleteOlderThanGlobal(ctx, 7); err == nil && n > 0 {
			log.Printf("[task] 日志自动清理: 删除 %d 条(7天前)", n)
		}
		for {
			select {
			case <-ticker.C:
				if n, err := e.logRepo.DeleteOlderThanGlobal(ctx, 7); err == nil && n > 0 {
					log.Printf("[task] 日志自动清理: 删除 %d 条(7天前)", n)
				}
			case <-ctx.Done():
				return
			}
		}
	})
}

func (e *Engine) Start(ctx context.Context) error {
	e.startLogCleanup(ctx)
	// Load all active tenants and register their plan quotas
	if err := e.loadTenantQuotas(ctx); err != nil {
		log.Printf("[engine] 加载租户配额失败: %v", err)
	}

	if err := e.startListen(ctx); err != nil {
		return fmt.Errorf("start listen: %w", err)
	}
	// P1-45 审计修复: worker 数原来硬编码 3, cfg.TaskWorkerCount 配置形同虚设——用配置值
	workers := e.cfg.TaskWorkerCount
	if workers < 1 {
		workers = 3
	}
	for i := 0; i < workers; i++ {
		e.wg.Add(1)
		safego.Go(func() { e.workerLoop(ctx) })
	}
	e.wg.Add(1)
	safego.Go(func() { e.pollLoop(ctx) })
	if e.linkScraper != nil {
		e.linkScraper.Start(ctx)
	}
	// 账号冷却到期自动恢复: 每分钟把 blocked_until 已过的受限账号恢复为 online
	e.wg.Add(1)
	safego.Go(func() { e.unblockExpiredLoop(ctx) })
	return nil
}

// unblockExpiredLoop 每分钟扫描并恢复冷却到期的账号
func (e *Engine) unblockExpiredLoop(ctx context.Context) {
	defer e.wg.Done()
	ticker := time.NewTicker(time.Minute)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			// P族修复: 原实现裸 SQL 恢复状态, 绕过 account engine 的 updateStatus,
			// 前端账号列表不会刷新(同 markAccountRestricted 的 P bug)。
			// 现在 RETURNING 取出受影响账号, 逐个走 UpdateAccountStatus
			// (幂等: 同值再写一次无害) 补 SSE 广播, 顺带纠偏状态漂移。
			// P2-83 审计修复: 原 SQL 不查 pool_status——standby 备用号冷却到期后被
			// 恢复成 online 并被拉去跑业务(占额度)。限定仅 active 号自动恢复。
			rows, err := e.db.Pool.Query(ctx, `UPDATE accounts SET status = 'online', blocked_until = NULL WHERE status = 'restricted' AND pool_status = 'active' AND blocked_until IS NOT NULL AND blocked_until <= NOW() RETURNING tenant_id, account_id`)
			if err != nil {
				log.Printf("[engine] 恢复冷却账号失败: %v", err)
				continue
			}
			type recoveredAccount struct {
				tenantID  uuid.UUID
				accountID uuid.UUID
			}
			var recovered []recoveredAccount
			for rows.Next() {
				var ra recoveredAccount
				if err := rows.Scan(&ra.tenantID, &ra.accountID); err != nil {
					log.Printf("[engine] 读取恢复账号失败: %v", err)
					continue
				}
				recovered = append(recovered, ra)
			}
			rows.Close()
			for _, ra := range recovered {
				if err := e.accEngine.UpdateAccountStatus(ctx, ra.tenantID, ra.accountID, domain.AccountStatusOnline); err != nil {
					log.Printf("[engine] 恢复账号广播状态失败 %s: %v", ra.accountID, err)
				}
			}
			if len(recovered) > 0 {
				log.Printf("[engine] 已自动恢复 %d 个冷却到期账号", len(recovered))
				e.sendTelegramAlert(fmt.Sprintf("✅ %d 个账号冷却到期，已自动恢复发信", len(recovered)))
			}
		}
	}
}

// sendTelegramAlert 通过 Telegram Bot 向管理员发送告警 (env: TG_ALERT_BOT_TOKEN / TG_ALERT_CHAT_ID)
func (e *Engine) sendTelegramAlert(text string) {
	token := e.cfg.AlertBotToken
	chatID := e.cfg.AlertChatID
	if token == "" || chatID == "" {
		return
	}
	safego.Go(func() {
		url := fmt.Sprintf("https://api.telegram.org/bot%s/sendMessage", token)
		payload, _ := json.Marshal(map[string]string{"chat_id": chatID, "text": text})
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(payload))
		if err != nil {
			return
		}
		req.Header.Set("Content-Type", "application/json")
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			log.Printf("[alert] TG 告警发送失败: %v", err)
			return
		}
		resp.Body.Close()
	})
}

// RegisterTenantQuota 新租户注册后立即同步并发配额到调度器(审计P1-02, 原先要重启才生效)。
func (e *Engine) RegisterTenantQuota(tenantID [16]byte, maxConcurrent int) {
	if e.scheduler != nil {
		e.scheduler.RegisterTenant(tenantID, maxConcurrent)
	}
}

func (e *Engine) Stop() {
	e.stopOnce.Do(func() { close(e.stopCh) })
	if e.linkScraper != nil {
		e.linkScraper.Stop()
	}
	if e.scheduler != nil {
		e.scheduler.Stop()
	}
	e.listenMu.Lock()
	if e.listenConn != nil {
		e.listenConn.Release()
	}
	e.listenMu.Unlock()
	// Clean up all scrape handler entries to prevent memory leaks
	scrapeHandlerRegistry.Range(func(key, value interface{}) bool {
		if entry, ok := value.(*scrapeHandlerEntry); ok {
			atomic.StoreInt32(&entry.active, 0)
		}
		scrapeHandlerRegistry.Delete(key)
		return true
	})
	e.wg.Wait()
}

// startLogArchiver periodically deletes old task logs to prevent disk bloat.

// N-01: 原 30 天归档循环已删除——startLogCleanup 每 24h 清理 7 天前日志，
// 是 30 天删除的超集，归档循环永不命中；且其绕过 repository 裸写 SQL，
// 造成"两条保留期（7天/30天）"的控制面混乱。保留期统一由 DeleteOlderThan 管理。

func (e *Engine) startListen(ctx context.Context) error {
	conn, err := e.db.ListenConn(ctx)
	if err != nil {
		return err
	}
	pgxConn := database.RawConn(conn)
	_, err = pgxConn.Exec(ctx, "LISTEN new_task")
	if err != nil {
		conn.Release()
		return err
	}
	e.listenConn = conn

	safego.Go(func() {
		defer func() {
			if r := recover(); r != nil {
				taskLog.Printf("startListen panic: %v\n%s\n", r, debug.Stack())
			}
		}()
		backoff := 3 * time.Second
		for {
			select {
			case <-e.stopCh:
				return
			default:
			}
			conn, err := e.db.ListenConn(ctx)
			if err != nil {
				select {
				case <-ctx.Done():
					return
				case <-time.After(backoff):
				}
				backoff = min(backoff*2, 60*time.Second)
				continue
			}
			pgxConn := database.RawConn(conn)
			_, err = pgxConn.Exec(ctx, "LISTEN new_task")
			if err != nil {
				conn.Release()
				select {
				case <-ctx.Done():
					return
				case <-time.After(backoff):
				}
				continue
			}
			e.listenMu.Lock()
			if e.listenConn != nil {
				e.listenConn.Release()
			}
			e.listenConn = conn
			e.listenMu.Unlock()
			backoff = 3 * time.Second

			// Read notifications in a blocking loop
			for {
				select {
				case <-e.stopCh:
					return
				case <-func() <-chan time.Time {
					timer := time.NewTimer(30 * time.Second)
					defer timer.Stop()
					return timer.C
				}():
					e.listenMu.Lock()
					if e.listenConn != nil {
						if err := database.RawConn(e.listenConn).Ping(ctx); err != nil {
							log.Printf("[engine] 数据库连接检测失败: %v", err)
						}
					}
					e.listenMu.Unlock()
				}

				e.listenMu.Lock()
				lc := e.listenConn
				e.listenMu.Unlock()
				if lc == nil {
					select {
					case <-ctx.Done():
						return
					case <-time.After(backoff):
					}
					break
				}
				// WaitForNotification blocks until a notification arrives or timeout
				notifyCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
				_, err := database.RawConn(lc).WaitForNotification(notifyCtx)
				cancel()
				if err != nil {
					// Timeout or connection error; check if we should reconnect
					if ctx.Err() != nil {
						return
					}
					if err == context.DeadlineExceeded {
						continue
					}
					// Connection broken, break to outer reconnect loop
					select {
					case <-ctx.Done():
						return
					case <-time.After(backoff):
					}
					break
				}
				// Notification received — trigger task claiming
				e.claimAndExecute(ctx)
			}
		}
	})
	return nil
}

func (e *Engine) workerLoop(ctx context.Context) {
	defer e.wg.Done()
	defer func() {
		if r := recover(); r != nil {
			taskLog.Printf("workerLoop panic: %v\n%s\n", r, debug.Stack())
		}
	}()
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-e.stopCh:
			return
		case <-ctx.Done():
			return
		case <-ticker.C:
			e.claimAndExecute(ctx)
		}
	}
}

func (e *Engine) pollLoop(ctx context.Context) {
	defer e.wg.Done()
	defer func() {
		if r := recover(); r != nil {
			taskLog.Printf("pollLoop panic: %v\n%s\n", r, debug.Stack())
		}
	}()
	// Smart scheduling: query next pending task time and sleep until then
	// Fallback to 30s tick if no pending tasks or query fails
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()
	for {
		// Query next pending task to determine optimal sleep duration
		nextTime, err := e.repo.GetNextPendingTaskTime(ctx)
		if err == nil && nextTime != nil {
			wait := time.Until(*nextTime)
			if wait > 0 && wait < 30*time.Second {
				// Sleep precisely until the next task is ready
				select {
				case <-e.stopCh:
					return
				case <-ctx.Done():
					return
				case <-time.After(wait):
					e.claimAndExecute(ctx)
					continue
				}
			}
		}
		// No pending tasks or next task is far away: use ticker
		select {
		case <-e.stopCh:
			return
		case <-ctx.Done():
			return
		case <-ticker.C:
			e.claimAndExecute(ctx)
		}
	}
}

func (e *Engine) claimAndExecute(ctx context.Context) {
	// Activate pending tasks whose scheduled_at has passed
	if err := e.repo.ActivatePendingTasks(ctx); err != nil {
		log.Printf("[engine] 激活待处理任务失败: %v", err)
	}

	// 全局并发上限(内存自适应): 达到上限则本轮暂停领取, 等下一轮(30s 内)
	if e.GlobalLimitFn != nil {
		if limit := e.GlobalLimitFn(); limit > 0 {
			running, err := e.repo.CountRunningGlobal(ctx)
			if err == nil && running >= limit {
				return
			}
		}
	}

	tasks, err := e.repo.ClaimTasks(ctx, e.workerID, e.cfg.TaskClaimBatch, e.cfg.TaskLockTimeoutMin)
	if err != nil {
		return
	}
	for _, t := range tasks {
		e.runTask(ctx, t)
	}

	timeoutTasks, err := e.repo.ClaimTimeoutTasks(ctx, e.workerID, e.cfg.TaskTimeoutClaim, e.cfg.TaskLockTimeoutMin)
	if err != nil {
		log.Printf("[engine] 认领超时任务失败: %v", err)
		return
	}
	for _, t := range timeoutTasks {
		e.runTask(ctx, t)
	}
}

// runTask executes a task with idempotency, concurrency limit, and per-task cancellation support.

func (e *Engine) runTask(ctx context.Context, task *domain.Task) {
	// P1-01: 全局并发原子锁(count+claim非原子会突破上限; 信号量保证任何时刻running<=上限)
	select {
	case e.globalSem <- struct{}{}:
		defer func() { <-e.globalSem }()
	case <-ctx.Done():
		return
	case <-e.stopCh:
		return
	}
	// Idempotency: skip if already running (memory + DB)
	taskKey := task.TaskID.String()
	if _, loaded := e.runningTasks.LoadOrStore(taskKey, true); loaded {
		return
	}

	// Double-check DB status to prevent duplicate execution after restart
	if dbTask, err := e.repo.GetByID(ctx, task.TenantID, task.TaskID); err == nil && dbTask != nil {
		if dbTask.Status == domain.TaskStatusCompleted || dbTask.Status == domain.TaskStatusCancelled {
			e.runningTasks.Delete(taskKey)
			return
		}
	}

	// 兜底预热：任务排队若超过池空闲回收时间(默认5分钟)，创建时的热连接可能已释放。
	// 此处再预热一次（幂等：已有热连接/在连时静默返回）。
	e.WarmAccounts(ctx, task.TenantID, task.AccountIDs)

	// Dynamic concurrency: check tenant quota under current system pressure
	if e.scheduler != nil && !e.scheduler.TryAcquire(task.TenantID) {
		// 任务在 ClaimTasks 已被标为 running。若此处静默 return，DB 里任务
		// 永远卡在 running —— 前端显示"进行中"空转，且永不被重新认领。
		// 必须回滚为 queued，让后续轮询重新认领（"retry later"落到实处）。
		e.runningTasks.Delete(taskKey)
		taskLog.Printf("[task] 租户配额已满/系统限压，任务 %s 回滚为 queued 等待重试", task.TaskID)
		if err := e.repo.UpdateStatus(ctx, task.TenantID, task.TaskID, domain.TaskStatusQueued); err != nil {
			taskLog.Printf("[task] 回滚任务状态失败 %s: %v", task.TaskID, err)
		}
		return
	}

	e.wg.Add(1)
	safego.Go(func() {
		defer e.wg.Done()
		// 任务结束(完成/失败/取消)时广播状态事件, 前端列表实时刷新
		defer func() {
			if e.sseBroker != nil {
				e.sseBroker.BroadcastToTenant(task.TenantID, "events", map[string]any{
					"type":      "task_status",
					"task_id":   task.TaskID.String(),
					"task_type": string(task.Type),
				})
			}
		}()
		// R119: 任务结束邮件通知(完成→task_finished, 失败→task_failed)
		defer func() {
			if e.finishNotify == nil {
				return
			}
			st := string(task.Status)
			if st == "completed" || st == "failed" {
				// Task.Progress 是值类型非指针; 任务名取 payload["name"], 回退类型名
				name, _ := task.Payload["name"].(string)
				if name == "" {
					name = string(task.Type)
				}
				e.finishNotify(task.TenantID, name, string(task.Type), st, task.Progress.Done, task.Progress.Failed)
			}
		}()
		defer func() {
			if e.scheduler != nil {
				e.scheduler.Release(task.TenantID)
			}
		}()
		defer e.runningTasks.Delete(taskKey)
		// P1修复(精读发现): progressCache 只 Store 从不 Delete(注释承诺的删除并不存在),
		// 每个任务永久泄漏一个 TaskProgress, sync.Map 随任务数无限增长。
		defer e.progressCache.Delete(taskKey)
		defer func() {
			if r := recover(); r != nil {
				taskLog.Printf("task panic: %v\n%s\n", r, debug.Stack())
				if err := e.repo.UpdateStatus(ctx, task.TenantID, task.TaskID, domain.TaskStatusFailed); err != nil {
					taskLog.Printf("[task] UpdateStatus failed: %v", err)
				}
			}
		}()

		// Create a cancellable context for this specific task
		taskCtx, cancel := context.WithCancel(ctx)
		e.taskCtxs.Store(taskKey, cancel)
		defer func() {
			cancel()
			e.taskCtxs.Delete(taskKey)
		}()

		// Check if task was cancelled before starting
		freshTask, err := e.repo.GetByID(ctx, task.TenantID, task.TaskID)
		if err == nil && freshTask.Status == domain.TaskStatusCancelled {
			return
		}

		e.executeTask(taskCtx, task)
	})
}

// CancelTask cancels a running task by its ID.

func (e *Engine) CancelTask(taskID uuid.UUID) {
	// M-05: 取消立即落库(原只触发内存cancel, DB状态要等worker退出才更新,
	// 取消后列表长时间显示running造成"取消没生效"的错觉; worker认领时的状态双查会跳过已取消任务)
	if t, err := e.repo.GetByTaskIDAnyTenant(context.Background(), taskID); err == nil && t != nil {
		if uerr := e.repo.UpdateStatus(context.Background(), t.TenantID, taskID, domain.TaskStatusCancelled); uerr != nil {
			taskLog.Printf("[task] cancel落库失败: %v", uerr)
		}
	}
	if cancel, ok := e.taskCtxs.Load(taskID.String()); ok {
		cancel.(context.CancelFunc)()
	}
}

func (e *Engine) executeTask(ctx context.Context, task *domain.Task) {
	switch task.Type {
	case domain.TaskTypeBroadcast:
		e.executeBroadcast(ctx, task)
	case domain.TaskTypeScrapeMembers:
		// mode=history: 历史消息活跃用户采集(隐私群 Participants 只暴露管理员时)
		if fmt.Sprint(task.Payload["mode"]) == "history" {
			e.executeScrapeActiveUsers(ctx, task)
		} else {
			e.executeScrapeMembers(ctx, task)
		}
	case domain.TaskTypeJoinGroups:
		e.executeJoinGroups(ctx, task)
	case domain.TaskTypeScrapeLinks:
		e.executeScrapeLinksReport(ctx, task)
	case domain.TaskTypeMonitorScrape:
		e.executeMonitorScrape(ctx, task)
	case domain.TaskTypeInviteMembers:
		e.executeInviteMembers(ctx, task)
	case domain.TaskTypeScheduledBroadcast:
		e.executeBroadcast(ctx, task)
		e.rescheduleLoopTask(ctx, task)
	default:
		if err := e.repo.UpdateStatus(ctx, task.TenantID, task.TaskID, domain.TaskStatusFailed); err != nil {
			taskLog.Printf("[task] UpdateStatus failed: %v", err)
		}
	}
}

func (e *Engine) markAccountRestricted(ctx context.Context, tenantID, accountID uuid.UUID, eventType, detail string, waitSec int) {
	e.recordRiskEvent(ctx, tenantID, accountID, eventType, "medium", detail)
	// 冷却时长: 优先按 Telegram FloodWait 实际秒数, 缺省 24 小时
	blockedUntil := time.Now().UTC().Add(24 * time.Hour)
	if waitSec > 0 {
		blockedUntil = time.Now().UTC().Add(time.Duration(waitSec) * time.Second)
	}
	if err := e.accEngine.UpdateAccountStatus(ctx, tenantID, accountID, domain.AccountStatusRestricted); err != nil {
		log.Printf("[engine] 更新状态失败: %v", err)
	}
	if _, err := e.db.Pool.Exec(ctx, `UPDATE accounts SET blocked_until = $1 WHERE tenant_id = $2 AND account_id = $3`,
		blockedUntil, tenantID, accountID); err != nil {
		log.Printf("[engine] 更新封禁时间失败: %v", err)
	}
	e.sendTelegramAlert(fmt.Sprintf("⚠️ 账号受限告警\n账号ID: %s\n原因: %s\n冷却至: %s\n详情: %s",
		accountID, eventType, blockedUntil.Format("01-02 15:04"), detail))
}

// markAccountBanned records a risk event AND updates account status to banned.
func (e *Engine) markAccountBanned(ctx context.Context, tenantID, accountID uuid.UUID, eventType, detail string) {
	e.recordRiskEvent(ctx, tenantID, accountID, eventType, "critical", detail)
	if err := e.accEngine.UpdateAccountStatus(ctx, tenantID, accountID, domain.AccountStatusBanned); err != nil {
		log.Printf("[engine] 更新状态失败: %v", err)
	}
	e.sendTelegramAlert(fmt.Sprintf("🚫 账号封禁告警\n账号ID: %s\n原因: %s\n详情: %s\n请到后台检查并重新登录", accountID, eventType, detail))
}
func (e *Engine) recordRiskEvent(ctx context.Context, tenantID, accountID uuid.UUID, eventType, severity, detail string) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if err := e.riskRepo.Create(ctx, &domain.RiskEvent{
		EventID:   uuid.New(),
		TenantID:  tenantID,
		AccountID: accountID,
		EventType: eventType,
		Severity:  severity,
		Details:   map[string]any{"detail": detail},
	}); err != nil {
		taskLog.Printf("[task] risk event create failed: %v", err)
	}
}

// executeMonitorScrape listens to group messages and scrapes usernames from matching messages.
// executeMonitorScrape listens to group messages via OnMessage and scrapes usernames from matching messages.
func (e *Engine) executeMonitorScrape(ctx context.Context, task *domain.Task) {
	if err := e.repo.UpdateStatus(ctx, task.TenantID, task.TaskID, domain.TaskStatusRunning); err != nil {
		taskLog.Printf("[task] failed to update status to running: %v", err)
	}

	payload := task.Payload
	matchType, _ := payload["match_type"].(string)
	matchContent, _ := payload["match_content"].(string)
	durationMinutes, _ := payload["duration_minutes"].(float64)
	maxResults, _ := payload["max_results"].(float64)
	groupIDStr, _ := payload["group_id"].(string)
	groupID, err := uuid.Parse(groupIDStr)
	if err != nil {
		taskLog.Printf("[monitor_scrape] 无效 group_id %q: %v", groupIDStr, err)
		if uerr := e.repo.UpdateStatus(ctx, task.TenantID, task.TaskID, domain.TaskStatusFailed); uerr != nil {
			taskLog.Printf("[task] failed to update status to failed: %v", uerr)
		}
		return
	}

	if matchContent == "" {
		if err := e.repo.UpdateStatus(ctx, task.TenantID, task.TaskID, domain.TaskStatusFailed); err != nil {
			taskLog.Printf("[task] failed to update status to failed: %v", err)
		}
		return
	}

	// Build matcher based on match type
	var matcher func(text string) bool
	switch matchType {
	case "regex":
		re, err := regexp.Compile(matchContent)
		if err != nil {
			taskLog.Printf("[monitor_scrape] invalid regex: %v", err)
			if err := e.repo.UpdateStatus(ctx, task.TenantID, task.TaskID, domain.TaskStatusFailed); err != nil {
				taskLog.Printf("[task] failed to update status to failed: %v", err)
			}
			return
		}
		matcher = func(text string) bool { return re.MatchString(text) }
	case "exact":
		matcher = func(text string) bool { return text == matchContent }
	case "fuzzy", "contains":
		keywords := strings.Split(matchContent, ",")
		for i := range keywords {
			keywords[i] = strings.TrimSpace(strings.ToLower(keywords[i]))
		}
		matcher = func(text string) bool {
			lowerText := strings.ToLower(text)
			for _, kw := range keywords {
				if kw != "" && strings.Contains(lowerText, kw) {
					return true
				}
			}
			return false
		}
	default:
		keywords := strings.Split(matchContent, ",")
		for i := range keywords {
			keywords[i] = strings.TrimSpace(strings.ToLower(keywords[i]))
		}
		matcher = func(text string) bool {
			lowerText := strings.ToLower(text)
			for _, kw := range keywords {
				if kw != "" && strings.Contains(lowerText, kw) {
					return true
				}
			}
			return false
		}
	}

	if len(task.AccountIDs) == 0 {
		taskLog.Printf("[monitor_scrape] 任务缺少账号，无法执行")
		if uerr := e.repo.UpdateStatus(ctx, task.TenantID, task.TaskID, domain.TaskStatusFailed); uerr != nil {
			taskLog.Printf("[task] failed to update status to failed: %v", uerr)
		}
		return
	}
	accountID := task.AccountIDs[0]
	client, err := e.accEngine.GetClient(ctx, task.TenantID, accountID)
	if err != nil {
		taskLog.Printf("[monitor_scrape] get client failed: %v", err)
		if err := e.repo.UpdateStatus(ctx, task.TenantID, task.TaskID, domain.TaskStatusFailed); err != nil {
			taskLog.Printf("[task] failed to update status to failed: %v", err)
		}
		return
	}
	defer e.accEngine.ReleaseClient(task.TenantID, accountID, client)

	group, err := e.groupRepo.GetByID(ctx, task.TenantID, groupID)
	if err != nil {
		taskLog.Printf("[monitor_scrape] get group failed: %v", err)
		if err := e.repo.UpdateStatus(ctx, task.TenantID, task.TaskID, domain.TaskStatusFailed); err != nil {
			taskLog.Printf("[task] failed to update status to failed: %v", err)
		}
		return
	}

	if err := e.logRepo.Create(ctx, &domain.TaskLog{
		TaskID:     task.TaskID,
		TenantID:   task.TenantID,
		AccountID:  accountID,
		Action:     "monitor_start",
		Status:     "success",
		TargetName: group.Title,
		ExecutedAt: time.Now(),
	}); err != nil {
		taskLog.Printf("[task] failed to create monitor_start log: %v", err)
	}

	e.publishProgress(task.TenantID, task.TaskID, domain.TaskProgress{Total: int(maxResults), Done: 0})

	// Message processing
	scrapedCount := 0
	scrapedUsers := make(map[string]bool)
	var countMu sync.Mutex

	// Register OnMessage handler for real-time message listening
	clientKey := task.TenantID.String() + ":" + accountID.String()

	e.scrapeRegMu.Lock()
	if e.scrapeRegistered[clientKey] {
		e.scrapeRegMu.Unlock()
		taskLog.Printf("[monitor_scrape] client %s already has OnMessage registered, falling back to polling", clientKey)
		// Fallback to polling if OnMessage already registered (by monitor engine or another task)
		e.runMonitorScrapePolling(ctx, task, group, matcher, durationMinutes, maxResults, &scrapedCount, scrapedUsers, &countMu)
		return
	}
	e.scrapeRegistered[clientKey] = true
	e.scrapeRegMu.Unlock()

	// Cleanup registration on exit
	defer func() {
		e.scrapeRegMu.Lock()
		delete(e.scrapeRegistered, clientKey)
		e.scrapeRegMu.Unlock()
	}()

	// Register OnMessage callback
	// P2止血(精读发现): OnMessage 无法摘除, 任务结束后把闭包拽住的
	// scrapedUsers 置 nil(持 countMu, handler 访问同锁, 无竞争)。
	defer func() {
		countMu.Lock()
		scrapedUsers = nil
		countMu.Unlock()
	}()
	client.OnMessage(func(c *telegram.Client, msg *types.Message) {
		select {
		case <-ctx.Done():
			return
		default:
		}

		if msg == nil || msg.ChatID == 0 || msg.ID == 0 {
			return
		}

		// Only process messages from target group
		if msg.ChatID != group.TGChatID {
			return
		}

		// Apply matcher
		if !matcher(msg.Text) {
			return
		}

		// Deduplicate by sender ID
		countMu.Lock()
		key := fmt.Sprintf("%d", msg.FromID)
		if scrapedUsers[key] {
			countMu.Unlock()
			return
		}
		scrapedUsers[key] = true
		currentCount := scrapedCount + 1
		scrapedCount = currentCount
		countMu.Unlock()

		// Save member to database (TGUserID only, username fetched if possible)
		member := &domain.Member{
			MemberID:  uuid.New(),
			TenantID:  task.TenantID,
			GroupID:   groupID,
			TGUserID:  msg.FromID,
			ScrapedAt: time.Now(),
		}
		if err := e.memberRepo.BatchCreate(ctx, []*domain.Member{member}); err != nil {
			taskLog.Printf("[task] failed to save member: %v", err)
		}

		// Update progress
		e.publishProgress(task.TenantID, task.TaskID, domain.TaskProgress{
			Total:         int(maxResults),
			Done:          currentCount,
			CurrentTarget: fmt.Sprintf("user_%d", msg.FromID),
		})

		if err := e.logRepo.Create(ctx, &domain.TaskLog{
			TaskID:     task.TaskID,
			TenantID:   task.TenantID,
			AccountID:  accountID,
			Action:     "scrape",
			Status:     "success",
			TargetID:   fmt.Sprintf("%d", msg.FromID),
			TargetName: fmt.Sprintf("User %d", msg.FromID),
			ExecutedAt: time.Now(),
		}); err != nil {
			taskLog.Printf("[task] failed to create scrape log: %v", err)
		}

		// Check if max results reached
		if maxResults > 0 && currentCount >= int(maxResults) {
			if err := e.repo.UpdateStatus(ctx, task.TenantID, task.TaskID, domain.TaskStatusCompleted); err != nil {
				taskLog.Printf("[task] failed to update status to completed: %v", err)
			}
		}
	})

	// Wait for duration or context cancellation
	duration := time.Duration(durationMinutes) * time.Minute
	if duration <= 0 {
		duration = 30 * time.Minute
	}

	select {
	case <-time.After(duration):
	case <-ctx.Done():
	}

	// Finalize
	countMu.Lock()
	finalCount := scrapedCount
	countMu.Unlock()

	if err := e.repo.UpdateStatus(ctx, task.TenantID, task.TaskID, domain.TaskStatusCompleted); err != nil {
		taskLog.Printf("[task] failed to update status to completed: %v", err)
	}

	e.publishProgress(task.TenantID, task.TaskID, domain.TaskProgress{
		Total: int(maxResults),
		Done:  finalCount,
	})

	if err := e.logRepo.Create(ctx, &domain.TaskLog{
		TaskID:     task.TaskID,
		TenantID:   task.TenantID,
		AccountID:  accountID,
		Action:     "monitor_complete",
		Status:     "success",
		TargetName: fmt.Sprintf("监控完成，共采集 %d 人", finalCount),
		ExecutedAt: time.Now(),
	}); err != nil {
		taskLog.Printf("[task] failed to create monitor_complete log: %v", err)
	}
}

// runMonitorScrapePolling is a fallback when OnMessage is already registered.
func (e *Engine) runMonitorScrapePolling(ctx context.Context, task *domain.Task, group *domain.Group, matcher func(string) bool, durationMinutes float64, maxResults float64, scrapedCount *int, scrapedUsers map[string]bool, countMu *sync.Mutex) {
	duration := time.Duration(durationMinutes) * time.Minute
	if duration <= 0 {
		duration = 30 * time.Minute
	}

	// P1-28 审计修复: 原实现是空转循环——每 5 秒 tick 一次什么都不做, 空等 duration
	// 后返回, monitor_scrape 的 fallback 路径一条不采, 结果恒 0 还白跑半小时。
	// 该模式尚未实现主动拉取; 立即返回并告警, 避免任务假运行空转占额度。
	log.Printf("[task] monitor_scrape fallback 轮询未实现(模式占位), 任务 %s 直接结束不空转", task.TaskID)
}

// rescheduleLoopTask requeues a finished loop task for its next run.
// Tasks that were paused or cancelled while running are left untouched.
func (e *Engine) rescheduleLoopTask(ctx context.Context, task *domain.Task) {
	if !task.IsLoop {
		return
	}
	fresh, err := e.repo.GetByID(ctx, task.TenantID, task.TaskID)
	if err != nil || fresh == nil {
		return
	}
	if fresh.Status == domain.TaskStatusPaused || fresh.Status == domain.TaskStatusCancelled {
		return
	}
	interval := task.LoopIntervalMinutes
	if interval <= 0 {
		interval = 60
	}
	next := time.Now().Add(time.Duration(interval) * time.Minute)
	if err := e.repo.RescheduleLoopTask(ctx, task.TenantID, task.TaskID, next); err != nil {
		taskLog.Printf("[task] failed to reschedule loop task %s: %v", task.TaskID, err)
	}
}
