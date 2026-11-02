package service

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"tg-sender/internal/domain"
	"tg-sender/internal/repository"
)

// DashboardService handles dashboard business logic.
type DashboardService struct {
	accountRepo  *repository.AccountRepository
	taskRepo     *repository.TaskRepository
	groupRepo    *repository.GroupRepository
	riskRepo     *repository.RiskRepository
	auditRepo    *repository.AuditRepository
	taskLogRepo  *repository.TaskLogRepository
}

// NewDashboardService creates a new dashboard service.
func NewDashboardService(accountRepo *repository.AccountRepository, taskRepo *repository.TaskRepository, groupRepo *repository.GroupRepository, riskRepo *repository.RiskRepository, auditRepo *repository.AuditRepository, taskLogRepo *repository.TaskLogRepository) *DashboardService {
	return &DashboardService{accountRepo: accountRepo, taskRepo: taskRepo, groupRepo: groupRepo, riskRepo: riskRepo, auditRepo: auditRepo, taskLogRepo: taskLogRepo}
}

// SendCalendar 发送日历: 按天统计成功发送量(近 N 个月), 供月历热力图展示
func (s *DashboardService) SendCalendar(ctx context.Context, tenantID uuid.UUID, months int) (map[string]int, error) {
	if months <= 0 || months > 12 {
		months = 6 // 默认近半年
	}
	// 2026-10-22 审计修复: from/to 原来用服务器本地时区构造, 与 SQL 分组用的
	// Asia/Shanghai 不一致(跨月边界丢 8 小时); 统一用上海时区, 与分组口径对齐
	now := time.Now()
	shanghai, _ := time.LoadLocation("Asia/Shanghai")
	nowSH := now.In(shanghai)
	from := time.Date(nowSH.Year(), nowSH.Month()-time.Month(months-1), 1, 0, 0, 0, 0, shanghai)
	return s.taskLogRepo.CountSuccessByDay(ctx, tenantID, from, nowSH.AddDate(0, 0, 1))
}

// Stats returns aggregated dashboard statistics.
func (s *DashboardService) Stats(ctx context.Context, tenantID uuid.UUID) (map[string]any, error) {
	return s.stats(ctx, tenantID, nil)
}

// StatsForAccount 仪表盘只看指定账号的数据(账号切换跟随)
func (s *DashboardService) StatsForAccount(ctx context.Context, tenantID, accountID uuid.UUID) (map[string]any, error) {
	return s.stats(ctx, tenantID, &accountID)
}

func (s *DashboardService) stats(ctx context.Context, tenantID uuid.UUID, accountID *uuid.UUID) (map[string]any, error) {
	// 账号维度: 走独立的账号统计查询
	if accountID != nil {
		acctStats, err := s.taskRepo.AccountStats(ctx, tenantID, *accountID)
		if err != nil {
			return nil, err
		}
		// 键名必须与租户分支及前端完全一致, 否则前端读到 undefined 显示全 0
		today := 0
		if v, ok := acctStats["completed_today"].(int); ok {
			today = v
		}
		yesterday := 0
		if v, ok := acctStats["completed_yesterday"].(int); ok {
			yesterday = v
		}
		// 在线账号与分布保持租户口径(账号 0/1 无意义, 用户明确要求)
		totalAccounts, onlineAccounts, err := s.accountRepo.CountStats(ctx, tenantID)
		if err != nil {
			totalAccounts, onlineAccounts = 0, 0
		}
		return map[string]any{
			"online_accounts":       onlineAccounts,
			"completed_tasks_today": today,
			"total_groups":          acctStats["total_groups"],
			"risk_events_24h":       acctStats["risks_24h"],
			"task_trend":            acctStats["task_trend"],
			"account_distribution": []map[string]any{
				{"value": onlineAccounts, "name": "在线"},
				{"value": totalAccounts - onlineAccounts, "name": "离线"},
			},
			"trends": map[string]any{
				"online_accounts": "+0",
				"task_trend":      formatTrend(today, yesterday),
				"risk_events_24h": "+0",
			},
		}, nil
	}
	_, onlineAccounts, err := s.accountRepo.CountStats(ctx, tenantID)
	if err != nil {
		return nil, err
	}

	completedToday, err := s.taskRepo.CountCompletedToday(ctx, tenantID)
	if err != nil {
		return nil, err
	}

	completedYesterday, err := s.taskRepo.CountCompletedYesterday(ctx, tenantID)
	if err != nil {
		return nil, err
	}

	totalGroups, err := s.groupRepo.CountByTenant(ctx, tenantID)
	if err != nil {
		return nil, err
	}

	risks24h, err := s.riskRepo.Count24h(ctx, tenantID)
	if err != nil {
		return nil, err
	}

	risks24hYesterday, err := s.riskRepo.Count24hYesterday(ctx, tenantID)
	if err != nil {
		return nil, err
	}

	onlineYesterday, err := s.accountRepo.CountOnlineYesterday(ctx, tenantID)
	if err != nil {
		return nil, err
	}

	// Get task trend for last 7 days (real data from DB)
	taskTrend, err := s.taskRepo.CountCompletedLast7Days(ctx, tenantID)
	if err != nil {
		taskTrend = []int{0, 0, 0, 0, 0, 0, 0}
	}

	// Get total accounts for accurate offline count
	totalAccounts, _, err := s.accountRepo.CountStats(ctx, tenantID)
	offlineAccounts := 0
	if err == nil {
		offlineAccounts = totalAccounts - onlineAccounts
	}
	accountDist := []map[string]any{
		{"value": onlineAccounts, "name": "在线"},
		{"value": offlineAccounts, "name": "离线"},
	}

	return map[string]any{
		"online_accounts":       onlineAccounts,
		"completed_tasks_today": completedToday,
		"total_groups":          totalGroups,
		"risk_events_24h":       risks24h,
		"task_trend":            taskTrend,
		"account_distribution":  accountDist,
		"trends": map[string]any{
			"online_accounts": formatTrend(onlineAccounts, onlineYesterday),
			"task_trend":      formatTrend(completedToday, completedYesterday),
			"risk_events_24h": formatTrend(risks24h, risks24hYesterday),
		},
	}, nil
}

func formatTrend(current, previous int) string {
	// P2-81 审计修复: 原来 diff==0 返回 "0"——前端按负值着色显示"跌 0"。
	// 0 也算持平, 带 + 号走中性/正向色。
	diff := current - previous
	return fmt.Sprintf("%+d", diff)
}

// validateTimeParam 校验可选时间过滤参数(V20/P1-42 审计修复):
// 非法格式原来直通 PG 触发 500; 现在返回明确错误, handler 侧映射为 400。
func validateTimeParam(v string) (string, error) {
	if v == "" {
		return "", nil
	}
	if _, err := time.Parse(time.RFC3339, v); err == nil {
		return v, nil
	}
	if _, err := time.Parse("2006-01-02", v); err == nil {
		return v, nil
	}
	return "", fmt.Errorf("invalid time format %q: use YYYY-MM-DD or RFC3339", v)
}

// RiskEvents returns risk events for a tenant with optional time range filter.
func (s *DashboardService) RiskEvents(ctx context.Context, tenantID uuid.UUID, limit, offset int, startTime, endTime string) ([]*domain.RiskEvent, error) {
	st, err := validateTimeParam(startTime)
	if err != nil {
		return nil, err
	}
	et, err := validateTimeParam(endTime)
	if err != nil {
		return nil, err
	}
	return s.riskRepo.ListByTenant(ctx, tenantID, limit, offset, st, et)
}

// AuditLogs returns audit logs for a tenant with optional time range filter.
func (s *DashboardService) AuditLogs(ctx context.Context, tenantID uuid.UUID, limit, offset int, startTime, endTime string) ([]*domain.AuditLog, error) {
	st, err := validateTimeParam(startTime)
	if err != nil {
		return nil, err
	}
	et, err := validateTimeParam(endTime)
	if err != nil {
		return nil, err
	}
	return s.auditRepo.List(ctx, tenantID, limit, offset, st, et)
}
