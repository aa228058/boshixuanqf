package app

import (
	"context"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"tg-sender/internal/safego"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"
	"golang.org/x/crypto/bcrypt"
	"tg-sender/internal/adaptive"
	"tg-sender/internal/backup"
	"tg-sender/internal/config"
	"tg-sender/internal/crypto"
	"tg-sender/internal/database"
	"tg-sender/internal/domain/grouplib"
	"tg-sender/internal/domain"
	"tg-sender/internal/domain/account"
	// R114: domain/campaign 导入已删(协议协同旧系统整包移除)
	"tg-sender/internal/domain/monitor"
	"tg-sender/internal/domain/task"
	"tg-sender/internal/notify"
	"tg-sender/internal/handler"
	"tg-sender/internal/middleware"
	"tg-sender/internal/repository"
	"tg-sender/internal/service"
	"tg-sender/internal/sse"
)

// Container holds all application dependencies, following the dependency injection pattern.
type Container struct {
	Cfg *config.Config
	DB  *database.DB

	// Repositories
	TenantRepo       *repository.TenantRepository
	AuditRepo        *repository.AuditRepository
	AccountRepo      *repository.AccountRepository
	GroupRepo        *repository.GroupRepository
	MemberRepo       *repository.MemberRepository
	TaskRepo         *repository.TaskRepository
	TaskLogRepo      *repository.TaskLogRepository
	QuotaRepo        *repository.QuotaRepository
	RiskRepo         *repository.RiskRepository
	MonitorRuleRepo  *repository.MonitorRuleRepository
	RefreshTokenRepo *repository.RefreshTokenRepository
	ApprovalRepo     *repository.PendingApprovalRepository
	PlanRepo         *repository.PlanRepository
	CodeRepo         *repository.ActivationCodeRepository
	TemplateRepo     *repository.MessageTemplateRepository
	AccountGroupRepo *repository.AccountGroupRepository
	SupportRepo      *repository.SupportConfigRepository
	// R119: 探测配置(管理端全局, 单行)
	ProbeRepo        *repository.ProbeConfigRepository
	AnnounceRepo     *repository.AnnouncementRepository
	AdminRepo        *repository.AdminRepository

	// Infrastructure
	SSEBroker *sse.Broker
	// 抽奖结果追踪(058 迁移): 参与回执/开奖解析落库
	RaffleResultRepo *repository.RaffleResultRepository

	// Engines
	AccountEngine  *account.Engine
	TaskEngine     *task.Engine
	MonitorEngine  *monitor.Engine
	AdaptiveEngine *adaptive.Engine

	// Persistent link scraper
	ScrapedLinkRepo domain.ScrapedLinkRepository
	LinkScraper     *task.LinkScraper

	// Services
	AccountService   *service.AccountService
	// R114: SquadRepo/SquadService 已删(小队系统整建制尸体)
	GroupService     *service.GroupService
	TaskService      *service.TaskService
	SystemGroupRepo  *repository.SystemGroupRepository
	GroupLibEngine   *grouplib.Engine
	GroupLibraryHandler *handler.GroupLibraryHandler
	TextDictRepo        *repository.TextDictRepository // R116: Handler 已删, repo 供 engine 用
	AIConfigRepo        *repository.AIConfigRepository
	VerificationLogRepo *repository.VerificationLogRepository
	AIConfigHandler     *handler.AIConfigHandler
	PlanLimitRepo       *repository.PlanLimitRepository
	MonitorService   *service.MonitorService
	DashboardService *service.DashboardService
	SettingsService  *service.SettingsService
	AdminService     *service.AdminService

	// Plan Checker
	PlanChecker *middleware.PlanChecker

	// Handlers
	AuthHandler      *handler.AuthHandler
	// R119: 通知中心邮件发送器(SMTP 未配置时为未配置状态, 静默跳过)
	Mailer           *notify.Mailer
	RaffleMailBatch *notify.Batcher // R148: 抽奖邮件聚合器(同活动多账号合一封)
	AccountHandler   *handler.AccountHandler
	// R114: CampaignService/Handler 与 SquadHandler 已删
	GroupHandler     *handler.GroupHandler
	MediaHandler     *handler.MediaHandler
	ContactHandler   *handler.ContactHandler
	TaskHandler      *handler.TaskHandler
	MonitorHandler   *handler.MonitorHandler
	DashboardHandler *handler.DashboardHandler
	SettingsHandler  *handler.SettingsHandler
	// R119: 通知中心邮件发送器(SMTP 未配置时静默跳过)
	ExportHandler    *handler.ExportHandler
	AdminHandler     *handler.AdminHandler

	// Lifecycle
	stopCh chan struct{}
}

// NewContainer creates and initializes all application dependencies.
func NewContainer(cfg *config.Config) (*Container, error) {
	c := &Container{Cfg: cfg, stopCh: make(chan struct{})}

	// Database
	db, err := database.New(context.Background(), cfg)
	if err != nil {
		return nil, fmt.Errorf("database init: %w", err)
	}
	c.DB = db

	// Migrations
	if err := runMigrations(db); err != nil {
		return nil, fmt.Errorf("migrations: %w", err)
	}

	// 系统租户种子：后台平面审计记录的外键挂载点（软删除态——不可登录、不在租户列表、
	// 但行存在使 audit_logs.tenant_id FK 成立）。幂等。
	if _, err := db.Pool.Exec(context.Background(), `
		INSERT INTO tenants (tenant_id, name, api_key_hash, username, password_hash, max_accounts, max_concurrent_tasks, plan_id, status, deleted_at)
		VALUES ('00000000-0000-0000-0000-000000000001', '__系统租户__', 'x', '__system__', '!', 0, 0, 'free', 'suspended', NOW())
		ON CONFLICT (tenant_id) DO NOTHING`); err != nil {
		return nil, fmt.Errorf("system tenant seed: %w", err)
	}

	// Repositories
	c.initRepositories()

	// Infrastructure
	c.SSEBroker = sse.NewBroker(cfg)

	// Engines
	if err := c.initEngines(); err != nil {
		return nil, fmt.Errorf("engines init: %w", err)
	}

	// Services
	c.initServices()

	// Handlers
	c.initHandlers()

	// Plan Checker
	c.PlanChecker = middleware.NewPlanChecker(c.PlanRepo, c.TenantRepo)

	// Seed default plans and admin
	if err := c.seedData(context.Background()); err != nil {
		log.Printf("初始化数据警告: %v", err)
	}

	return c, nil
}

func runMigrations(db *database.DB) error {
	sqlDB := stdlib.OpenDBFromPool(db.Pool)
	defer sqlDB.Close()
	// R86 修复: goose v3 全局 dialect 默认 sqlite3, 会按分号朴素切分 SQL。
	// 065_groups_updated_at.sql 含 $$...$$ dollar-quoted 函数体(内部有分号),
	// 被切断后报 "unterminated dollar-quoted string"(2026-10-23 运行时实证)。
	// 必须显式声明 postgres dialect, 其解析器才能正确识别 dollar-quoted 字符串。
	if err := goose.SetDialect("postgres"); err != nil {
		return fmt.Errorf("set goose dialect: %w", err)
	}
	return goose.Up(sqlDB, "migrations")
}

func (c *Container) initRepositories() {
	pool := c.DB.Pool
	cfg := c.Cfg
	c.TenantRepo = repository.NewTenantRepository(pool, cfg)
	c.AuditRepo = repository.NewAuditRepository(pool)
	c.AccountRepo = repository.NewAccountRepository(pool)
	c.GroupRepo = repository.NewGroupRepository(pool)
	c.MemberRepo = repository.NewMemberRepository(pool)
	c.TaskRepo = repository.NewTaskRepository(pool)
	c.TaskLogRepo = repository.NewTaskLogRepository(pool)
	c.QuotaRepo = repository.NewQuotaRepository(pool)
	c.RiskRepo = repository.NewRiskRepository(pool)
	c.MonitorRuleRepo = repository.NewMonitorRuleRepository(pool)
	c.RefreshTokenRepo = repository.NewRefreshTokenRepository(pool)
	c.ApprovalRepo = repository.NewPendingApprovalRepository(pool)
	c.PlanRepo = repository.NewPlanRepository(pool)
	c.CodeRepo = repository.NewActivationCodeRepository(pool)
	c.SupportRepo = repository.NewSupportConfigRepository(pool)
	c.ProbeRepo = repository.NewProbeConfigRepository(pool)
	c.AnnounceRepo = repository.NewAnnouncementRepository(pool)
	c.AdminRepo = repository.NewAdminRepository(pool)
	c.TemplateRepo = repository.NewMessageTemplateRepository(pool)
	c.AccountGroupRepo = repository.NewAccountGroupRepository(pool)
	// R114: SquadRepo 构建已删
	c.ScrapedLinkRepo = repository.NewScrapedLinkRepository(pool)
}

func (c *Container) initEngines() error {
	accEngine, err := account.NewEngine(c.Cfg, c.DB.Pool, c.AccountRepo, c.RiskRepo, c.QuotaRepo)
	if err != nil {
		return fmt.Errorf("account engine: %w", err)
	}
	accEngine.StartPoolCleaner()
	accEngine.StartSessionBackup()
	backup.StartDBBackup(c.Cfg)
	// R114: Campaign 子系统整建制删除(引擎/服务/处理器/仓库/路由——前端零调用;
	// campaigns 表的定期清理 SQL 由 account engine 内联保留, 不依赖任何 campaign 代码)
	c.AccountEngine = accEngine

	taskEngine := task.NewEngine(c.Cfg, c.DB, c.TaskRepo, c.TaskLogRepo, c.QuotaRepo, c.RiskRepo)
	taskEngine.SetAccountEngine(accEngine)
	taskEngine.SetMemberRepo(c.MemberRepo)
	// R87-M10: 注入群库仓库 —— 加入失败时回写"已失效"状态
	taskEngine.SetSystemGroupRepo(c.SystemGroupRepo)
	taskEngine.SetApprovalRepo(c.ApprovalRepo)
	taskEngine.SetGroupRepo(c.GroupRepo)
	taskEngine.SetSSEBroker(c.SSEBroker)
	accEngine.SetSSEBroker(c.SSEBroker)

	// 初始化常驻链接采集服务
	c.LinkScraper = task.NewLinkScraper(c.Cfg, accEngine, c.ScrapedLinkRepo, c.GroupRepo, c.AccountRepo)
	taskEngine.SetLinkScraper(c.LinkScraper)
	taskEngine.SetScrapedLinkRepo(c.ScrapedLinkRepo)

	// M-03: 引擎根 ctx 原本传 Background（ctx.Done 分支是死代码，停止全靠 stopCh）。
	// 改为随 stopCh 取消的 ctx，使引擎内 DB/RPC 操作在停机时能被真正取消。
	engineCtx, cancelEngines := context.WithCancel(context.Background())
	go func() {
		select {
		case <-c.stopCh:
			cancelEngines()
		case <-engineCtx.Done():
		}
	}()
	if err := taskEngine.Start(engineCtx); err != nil {
		return fmt.Errorf("task engine: %w", err)
	}
	// R119: 任务结束邮件通知 —— 完成/失败时按租户订阅推送
	taskEngine.SetFinishNotifier(func(tenantID uuid.UUID, taskName, taskType, status string, done, failed int) {
		if c.Mailer == nil {
			return
		}
		t, terr := c.TenantRepo.GetByID(context.Background(), tenantID)
		if terr != nil || t == nil {
			return
		}
		var subject, body, event string
		if status == "completed" {
			event = notify.EventTaskFinished
			subject = "【博士轩群发器】任务完成 — " + taskName
			body = fmt.Sprintf("✅ 任务完成\n\n任务: %s\n类型: %s\n成功: %d\n失败: %d\n时间: %s\n——博士轩群发器通知中心", taskName, taskType, done, failed, time.Now().Format("2006-01-02 15:04:05"))
		} else {
			event = notify.EventTaskFailed
			subject = "【博士轩群发器】任务失败 — " + taskName
			body = fmt.Sprintf("❌ 任务失败/异常停止\n\n任务: %s\n类型: %s\n成功: %d\n失败: %d\n时间: %s\n请到任务详情查看失败原因。\n——博士轩群发器通知中心", taskName, taskType, done, failed, time.Now().Format("2006-01-02 15:04:05"))
		}
		c.Mailer.Notify(t, event, subject, body)
	})
	c.TaskEngine = taskEngine

	c.AdaptiveEngine = adaptive.NewEngine(c.Cfg, c.SSEBroker)
	// 全局并发上限 = min(套餐需求总和, 内存硬件档位)
	// 需求侧: 所有在册租户 max_concurrent_tasks 之和——租户增多/升套餐时
	//         总容量自动放大，不用再改代码调档位。
	// 硬件侧: adaptive 内存档位（25/40/60）作为安全天花板，内存吃紧时
	//         自动降档兜底，防 OOM。业务需求永远不超过机器能力。
	c.TaskEngine.GlobalLimitFn = func() int {
		demand := c.TaskEngine.PlanDemandLimit()
		hwCap := c.AdaptiveEngine.CurrentProfile().MaxConcurrent
		limit := hwCap
		if demand > 0 && demand < hwCap {
			limit = demand
		}
		if limit < 1 {
			limit = 1
		}
		return limit
	}
	c.AdaptiveEngine.Start(context.Background(), func() int {
		n, _ := c.TaskRepo.CountByStatus(context.Background(), uuid.Nil, "queued")
		return int(n)
	})

	taskEngine.StartApprovalChecker(engineCtx)
	taskEngine.StartResourceMonitor(engineCtx)

	c.MonitorEngine = monitor.NewEngine(c.Cfg, c.AccountEngine, c.MonitorRuleRepo, c.SSEBroker)

	// Auto-sync joined groups for all online accounts on startup
	safego.Go(func() {
		select {
		case <-c.stopCh:
			return
		case <-time.After(10 * time.Second):
		}
		accounts, _ := c.AccountRepo.ListAll(context.Background())
		for _, acc := range accounts {
			// R182: 上次异常退出时状态可能停在 authenticated/restricted(崩溃恢复路径),
			// 只同步 online 会让这些号的群组数据永远陈旧(审计报告#14)。
			if acc.Status == domain.AccountStatusOnline ||
				acc.Status == domain.AccountStatusAuthenticated ||
				acc.Status == domain.AccountStatusRestricted {
				if err := c.TaskEngine.SyncJoinedGroups(context.Background(), acc.TenantID, acc.AccountID); err != nil {
					log.Printf("[startup] 同步账号 %s 的群组失败: %v", acc.AccountID, err)
				}
			}
		}
	})
	return nil
}

func (c *Container) initServices() {
	c.AccountService = service.NewAccountService(c.Cfg, c.AccountRepo, c.ProbeRepo, c.QuotaRepo, c.RiskRepo, c.AccountEngine, c.TenantRepo, c.TaskLogRepo)
	// R114: SquadService/CampaignService 已删(整建制尸体, 前端零调用)
	c.GroupService = service.NewGroupService(c.DB.Pool, c.GroupRepo, c.MemberRepo)
	// 抽奖结果追踪(058): RaffleResultRepo 在此初始化, 供 NewTaskService 注入
	c.RaffleResultRepo = repository.NewRaffleResultRepository(c.DB.Pool)
	c.TaskService = service.NewTaskService(c.Cfg, c.TaskRepo, c.TaskLogRepo, c.QuotaRepo, c.AccountRepo, c.TenantRepo, c.GroupRepo, c.MemberRepo, nil, c.SSEBroker, c.TaskEngine, c.ScrapedLinkRepo, c.RaffleResultRepo)
	c.TaskService.SetLinkScraper(c.LinkScraper)
	// 群组库(找群): 平台级聚合器 + 接口
	c.SystemGroupRepo = repository.NewSystemGroupRepository(c.DB.Pool)
	c.GroupLibEngine = grouplib.NewEngine(c.DB.Pool, c.SystemGroupRepo, c.AccountEngine)
	c.GroupLibEngine.Start()
	// P0-6 审计修复(2026-10-22): 此处的 LinkScraper.Start 已删——原来 container 和
	// task/engine.go 各调一次 Start, 4 个常驻 goroutine 双份空转。保留 engine 内的
	// 调用(有 ctx 生命周期管理), 这里不再启动。
	c.GroupLibraryHandler = handler.NewGroupLibraryHandler(c.SystemGroupRepo, c.TaskService, c.AccountRepo, c.GroupLibEngine, c.PlanLimitRepo)
	// 文案变量(数据字典): 群发 {{词典名}} 随机替换(高级语法) + 词典管理接口
	c.TextDictRepo = repository.NewTextDictRepository(c.DB.Pool)
	c.TaskEngine.SetTextDictRepo(c.TextDictRepo)
	// R116: TextDictHandler 已删(词典管理 HTTP 层零调用; repo 保留供 engine 变量替换)
	// 入群验证: AI配置(加密存储) + 验证记录 + 监控引擎依赖注入
	c.AIConfigRepo = repository.NewAIConfigRepository(c.DB.Pool)
	c.VerificationLogRepo = repository.NewVerificationLogRepository(c.DB.Pool)
	// R119: 验证通过邮件通知(verify_solved 事件)
	
	// R153: 登录验证码捕获 → 落库 + SSE 实时推送 + 邮件(走 need_verify 订阅, 设置里可关)
		// R164: 启动时清理历史遗留的登录临时会话(sessions 表防膨胀, 含已删账号残留旧密钥)
	if n, err := c.AccountRepo.CleanupLoginSessions(context.Background()); err != nil {
		log.Printf("[cleanup] 登录临时会话清理失败: %v", err)
	} else if n > 0 {
		log.Printf("[cleanup] 清理登录临时会话 %d 条", n)
	}
	// R159: 账号删除后, 监控会话 keepAlive 检测到账号不存在即自动停止(防僵尸 401 风暴)
	c.MonitorEngine.SetAccountExistsProvider(func(tenantID, accountID uuid.UUID) bool {
		ctx2, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		_, err := c.AccountRepo.GetByID(ctx2, tenantID, accountID)
		return err == nil
	})
c.MonitorEngine.SetLoginCodeCallback(func(tenantID, accountID uuid.UUID, code string) {
		ctx2, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := c.AccountRepo.UpdateLoginCode(ctx2, tenantID, accountID, code); err != nil {
			log.Printf("[logincode] 落库失败: %v", err)
		}
		accName := "账号"
		if acc, aerr := c.AccountRepo.GetByID(ctx2, tenantID, accountID); aerr == nil && acc != nil {
			accName = acc.Phone
			if acc.Username != "" {
				accName += " (@" + acc.Username + ")"
			}
			if acc.Remark != "" {
				accName = acc.Remark + " " + accName
			}
		}
		c.SSEBroker.BroadcastToTenant(tenantID, "login_code", map[string]any{"account_id": accountID, "name": accName, "code": code})
		if c.Mailer != nil {
			if t, terr := c.TenantRepo.GetByID(ctx2, tenantID); terr == nil && t != nil {
				cst := time.FixedZone("CST", 8*3600)
				c.Mailer.Notify(t, notify.EventNeedVerify,
					fmt.Sprintf("【博士轩群发器】%s 登录验证码: %s (5分钟内有效)", accName, code),
					fmt.Sprintf("🔑 登录验证码\n\n账号: %s\n验证码: %s\n\n收到时间: %s（北京时间）\n\n把此码填入手机 Telegram 登录界面即可完成登录, 请勿泄露给他人。\n——博士轩群发器通知中心", accName, code, time.Now().In(cst).Format("2006-01-02 15:04:05")))
			}
		}
	})
	// R154: 假在线告警 → SSE + 日志(在线但 30 分钟无消息流过, 已自动重挂, 持续不恢复请手动重连)
	c.MonitorEngine.SetFlowDeadCallback(func(tenantID, accountID uuid.UUID) {
		accName := "账号"
		ctx2, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if acc, aerr := c.AccountRepo.GetByID(ctx2, tenantID, accountID); aerr == nil && acc != nil {
			accName = acc.Remark + " " + acc.Phone
		}
		log.Printf("[monitor] 假在线告警: tenant=%s account=%s(%s)", tenantID, accountID, accName)
		c.SSEBroker.BroadcastToTenant(tenantID, "flow_dead", map[string]any{"account_id": accountID, "name": accName})
	})
c.MonitorEngine.SetVerifyNotifier(func(tenantID, accountID uuid.UUID, groupTitle, status, reason string) {
		if status != "pass" || c.Mailer == nil {
			return
		}
		t, terr := c.TenantRepo.GetByID(context.Background(), tenantID)
		if terr != nil || t == nil {
			return
		}
		accName := accountID.String()[:8]
		if acc, aerr := c.AccountRepo.GetByID(context.Background(), tenantID, accountID); aerr == nil && acc != nil {
			if acc.FirstName != "" {
				accName = acc.FirstName
			} else if acc.Remark != "" {
				accName = acc.Remark
			} else if acc.Phone != "" {
				accName = acc.Phone
			}
		}
		subject := "【博士轩群发器】账号 " + accName + " 验证通过 — " + groupTitle
		body := "✅ 入群验证已通过\n\n账号: " + accName + "\n群组: " + groupTitle + "\n说明: " + reason + "\n时间: " + time.Now().Format("2006-01-02 15:04:05") + "\n——博士轩群发器通知中心"
		c.Mailer.Notify(t, notify.EventVerifySolved, subject, body)
	})
	c.MonitorEngine.SetVerifierDeps(c.VerificationLogRepo, func(tenantID uuid.UUID) (string, string, string, bool) {
		enabled, err := c.AIConfigRepo.VerifierEnabled(context.Background(), tenantID)
		if err != nil || !enabled {
			return "", "", "", false
		}
		provider, enc, model, hasKey, err := c.AIConfigRepo.Load(context.Background(), tenantID)
		if err != nil || !hasKey {
			return provider, "", model, true // 已启用但无Key: 文字类验证仍可用
		}
		raw, err := crypto.Decrypt(enc)
		if err != nil {
			log.Printf("[verifier] AI Key 解密失败 tenant=%s: %v", tenantID, err)
			return provider, "", model, true
		}
		return provider, string(raw), model, true
	})
	c.AIConfigHandler = handler.NewAIConfigHandler(c.AIConfigRepo)
	c.AIConfigHandler.SetLogRepo(c.VerificationLogRepo)
	c.AIConfigHandler.SetTenantRepo(c.TenantRepo)
	// 抽奖助手: 开关 provider 注入监控引擎
	c.MonitorEngine.SetRaffleProvider(func(tenantID uuid.UUID) bool {
		en, err := c.TenantRepo.IsRaffleEnabled(context.Background(), tenantID)
		return err == nil && en
	})
	// 中奖回复文案 provider(060): 引擎侧只读, 发送触发在 watchRaffleMessage 中奖分支
	c.MonitorEngine.SetRaffleReplyProvider(func(tenantID uuid.UUID) (bool, string) {
		en, text, err := c.TenantRepo.GetRaffleReply(context.Background(), tenantID)
		if err != nil {
			return false, ""
		}
		return en, text
	})
	// 私聊机器人激活 provider(061): 参与成功后私聊公告 bot, 建立私聊通道(中奖私聊通知=铁证)
	c.MonitorEngine.SetRaffleBotPingProvider(func(tenantID uuid.UUID) (bool, string) {
		en, text, err := c.TenantRepo.GetRaffleBotPing(context.Background(), tenantID)
		if err != nil {
			return false, ""
		}
		return en, text
	})
	// ===== 抽奖结果追踪接线(2026-10-20): 落库 + SSE + 持久化防重 =====
	// 结果处理: 落 raffle_results 表 + SSE 实时推送(参与成功/中奖)
	c.MonitorEngine.SetRaffleResultHandler(func(tenantID, accountID uuid.UUID, chatID int64, kind, keyword, prize, detail string) {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := c.RaffleResultRepo.Insert(ctx, tenantID, accountID, chatID, kind, keyword, prize, detail); err != nil {
			log.Printf("[raffle] 结果落库失败: %v", err)
		}
		// SSE 推送: 租户实时收到"参与成功/中奖"通知
		accName := accountID.String()[:8]
		if acc, err := c.AccountRepo.GetByID(ctx, tenantID, accountID); err == nil {
			accName = acc.Remark
			if accName == "" {
				accName = acc.Phone
			}
			_ = accName
		}
		var msg string
		switch kind {
		case "joined":
			msg = "✅ 账号 " + accName + " 参与成功: " + keyword
		case "winner":
			msg = "🎉 账号 " + accName + " 中奖!" + prize
		case "mentioned":
			msg = "📢 账号 " + accName + " 所在群开奖(未中奖)"
		}
		if msg != "" {
			c.SSEBroker.BroadcastToTenant(tenantID, "raffle_result", map[string]interface{}{
				"kind": kind, "account": accName, "keyword": keyword, "prize": prize, "message": msg,
			})
		}
				// R150: 开奖通知 = 机器人开奖原文转发 + 群号(租户要求: 完整名单自己看, 不按自己号过滤)
		if kind == "draw" && c.RaffleMailBatch != nil {
			c.RaffleMailBatch.Draw(tenantID, chatID, keyword, detail)
		}
// R148: 发起通知聚合 —— 同一活动 40s 内合并为一封
		if kind == "joined" && c.RaffleMailBatch != nil && keyword != "" {
			c.RaffleMailBatch.Joined(tenantID, chatID, keyword, accName)
		}
	})
	// 账号身份注入: 判中奖用(2026-10-21 修复: 此前 first_name 拿的是备注/手机号,
	// tg_user_id 恒为 0——"尊乐"中奖判未中奖实锤。改为读 sessions 表真实 TG 资料,
	// first+last 拼全名: TG 把姓名拆存(名"尊"+姓"乐"显示"尊 乐"), 只比名单字必漏)
	c.MonitorEngine.SetRaffleSelfInfo(func(tenantID, accountID uuid.UUID) (int64, string, string) {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		acc, err := c.AccountRepo.GetByID(ctx, tenantID, accountID)
		if err != nil || acc == nil {
			return 0, "", ""
		}
		// 真实资料: sessions 表 first_name/last_name/username
		if acc.SessionName != "" {
			if profiles, err := c.AccountRepo.GetSessionProfiles(ctx, []string{acc.SessionName}); err == nil {
				if p, ok := profiles[acc.SessionName]; ok && p != nil {
					full := strings.TrimSpace(strings.TrimSpace(p.FirstName) + " " + strings.TrimSpace(p.LastName))
					return 0, p.Username, full
				}
			}
		}
		// 兜底: 备注(不再用手机号——数字串不可能命中开奖名单)
		return 0, "", acc.Remark
	})
	// 持久化防重查询: 一个号一个活动一生一次
	c.MonitorEngine.SetRaffleJoinedQuery(func(tenantID, accountID uuid.UUID, chatID int64, keyword string) bool {
		ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
		defer cancel()
		ok, err := c.RaffleResultRepo.HasJoined(ctx, tenantID, accountID, chatID, keyword)
		if err != nil {
			return false
		}
		return ok
	})

	// 账号级抽奖开关(改进#1): 30s 缓存避免每条消息查库; 单账号关闭后不参与
	raffleAccCache := map[string]struct {
		en bool
		ts time.Time
	}{}
	var raffleAccMu sync.Mutex
	c.MonitorEngine.SetAccountRaffleProvider(func(tenantID, accountID uuid.UUID) bool {
		k := tenantID.String() + ":" + accountID.String()
		raffleAccMu.Lock()
		defer raffleAccMu.Unlock()
		if v, ok := raffleAccCache[k]; ok && time.Since(v.ts) < 30*time.Second {
			return v.en
		}
		acc, err := c.AccountRepo.GetByID(context.Background(), tenantID, accountID)
		en := err == nil && acc.RaffleEnabled
		raffleAccCache[k] = struct {
			en bool
			ts time.Time
		}{en, time.Now()}
		return en
	})
	// 套餐日额度(051): 免费体验版限额
	c.PlanLimitRepo = repository.NewPlanLimitRepository(c.DB.Pool)
	c.TaskService.SetPlanLimitRepo(c.PlanLimitRepo)
	c.TaskService.SetPlanRepo(c.PlanRepo) // 任务创建按套餐功能服务端强制门禁用
	c.MonitorService = service.NewMonitorService(c.MonitorRuleRepo, c.MonitorEngine)
	c.DashboardService = service.NewDashboardService(c.AccountRepo, c.TaskRepo, c.GroupRepo, c.RiskRepo, c.AuditRepo, c.TaskLogRepo)
	c.SettingsService = service.NewSettingsService(c.TenantRepo, c.PlanRepo, c.CodeRepo, c.SupportRepo, c.AnnounceRepo, c.TemplateRepo, c.AccountGroupRepo, c.MemberRepo)
	c.AdminService = service.NewAdminService(c.AdminRepo, c.TenantRepo, c.PlanRepo, c.CodeRepo, c.SupportRepo, c.AnnounceRepo, c.AuditRepo, c.ProbeRepo, c.Cfg, c.DB.Pool)
}

func (c *Container) initHandlers() {
	c.AuthHandler = handler.NewAuthHandler(c.Cfg, c.DB.Pool, c.TenantRepo, c.CodeRepo, c.PlanRepo, c.RefreshTokenRepo, c.AuditRepo, c.TaskEngine)
	// R181: 注入 TaskService —— 群转移接口要用目标账号创建 join_groups 任务
	c.AccountHandler = handler.NewAccountHandler(c.AccountService, c.MemberRepo, c.AuditRepo, c.TaskService)
	// R114: SquadHandler 构建已删
	// R114: CampaignHandler 已删
	c.GroupHandler = handler.NewGroupHandler(c.Cfg, c.GroupService, c.TaskService, c.TaskEngine, c.AccountRepo)
	c.MediaHandler = handler.NewMediaHandler(c.Cfg)
	c.ContactHandler = handler.NewContactHandler(c.MemberRepo)
	c.TaskHandler = handler.NewTaskHandler(c.TaskService, c.PlanChecker, c.AuditRepo, c.TaskEngine)
	// R98: 注入 SSE broker —— 任务详情流订阅事件流转发实时日志
	c.TaskHandler.SetSSEBroker(c.SSEBroker)
	c.MonitorHandler = handler.NewMonitorHandler(c.MonitorService, c.MonitorEngine, c.AccountService)

	// 重启对账: 服务启动 20 秒后(等账号池完成首批连接), 自动为存在
	// 启用规则的租户恢复监控会话——否则重启后所有监控会静默丢失,
	// 必须等用户手动拨杆才会恢复。
	safego.Go(func() {
		time.Sleep(20 * time.Second)
		c.MonitorHandler.BootReconcile(context.Background())
	})

	// 抽奖监听自动对账(2026-10-21): 抽奖抢答(tapRaffle)挂在监控引擎 OnMessage 上,
	// 会话不存在抽奖助手就是死的。总开关/勾选框只管"是否发送", 会话由这里兜底:
	// 启动 25 秒后首扫, 之后每 60 秒扫一遍"总开关开+账号勾选"的账号, 确保监听在线。
	// 覆盖: 服务重启、账号掉线重连、用户从不打开监控页等一切场景。
	safego.Go(func() {
		time.Sleep(25 * time.Second)
		for {
			c.reconcileRaffleWatch(context.Background())
			time.Sleep(60 * time.Second)
		}
	})
	// P2-87 审计修复: refresh_tokens 的 CleanupExpired 从未被调用, 表无限增长。
	// 挂在后台: 每小时清一次过期 token。
	safego.Go(func() {
		time.Sleep(30 * time.Second)
		for {
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			if err := c.RefreshTokenRepo.CleanupExpired(ctx); err != nil {
				log.Printf("[container] 清理过期 refresh token 失败: %v", err)
			}
			cancel()
			time.Sleep(time.Hour)
		}
	})
	// 总开关拨动即时触发(不等下一个 60 秒周期)
	c.AIConfigHandler.SetRaffleWatchSync(func(ctx context.Context, tenantID uuid.UUID) {
		c.reconcileRaffleWatchForTenant(ctx, tenantID)
	})

	c.DashboardHandler = handler.NewDashboardHandler(c.DashboardService)
	// R119: 通知中心 —— 构建邮件发送器并注入
	// R120: 租户级 SMTP —— Mailer 是无状态发送工具, 发件配置存各租户 notify_smtp 字段
	c.Mailer = notify.NewMailer()
	// R148: 抽奖邮件聚合 —— 发起40s窗口, 开奖5s窗口(开奖名单同一条消息, 几乎同时解析)
	c.RaffleMailBatch = notify.NewBatcher(c.Mailer, func(id uuid.UUID) *domain.Tenant {
		ctx2, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		t, err := c.TenantRepo.GetByID(ctx2, id)
		if err != nil {
			return nil
		}
		return t
	}, map[string]time.Duration{"joined": 40 * time.Second, "winner": 5 * time.Second})
	c.SettingsHandler = handler.NewSettingsHandler(c.SettingsService, c.Mailer)
	c.ExportHandler = handler.NewExportHandler(c.TenantRepo, c.MemberRepo, c.GroupRepo, c.TaskRepo, c.AuditRepo)
	c.AdminHandler = handler.NewAdminHandler(c.AdminService)
}

// reconcileRaffleWatch 全租户对账: 总开关开启的租户, 其勾选账号必须在线监听。
func (c *Container) reconcileRaffleWatch(ctx context.Context) {
	// P2-67 审计修复: 原来硬编码 List(1,1000)——超 1000 租户时漏对账。
	// 改为大页循环(99万上限=实际无上限, 分页接口要求 int 传参)。
	tenants, _, err := c.TenantRepo.List(ctx, 1, 990000, "", "")
	if err != nil {
		return
	}
	for _, t := range tenants {
		c.reconcileRaffleWatchForTenant(ctx, t.TenantID)
	}
}

// reconcileRaffleWatchForTenant 单租户对账(总开关拨动即时触发用)。
func (c *Container) reconcileRaffleWatchForTenant(ctx context.Context, tenantID uuid.UUID) {
	enabled, err := c.TenantRepo.IsRaffleEnabled(ctx, tenantID)
	if err != nil || !enabled {
		return
	}
	accs, err := c.AccountRepo.List(ctx, tenantID, "")
	if err != nil {
		return
	}
	// 启用规则全量传入(与 autoManageMonitoring 同语义, 规则内 AccountID 由匹配层过滤)
	allRules, _ := c.MonitorRuleRepo.ListByTenant(ctx, tenantID)
	var active []domain.MonitorRule
	for _, r := range allRules {
		if r.IsActive {
			active = append(active, *r)
		}
	}
	for _, acc := range accs {
		if !acc.RaffleEnabled {
			continue
		}
		// 会话已在线则 EnsureSession 直接跳过
		_ = c.MonitorEngine.EnsureSession(ctx, tenantID, acc.AccountID, active)
	}
}

func (c *Container) seedData(ctx context.Context) error {
	// Seed default plans: 仅在缺失时创建。已存在的套餐绝不在启动时覆盖 ——
	// 套餐数据归管理端所有(修复: 原实现每次启动都用硬编码值 upsert, 管理端改动
	// 和 SQL 调整全部在重启后被重置; 库里的中文 features 就是这个 seeder 写入的)。
	plans := []domain.Plan{
		{PlanID: "basic", Name: "基础版", PriceMonthly: 30, MaxAccounts: 10, MaxConcurrent: 5, Description: "10账号/5并发 手动核心不限量",
			Features: []string{"broadcast", "scrape_members", "join_groups", "discover", "search", "export"},
			IsActive: true, CanBroadcast: true, CanScrapeMembers: true, CanScrapeLinks: false, CanJoinGroups: true, CanMonitor: false, CanExport: true, CanUseTemplates: false},
		{PlanID: "pro", Name: "专业版", PriceMonthly: 70, MaxAccounts: 30, MaxConcurrent: 15, Description: "30账号/15并发 自动化全家桶",
			Features: []string{"broadcast", "scrape_members", "scrape_links", "join_groups", "monitor", "templates", "export", "scheduled_tasks", "discover", "search", "proxy", "invite", "verifier", "checkup", "dict"},
			IsActive: true, CanBroadcast: true, CanScrapeMembers: true, CanScrapeLinks: true, CanJoinGroups: true, CanMonitor: true, CanExport: true, CanUseTemplates: true},
		{PlanID: "enterprise", Name: "企业版", PriceMonthly: 140, MaxAccounts: 100, MaxConcurrent: 40, Description: "100账号/40并发 全功能不限量",
			Features: []string{"broadcast", "scrape_members", "scrape_links", "join_groups", "monitor", "templates", "export", "scheduled_tasks", "discover", "search", "proxy", "invite", "verifier", "ai_verify", "checkup", "dict", "api"},
			IsActive: true, CanBroadcast: true, CanScrapeMembers: true, CanScrapeLinks: true, CanJoinGroups: true, CanMonitor: true, CanExport: true, CanUseTemplates: true},
	}
	for _, p := range plans {
		if _, err := c.PlanRepo.GetByID(ctx, p.PlanID); err != nil {
			if err := c.PlanRepo.Create(ctx, &p); err != nil {
				log.Printf("[seed] 创建套餐 %s 失败: %v", p.PlanID, err)
			} else {
				log.Printf("[seed] 已创建套餐: %s", p.PlanID)
			}
		}
		// 已存在: 什么都不做(管理端/数据库是唯一事实来源)
	}

	rawPass := os.Getenv("ADMIN_PASSWORD")

	if admin, err := c.AdminRepo.GetByUsername(ctx, "admin"); err != nil {
		// 管理员不存在：必须创建。优先用 env；未设置则生成一次性随机密码并打印，
		// 之后任何重启都不会再改密码（数据库是唯一事实来源）。
		if rawPass == "" {
			rawPass = uuid.New().String()[:12] + uuid.New().String()[:4]
			// F-28: 初始密码写入 0600 文件而非日志（日志常被采集/外传）
			passFile := "admin_initial_password.txt"
			content := fmt.Sprintf("username: admin\npassword: %s\n", rawPass)
			if werr := os.WriteFile(passFile, []byte(content), 0o600); werr != nil {
				log.Printf("[SECURITY] 初始密码文件写入失败(%v)，退回日志输出: %s", werr, rawPass)
			} else {
				abs, _ := filepath.Abs(passFile)
				log.Printf("[SECURITY] 未设置 ADMIN_PASSWORD，已生成一次性管理员初始密码并写入 %s（0600，读取后请删除）", abs)
				log.Printf("[SECURITY] 请立即登录后台修改密码，并建议设置 ADMIN_PASSWORD。仅此一次生成，重启不会重置。")
			}
		} else {
			log.Printf("[seed] 使用.env中的ADMIN_PASSWORD创建管理员")
		}
		hash, err := bcrypt.GenerateFromPassword([]byte(rawPass), 12)
		if err != nil {
			log.Printf("[seed] 密码哈希失败: %v", err)
			return err
		}
		admin := &domain.Admin{
			AdminID:      uuid.New(),
			Username:     "admin",
			PasswordHash: string(hash),
			Role:         "super",  // 设计修复: 种子(唯一)管理员必须是最高权限, 否则套餐保存等 super 操作被自己 403
		}
		if err := c.AdminRepo.Create(ctx, admin); err != nil {
			log.Printf("[seed] 创建管理员失败: %v", err)
		} else {
			log.Printf("[SECURITY] Default admin created.")
		}
	} else {
		// 管理员已存在：只有显式设置了 ADMIN_PASSWORD 才同步；
		// 未设置时绝不重置密码，避免每次重启随机漂移把管理员锁死。
		if rawPass != "" {
			hash, err := bcrypt.GenerateFromPassword([]byte(rawPass), 12)
			if err != nil {
				log.Printf("[seed] 密码哈希失败: %v", err)
				return err
			}
			_, err = c.DB.Pool.Exec(ctx, `UPDATE admins SET password_hash = $1 WHERE admin_id = $2`, string(hash), admin.AdminID)
			if err != nil {
				log.Printf("[seed] 更新管理员密码失败: %v", err)
			} else {
				log.Printf("[seed] 管理员密码已同步为 env 中的 ADMIN_PASSWORD")
			}
		} else {
			log.Printf("[seed] 管理员已存在且未设置 ADMIN_PASSWORD，保持现有密码不变")
		}
	}

	// 设计修复: 单管理员部署自动保证 super(存量管理员零手工升级)
	upgradeSoleAdminToSuper(ctx, c.DB)
	return nil
}

// upgradeSoleAdminToSuper: 系统仅一名管理员时确保 role=super(单管理员场景无角色分级无意义)
func upgradeSoleAdminToSuper(ctx context.Context, db *database.DB) {
	var n int
	if err := db.Pool.QueryRow(ctx, `SELECT COUNT(*) FROM admins`).Scan(&n); err != nil || n != 1 {
		return
	}
	if tag, err := db.Pool.Exec(ctx, `UPDATE admins SET role='super' WHERE role IS DISTINCT FROM 'super'`); err == nil && tag.RowsAffected() > 0 {
		log.Println("[seed] 唯一管理员角色已自动升级为 super")
	}
}

// Shutdown performs graceful shutdown of all components.
func (c *Container) Shutdown() {
	close(c.stopCh)
	if c.GroupLibEngine != nil {
		c.GroupLibEngine.Stop()
	}
	if c.LinkScraper != nil {
		c.LinkScraper.Stop()
	}
	c.MonitorEngine.StopAll()
	c.TaskEngine.Stop()
	// R114: CampaignEngine.Stop 已删(引擎整建制移除)
	c.AccountEngine.Stop()
	c.AccountEngine.Close()
	// M-02: 主动关闭 SSE hub（此前 Broker.Close 存在但从未被调用，
	// 依赖请求 ctx 的 handler goroutine 无法被聚合回收）
	if c.SSEBroker != nil {
		c.SSEBroker.Close()
	}
	// P2-68 审计修复: 原 Shutdown 漏 backup.Stop()——备份 goroutine 随进程退出,
	// 但优雅停机场景(滚动更新)下会泄漏; 补上统一退出。
	backup.Stop()
	c.DB.Close()
}

// StopCh returns the stop channel for lifecycle management.
func (c *Container) StopCh() <-chan struct{} {
	return c.stopCh
}
