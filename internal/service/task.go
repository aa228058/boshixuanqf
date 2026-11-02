package service

import (
	"tg-sender/internal/domain/task"
	"context"
	"fmt"
	"log"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"tg-sender/internal/config"
	"tg-sender/internal/domain"
	"tg-sender/internal/repository"
	"tg-sender/internal/sse"
)

// tgUsernameRe 是 TG 官方用户名规则(P1-38): 字母开头, 仅字母/数字/下划线, 总长 5-32。
var tgUsernameRe = regexp.MustCompile("^[A-Za-z][A-Za-z0-9_]{4,31}$")

// TaskEngine interface for cancelling running tasks.
type TaskEngine interface {
	CancelTask(taskID uuid.UUID)
	WarmAccounts(ctx context.Context, tenantID uuid.UUID, accountIDs []uuid.UUID)
	LeaveAllGroups(ctx context.Context, tenantID, accountID uuid.UUID) (*task.LeaveAllResult, error)
}

// TaskService handles task business logic.
type TaskService struct {
	cfg         *config.Config
	repo        *repository.TaskRepository
	logRepo     *repository.TaskLogRepository
	quota       *repository.QuotaRepository
	accountRepo *repository.AccountRepository
	tenantRepo  *repository.TenantRepository
	groupRepo   *repository.GroupRepository
	planLimits  *repository.PlanLimitRepository
	raffleResultRepo *repository.RaffleResultRepository
	planRepo    *repository.PlanRepository
	memberRepo  domain.MemberRepository
	batchRepo   domain.BatchTaskRepository
	sseBroker   *sse.Broker
	engine      TaskEngine

	scrapedLinkRepo domain.ScrapedLinkRepository

	// 常驻链接采集服务(可空, 容器装配后注入)
	linkScraper LinkScraperController
}

// LinkScraperController 常驻链接采集服务的控制面(由 domain/task.LinkScraper 实现)。
type LinkScraperController interface {
	RegisterAccount(ctx context.Context, tenantID, accountID uuid.UUID)
	UnregisterAccount(accountID uuid.UUID)
	IsRegistered(accountID uuid.UUID) bool
}

// NewTaskService creates a new task service.
func NewTaskService(cfg *config.Config, repo *repository.TaskRepository, logRepo *repository.TaskLogRepository, quota *repository.QuotaRepository, accountRepo *repository.AccountRepository, tenantRepo *repository.TenantRepository, groupRepo *repository.GroupRepository, memberRepo domain.MemberRepository, batchRepo domain.BatchTaskRepository, sseBroker *sse.Broker, engine TaskEngine, scrapedLinkRepo domain.ScrapedLinkRepository, raffleResultRepo *repository.RaffleResultRepository) *TaskService {
	return &TaskService{cfg: cfg, repo: repo, logRepo: logRepo, quota: quota, accountRepo: accountRepo, tenantRepo: tenantRepo, groupRepo: groupRepo, memberRepo: memberRepo, batchRepo: batchRepo, sseBroker: sseBroker, engine: engine, scrapedLinkRepo: scrapedLinkRepo, raffleResultRepo: raffleResultRepo}
}

// ListScrapedLinks 查询链接采集结果(按账号+可选时间窗口过滤)。
// since 传任务创建时间即可圈出单次任务的采集窗口。
func (s *TaskService) ListScrapedLinks(ctx context.Context, tenantID uuid.UUID, accountID uuid.UUID, since time.Time, page, pageSize int) ([]*domain.ScrapedLink, int, error) {
	if page < 1 {
		page = 1
	}
	if pageSize <= 0 || pageSize > 1000 {
		pageSize = 100
	}
	return s.scrapedLinkRepo.ListByAccount(ctx, tenantID, accountID, since, pageSize, (page-1)*pageSize)
}

// DeleteTaskLogs 清空某任务的全部日志。
func (s *TaskService) DeleteTaskLogs(ctx context.Context, tenantID, taskID uuid.UUID) error {
	return s.logRepo.DeleteByTask(ctx, tenantID, taskID)
}

// R116: CleanupLogs 已删(/logs/cleanup 端点移除后零调用)

// SetLinkScraper 注入常驻链接采集服务(容器装配用)。
// ValidateTargets 发送前目标预检: 数字 ID 查 groups/members 表确认已知(含 hash),
// username 做格式校验(实际可达性需发送时实时解析, 无法预检)。
func (s *TaskService) ValidateTargets(ctx context.Context, tenantID uuid.UUID, targets []string) ([]string, []map[string]string) {
	valid := []string{}
	invalid := []map[string]string{}
	for _, t := range targets {
		t = strings.TrimSpace(t)
		if t == "" {
			continue
		}
		if id, err := strconv.ParseInt(t, 10, 64); err == nil {
			if g, _ := s.groupRepo.GetByTGChatID(ctx, tenantID, id); g != nil && g.AccessHash != 0 {
				valid = append(valid, t)
				continue
			}
			if h, _ := s.memberRepo.GetAccessHashByTGUserID(ctx, tenantID, id); h != 0 {
				valid = append(valid, t)
				continue
			}
			invalid = append(invalid, map[string]string{"target": t, "reason": "未知目标(未同步/已退出)"})
		} else {
			u := strings.TrimPrefix(t, "@")
			// P1-38 审计修复: 原只查长度——"!!!@#$%" 这类脏数据也算 valid,
			// 整批任务带着必死目标空转。按 TG 官方规则收紧: 字母开头,
			// 其余仅字母/数字/下划线, 总长 5-32。
			if !tgUsernameRe.MatchString(u) {
				invalid = append(invalid, map[string]string{"target": t, "reason": "用户名格式不正确"})
			} else {
				valid = append(valid, t)
			}
		}
	}
	return valid, invalid
}

func (s *TaskService) SetLinkScraper(ls LinkScraperController) { s.linkScraper = ls }

// SetLinkScraping 开启/关闭某账号的常驻链接监听。
func (s *TaskService) SetLinkScraping(ctx context.Context, tenantID, accountID uuid.UUID, enabled bool) {
	if s.linkScraper == nil {
		return
	}
	if enabled {
		s.linkScraper.RegisterAccount(ctx, tenantID, accountID)
	} else {
		s.linkScraper.UnregisterAccount(accountID)
	}
}

// LinkScrapingEnabled 返回某账号的常驻链接监听是否开启。
func (s *TaskService) LinkScrapingEnabled(accountID uuid.UUID) bool {
	return s.linkScraper != nil && s.linkScraper.IsRegistered(accountID)
}

// validateAccountOwnership checks that all account IDs belong to the tenant.
func (s *TaskService) validateAccountOwnership(ctx context.Context, tenantID uuid.UUID, accountIDs []uuid.UUID) error {
	if len(accountIDs) == 0 {
		return nil
	}
	found, err := s.accountRepo.ListByIDs(ctx, tenantID, accountIDs)
	if err != nil {
		return fmt.Errorf("validate accounts: %w", err)
	}
	if len(found) != len(accountIDs) {
		return fmt.Errorf("some accounts not found or not owned by tenant")
	}
	return nil
}

// RaffleResultClear R128: 清空抽奖参与记录(调仓库 DeleteAll)
func (s *TaskService) RaffleResultClear(ctx context.Context, tenantID uuid.UUID) error {
	return s.raffleResultRepo.DeleteAll(ctx, tenantID)
}

// List returns tasks for a tenant with optional account filtering.
func (s *TaskService) List(ctx context.Context, tenantID uuid.UUID, status *domain.TaskStatus, accountID *uuid.UUID, taskType *domain.TaskType, hideLoops bool, since *time.Time, page, pageSize int) ([]*domain.Task, error) {
	if pageSize <= 0 {
		pageSize = s.cfg.DefaultPageSize
	}
	offset := (page - 1) * pageSize
	return s.repo.List(ctx, tenantID, status, accountID, taskType, hideLoops, since, pageSize, offset)
}

// Create dispatches to specific task creation based on type.
// taskTypeFeature 任务类型 → 套餐功能 key(服务端强制, 防止绕过前端直接调 API)
var taskTypeFeature = map[domain.TaskType]string{
	domain.TaskTypeBroadcast:        "broadcast",
	domain.TaskTypeScrapeMembers:    "scrape_members",
	domain.TaskTypeScrapeLinks:      "scrape_links",
	domain.TaskTypeJoinGroups:       "join_groups",
	domain.TaskTypeMonitorScrape:    "monitor",
	domain.TaskTypeInviteMembers:    "invite",
	domain.TaskTypeScheduledBroadcast: "scheduled_tasks",
}

// checkPlanFeature 校验租户套餐是否含指定功能(FeatureMap 单一数据源, ≤60s 缓存)
func (s *TaskService) checkPlanFeature(ctx context.Context, tenantID uuid.UUID, feature string) error {
	if s.planRepo == nil || feature == "" {
		return nil
	}
	t, err := s.tenantRepo.GetByID(ctx, tenantID)
	if err != nil || t == nil {
		return nil // 取不到租户不阻断(保守)
	}
	plan, err := s.planRepo.GetByID(ctx, t.PlanID)
	if err != nil || plan == nil {
		return nil
	}
	if v, ok := plan.FeatureMap()[feature]; !ok || v == false {
		return fmt.Errorf("当前套餐不含此功能(%s), 升级套餐后可用", feature)
	}
	return nil
}

func (s *TaskService) Create(ctx context.Context, tenantID uuid.UUID, taskType domain.TaskType, accountIDs []uuid.UUID, targetIDs []string, payload map[string]any, scheduledAt *time.Time) (*domain.Task, error) {
	// 套餐功能门禁(服务端强制)
	if fk, ok := taskTypeFeature[taskType]; ok {
		if err := s.checkPlanFeature(ctx, tenantID, fk); err != nil {
			return nil, err
		}
	}
	// Check all accounts are active (not banned or restricted)
	// Batch fetch all accounts to avoid N+1 query
	accounts, err := s.accountRepo.ListByIDs(ctx, tenantID, accountIDs)
	if err != nil {
		return nil, fmt.Errorf("fetch accounts: %w", err)
	}
	if len(accounts) != len(accountIDs) {
		return nil, fmt.Errorf("some accounts not found (expected %d, got %d)", len(accountIDs), len(accounts))
	}
	for _, acc := range accounts {
		if acc.Status == domain.AccountStatusBanned {
			return nil, fmt.Errorf("account %s is banned", acc.AccountID)
		}
		if acc.Status == domain.AccountStatusRestricted {
			return nil, fmt.Errorf("account %s is restricted", acc.AccountID)
		}
	}

	// Check tenant expiration
	if s.tenantRepo != nil {
		if tenant, err := s.tenantRepo.GetByID(ctx, tenantID); err == nil && tenant != nil {
			if tenant.ExpiresAt != nil && tenant.ExpiresAt.Before(time.Now()) {
				return nil, fmt.Errorf("tenant subscription expired")
			}
		}
	}

	// Check tenant concurrent task limit
	runningCount, err := s.repo.CountByStatus(ctx, tenantID, string(domain.TaskStatusRunning))
	if err != nil {
		return nil, fmt.Errorf("count running tasks: %w", err)
	}
	queuedCount, err := s.repo.CountByStatus(ctx, tenantID, string(domain.TaskStatusQueued))
	if err != nil {
		return nil, fmt.Errorf("count queued tasks: %w", err)
	}
	maxConcurrent := s.cfg.MaxConcurrent
	if s.tenantRepo != nil {
		if tenant, err := s.tenantRepo.GetByID(ctx, tenantID); err == nil && tenant.MaxConcurrentTasks > 0 {
			maxConcurrent = tenant.MaxConcurrentTasks
		}
	}
	if runningCount+queuedCount >= maxConcurrent {
		return nil, fmt.Errorf("max concurrent tasks reached for tenant: %d/%d", runningCount+queuedCount, maxConcurrent)
	}

	// 预热：后台并行建好所有账号的连接，worker 认领时直接命中热连接，零等待
	if s.engine != nil {
		s.engine.WarmAccounts(context.Background(), tenantID, accountIDs)
	}

	// P2修复(精读发现): 采集/加群类任务下面取 accountIDs[0], 空数组会
	// index out of range panic(前面的 len(accounts)==len(accountIDs) 校验 0==0 可绕过)。
	if len(accountIDs) == 0 && (taskType == domain.TaskTypeScrapeMembers || taskType == domain.TaskTypeScrapeLinks ||
		taskType == domain.TaskTypeJoinGroups || taskType == domain.TaskTypeMonitorScrape || taskType == domain.TaskTypeInviteMembers) {
		return nil, fmt.Errorf("任务类型 %s 至少需要一个账号", taskType)
	}

	switch taskType {
	case domain.TaskTypeBroadcast:
		message, ok := payload["message"].(string)
		if !ok || message == "" {
			return nil, fmt.Errorf("payload missing or invalid message field")
		}
		// 重试时读取原任务的间隔设置与优先级（extraPayload 会被 merge 进 payload，
		// 之前传 nil 导致续发任务优先级归零、永远排在队尾）
		intervalMin, _ := payload["interval_min"].(float64)
		intervalMax, _ := payload["interval_max"].(float64)
		extra := map[string]any{}
		if p, ok := payload["priority"].(float64); ok && p >= 1 && p <= 10 {
			extra["priority"] = p
		}
		return s.CreateBroadcast(ctx, tenantID, accountIDs, targetIDs, message, scheduledAt, int(intervalMin), int(intervalMax), extra)
	case domain.TaskTypeScrapeMembers:
		groupIDStr, _ := payload["group_id"].(string)
		groupID, err := uuid.Parse(groupIDStr)
		if err != nil {
			return nil, fmt.Errorf("群组ID格式错误: %w", err)
		}
		return s.CreateScrapeMembers(ctx, tenantID, accountIDs[0], groupID, nil)
	case domain.TaskTypeScrapeLinks:
		groupIDStr, _ := payload["group_id"].(string)
		groupID, err := uuid.Parse(groupIDStr)
		if err != nil {
			return nil, fmt.Errorf("群组ID格式错误: %w", err)
		}
		return s.CreateScrapeLinks(ctx, tenantID, accountIDs[0], []string{groupID.String()}, 0)
	case domain.TaskTypeJoinGroups:
		links := targetIDs
		return s.CreateJoinGroups(ctx, tenantID, accountIDs[0], links)
	case domain.TaskTypeMonitorScrape:
		groupIDStr, _ := payload["group_id"].(string)
		groupID, err := uuid.Parse(groupIDStr)
		if err != nil {
			return nil, fmt.Errorf("群组ID格式错误: %w", err)
		}
		matchType, _ := payload["match_type"].(string)
		matchContent, _ := payload["match_content"].(string)
		durationMinutes, _ := payload["duration_minutes"].(float64)
		maxResults, _ := payload["max_results"].(float64)
		return s.CreateMonitorScrape(ctx, tenantID, accountIDs[0], groupID, matchType, matchContent, int(durationMinutes), int(maxResults))
	case domain.TaskTypeInviteMembers:
		tgStr, _ := payload["target_group_id"].(string)
		sgStr, _ := payload["source_group_id"].(string)
		tgID, err := uuid.Parse(tgStr)
		if err != nil {
			return nil, fmt.Errorf("目标群ID格式错误: %w", err)
		}
		sgID, err := uuid.Parse(sgStr)
		if err != nil {
			return nil, fmt.Errorf("来源群ID格式错误: %w", err)
		}
		return s.CreateInviteMembers(ctx, tenantID, accountIDs[0], tgID, sgID)
	default:
		return nil, fmt.Errorf("unsupported task type: %s", taskType)
	}
}

// CreateBroadcast creates a broadcast task.
// Supports two modes:
//   - cartesian: all accounts send to all targets (legacy, default)
//   - mapping: each account sends only to its assigned targets (precise control)
func (s *TaskService) CreateBroadcast(ctx context.Context, tenantID uuid.UUID, accountIDs []uuid.UUID, targetIDs []string, message string, scheduledAt *time.Time, intervalMin, intervalMax int, extraPayload map[string]any) (*domain.Task, error) {
	return s.CreateBroadcastWithMapping(ctx, tenantID, nil, accountIDs, targetIDs, message, scheduledAt, intervalMin, intervalMax, extraPayload)
}

// CreateScheduledBroadcast creates a broadcast task with loop scheduling.
// First run happens immediately; afterwards the engine requeues it every
// loopIntervalMinutes until it is paused or cancelled.
func (s *TaskService) CreateScheduledBroadcast(ctx context.Context, tenantID uuid.UUID, accountIDs []uuid.UUID, targetIDs []string, message string, loopIntervalMinutes, intervalMin, intervalMax int, isLoop bool) (*domain.Task, error) {
	now := time.Now()
	task, err := s.CreateBroadcast(ctx, tenantID, accountIDs, targetIDs, message, &now, intervalMin, intervalMax, nil)
	if err != nil {
		return nil, err
	}
	if err := s.repo.SetLoopConfig(ctx, tenantID, task.TaskID, isLoop, loopIntervalMinutes, intervalMin, intervalMax, domain.TaskTypeScheduledBroadcast); err != nil {
		return nil, fmt.Errorf("set loop config: %w", err)
	}
	task.IsLoop = isLoop
	task.LoopIntervalMinutes = loopIntervalMinutes
	task.Type = domain.TaskTypeScheduledBroadcast
	return task, nil
}

// CreateBroadcastWithMapping creates a broadcast task with account-target mapping.
// If mappings is nil, falls back to cartesian mode (all accounts × all targets).
func (s *TaskService) CreateBroadcastWithMapping(ctx context.Context, tenantID uuid.UUID, mappings []domain.AccountTargetMapping, accountIDs []uuid.UUID, targetIDs []string, message string, scheduledAt *time.Time, intervalMin, intervalMax int, extraPayload map[string]any) (*domain.Task, error) {
	mode := domain.BroadcastModeCartesian
	var totalMsgs int

	// P1-44 审计修复: 空账号/空目标不报错——任务创建后空转一圈标 completed, 浪费额度且误导。
	if len(accountIDs) == 0 && (mappings == nil || len(mappings) == 0) {
		return nil, fmt.Errorf("未选择发送账号")
	}
	if len(targetIDs) == 0 && (mappings == nil || len(mappings) == 0) {
		return nil, fmt.Errorf("未选择发送目标")
	}

	if mappings != nil && len(mappings) > 0 {
		// Limit: max 500 mappings per task (prevents oversized payloads)
		const maxMappings = 500
		if len(mappings) > maxMappings {
			return nil, fmt.Errorf("mappings exceeds limit: %d > %d", len(mappings), maxMappings)
		}

		// Deduplicate accounts in mappings (keep first occurrence)
		seen := make(map[uuid.UUID]bool)
		deduped := make([]domain.AccountTargetMapping, 0, len(mappings))
		for _, m := range mappings {
			if seen[m.AccountID] {
				continue
			}
			seen[m.AccountID] = true
			// Also deduplicate targets within each mapping
			if len(m.Targets) > 0 {
				targetSeen := make(map[string]bool)
				targetDeduped := make([]string, 0, len(m.Targets))
				for _, t := range m.Targets {
					if targetSeen[t] {
						continue
					}
					targetSeen[t] = true
					targetDeduped = append(targetDeduped, t)
				}
				m.Targets = targetDeduped
			}
			deduped = append(deduped, m)
		}
		mappings = deduped
		// Mapping mode: validate and count
		mode = domain.BroadcastModeMapping
		allAccIDs := make([]uuid.UUID, 0, len(mappings))
		allTargetIDs := make([]string, 0)
		for _, m := range mappings {
			allAccIDs = append(allAccIDs, m.AccountID)
			allTargetIDs = append(allTargetIDs, m.Targets...)
			totalMsgs += len(m.Targets)
		}
		if err := s.validateAccountOwnership(ctx, tenantID, allAccIDs); err != nil {
			return nil, err
		}
		accountIDs = allAccIDs
		targetIDs = allTargetIDs
	} else {
		// Cartesian mode
		if err := s.validateAccountOwnership(ctx, tenantID, accountIDs); err != nil {
			return nil, err
		}
		totalMsgs = len(targetIDs) * len(accountIDs)
	}

	// Quota check removed — quotas are now for display only, not enforcement
	payload := map[string]any{
		"message":      message,
		"mode":         string(mode),
		"interval_min": intervalMin,
		"interval_max": intervalMax,
	}
	// 合并前端传入的扩展字段(富媒体/URL按钮等)
	for k, v := range extraPayload {
		payload[k] = v
	}
	if mode == domain.BroadcastModeMapping {
		// Store mappings as JSON-compatible structure
		mappingsData := make([]map[string]any, len(mappings))
		for i, m := range mappings {
			mappingsData[i] = map[string]any{
				"account_id": m.AccountID.String(),
				"targets":    m.Targets,
			}
		}
		payload["mappings"] = mappingsData
	}

	// 套餐日额度(051): 免费体验版限 50 条/天, 原子计数防并发超发; 付费档 0=不限
	if s.planLimits != nil {
		if dailyCap, _, err := s.planLimits.GetPlanLimits(ctx, tenantID); err == nil && dailyCap > 0 {
			if used, err := s.planLimits.AddDailyUsage(ctx, tenantID, repository.KindBroadcastMsgs, totalMsgs); err == nil && used > dailyCap {
				return nil, fmt.Errorf("今日群发额度已用完(体验版 %d 条/天, 已用 %d), 升级套餐不限量", dailyCap, used)
			}
		}
	}

	// Auto-split: if total messages exceed threshold, split into multiple sub-tasks
	maxMsgsPerTask := s.cfg.MaxMsgsPerTask
	if mode == domain.BroadcastModeMapping && totalMsgs > maxMsgsPerTask {
		return s.createSplitBroadcast(ctx, tenantID, mappings, message, scheduledAt, totalMsgs, maxMsgsPerTask, intervalMin, intervalMax, extraPayload)
	}

	// 优先级: 默认 5, 前端可传 1-10(8=优先插队, ClaimTasks 按 priority DESC 排序)
	priority := 5
	if p, ok := extraPayload["priority"].(float64); ok && p >= 1 && p <= 10 {
		priority = int(p)
	}
	task := &domain.Task{
		TaskID:      uuid.New(),
		TenantID:    tenantID,
		Type:        domain.TaskTypeBroadcast,
		Status:      domain.TaskStatusQueued,
		Priority:    priority,
		AccountIDs:  accountIDs,
		TargetIDs:   targetIDs,
		Payload:     payload,
		Progress:    domain.TaskProgress{Total: totalMsgs},
		ScheduledAt: scheduledAt,
		CreatedAt:   time.Now(),
	}
	if scheduledAt == nil || scheduledAt.Before(time.Now()) {
		task.Status = domain.TaskStatusQueued
	} else {
		task.Status = domain.TaskStatusPending
	}
	if err := s.repo.Create(ctx, task); err != nil {
		return nil, fmt.Errorf("创建群发任务失败: %w", err)
	}
	return task, nil
}

// createSplitBroadcast splits a large broadcast into multiple sub-tasks.
func (s *TaskService) createSplitBroadcast(ctx context.Context, tenantID uuid.UUID, mappings []domain.AccountTargetMapping, message string, scheduledAt *time.Time, totalMsgs, maxMsgsPerTask, intervalMin, intervalMax int, extraPayload map[string]any) (*domain.Task, error) {
	// 2026-10-22 审计修复: maxMsgsPerTask<=0 时 splitMappings 会死循环/除零——入口护栏
	if maxMsgsPerTask < 1 {
		maxMsgsPerTask = 1
	}
	// Split mappings into chunks where each chunk has ≤ maxMsgsPerTask messages
	chunks := splitMappings(mappings, maxMsgsPerTask)

	batch := &domain.BatchTask{
		BatchID:   uuid.New(),
		TenantID:  tenantID,
		Message:   message,
		TotalMsgs: totalMsgs,
		SubTasks:  make([]uuid.UUID, 0, len(chunks)),
		CreatedAt: time.Now(),
	}

	// Create batch record
	if s.batchRepo != nil {
		if err := s.batchRepo.Create(ctx, batch); err != nil {
			log.Printf("[batch] failed to create batch record: %v", err)
		}
	}

	// Create sub-tasks
	for i, chunk := range chunks {
		chunkMsgs := 0
		for _, m := range chunk {
			chunkMsgs += len(m.Targets)
		}

		payload := map[string]any{
			"message":      message,
			"mode":         string(domain.BroadcastModeMapping),
			"mappings":     mappingsToPayload(chunk),
			"batch_id":     batch.BatchID.String(),
			"batch_index":  i + 1,
			"batch_total":  len(chunks),
			"interval_min": intervalMin,
			"interval_max": intervalMax,
		}
		for k, v := range extraPayload {
			payload[k] = v
		}

		var accIDs []uuid.UUID
		var allTargets []string
		for _, m := range chunk {
			accIDs = append(accIDs, m.AccountID)
			allTargets = append(allTargets, m.Targets...)
		}

		task := &domain.Task{
			TaskID:      uuid.New(),
			TenantID:    tenantID,
			Type:        domain.TaskTypeBroadcast,
			Status:      domain.TaskStatusQueued,
			Priority:    5,
			AccountIDs:  accIDs,
			TargetIDs:   allTargets,
			Payload:     payload,
			Progress:    domain.TaskProgress{Total: chunkMsgs},
			ScheduledAt: scheduledAt,
		}
		if scheduledAt == nil || scheduledAt.Before(time.Now()) {
			task.Status = domain.TaskStatusQueued
		} else {
			task.Status = domain.TaskStatusPending
		}
		if err := s.repo.Create(ctx, task); err != nil {
			return nil, fmt.Errorf("create sub-task %d/%d: %w", i+1, len(chunks), err)
		}
		batch.SubTasks = append(batch.SubTasks, task.TaskID)
		if s.batchRepo != nil {
			if err := s.batchRepo.UpdateSubTask(ctx, tenantID, batch.BatchID, task.TaskID); err != nil {
				log.Printf("[task] 更新子任务失败: %v", err)
			}
		}
	}

	// Notify tenant via SSE（走租户隔离广播， previously 全局 Broadcast 发到无人订阅的 topic）
	if s.sseBroker != nil {
		s.sseBroker.BroadcastToTenant(tenantID, "task_split", map[string]any{
			"batch_id":     batch.BatchID,
			"total_msgs":   totalMsgs,
			"sub_tasks":    len(chunks),
			"max_per_task": maxMsgsPerTask,
			"message":      fmt.Sprintf("任务包含 %d 条消息，已自动拆分为 %d 个子任务并行执行", totalMsgs, len(chunks)),
			"time":         time.Now().UTC(),
		})
	}

	log.Printf("[batch] created batch %s with %d sub-tasks (%d total msgs)", batch.BatchID, len(chunks), totalMsgs)

	if len(batch.SubTasks) == 0 {
		return nil, fmt.Errorf("no valid sub-tasks created (all chunks empty)")
	}

	// Return the first sub-task as the "main" task (frontend can poll all via batch_id)
	return s.repo.GetByID(ctx, tenantID, batch.SubTasks[0])
}

// splitMappings splits account-target mappings into chunks where each chunk has ≤ maxMsgs messages.
func splitMappings(mappings []domain.AccountTargetMapping, maxMsgs int) [][]domain.AccountTargetMapping {
	var chunks [][]domain.AccountTargetMapping
	var current []domain.AccountTargetMapping
	currentMsgs := 0

	for _, m := range mappings {
		// If adding this mapping would exceed limit, start a new chunk
		// (but always include at least one mapping per chunk)
		if currentMsgs > 0 && currentMsgs+len(m.Targets) > maxMsgs {
			chunks = append(chunks, current)
			current = nil
			currentMsgs = 0
		}
		current = append(current, m)
		currentMsgs += len(m.Targets)
	}
	if len(current) > 0 {
		chunks = append(chunks, current)
	}
	return chunks
}

// mappingsToPayload converts mappings to JSON-compatible payload format.
func mappingsToPayload(mappings []domain.AccountTargetMapping) []map[string]any {
	result := make([]map[string]any, len(mappings))
	for i, m := range mappings {
		result[i] = map[string]any{
			"account_id": m.AccountID.String(),
			"targets":    m.Targets,
		}
	}
	return result
}

// CreateScrapeMembers creates a scrape members task.
func (s *TaskService) CreateScrapeMembers(ctx context.Context, tenantID uuid.UUID, accountID uuid.UUID, groupID uuid.UUID, extraPayload map[string]any) (*domain.Task, error) {
	if err := s.validateAccountOwnership(ctx, tenantID, []uuid.UUID{accountID}); err != nil {
		return nil, err
	}

	// Resolve group to get Telegram identifier (TGChatID or username)
	group, err := s.groupRepo.GetByID(ctx, tenantID, groupID)
	if err != nil {
		return nil, fmt.Errorf("group not found: %w", err)
	}

	// Use username as target if available, otherwise use TGChatID
	targetID := group.Username
	if targetID == "" {
		targetID = fmt.Sprintf("%d", group.TGChatID)
	}

	task := &domain.Task{
		TaskID:     uuid.New(),
		TenantID:   tenantID,
		Type:       domain.TaskTypeScrapeMembers,
		Status:     domain.TaskStatusQueued,
		Priority:   3,
		AccountIDs: []uuid.UUID{accountID},
		TargetIDs:  []string{targetID},
		Payload: map[string]any{
			"group_id":    groupID.String(),
			"fallback_id": fmt.Sprintf("%d", group.TGChatID),
		},
		CreatedAt: time.Now(),
	}
	if err := s.repo.Create(ctx, task); err != nil {
		return nil, fmt.Errorf("create scrape task: %w", err)
	}
	return task, nil
}

// CreateJoinGroups creates a join groups task.
func (s *TaskService) CreateJoinGroups(ctx context.Context, tenantID uuid.UUID, accountID uuid.UUID, links []string) (*domain.Task, error) {
	return s.CreateJoinGroupsWithInterval(ctx, tenantID, accountID, links, 0, 0)
}

// CreateJoinGroupsWithInterval 创建加群任务并允许租户指定随机间隔区间(分钟)。
// intervalMin/intervalMax 均 >0 时生效, 执行器每次在 [min,max] 内随机取值(反风控);
// 否则退回全局 JOIN_INTERVAL_MINUTES(默认2分钟) + 0~50% 抖动。
// 合法性: 1 ≤ min ≤ 60, min ≤ max ≤ 120。
func (s *TaskService) CreateJoinGroupsWithInterval(ctx context.Context, tenantID uuid.UUID, accountID uuid.UUID, links []string, intervalMin, intervalMax int) (*domain.Task, error) {
	if err := s.validateAccountOwnership(ctx, tenantID, []uuid.UUID{accountID}); err != nil {
		return nil, err
	}
	// 2026-09-22 服务端兜底：空链接直接拒绝，杜绝零目标任务
	// （前端校验宽松化之前，用户填了但格式被滤掉时会建出空任务，日志实证 target_ids=[]）
	if len(links) == 0 {
		return nil, fmt.Errorf("加群链接不能为空")
	}

	// 间隔合法性兜底(服务端强制, 防绕过前端直接调 API 填 0 或超大值)
	payload := map[string]any{}
	if intervalMin > 0 || intervalMax > 0 {
		if intervalMin < 1 {
			intervalMin = 1
		}
		if intervalMin > 60 {
			intervalMin = 60
		}
		if intervalMax < intervalMin {
			intervalMax = intervalMin
		}
		if intervalMax > 120 {
			intervalMax = 120
		}
		payload["interval_min_minutes"] = intervalMin
		payload["interval_max_minutes"] = intervalMax
	}

	task := &domain.Task{
		TaskID:     uuid.New(),
		TenantID:   tenantID,
		Type:       domain.TaskTypeJoinGroups,
		Status:     domain.TaskStatusQueued,
		Priority:   4,
		AccountIDs: []uuid.UUID{accountID},
		TargetIDs:  links,
		Payload:    payload,
		Progress:   domain.TaskProgress{Total: len(links)},
		CreatedAt:  time.Now(),
	}
	if err := s.repo.Create(ctx, task); err != nil {
		return nil, fmt.Errorf("create join task: %w", err)
	}
	return task, nil
}

// CreateInviteMembers 邀请入群: 把来源群已采集成员批量邀请进本账号的目标群。
func (s *TaskService) CreateInviteMembers(ctx context.Context, tenantID, accountID, targetGroupID, sourceGroupID uuid.UUID) (*domain.Task, error) {
	if err := s.validateAccountOwnership(ctx, tenantID, []uuid.UUID{accountID}); err != nil {
		return nil, err
	}
	// 目标群必须是本账号已加入的群
	target, err := s.groupRepo.GetByID(ctx, tenantID, targetGroupID)
	if err != nil || target == nil {
		return nil, fmt.Errorf("目标群不存在")
	}
	if !target.IsJoined {
		return nil, fmt.Errorf("目标群尚未加入, 请先把账号拉进该群")
	}
	// 来源群须存在(成员在执行器里再取)
	if _, err := s.groupRepo.GetByID(ctx, tenantID, sourceGroupID); err != nil {
		return nil, fmt.Errorf("来源群不存在")
	}

	task := &domain.Task{
		TaskID:     uuid.New(),
		TenantID:   tenantID,
		Type:       domain.TaskTypeInviteMembers,
		Status:     domain.TaskStatusQueued,
		Priority:   4,
		AccountIDs: []uuid.UUID{accountID},
		Payload: map[string]any{
			"target_group_id": targetGroupID.String(),
			"source_group_id": sourceGroupID.String(),
			"target_title":    target.Title,
		},
		Progress:  domain.TaskProgress{Total: 0}, // 执行器加载成员后回填
		CreatedAt: time.Now(),
	}
	if err := s.repo.Create(ctx, task); err != nil {
		return nil, fmt.Errorf("create invite task: %w", err)
	}
	return task, nil
}

// CreateScrapeLinks creates a scrape links task.
func (s *TaskService) CreateScrapeLinks(ctx context.Context, tenantID uuid.UUID, accountID uuid.UUID, _ []string, durationMinutes int) (*domain.Task, error) {
	if err := s.validateAccountOwnership(ctx, tenantID, []uuid.UUID{accountID}); err != nil {
		return nil, err
	}

	// 采集时长写入 payload, 供 executeScrapeLinksReport 读取;
	// 不写则该函数内部默认 10 分钟。
	payload := map[string]any{}
	if durationMinutes > 0 {
		payload["duration_minutes"] = durationMinutes
	}

	task := &domain.Task{
		TaskID:     uuid.New(),
		TenantID:   tenantID,
		Type:       domain.TaskTypeScrapeLinks,
		Status:     domain.TaskStatusQueued,
		Priority:   3,
		AccountIDs: []uuid.UUID{accountID},
		TargetIDs:  []string{},
		Payload:    payload,
		Progress:   domain.TaskProgress{Total: 0, Done: 0},
		CreatedAt:  time.Now(),
	}
	if err := s.repo.Create(ctx, task); err != nil {
		return nil, fmt.Errorf("创建链接采集任务失败: %w", err)
	}
	return task, nil
}

// validTransitions defines allowed task status transitions.
var validTransitions = map[domain.TaskStatus]map[domain.TaskStatus]bool{
	domain.TaskStatusQueued:    {domain.TaskStatusPaused: true, domain.TaskStatusCancelled: true},
	domain.TaskStatusRunning:   {domain.TaskStatusPaused: true, domain.TaskStatusCancelled: true},
	domain.TaskStatusPaused:    {domain.TaskStatusQueued: true, domain.TaskStatusCancelled: true},
	domain.TaskStatusFailed:    {domain.TaskStatusQueued: true},
	domain.TaskStatusCompleted: {},
	domain.TaskStatusCancelled: {},
	domain.TaskStatusPending:   {domain.TaskStatusQueued: true, domain.TaskStatusCancelled: true},
	domain.TaskStatusScheduled: {domain.TaskStatusPaused: true, domain.TaskStatusCancelled: true, domain.TaskStatusQueued: true},
}

// Pause pauses a task.
func (s *TaskService) Pause(ctx context.Context, tenantID, taskID uuid.UUID) error {
	task, err := s.repo.GetByID(ctx, tenantID, taskID)
	if err != nil {
		return fmt.Errorf("获取任务失败: %w", err)
	}
	// 循环任务在非 running 状态(排队/等待下一轮)时, 停止=关循环+暂停
	if task.IsLoop && task.Status != domain.TaskStatusRunning {
		return s.repo.StopLoop(ctx, tenantID, taskID)
	}
	if !validTransitions[task.Status][domain.TaskStatusPaused] {
		return fmt.Errorf("cannot pause task with status %s", task.Status)
	}
	return s.repo.UpdateStatus(ctx, tenantID, taskID, domain.TaskStatusPaused)
}

// Resume resumes a paused task.
func (s *TaskService) Resume(ctx context.Context, tenantID, taskID uuid.UUID) error {
	task, err := s.repo.GetByID(ctx, tenantID, taskID)
	if err != nil {
		return fmt.Errorf("获取任务失败: %w", err)
	}
	if !validTransitions[task.Status][domain.TaskStatusQueued] {
		return fmt.Errorf("cannot resume task with status %s", task.Status)
	}
	return s.repo.ResetToQueued(ctx, tenantID, taskID)
}

// Cancel cancels a task (signals the engine to stop execution, then updates DB).
func (s *TaskService) Cancel(ctx context.Context, tenantID, taskID uuid.UUID) error {
	task, err := s.repo.GetByID(ctx, tenantID, taskID)
	if err != nil {
		return fmt.Errorf("获取任务失败: %w", err)
	}
	if !validTransitions[task.Status][domain.TaskStatusCancelled] {
		return fmt.Errorf("cannot cancel task with status %s", task.Status)
	}
	if s.engine != nil {
		s.engine.CancelTask(taskID)
	}
	return s.repo.UpdateStatus(ctx, tenantID, taskID, domain.TaskStatusCancelled)
}

// Retry retries a failed task.
// Retry 重试任务。scope 为空: 原任务重置回队列(全部目标重跑);
// scope="failed": 断点续发——仅对日志中失败的目标克隆一个新任务, 已成功的目标不会收到重复消息。
func (s *TaskService) Retry(ctx context.Context, tenantID, taskID uuid.UUID, scope string) error {
	task, err := s.repo.GetByID(ctx, tenantID, taskID)
	if err != nil {
		return fmt.Errorf("获取任务失败: %w", err)
	}
	if scope == "failed" {
		failed, err := s.logRepo.ListFailedTargets(ctx, tenantID, taskID)
		if err != nil {
			return fmt.Errorf("获取失败目标失败: %w", err)
		}
		if len(failed) == 0 {
			return fmt.Errorf("该任务没有失败目标, 无需续发")
		}
		// P1-15 审计修复: mapping 模式的续发任务必须保留原定向映射。
		// 原来直接走 Create → CreateBroadcast(mapping=nil) → cartesian,
		// 把"账号A只发群1"退化成"所有账号×所有失败目标", 既发错群又浪费额度。
		// 现在从原 payload 取出 mappings, 按失败目标过滤后走 CreateBroadcastWithMapping。
		if raw, ok := task.Payload["mappings"].([]any); ok && len(raw) > 0 {
			failedSet := make(map[string]bool, len(failed))
			for _, t := range failed {
				failedSet[t] = true
			}
			message, _ := task.Payload["message"].(string)
			intervalMin, _ := task.Payload["interval_min"].(float64)
			intervalMax, _ := task.Payload["interval_max"].(float64)
			extra := map[string]any{}
			if p, ok := task.Payload["priority"].(float64); ok && p >= 1 && p <= 10 {
				extra["priority"] = p
			}
			var filtered []domain.AccountTargetMapping
			for _, item := range raw {
				m, ok := item.(map[string]any)
				if !ok {
					continue
				}
				accStr, _ := m["account_id"].(string)
				accID, err := uuid.Parse(accStr)
				if err != nil {
					continue
				}
				tlist, _ := m["targets"].([]any)
				var kept []string
				for _, t := range tlist {
					ts, _ := t.(string)
					if failedSet[ts] {
						kept = append(kept, ts)
					}
				}
				if len(kept) > 0 {
					filtered = append(filtered, domain.AccountTargetMapping{AccountID: accID, Targets: kept})
				}
			}
			if len(filtered) == 0 {
				return fmt.Errorf("失败目标不在原定向映射内，无法按定向续发")
			}
			if _, err := s.CreateBroadcastWithMapping(ctx, tenantID, filtered, nil, failed, message, nil, int(intervalMin), int(intervalMax), extra); err != nil {
				return fmt.Errorf("创建续发任务失败: %w", err)
			}
			return nil
		}
		// 优先级透传已在 Create 的 broadcast 分支内从 payload 读取，这里直接重建即可
		if _, err := s.Create(ctx, tenantID, task.Type, task.AccountIDs, failed, task.Payload, nil); err != nil {
			return fmt.Errorf("创建续发任务失败: %w", err)
		}
		return nil
	}
	if !validTransitions[task.Status][domain.TaskStatusQueued] {
		return fmt.Errorf("cannot retry task with status %s", task.Status)
	}
	return s.repo.ResetToQueued(ctx, tenantID, taskID)
}

// GetLogs returns task logs.
func (s *TaskService) GetLogs(ctx context.Context, tenantID, taskID uuid.UUID, page, pageSize int) ([]*domain.TaskLog, error) {
	if pageSize <= 0 {
		pageSize = 50
	}
	offset := (page - 1) * pageSize
	return s.logRepo.ListByTask(ctx, tenantID, taskID, pageSize, offset)
}

// GetLogsByAccount returns all task logs for a specific account (for independent log page).
func (s *TaskService) GetLogsByAccount(ctx context.Context, tenantID, accountID uuid.UUID, page, pageSize int) ([]*domain.TaskLog, int, error) {
	if pageSize <= 0 {
		pageSize = 50
	}
	return s.logRepo.ListByAccount(ctx, tenantID, accountID, "", page, pageSize)
}

// CountLogsByAccount returns total log count for an account.
func (s *TaskService) CountLogsByAccount(ctx context.Context, tenantID, accountID uuid.UUID) (int, error) {
	_, count, err := s.logRepo.ListByAccount(ctx, tenantID, accountID, "", 1, 1)
	return count, err
}

// GetByID returns a task by ID.
func (s *TaskService) GetByID(ctx context.Context, tenantID, taskID uuid.UUID) (*domain.Task, error) {
	return s.repo.GetByID(ctx, tenantID, taskID)
}

// Delete removes a task. Only terminal tasks (completed/failed/cancelled) can be deleted.
func (s *TaskService) Delete(ctx context.Context, tenantID, taskID uuid.UUID) error {
	task, err := s.repo.GetByID(ctx, tenantID, taskID)
	if err != nil {
		return fmt.Errorf("获取任务失败: %w", err)
	}
	// Prevent deletion of active tasks to avoid orphan worker processes
	if task.Status == domain.TaskStatusRunning || task.Status == domain.TaskStatusQueued {
		return fmt.Errorf("cannot delete task with status %s: stop or cancel first", task.Status)
	}
	return s.repo.Delete(ctx, tenantID, taskID)
}

// BatchCancel cancels multiple tasks.
func (s *TaskService) BatchCancel(ctx context.Context, tenantID uuid.UUID, taskIDs []uuid.UUID) (int, error) {
	cancelled := 0
	for _, taskID := range taskIDs {
		if err := s.Cancel(ctx, tenantID, taskID); err == nil {
			cancelled++
		}
	}
	return cancelled, nil
}

// BatchDelete deletes multiple terminal tasks.
// RaffleResultList 抽奖参与记录透传
func (s *TaskService) RaffleResultList(ctx context.Context, tenantID uuid.UUID) ([]map[string]interface{}, error) {
	return s.raffleResultRepo.List(ctx, tenantID, 50)
}

// LeaveAllGroups 一键退群透传(domain/task 引擎执行)
func (s *TaskService) LeaveAllGroups(ctx context.Context, tenantID, accountID uuid.UUID) (*task.LeaveAllResult, error) {
	return s.engine.LeaveAllGroups(ctx, tenantID, accountID)
}

// TaskLogStatsByAccount 单任务按账号聚合成功/失败/跳过(任务展开报表)
func (s *TaskService) TaskLogStatsByAccount(ctx context.Context, tenantID, taskID uuid.UUID) (map[string]map[string]int, error) {
	return s.logRepo.StatsByAccountForTask(ctx, tenantID, taskID)
}

func (s *TaskService) BatchDelete(ctx context.Context, tenantID uuid.UUID, taskIDs []uuid.UUID) (int, error) {
	deleted := 0
	for _, taskID := range taskIDs {
		if err := s.Delete(ctx, tenantID, taskID); err == nil {
			deleted++
		}
	}
	return deleted, nil
}

// Count returns total tasks for a tenant with optional account/type filtering.
func (s *TaskService) Count(ctx context.Context, tenantID uuid.UUID, status *domain.TaskStatus, accountID *uuid.UUID, taskType *domain.TaskType, since *time.Time) (int, error) {
	return s.repo.Count(ctx, tenantID, status, accountID, taskType, since)
}

// Update updates a pending or paused task.
func (s *TaskService) Update(ctx context.Context, tenantID, taskID uuid.UUID, accountIDs []uuid.UUID, targetIDs []string, payload map[string]any) error {
	// M-02: 任务编辑写入的账号必须都属于本租户
	if len(accountIDs) > 0 {
		owned, err := s.repo.CountOwnedAccounts(ctx, tenantID, accountIDs)
		if err != nil {
			return fmt.Errorf("校验账号归属失败: %w", err)
		}
		if owned != len(accountIDs) {
			return fmt.Errorf("存在不属于当前租户的账号, 已拒绝")
		}
	}

	task, err := s.repo.GetByID(ctx, tenantID, taskID)
	if err != nil {
		return fmt.Errorf("获取任务失败: %w", err)
	}
	if task.Status != domain.TaskStatusPending && task.Status != domain.TaskStatusPaused {
		return fmt.Errorf("cannot update task with status %s: only pending or paused tasks can be updated", task.Status)
	}
	return s.repo.Update(ctx, tenantID, taskID, accountIDs, targetIDs, payload)
}

// CreateMonitorScrape creates a monitor-based member scraping task.
func (s *TaskService) CreateMonitorScrape(ctx context.Context, tenantID, accountID, groupID uuid.UUID, matchType, matchContent string, durationMinutes, maxResults int) (*domain.Task, error) {
	if err := s.validateAccountOwnership(ctx, tenantID, []uuid.UUID{accountID}); err != nil {
		return nil, err
	}
	group, err := s.groupRepo.GetByID(ctx, tenantID, groupID)
	if err != nil {
		return nil, fmt.Errorf("group not found: %w", err)
	}
	targetID := group.Username
	if targetID == "" {
		targetID = fmt.Sprintf("%d", group.TGChatID)
	}
	if matchType == "" {
		matchType = "contains"
	}
	if durationMinutes <= 0 {
		durationMinutes = 30
	}
	task := &domain.Task{
		TaskID:     uuid.New(),
		TenantID:   tenantID,
		Type:       domain.TaskTypeMonitorScrape,
		Status:     domain.TaskStatusQueued,
		Priority:   3,
		AccountIDs: []uuid.UUID{accountID},
		TargetIDs:  []string{targetID},
		Payload: map[string]any{
			"group_id":         groupID.String(),
			"match_type":       matchType,
			"match_content":    matchContent,
			"duration_minutes": durationMinutes,
			"max_results":      maxResults,
		},
		CreatedAt: time.Now(),
	}
	if err := s.repo.Create(ctx, task); err != nil {
		return nil, fmt.Errorf("create monitor scrape task: %w", err)
	}
	return task, nil
}

// SetPlanLimitRepo 注入套餐日额度 repo(免费体验版限额用, container 接线)
func (s *TaskService) SetPlanLimitRepo(r *repository.PlanLimitRepository) {
	s.planLimits = r
}

// SetPlanRepo 注入套餐 repo(任务创建按套餐功能服务端强制门禁用, container 接线)
func (s *TaskService) SetPlanRepo(r *repository.PlanRepository) {
	s.planRepo = r
}
