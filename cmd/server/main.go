package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	_ "net/http/pprof" // E-05: pprof 剖析端点（ENABLE_PPROF=1 时生效）
	"os"
	"os/signal"
	"path/filepath"
	"runtime/debug"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/labstack/echo/v4"
	echomiddleware "github.com/labstack/echo/v4/middleware"
	echoSwagger "github.com/swaggo/echo-swagger"
	_ "tg-sender/docs"
	"tg-sender/internal/app"
	"tg-sender/internal/config"
	"tg-sender/internal/middleware"
	"tg-sender/internal/safego"
)

// getStaticPath returns the absolute path to web/dist based on the executable location
func getStaticPath() string {
	// Try to get the executable's directory
	ex, err := os.Executable()
	if err != nil {
		return "web/dist"
	}
	exeDir := filepath.Dir(ex)

	// If running from go run (temp dir), fall back to working directory
	if strings.Contains(exeDir, "/tmp/") || strings.Contains(exeDir, "go-build") {
		wd, err := os.Getwd()
		if err != nil {
			return "web/dist"
		}
		return filepath.Join(wd, "web/dist")
	}

	return filepath.Join(exeDir, "web/dist")
}

func main() {
	cfg := config.Load()
	if err := cfg.Validate(); err != nil {
		log.Fatalf("config validation failed: %v", err)
	}

	// Set memory limit to prevent OOM killer (默认 2800MiB，可用 MEMORY_LIMIT_MB 调整)
	debug.SetMemoryLimit(int64(cfg.MemoryLimitMB) * 1024 * 1024)


	// Panic → TG 告警: 任何后台 goroutine panic 时推送给你(需配 TG_ALERT_BOT_TOKEN/CHAT_ID)
	if cfg.AlertBotToken != "" && cfg.AlertChatID != "" {
		safego.SetPanicHook(func(msg string) {
			url := fmt.Sprintf("https://api.telegram.org/bot%s/sendMessage", cfg.AlertBotToken)
			payload, _ := json.Marshal(map[string]string{
				"chat_id": cfg.AlertChatID,
				"text":    "🚨 TG-Sender 后台异常(panic):\n" + msg,
			})
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, strings.NewReader(string(payload)))
			if err != nil {
				return
			}
			req.Header.Set("Content-Type", "application/json")
			resp, err := http.DefaultClient.Do(req)
			if err == nil && resp != nil {
				resp.Body.Close()
			}
		})
	}

	// Setup log rotation
	if cfg.LogFile != "" {
		f, err := os.OpenFile(cfg.LogFile, os.O_CREATE|os.O_WRONLY|os.O_APPEND, cfg.LogFileMode)
		if err != nil {
			log.Printf("failed to open log file %s: %v", cfg.LogFile, err)
		} else {
			log.SetOutput(&rotatingLogWriter{f: f, maxSize: defaultLogMaxSize, maxBackups: defaultLogMaxBackups, mode: cfg.LogFileMode})
		}
	}

	// P1-32 审计修复: LOG_LEVEL 配置读了却从没接线。标准库 log 无内建级别过滤,
	// 这里接可落地部分: debug 打开文件行号便于排障; warn/error 如实告知限制
	// (真过滤需 slog 迁移, 不在本批引入大改动)。
	switch strings.ToLower(cfg.LogLevel) {
	case "debug":
		log.SetFlags(log.LstdFlags | log.Lshortfile)
	case "warn", "error":
		log.Printf("[config] LOG_LEVEL=%s 已接收, 但标准库 logger 无级别过滤, 暂按 info 输出", cfg.LogLevel)
	}

	// Initialize dependency container
	container, err := app.NewContainer(cfg)
	if err != nil {
		log.Fatalf("container init failed: %v", err)
	}
	defer container.Shutdown()

	// Initialize token blacklist cache from DB
	middleware.InitTokenBlacklist(container.DB.Pool, container.StopCh())
	middleware.InitAdminBlacklist(container.DB.Pool) // F-07

	// Setup Echo server
	e := newEchoServer(cfg, container)

	// E-05: pprof 条件开启——仅监听 127.0.0.1（需 docker exec/kubectl exec 进容器后 curl），
	// 设 ENABLE_PPROF=1 开启，默认关闭。net/http/pprof 的空导入注册到 DefaultServeMux。
	if os.Getenv("ENABLE_PPROF") == "1" {
		go func() {
			log.Print("[pprof] listening on 127.0.0.1:6060 (localhost only)")
			if err := http.ListenAndServe("127.0.0.1:6060", nil); err != nil {
				log.Printf("[pprof] %v", err)
			}
		}()
	}

	// Start server
	go func() {
		// 审计P1-06: 应用超时配置(WriteTimeout=0: SSE长连接不设写超时)
		srv := &http.Server{
			Addr:              fmt.Sprintf(":%s", cfg.ServerPort),
			ReadTimeout:       cfg.ReadTimeout,
			ReadHeaderTimeout: cfg.ReadTimeout,
			WriteTimeout:      0,
			MaxHeaderBytes:    cfg.MaxHeaderBytes,
			Handler:           e,
		}
		if err := e.StartServer(srv); err != nil && err != http.ErrServerClosed {
			log.Fatalf("server start failed: %v", err)
		}
	}()

	// Wait for interrupt signal
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	log.Println("Shutting down server...")

	// Graceful shutdown
	shutdownCtx, cancel := context.WithTimeout(context.Background(), cfg.ShutdownTimeout)
	defer cancel()

	if err := e.Shutdown(shutdownCtx); err != nil {
		log.Printf("server shutdown error: %v", err)
	}

	log.Println("Server exited")
}

func newEchoServer(cfg *config.Config, c *app.Container) *echo.Echo {
	e := echo.New()
	// 访问日志（自实现，替代 Echo 默认 Logger）：
	// 原实现 ${uri} 含完整查询串，会把 ?phone=、?keyword= 等 PII 与 SSE ticket
	// 写进日志。这里只记 path，查询串一律不落盘；SSE 端点整体跳过。
	e.Use(func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			start := time.Now()
			err := next(c)
			path := c.Path()
			if strings.HasPrefix(path, "/api/v1/events") ||
				(strings.HasPrefix(path, "/api/v1/tasks/") && strings.HasSuffix(path, "/progress/stream")) {
				return err // SSE 长连接，跳过
			}
			// 日志只留"出问题"的: 2xx/3xx 静默(防刷屏), 4xx/5xx 全保留(排查线索)
			if c.Response().Status >= 400 {
				log.Printf("[http] %s %s %d %s ip=%s",
					c.Request().Method, path, c.Response().Status, time.Since(start), c.RealIP())
			}
			return err
		}
	})
	e.Use(echomiddleware.Recover())

	// CORS - 生产环境禁止允许所有来源
	allowedOrigins := []string{"http://localhost:3000", "http://localhost:5173"}
	if cfg.AllowedOrigins != "" {
		allowedOrigins = strings.Split(cfg.AllowedOrigins, ",")
	}
	e.Use(echomiddleware.CORSWithConfig(echomiddleware.CORSConfig{
		AllowOrigins:     allowedOrigins,
		AllowMethods:     []string{http.MethodGet, http.MethodPost, http.MethodPut, http.MethodDelete, http.MethodOptions},
		AllowHeaders:     []string{echo.HeaderOrigin, echo.HeaderContentType, echo.HeaderAccept, echo.HeaderAuthorization},
		AllowCredentials: true,
	}))

	// Validate CORS configuration in production
	if cfg.Env == "production" {
		hasWildcard := false
		for _, o := range allowedOrigins {
			if strings.TrimSpace(o) == "*" {
				hasWildcard = true
				break
			}
		}
		if hasWildcard || cfg.AllowedOrigins == "" {
			log.Fatal("production environment requires explicit AllowedOrigins (wildcard '*' is not allowed)")
		}
	}

	e.Use(echomiddleware.BodyLimit(defaultBodyLimit))

	// Security headers
	e.Use(func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			c.Response().Header().Set("X-Frame-Options", "DENY")
			c.Response().Header().Set("X-Content-Type-Options", "nosniff")
			// F-13: script-src 收紧为 'self'（前端构建产物无内联脚本，已核实 index.html）
			c.Response().Header().Set("Content-Security-Policy", "default-src 'self'; script-src 'self'; style-src 'self' 'unsafe-inline'; img-src 'self' data: blob: https:; connect-src 'self' ws: wss:;")
			c.Response().Header().Set("Referrer-Policy", "strict-origin-when-cross-origin")
			return next(c)
		}
	})

	// ===== Public routes =====
	// F-01: 认证端点挂严格限流(10次/分/IP)，防密码爆破与卡密枚举。
	authLimit := middleware.AuthRateLimit()
	e.POST("/api/v1/auth/register", c.AuthHandler.Register, authLimit)
	e.POST("/api/v1/auth/login", c.AuthHandler.Login, authLimit)
	e.POST("/api/v1/auth/renew", c.AuthHandler.Renew, authLimit)
	e.POST("/api/v1/auth/refresh", c.AuthHandler.Refresh, authLimit)
	e.POST("/api/v1/auth/logout", c.AuthHandler.Logout)

	// Public support info (no auth required)
	e.GET("/api/v1/support", c.SettingsHandler.GetSupport)

	// ===== Protected tenant routes =====
	api := e.Group("/api/v1")
	// 流式端点不设请求级超时：/progress/stream 与 /events(SSE) 都是长连接，
	// 30s deadline 会到点掐断（/events 后缀匹配仅命中 SSE 端点，risk-events 不受影响）
	api.Use(middleware.TimeoutWithSkip(cfg.RequestTimeout, "/progress/stream", "/events"))
	api.Use(middleware.JWTAuth(cfg, c.TenantRepo, c.DB.Pool))
	api.Use(middleware.TenantIsolation())
	api.Use(middleware.PlanContext(c.PlanRepo, c.TenantRepo))
	api.Use(middleware.RateLimit())

	api.GET("/auth/me", c.AuthHandler.Me)

	// SSE 事件流（ticket 或 ?token= 均可；路由在 JWTAuth 组内，ticket 校验后注入租户上下文）
	api.GET("/events", c.SSEBroker.Handler("events"))

	// Account routes
	// SSE 短期ticket(审计P0-03): 带JWT换60秒ticket, EventSource用ticket连/events
	api.POST("/events/ticket", func(c echo.Context) error {
		tid, err := middleware.GetTenantID(c)
		if err != nil {
			return err
		}
		return c.JSON(200, map[string]string{"ticket": middleware.IssueEventTicket(tid)})
	})
	api.GET("/accounts", c.AccountHandler.List)
	api.GET("/accounts/stats", c.AccountHandler.Stats)
	api.POST("/accounts/:id/avatar/refresh", c.AccountHandler.RefreshAvatar)
	api.POST("/accounts", c.AccountHandler.Create)
	api.GET("/accounts/:id", c.AccountHandler.GetByID)
	api.DELETE("/accounts/:id", c.AccountHandler.Delete)
	api.POST("/accounts/:id/connect", c.AccountHandler.Connect)
	api.POST("/accounts/:id/disconnect", c.AccountHandler.Disconnect)
	api.POST("/accounts/:id/code", c.AccountHandler.SubmitCode)
	api.GET("/accounts/:id/auth-state", c.AccountHandler.GetAuthState)
	api.POST("/accounts/:id/password", c.AccountHandler.Submit2FA)
	api.POST("/accounts/import", c.AccountHandler.ImportSessions)
	api.POST("/accounts/import-files", c.AccountHandler.ImportSessionFiles)
	api.PUT("/accounts/:id/profile", c.AccountHandler.UpdateProfile)
	api.PUT("/accounts/:id/pool", c.AccountHandler.SetPool)
	api.POST("/accounts/pool-batch", c.AccountHandler.BatchSetPool)
	api.POST("/accounts/profile-batch", c.AccountHandler.BatchUpdateProfile)
	api.PUT("/accounts/:id/2fa", c.AccountHandler.Update2FA)
	api.POST("/accounts/:id/reconnect", c.AccountHandler.Reconnect)
	api.POST("/accounts/:id/leave-all-groups", c.TaskHandler.LeaveAllGroups)
	// R181 群转移: 源号导出邀请链接 → 目标号创建 join_groups 任务(间隔租户可调)
	api.POST("/accounts/:id/transfer-groups", c.AccountHandler.TransferGroups)
	api.PUT("/accounts/:id/raffle", c.PlanChecker.RequirePlanFeature(middleware.FeatureRaffle)(c.AccountHandler.SetRaffle))
	api.POST("/accounts/:id/avatar", c.AccountHandler.UploadAvatar)
	// R119: 账号能力探测(8 项矩阵, 管理端配置探测目标, 全部套餐可用)
	api.POST("/accounts/:id/probe", c.AccountHandler.RunProbe)
	api.GET("/accounts/:id/probe", c.AccountHandler.GetProbe)
	api.GET("/accounts/:id/avatar", c.AccountHandler.GetAvatar) // 拉取账号TG头像(前端走 axios blob)
	// Echo v4: 中间件以"调用"方式包裹 handler —— mw(handler), 不是逗号并列
	// 能力体检/SpamBot申诉 后端已随前端一并移除(2026-10-21)
	api.PUT("/accounts/:id/proxy", c.AccountHandler.SetProxy)   // 设置账号代理
	api.PUT("/accounts/:id/profile", c.AccountHandler.UpdateProfile) // 修改 TG 资料(名字/签名/用户名)
	api.POST("/accounts/clone-profile", c.AccountHandler.CloneProfile) // 批量克隆他人资料
	api.POST("/accounts/:id/proxy/test", c.AccountHandler.TestProxy) // 代理连通性检测
	// 找群: 平台级群组库(所有租户采集汇总, 平台共享)
	// 群库浏览平台共享(引流): 登录即可看; 批量加入(discover)与搜群(search)才需套餐权限
	api.GET("/group-library", c.GroupLibraryHandler.List)
	api.POST("/group-library/join", c.PlanChecker.RequirePlanFeature(middleware.FeatureDiscover)(c.GroupLibraryHandler.JoinSelected))
	api.POST("/group-library/search", c.GroupLibraryHandler.Search) // 关键词搜群(借在线账号TG搜索, 入库)
	// R116: /text-dicts 词典管理 API 七端点已删(前端词典管理 UI 消亡后零调用;
	// 词典数据本身仍服务群发变量 {{词典名}} —— engine 直读 TextDictRepo, 不经过 HTTP 层)
	// 入群验证: AI 配置(加密存储) + 总开关 + 验证记录
	api.GET("/ai-config", c.PlanChecker.RequirePlanFeature(middleware.FeatureVerifier)(c.AIConfigHandler.Get))
	api.PUT("/ai-config", c.PlanChecker.RequirePlanFeature(middleware.FeatureVerifier)(c.AIConfigHandler.Save))
	api.POST("/ai-config/test", c.AIConfigHandler.Test)
	api.POST("/verifier/toggle", c.PlanChecker.RequirePlanFeature(middleware.FeatureVerifier)(c.AIConfigHandler.Toggle))
	// 抽奖助手: 群内抽奖公告自动参与
	api.GET("/raffle/status", c.PlanChecker.RequirePlanFeature(middleware.FeatureRaffle)(c.AIConfigHandler.RaffleStatus))
	api.POST("/raffle/toggle", c.PlanChecker.RequirePlanFeature(middleware.FeatureRaffle)(c.AIConfigHandler.RaffleToggle))
	api.POST("/raffle/reply", c.PlanChecker.RequirePlanFeature(middleware.FeatureRaffle)(c.AIConfigHandler.RaffleReply))
	api.POST("/raffle/ping", c.PlanChecker.RequirePlanFeature(middleware.FeatureRaffle)(c.AIConfigHandler.RafflePing))
	api.GET("/raffle/results", c.PlanChecker.RequirePlanFeature(middleware.FeatureRaffle)(c.TaskHandler.RaffleResults))
	// R128 清空参与记录
	api.DELETE("/raffle/results", c.PlanChecker.RequirePlanFeature(middleware.FeatureRaffle)(c.TaskHandler.RaffleResultsClear))
	api.GET("/verification-logs", c.PlanChecker.RequirePlanFeature(middleware.FeatureVerifier)(c.AIConfigHandler.Logs))
	api.POST("/accounts/:id/migrate", c.AccountHandler.MigrateDC)
	// 协议号系统: 编队 + 舰队看板(专业版/企业版, service层校验套餐)
	// R114: /squads 七端点已删(小队系统整建制尸体——前端零页面/路由引用)。
	// squad_members 表清理 SQL 保留在 account/admin(删号/删租户清外键)。
	// R114: /campaigns 五端点已删(协议协同旧系统, 前端零调用)。
	// 表保留, account 删号时的外键清理仍走 repository.CampaignRepository。
	api.PUT("/accounts/:id", c.AccountHandler.Update)
	api.GET("/accounts/:id/quotas", c.AccountHandler.GetQuotas)
	api.GET("/accounts/:id/telegram-dialogs", c.AccountHandler.GetTelegramDialogs)
	api.GET("/accounts/:id/telegram-contacts", c.AccountHandler.GetTelegramContacts)
	api.POST("/accounts/:id/resolve-peer", c.AccountHandler.ResolvePeer)

	// Group routes
	api.GET("/groups", c.GroupHandler.List)
	api.POST("/groups/sync", c.GroupHandler.SyncGroups)
	api.POST("/groups/sync-all", c.GroupHandler.SyncAllGroups)
	// P1-29 审计修复: 全局 BodyLimit=10M 会把 media 的 50MB 上限直接拒在 handler 之外。
	// 给 media 路由单独放宽到 52M(留 multipart 开销余量), 其他接口仍受 10M 保护。
	mediaApi := api.Group("", echomiddleware.BodyLimit("52M"))
	mediaApi.POST("/media", c.MediaHandler.Upload)
	api.GET("/contacts", c.ContactHandler.List)
	api.POST("/groups", c.GroupHandler.Create)
	api.GET("/groups/:id", c.GroupHandler.GetByID)
	api.PUT("/groups/:id", c.GroupHandler.Update)
	api.GET("/groups/:id/members", c.GroupHandler.GetMembers)
	api.GET("/members", c.GroupHandler.GetMembersGlobal)
	// P1-1 审计修复: 原来无 RequirePlanFeature——免费版可绕过 scrape_members 限制
	api.POST("/groups/:id/scrape", c.PlanChecker.RequirePlanFeature(middleware.FeatureScrapeMembers)(c.GroupHandler.ScrapeMembers))

	// Task routes
	api.GET("/tasks", c.TaskHandler.List)
	api.POST("/tasks", c.TaskHandler.Create)
	api.POST("/tasks/validate-targets", c.TaskHandler.ValidateTargets)
	api.GET("/tasks/:id", c.TaskHandler.GetByID)
	api.PUT("/tasks/:id", c.TaskHandler.Update)
	api.POST("/tasks/:id/pause", c.TaskHandler.Pause)
	api.POST("/tasks/:id/resume", c.TaskHandler.Resume)
	api.POST("/tasks/:id/retry", c.TaskHandler.Retry)
	api.POST("/tasks/:id/cancel", c.TaskHandler.Cancel)
	api.POST("/tasks/batch/cancel", c.TaskHandler.BatchCancel)
	api.POST("/tasks/batch/delete", c.TaskHandler.BatchDelete)
	api.DELETE("/tasks/:id", c.TaskHandler.Delete)
	api.GET("/tasks/:id/logs", c.TaskHandler.GetLogs)
	api.GET("/tasks/:id/stats-by-account", c.TaskHandler.GetStatsByAccount)
	api.DELETE("/tasks/:id/logs", c.TaskHandler.DeleteTaskLogs)
	// R116: /logs/cleanup 已删(前端封装零调用)
	api.GET("/scrape-links", c.TaskHandler.ListScrapedLinks)
	api.GET("/scrape-links/export", c.TaskHandler.ExportScrapedLinks)
	api.POST("/scrape-links/toggle", c.TaskHandler.ToggleLinkScraping)
	api.GET("/scrape-links/status", c.TaskHandler.LinkScrapingStatus)
	api.GET("/tasks/logs", c.TaskHandler.GetAccountLogs)
	api.GET("/tasks/:id/progress", c.TaskHandler.GetProgress)
	api.GET("/tasks/:id/progress/stream", c.TaskHandler.StreamProgress)

	// Monitor routes
	api.GET("/monitor/rules", c.MonitorHandler.List, c.PlanChecker.RequirePlanFeature(middleware.FeatureMonitor))
	api.POST("/monitor/rules", c.MonitorHandler.Create, c.PlanChecker.RequirePlanFeature(middleware.FeatureMonitor))
	api.GET("/monitor/rules/:id", c.MonitorHandler.GetByID, c.PlanChecker.RequirePlanFeature(middleware.FeatureMonitor))
	api.PUT("/monitor/rules/:id", c.MonitorHandler.Update, c.PlanChecker.RequirePlanFeature(middleware.FeatureMonitor))
	api.POST("/monitor/rules/:id/toggle", c.MonitorHandler.Toggle, c.PlanChecker.RequirePlanFeature(middleware.FeatureMonitor))
	api.DELETE("/monitor/rules/:id", c.MonitorHandler.Delete, c.PlanChecker.RequirePlanFeature(middleware.FeatureMonitor))
	api.GET("/monitor/messages", c.MonitorHandler.GetMessages, c.PlanChecker.RequirePlanFeature(middleware.FeatureMonitor))
	api.POST("/monitor/start/:account_id", c.MonitorHandler.StartMonitoring, c.PlanChecker.RequirePlanFeature(middleware.FeatureMonitor))
	api.POST("/monitor/stop/:account_id", c.MonitorHandler.StopMonitoring, c.PlanChecker.RequirePlanFeature(middleware.FeatureMonitor))

	// Dashboard routes
	api.GET("/dashboard/stats", c.DashboardHandler.Stats)
	api.GET("/dashboard/activities", c.DashboardHandler.AuditLogs)
	api.GET("/dashboard/send-calendar", c.DashboardHandler.SendCalendar)
	api.GET("/dashboard/risk-events", c.DashboardHandler.RiskEvents)

	// Settings routes
	api.GET("/tenant/profile", c.SettingsHandler.GetProfile)
	// R119: 通知中心(邮箱订阅)
	api.GET("/tenant/notify", c.SettingsHandler.GetNotify)
	api.PUT("/tenant/notify", c.SettingsHandler.UpdateNotify)
	api.POST("/tenant/notify/test", c.SettingsHandler.TestNotify)
	api.GET("/tenant/export", c.ExportHandler.ExportMyData) // G-05 租户自助数据导出（合规）
	api.POST("/tenant/activate", c.SettingsHandler.ActivateCode)
	api.GET("/plans", c.SettingsHandler.GetPlans)
	api.GET("/announcements", c.SettingsHandler.GetAnnouncements)
	api.POST("/announcements/:id/read", c.SettingsHandler.MarkAnnouncementRead)

	// Message Templates
	api.GET("/templates", c.SettingsHandler.ListTemplates, c.PlanChecker.RequirePlanFeature(middleware.FeatureTemplates))
	api.POST("/templates", c.SettingsHandler.CreateTemplate, c.PlanChecker.RequirePlanFeature(middleware.FeatureTemplates))
	api.PUT("/templates/:id", c.SettingsHandler.UpdateTemplate, c.PlanChecker.RequirePlanFeature(middleware.FeatureTemplates))
	api.DELETE("/templates/:id", c.SettingsHandler.DeleteTemplate, c.PlanChecker.RequirePlanFeature(middleware.FeatureTemplates))

	// Account Groups
	api.GET("/account-groups", c.SettingsHandler.ListAccountGroups)
	api.POST("/account-groups", c.SettingsHandler.CreateAccountGroup)
	api.PUT("/account-groups/:id", c.SettingsHandler.UpdateAccountGroup)
	api.DELETE("/account-groups/:id", c.SettingsHandler.DeleteAccountGroup)

	// Export members
	api.GET("/scrape/:id/export", c.SettingsHandler.ExportScrape) // 导出是数据自有权利

	// SPA index.html：绝不可缓存。浏览器启发式缓存（RFC 7234：无显式 Cache-Control
	// 时按 Last-Modified 的 10% 自动缓存且不重新验证）会让旧 index.html 在重新
	// 构建后继续存活，其中引用的 content-hash chunk 已不存在 → 刷新白屏。
	staticPath := getStaticPath()
	serveIndex := func(c echo.Context) error {
		c.Response().Header().Set("Cache-Control", "no-cache, no-store, must-revalidate")
		return c.File(filepath.Join(staticPath, "index.html"))
	}

	// Admin SPA fallback (before API routes to avoid middleware interception)
	e.GET("/admin", serveIndex)
	e.GET("/admin/*", serveIndex)

	// ===== Admin routes =====
	admin := e.Group("/admin")
	admin.POST("/auth/login", c.AdminHandler.AdminLogin, middleware.RateLimit())
	admin.POST("/auth/logout", c.AdminHandler.AdminLogout, middleware.AdminAuth(cfg, c.DB.Pool))

	// Protected admin routes
	adminAuth := admin.Group("")
	adminAuth.Use(middleware.AdminAuth(cfg, c.DB.Pool)) // F-07: 持久化黑名单

	adminAuth.GET("/stats", c.AdminHandler.GetStats)
	adminAuth.GET("/system", c.AdminHandler.GetSystemMetrics)
	adminAuth.GET("/tenants", c.AdminHandler.ListTenants)
	adminAuth.POST("/tenants", c.AdminHandler.CreateTenant)
	adminAuth.GET("/tenants/:id", c.AdminHandler.GetTenant)
	adminAuth.PUT("/tenants/:id", c.AdminHandler.UpdateTenant)
	// F-06: 高危操作仅 super 角色可执行（RequireRole 读取 AdminAuth 设置的 admin_role）。
	adminAuth.POST("/tenants/:id/reset-key", c.AdminHandler.ResetAPIKey, middleware.RequireRole("super"))
	adminAuth.DELETE("/tenants/:id", c.AdminHandler.DeleteTenant, middleware.RequireRole("super"))
	// F-15: 软删除保留期后的物理清除（super）
	adminAuth.POST("/tenants/purge-expired", c.AdminHandler.PurgeExpiredTenants, middleware.RequireRole("super"))

	adminAuth.GET("/plans", c.AdminHandler.ListPlans)
	adminAuth.PUT("/plans/:id", c.AdminHandler.UpdatePlan, middleware.RequireRole("super"))

	adminAuth.POST("/codes", c.AdminHandler.GenerateCodes)
	adminAuth.GET("/codes", c.AdminHandler.ListCodes)
	adminAuth.GET("/codes/export", c.AdminHandler.ExportCodes)
	adminAuth.DELETE("/codes/:id", c.AdminHandler.RevokeCode)
	adminAuth.GET("/codes/:id", c.AdminHandler.GetCode)
	adminAuth.POST("/codes/batch-revoke", c.AdminHandler.BatchRevokeCodes)
	adminAuth.POST("/codes/batch-delete", c.AdminHandler.BatchDeleteCodes, middleware.RequireRole("super"))

	adminAuth.GET("/support", c.AdminHandler.GetSupport)
	adminAuth.PUT("/support", c.AdminHandler.UpdateSupport)
	// R119: 账号能力探测配置(全局)
	adminAuth.GET("/probe", c.AdminHandler.GetProbe)
	adminAuth.PUT("/probe", c.AdminHandler.UpdateProbe)

	adminAuth.GET("/announcements", c.AdminHandler.ListAnnouncements)
	adminAuth.POST("/announcements", c.AdminHandler.CreateAnnouncement)
	adminAuth.PUT("/announcements/:id", c.AdminHandler.UpdateAnnouncement)
	adminAuth.DELETE("/announcements/:id", c.AdminHandler.DeleteAnnouncement)

	adminAuth.GET("/audit-logs", c.AdminHandler.GetAuditLogs)

	// Health check
	version := cfg.Version
	if version == "" {
		version = defaultVersion
	}
	// G-04: 健康检查探活 DB——原实现静态 200，DB 故障时容器仍报 healthy（假健康）。
	// DB 不可用时返回 503，编排器据此标记 unhealthy 并重启/摘除。
	dbPool := c.DB.Pool // 在闭包外捕获，避免与 echo.Context 形参 c 遮蔽
	e.GET("/health", func(c echo.Context) error {
		ctx, cancel := context.WithTimeout(c.Request().Context(), 2*time.Second)
		defer cancel()
		if err := dbPool.Ping(ctx); err != nil {
			return c.JSON(http.StatusServiceUnavailable, map[string]any{
				"status": "db_unavailable", "error": err.Error(),
				"version": version, "time": time.Now().UTC(),
			})
		}
		return c.JSON(http.StatusOK, map[string]any{
			"status":  "ok",
			"version": version,
			"time":    time.Now().UTC(),
		})
	})

	// Static assets (absolute path based on executable location)
	// Content-hash 资源（vite 输出文件名含哈希，内容变则哈希变）：immutable 长缓存
	e.GET("/assets/*", func(c echo.Context) error {
		c.Response().Header().Set("Cache-Control", "public, max-age=31536000, immutable")
		return echo.WrapHandler(http.StripPrefix("/assets", http.FileServer(http.Dir(filepath.Join(staticPath, "assets")))))(c)
	})
	e.File("/vite.svg", filepath.Join(staticPath, "vite.svg"))
	e.File("/manifest.json", filepath.Join(staticPath, "manifest.json"))

	// Root -> index.html
	e.GET("/", serveIndex)

	// SPA fallback: all unmatched routes -> index.html
	e.GET("/*", serveIndex)

	// Swagger: 仅非生产环境开放(审计P1-15: 生产暴露API结构降低攻击者枚举成本)
	if cfg.Env != "production" {
		e.GET("/swagger/*", echoSwagger.WrapHandler)
	}
	e.GET("/docs/", func(c echo.Context) error {
		return c.File(filepath.Join(staticPath, "../docs/swagger.html"))
	})

	return e
}

const (
	defaultLogMaxSize    = 100 * 1024 * 1024 // 100MB
	defaultLogMaxBackups = 5
	defaultBodyLimit     = "10M"
	defaultVersion       = "v2.6.0"
)

type rotatingLogWriter struct {
	f          *os.File
	maxSize    int64
	maxBackups int
	mode       os.FileMode
	mu         sync.Mutex
}

func (w *rotatingLogWriter) Write(p []byte) (n int, err error) {
	w.mu.Lock()
	defer w.mu.Unlock()

	info, err := w.f.Stat()
	if err == nil && info.Size()+int64(len(p)) > w.maxSize {
		if rerr := w.rotate(); rerr != nil {
			log.Printf("log rotation failed: %v", rerr)
		}
	}
	return w.f.Write(p)
}

func (w *rotatingLogWriter) Close() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.f.Close()
}

// degrade 在 rotate 任一步失败时保证日志仍有出口(P1-33 审计修复):
// 原实现 open 失败路径会让 w.f 指向已 Close 的旧文件——此后每次 Write 都
// 静默失败, 一次 rotate 失败=日志永久丢失。现统一在 error 出口恢复可写性:
// 优先重开原文件, 重开也失败则降级到 stderr, 绝不留失效句柄。
func (w *rotatingLogWriter) degrade(name string, cause error) error {
	if f, err := os.OpenFile(name, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0666); err == nil {
		w.f = f
	} else {
		w.f = os.Stderr
		log.Printf("log rotate degraded to stderr: %v", cause)
	}
	return cause
}

func (w *rotatingLogWriter) rotate() error {
	oldName := w.f.Name()
	if err := w.f.Close(); err != nil {
		return w.degrade(oldName, fmt.Errorf("close old log: %w", err))
	}
	for i := w.maxBackups - 1; i > 0; i-- {
		oldPath := fmt.Sprintf("%s.%d", oldName, i)
		newPath := fmt.Sprintf("%s.%d", oldName, i+1)
		_ = os.Remove(newPath)
		_ = os.Rename(oldPath, newPath)
	}
	_ = os.Remove(oldName + ".1")
	if err := os.Rename(oldName, oldName+".1"); err != nil {
		return w.degrade(oldName, fmt.Errorf("rename log: %w", err))
	}
	f, err := os.OpenFile(oldName, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0666)
	if err != nil {
		return w.degrade(oldName, fmt.Errorf("open new log: %w", err))
	}
	w.f = f
	return nil
			// R169: img-src 补 blob: —— 账号头像是前端 blob:ObjectURL, 原 CSP 缺 blob: 会被浏览器拦截
}
