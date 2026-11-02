package service

import (
	"context"
	"fmt"
	"log"
	"os"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"golang.org/x/crypto/bcrypt"
	"tg-sender/internal/config"
	"tg-sender/internal/domain"
	"tg-sender/internal/middleware"
	"tg-sender/internal/repository"
)

// AdminService handles admin operations.
type AdminService struct {
	adminRepo    *repository.AdminRepository
	tenantRepo   *repository.TenantRepository
	planRepo     *repository.PlanRepository
	codeRepo     *repository.ActivationCodeRepository
	supportRepo  *repository.SupportConfigRepository
	probeRepo    *repository.ProbeConfigRepository // R119: 探测配置
	announceRepo *repository.AnnouncementRepository
	auditRepo    *repository.AuditRepository // F-12: admin 平面审计
	cfg          *config.Config
	db           *pgxpool.Pool
}

// NewAdminService creates a new admin service.
func NewAdminService(
	ar *repository.AdminRepository,
	tr *repository.TenantRepository,
	pr *repository.PlanRepository,
	cr *repository.ActivationCodeRepository,
	sr *repository.SupportConfigRepository,
	anr *repository.AnnouncementRepository,
	audr *repository.AuditRepository,
	pr2 *repository.ProbeConfigRepository, // R119: 探测配置
	cfg *config.Config,
	db *pgxpool.Pool,
) *AdminService {
	return &AdminService{
		adminRepo:    ar,
		tenantRepo:   tr,
		planRepo:     pr,
		codeRepo:     cr,
		supportRepo:  sr,
		announceRepo: anr,
		auditRepo:    audr,
		probeRepo:    pr2,
		cfg:          cfg,
		db:           db,
	}
}

// writeAudit 记录 admin 平面操作（F-12）。租户级动作用目标租户 ID，
// 平台级动作用 uuid.Nil 作为系统占位。审计失败不阻断业务，仅记日志。
// SystemTenantID 是后台平面审计记录挂载的系统租户（软删除态：不可登录、不在租户列表显示，
// 但行存在使 audit_logs.tenant_id 外键成立——修复 admin 审计 FK 冲突）。
const SystemTenantID = "00000000-0000-0000-0000-000000000001"

func (s *AdminService) writeAudit(ctx context.Context, tenantID uuid.UUID, action, detail string) {
	if s.auditRepo == nil {
		return
	}
	// 非租户上下文的调用（传 uuid.Nil）统一挂到系统租户
	if tenantID == uuid.Nil {
		tenantID = uuid.MustParse(SystemTenantID)
	}
	if err := s.auditRepo.Create(ctx, &domain.AuditLog{
		TenantID: tenantID,
		UserID:   "admin",
		Action:   action,
		Target:   detail,
		IP:       "admin-console",
	}); err != nil {
		log.Printf("[admin] audit write failed (%s): %v", action, err)
	}
}

// AdminLogin authenticates an admin and returns a JWT token.
func (s *AdminService) AdminLogin(ctx context.Context, username, password string) (string, error) {
	admin, err := s.adminRepo.GetByUsername(ctx, username)
	if err != nil {
		return "", fmt.Errorf("invalid credentials")
	}
	if err := bcrypt.CompareHashAndPassword([]byte(admin.PasswordHash), []byte(password)); err != nil {
		return "", fmt.Errorf("invalid credentials")
	}
	return middleware.GenerateAdminToken(s.cfg, admin.AdminID, admin.Username, admin.Role)
}

// GetStats returns platform statistics. Any DB error is returned instead of
// being swallowed: previously a dead database made the dashboard quietly show
// all zeros, looking like "platform idle" instead of "platform broken".
func (s *AdminService) GetStats(ctx context.Context) (map[string]any, error) {
	var totalTenants int
	if err := s.db.QueryRow(ctx, `SELECT COUNT(*) FROM tenants WHERE status = 'active'`).Scan(&totalTenants); err != nil {
		return nil, fmt.Errorf("stats: count tenants: %w", err)
	}
	var totalAccounts int
	if err := s.db.QueryRow(ctx, `SELECT COUNT(*) FROM accounts WHERE status = 'online'`).Scan(&totalAccounts); err != nil {
		return nil, fmt.Errorf("stats: count accounts: %w", err)
	}
	var todayTasks int
	if err := s.db.QueryRow(ctx, `SELECT COUNT(*) FROM tasks WHERE created_at >= CURRENT_DATE`).Scan(&todayTasks); err != nil {
		return nil, fmt.Errorf("stats: count tasks: %w", err)
	}
	var revenue int
	if err := s.db.QueryRow(ctx, `
		-- P2-78 审计修复: 原来 SUM 所有已用卡密——是累计总流水, 前端却显示"本月收入"。
		-- 加本月过滤(used_at >= 月初), 口径与字段名一致。
		SELECT COALESCE(SUM(p.price_monthly), 0)
		FROM activation_codes ac
		JOIN plans p ON ac.plan_id = p.plan_id
		WHERE ac.status = 'used' AND ac.used_at >= date_trunc('month', now())'`).Scan(&revenue); err != nil {
		return nil, fmt.Errorf("stats: revenue: %w", err)
	}

	var totalCodes, unusedCodes int
	if err := s.db.QueryRow(ctx, `SELECT COUNT(*), COUNT(*) FILTER (WHERE status = 'unused') FROM activation_codes`).Scan(&totalCodes, &unusedCodes); err != nil {
		return nil, fmt.Errorf("stats: count activation codes: %w", err)
	}

	return map[string]any{
		"total_tenants":  totalTenants,
		"active_tenants": totalTenants,
		"total_accounts": totalAccounts,
		"today_tasks":    todayTasks,
		"today_revenue":  revenue,
		"total_codes":    totalCodes,
		"unused_codes":   unusedCodes,
	}, nil
}

// GetSystemMetrics returns real system resource metrics.
func (s *AdminService) GetSystemMetrics(ctx context.Context) (map[string]any, error) {
	var dbConns int
	if err := s.db.QueryRow(ctx, `SELECT count(*) FROM pg_stat_activity WHERE datname = current_database()`).Scan(&dbConns); err != nil {
		log.Printf("[admin] 查询失败: %v", err)
	}
	var onlineConns int
	if err := s.db.QueryRow(ctx, `SELECT COUNT(*) FROM accounts WHERE status = 'online'`).Scan(&onlineConns); err != nil {
		log.Printf("[admin] 查询失败: %v", err)
	}
	var queueDepth int
	if err := s.db.QueryRow(ctx, `SELECT COUNT(*) FROM tasks WHERE status = 'queued'`).Scan(&queueDepth); err != nil {
		log.Printf("[admin] 查询失败: %v", err)
	}

	sysMemUsed, sysMemTotal := getMemInfo()
	memPercent := 0.0
	if sysMemTotal > 0 {
		memPercent = sysMemUsed / sysMemTotal * 100
	}

	sysDiskUsed, sysDiskTotal := getDiskInfo()
	diskPercent := 0.0
	if sysDiskTotal > 0 {
		diskPercent = sysDiskUsed / sysDiskTotal * 100
	}

	// 总账号与在线率(系统监控卡片用; 之前后端没返回导致前端永远显示 0)
	var totalAccs int
	if err := s.db.QueryRow(ctx, `SELECT COUNT(*) FROM accounts`).Scan(&totalAccs); err != nil {
		log.Printf("[admin] 查询总账号失败: %v", err)
	}
	onlineRate := 0.0
	if totalAccs > 0 {
		onlineRate = float64(onlineConns) / float64(totalAccs) * 100
	}

	return map[string]any{
		"cpu":                getCPUUsage(),
		"memory":             memPercent,
		"memoryUsed":         sysMemUsed,
		"memoryTotal":        sysMemTotal,
		"disk":               diskPercent,
		"diskUsed":           sysDiskUsed,
		"diskTotal":          sysDiskTotal,
		"online_connections": onlineConns,
		"total_accounts":     totalAccs,
		"online_rate":        onlineRate,
		"dbConnections":      dbConns,
		"queueDepth":         queueDepth,
		"goroutines":         runtime.NumGoroutine(),
	}, nil
}

// getCPUUsage reads /proc/loadavg for a non-blocking CPU estimate.
// Avoids the 500ms sleep that blocked HTTP goroutines.
func getCPUUsage() float64 {
	data, err := os.ReadFile("/proc/loadavg") // #nosec G304 - hardcoded system path
	if err != nil {
		return 0
	}
	fields := strings.Fields(string(data))
	if len(fields) < 1 {
		return 0
	}
	load1m, _ := strconv.ParseFloat(fields[0], 64)
	// 负载/实际核数 = CPU 使用率估算(写死 4 核在核数不符的机器上严重失真, 如 2 核机显示虚低)
	cores := float64(runtime.NumCPU())
	if cores < 1 {
		cores = 1
	}
	cpuPercent := load1m / cores * 100
	if cpuPercent > 100 {
		cpuPercent = 100
	}
	if cpuPercent < 0 {
		cpuPercent = 0
	}
	return cpuPercent
}

// getMemInfo reads /proc/meminfo to get system memory usage in MB.
// Falls back to Free+Buffers+Cached when MemAvailable is 0 (common in containers).
func getMemInfo() (used, total float64) {
	data, err := os.ReadFile("/proc/meminfo") // #nosec G304 - hardcoded system path
	if err != nil {
		return 0, 4096
	}
	lines := strings.Split(string(data), "\n")
	var memTotal, memAvailable, memFree, memBuffers, memCached uint64
	for _, line := range lines {
		if strings.HasPrefix(line, "MemTotal:") {
			_, _ = fmt.Sscanf(line, "MemTotal: %d", &memTotal)
		} else if strings.HasPrefix(line, "MemAvailable:") {
			_, _ = fmt.Sscanf(line, "MemAvailable: %d", &memAvailable)
		} else if strings.HasPrefix(line, "MemFree:") {
			_, _ = fmt.Sscanf(line, "MemFree: %d", &memFree)
		} else if strings.HasPrefix(line, "Buffers:") {
			_, _ = fmt.Sscanf(line, "Buffers: %d", &memBuffers)
		} else if strings.HasPrefix(line, "Cached:") {
			_, _ = fmt.Sscanf(line, "Cached: %d", &memCached)
		}
	}
	if memTotal == 0 {
		return 0, 4096
	}
	// /proc/meminfo 单位是 kB: kB/1024/1024 = GB (原 /1024 得到的是MB, 被误标GB导致 4G 机器显示 3915GB)
	total = float64(memTotal) / 1024 / 1024
	// Prefer MemAvailable; fallback to Free+Buffers+Cached for containers
	available := memAvailable
	if available == 0 {
		available = memFree + memBuffers + memCached
	}
	used = total - float64(available)/1024/1024
	if used < 0 {
		used = 0
	}
	return used, total
}

// getDiskInfo uses syscall.Statfs to get root partition usage in GB
func getDiskInfo() (used, total float64) {
	var stat syscall.Statfs_t
	if err := syscall.Statfs("/", &stat); err != nil {
		return 0, 100
	}
	// stat.Bsize = block size in bytes
	// stat.Blocks = total blocks
	// stat.Bavail = available blocks (for non-root)
	// stat.Bfree = free blocks
	blockSize := float64(stat.Bsize)
	totalBytes := float64(stat.Blocks) * blockSize
	freeBytes := float64(stat.Bfree) * blockSize
	usedBytes := totalBytes - freeBytes
	return usedBytes / 1024 / 1024 / 1024, totalBytes / 1024 / 1024 / 1024
}

// ListTenants returns paginated tenant list.
func (s *AdminService) ListTenants(ctx context.Context, page, pageSize int, status, planID string) ([]*domain.Tenant, int, error) {
	return s.tenantRepo.List(ctx, page, pageSize, status, planID)
}

// CreateTenant creates a new tenant with a generated API key.
func (s *AdminService) CreateTenant(ctx context.Context, name, username, password, planID string) (*domain.Tenant, string, error) {
	apiKey, hash, err := middleware.GenerateAPIKey()
	if err != nil {
		return nil, "", err
	}
	plan, err := s.planRepo.GetByID(ctx, planID)
	if err != nil {
		return nil, "", fmt.Errorf("plan not found")
	}
	// Hash password
	passHash, err := bcrypt.GenerateFromPassword([]byte(password), 12)
	if err != nil {
		return nil, "", fmt.Errorf("password hash failed: %w", err)
	}
	t := &domain.Tenant{
		TenantID:           uuid.New(),
		Name:               name,
		Username:           username,
		PasswordHash:       string(passHash),
		APIKeyHash:         hash,
		MaxAccounts:        plan.MaxAccounts,
		MaxConcurrentTasks: plan.MaxConcurrent,
		PlanID:             planID,
		Status:             "active",
	}
	if err := s.tenantRepo.Create(ctx, t, apiKey); err != nil {
		return nil, "", err
	}
	return t, apiKey, nil
}

// GetTenant returns tenant details.
func (s *AdminService) GetTenant(ctx context.Context, tenantID uuid.UUID) (*domain.Tenant, error) {
	return s.tenantRepo.GetByID(ctx, tenantID)
}

// UpdateTenant updates tenant plan or status.
func (s *AdminService) UpdateTenant(ctx context.Context, tenantID uuid.UUID, planID, status string, maxAccounts, maxConcurrent *int, expiresAtStr *string) error {
	updates := map[string]any{}
	if planID != "" {
		updates["plan_id"] = planID
		plan, err := s.planRepo.GetByID(ctx, planID)
		if err == nil {
			updates["max_accounts"] = plan.MaxAccounts
			updates["max_concurrent_tasks"] = plan.MaxConcurrent
		}
	}
	if status != "" {
		updates["status"] = status
	}
	if maxAccounts != nil {
		updates["max_accounts"] = *maxAccounts
	}
	if maxConcurrent != nil {
		updates["max_concurrent_tasks"] = *maxConcurrent
	}
	if expiresAtStr != nil {
		if *expiresAtStr == "" {
			updates["expires_at"] = nil
		} else {
			// Support both "2006-01-02T15:04" (datetime-local) and RFC3339
			var t time.Time
			var err error
			t, err = time.ParseInLocation("2006-01-02T15:04", *expiresAtStr, time.Local)
			if err != nil {
				t, err = time.Parse(time.RFC3339, *expiresAtStr)
			}
			if err != nil {
				return fmt.Errorf("invalid expires_at format: %w", err)
			}
			updates["expires_at"] = t.UTC()
		}
	}

	if err := s.tenantRepo.Update(ctx, tenantID, updates); err != nil {
		return err
	}
	// F-04: 封禁/改期/改套餐后立即失效租户状态缓存（原最长 60s 窗口）。
	// 注：API key 认证原语已随 F-29 移除，无对应缓存可失效。
	middleware.InvalidateTenantCache(tenantID)
	s.writeAudit(ctx, tenantID, "admin_tenant_update", fmt.Sprintf("plan_id=%q status=%q", planID, status))
	return nil
}

// ResetAPIKey generates a new API key for a tenant.
func (s *AdminService) ResetAPIKey(ctx context.Context, tenantID uuid.UUID) (string, error) {
	apiKey, hash, err := middleware.GenerateAPIKey()
	if err != nil {
		return "", err
	}
	if err := s.tenantRepo.UpdateAPIKeyHash(ctx, tenantID, hash, apiKey); err != nil {
		return "", err
	}
	// F-29 注：API key 认证原语已移除，仅存哈希列；重置仍轮换哈希供未来启用。
	s.writeAudit(ctx, tenantID, "admin_apikey_reset", "")
	return apiKey, nil
}

// DeleteTenant 执行软删除（F-15）：置 deleted_at + status='suspended' 并立即失效缓存。
// 数据默认保留 30 天，期间租户数据可通过 /tenant/export 导出；
// 到期由 PurgeExpiredTenants 物理清除（含全部级联）。
func (s *AdminService) DeleteTenant(ctx context.Context, tenantID uuid.UUID) error {
	res, err := s.db.Exec(ctx,
		`UPDATE tenants SET status = 'suspended', deleted_at = NOW()
		 WHERE tenant_id = $1 AND deleted_at IS NULL`, tenantID)
	if err != nil {
		return fmt.Errorf("soft delete tenant: %w", err)
	}
	if res.RowsAffected() == 0 {
		return fmt.Errorf("tenant not found or already deleted")
	}
	middleware.InvalidateTenantCache(tenantID)
	s.writeAudit(ctx, tenantID, "admin_tenant_delete", "tenant soft-deleted (retention 30d)")
	return nil
}

// PurgeExpiredTenants 物理清除软删除超过保留期（默认 30 天）的租户。
func (s *AdminService) PurgeExpiredTenants(ctx context.Context, retentionDays int) (int, error) {
	if retentionDays <= 0 {
		retentionDays = 30
	}
	rows, err := s.db.Query(ctx,
		`SELECT tenant_id FROM tenants
		 WHERE deleted_at IS NOT NULL AND deleted_at < NOW() - make_interval(days => $1)`, retentionDays)
	if err != nil {
		return 0, fmt.Errorf("list expired tenants: %w", err)
	}
	var ids []uuid.UUID
	for rows.Next() {
		var id uuid.UUID
		if err := rows.Scan(&id); err == nil {
			ids = append(ids, id)
		}
	}
	rows.Close()
	purged := 0
	for _, id := range ids {
		if err := s.PurgeTenant(ctx, id); err != nil {
			log.Printf("[admin] purge tenant %s failed: %v", id, err)
			continue
		}
		purged++
	}
	if purged > 0 {
		s.writeAudit(ctx, uuid.Nil, "admin_tenants_purge", fmt.Sprintf("purged=%d retention=%dd", purged, retentionDays))
	}
	return purged, nil
}

// PurgeTenant physically deletes a tenant and all related data.
// F-15: 仅用于保留期后的物理清除（PurgeExpiredTenants），日常删除走 DeleteTenant 软删除。
// All deletions run in one transaction: a mid-way failure previously left
// orphaned rows (e.g. tasks deleted but accounts kept).
func (s *AdminService) PurgeTenant(ctx context.Context, tenantID uuid.UUID) error {
	exec := func(tx pgx.Tx) error {
		// 先解除 activation_codes 的外键关联（used_by 字段引用 tenants.tenant_id）。
		// F-05: 置为 revoked 而非 unused，防止已核销卡密被第三人重新注册。
		if _, err := tx.Exec(ctx, "UPDATE activation_codes SET used_by = NULL, status = 'revoked', used_at = NULL WHERE used_by = $1", tenantID); err != nil {
			return fmt.Errorf("release activation codes: %w", err)
		}

		// P1-14 审计修复: sessions 表无 tenant_id 列, 靠 session_id = accounts.session_name
		// 关联。必须赶在 accounts 删除之前清理——否则 TG 登录态(auth_key 等敏感数据)
		// 永久残留, 且主键占用导致重新导入同名号冲突。
		if _, err := tx.Exec(ctx, `
			DELETE FROM sessions
			WHERE session_id IN (SELECT session_name FROM accounts WHERE tenant_id = $1)
		`, tenantID); err != nil {
			return fmt.Errorf("delete sessions: %w", err)
		}

		// 清理 account_groups 关联（该表无 tenant_id 列，需通过 account_id 关联删除）
		if _, err := tx.Exec(ctx, `
			DELETE FROM account_groups
			WHERE account_id IN (SELECT account_id FROM accounts WHERE tenant_id = $1)
		`, tenantID); err != nil {
			return fmt.Errorf("delete account_groups: %w", err)
		}

		// account_quotas 表没有 tenant_id 列, 经由 accounts 子查询删除
		if _, err := tx.Exec(ctx, `
			DELETE FROM account_quotas
			WHERE account_id IN (SELECT account_id FROM accounts WHERE tenant_id = $1)
		`, tenantID); err != nil {
			return fmt.Errorf("delete account_quotas: %w", err)
		}

		// F-05: 以下关联表无 tenant_id 列（或仅有 NOT VALID 外键），必须经
		// accounts/campaigns 子查询删除，否则删租户后残留孤儿数据（含 PII）。
		if _, err := tx.Exec(ctx, `
			DELETE FROM squad_members
			WHERE account_id IN (SELECT account_id FROM accounts WHERE tenant_id = $1)
		`, tenantID); err != nil {
			return fmt.Errorf("delete squad_members: %w", err)
		}
		if _, err := tx.Exec(ctx, `
			DELETE FROM campaign_accounts
			WHERE campaign_id IN (SELECT id FROM campaigns WHERE tenant_id = $1)
		`, tenantID); err != nil {
			return fmt.Errorf("delete campaign_accounts: %w", err)
		}
		if _, err := tx.Exec(ctx, `
			DELETE FROM campaign_targets
			WHERE campaign_id IN (SELECT id FROM campaigns WHERE tenant_id = $1)
		`, tenantID); err != nil {
			return fmt.Errorf("delete campaign_targets: %w", err)
		}

		// batch_tasks/contacts/tenant_subscriptions/refresh_tokens/audit_logs/ai_configs/
		// raffle_results/text_dicts/account_group_categories 已带 ON DELETE CASCADE 外键，
		// 删 tenants 时自动清理；此处仅补无级联外键的表。
		// P1-14: announcements/plan_daily_counters/verification_logs 有 tenant_id 列
		// 但无外键, 删租户后残留——补进显式清单。
		tables := []string{
			"risk_events", "task_logs", "pending_approvals",
			"scraped_links", "tasks",
			"accounts", "monitor_rules",
			"members", "groups", "message_templates",
			"squads", "campaigns", "token_blacklist",
			"announcements", "plan_daily_counters", "verification_logs",
		}
		allowedTables := map[string]bool{"risk_events": true, "task_logs": true, "pending_approvals": true, "scraped_links": true, "tasks": true, "accounts": true, "monitor_rules": true, "members": true, "groups": true, "message_templates": true, "squads": true, "campaigns": true, "token_blacklist": true, "announcements": true, "plan_daily_counters": true, "verification_logs": true}
		for _, table := range tables {
			if !allowedTables[table] {
				return fmt.Errorf("invalid table: %s", table)
			}
			if _, err := tx.Exec(ctx, "DELETE FROM "+table+" WHERE tenant_id = $1", tenantID); err != nil { // #nosec G202 - table name validated by allowedTables whitelist
				return fmt.Errorf("delete from %s: %w", table, err)
			}
		}
		if _, err := tx.Exec(ctx, "DELETE FROM tenants WHERE tenant_id = $1", tenantID); err != nil {
			return fmt.Errorf("delete tenant: %w", err)
		}
		return nil
	}

	err := pgx.BeginFunc(ctx, s.db, func(tx pgx.Tx) error {
		if err := exec(tx); err != nil {
			log.Printf("[admin] 删除租户 %s 失败，已回滚: %v", tenantID, err)
			return err
		}
		return nil
	})
	if err != nil {
		return err
	}
	// F-04: 删除后立即失效租户状态缓存，避免已删租户 token 在窗口内继续可用。
	middleware.InvalidateTenantCache(tenantID)
	s.writeAudit(ctx, tenantID, "admin_tenant_delete", "tenant purged (retention expired)")
	return nil
}

// ListPlans returns all plans (P1-43: 含已停用——管理端需要恢复误删/误停的套餐).
func (s *AdminService) ListPlans(ctx context.Context) ([]domain.Plan, error) {
	return s.planRepo.ListAll(ctx)
}

// UpdatePlan updates a plan.
func (s *AdminService) UpdatePlan(ctx context.Context, planID string, updates map[string]any) error {
	plan, err := s.planRepo.GetByID(ctx, planID)
	if err != nil {
		return err
	}
	if v, ok := updates["name"].(string); ok {
		plan.Name = v
	}
	if v, ok := updates["price_monthly"].(float64); ok {
		plan.PriceMonthly = int(v)
	}
	if v, ok := updates["max_accounts"].(float64); ok {
		plan.MaxAccounts = int(v)
	}
	if v, ok := updates["max_concurrent"].(float64); ok {
		plan.MaxConcurrent = int(v)
	}
	if v, ok := updates["description"].(string); ok {
		plan.Description = v
	}
	if v, ok := updates["is_active"].(bool); ok {
		plan.IsActive = v
	}
	if err := s.planRepo.Update(ctx, planID, updates); err != nil {
		return err
	}
	// P1-5 审计修复: 改套餐后清空 plan 缓存——原 60 秒 TTL 内仍按旧 features 校验。
	middleware.InvalidatePlanCache()
	s.writeAudit(ctx, uuid.Nil, "admin_plan_update", fmt.Sprintf("plan_id=%s", planID))
	return nil
}

// GenerateCodes generates activation codes.
func (s *AdminService) GenerateCodes(ctx context.Context, adminID uuid.UUID, planID string, count int, timingType string, expiresDays *int, notes string) ([]domain.ActivationCode, error) {
	codes, err := s.codeRepo.Generate(ctx, planID, count, timingType, expiresDays, adminID, notes)
	if err != nil {
		return nil, err
	}
	s.writeAudit(ctx, uuid.Nil, "admin_codes_generate", fmt.Sprintf("plan_id=%s count=%d", planID, count))
	return codes, nil
}

// ListCodes returns activation codes.
func (s *AdminService) ListCodes(ctx context.Context, status, planID string, page, pageSize int) ([]domain.ActivationCode, int, error) {
	return s.codeRepo.List(ctx, status, planID, page, pageSize)
}

// RevokeCode revokes an activation code.
func (s *AdminService) RevokeCode(ctx context.Context, codeID uuid.UUID) error {
	return s.codeRepo.Revoke(ctx, codeID)
}

// ExportCodes returns all unused activation codes.
func (s *AdminService) ExportCodes(ctx context.Context) ([]domain.ActivationCode, error) {
	return s.codeRepo.Export(ctx)
}

// GetSupport returns support config.
func (s *AdminService) GetSupport(ctx context.Context) (*domain.SupportConfig, error) {
	return s.supportRepo.Get(ctx)
}

// UpdateSupport updates support config.
func (s *AdminService) UpdateSupport(ctx context.Context, cfg *domain.SupportConfig) error {
	return s.supportRepo.Update(ctx, cfg)
}

// GetProbe R119: 读取账号能力探测配置(全局单行)
func (s *AdminService) GetProbe(ctx context.Context) (*domain.ProbeConfig, error) {
	return s.probeRepo.Get(ctx)
}

// UpdateProbe R119: 更新探测配置
func (s *AdminService) UpdateProbe(ctx context.Context, cfg *domain.ProbeConfig) error {
	return s.probeRepo.Update(ctx, cfg.TestUsername, cfg.PrivateInvite, cfg.PublicLink)
}

// ListAnnouncements returns announcements.
func (s *AdminService) ListAnnouncements(ctx context.Context) ([]domain.Announcement, error) {
	return s.announceRepo.ListAll(ctx)
}

// CreateAnnouncement creates an announcement.
func (s *AdminService) CreateAnnouncement(ctx context.Context, a *domain.Announcement) error {
	return s.announceRepo.Create(ctx, a)
}

// UpdateAnnouncement updates an announcement.
func (s *AdminService) UpdateAnnouncement(ctx context.Context, req *domain.Announcement) error {
	existing, err := s.announceRepo.ListAll(ctx)
	if err != nil {
		return err
	}
	for i := range existing {
		if existing[i].AnnouncementID == req.AnnouncementID {
			existing[i].Title = req.Title
			existing[i].Content = req.Content
			existing[i].Priority = req.Priority
			existing[i].TargetType = req.TargetType
			existing[i].TargetPlans = req.TargetPlans
			existing[i].IsActive = req.IsActive
			return s.announceRepo.Update(ctx, &existing[i])
		}
	}
	return fmt.Errorf("announcement not found")
}

// DeleteAnnouncement deletes an announcement.
func (s *AdminService) DeleteAnnouncement(ctx context.Context, id uuid.UUID) error {
	return s.announceRepo.Delete(ctx, id)
}

// parseAuditTime parses an audit-log time filter. Empty input = no filter.
// Accepts YYYY-MM-DD or RFC3339. Returns nil (no filter) or the parsed time.
func parseAuditTime(v string) (*time.Time, error) {
	if v == "" {
		return nil, nil
	}
	for _, layout := range []string{"2006-01-02", time.RFC3339, time.RFC3339Nano} {
		if t, err := time.Parse(layout, v); err == nil {
			return &t, nil
		}
	}
	return nil, fmt.Errorf("invalid time format %q: use YYYY-MM-DD or RFC3339", v)
}

// GetAuditLogs returns audit logs. startTime/endTime filter created_at
// (previously accepted but ignored — admin frontend time filter was broken).
func (s *AdminService) GetAuditLogs(ctx context.Context, page, pageSize int, startTime, endTime string) ([]*domain.AuditLog, int, error) {
	start, err := parseAuditTime(startTime)
	if err != nil {
		return nil, 0, err
	}
	end, err := parseAuditTime(endTime)
	if err != nil {
		return nil, 0, err
	}

	where, args := "", []interface{}{}
	if start != nil {
		args = append(args, *start)
		where = fmt.Sprintf("WHERE created_at >= $%d", len(args))
	}
	if end != nil {
		args = append(args, *end)
		if where == "" {
			where = fmt.Sprintf("WHERE created_at <= $%d", len(args))
		} else {
			where += fmt.Sprintf(" AND created_at <= $%d", len(args))
		}
	}

	var total int
	countQuery := "SELECT COUNT(*) FROM audit_logs " + where
	if err := s.db.QueryRow(ctx, countQuery, args...).Scan(&total); err != nil {
		return nil, 0, err
	}

	args = append(args, pageSize, (page-1)*pageSize)
	rows, err := s.db.Query(ctx, `
		SELECT log_id, tenant_id, user_id, action, target, ip::text, created_at
		FROM audit_logs `+where+` ORDER BY created_at DESC LIMIT $`+fmt.Sprint(len(args)-1)+` OFFSET $`+fmt.Sprint(len(args)), args...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	logs := []*domain.AuditLog{}
	for rows.Next() {
		var al domain.AuditLog
		if err := rows.Scan(&al.LogID, &al.TenantID, &al.UserID, &al.Action, &al.Target, &al.IP, &al.CreatedAt); err != nil {
			continue
		}
		logs = append(logs, &al)
	}
	return logs, total, rows.Err()
}

// GetCode returns a single activation code by ID (admin only, no tenant filter needed for global codes table).
func (s *AdminService) GetCode(ctx context.Context, codeID uuid.UUID) (*domain.ActivationCode, error) {
	return s.codeRepo.GetByID(ctx, codeID)
}

// DeleteCode hard-deletes an activation code.
func (s *AdminService) DeleteCode(ctx context.Context, codeID uuid.UUID) error {
	return s.codeRepo.Delete(ctx, codeID)
}

// BatchRevokeCodes revokes multiple codes.
func (s *AdminService) BatchRevokeCodes(ctx context.Context, ids []uuid.UUID) error {
	return s.codeRepo.BatchRevoke(ctx, ids)
}

// BatchDeleteCodes deletes multiple codes.
func (s *AdminService) BatchDeleteCodes(ctx context.Context, ids []uuid.UUID) error {
	return s.codeRepo.BatchDelete(ctx, ids)
}
