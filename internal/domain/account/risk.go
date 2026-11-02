package account

import (
	"context"
	"log"
	"regexp"
	"strconv"
	"time"

	"tg-sender/internal/domain"

	"github.com/google/uuid"
)

var floodWaitRe = regexp.MustCompile(`(?i)flood_wait_(\d+)`)

func (e *Engine) recordRiskEvent(ctx context.Context, tenantID, accountID uuid.UUID, eventType, severity, detail string) {
	if e.riskRepo == nil {
		log.Printf("[RISK] %s | 租户=%s 账号=%s | %s\n", eventType, tenantID, accountID, detail)
		return
	}
	if err := e.riskRepo.Create(ctx, &domain.RiskEvent{
		EventID:   uuid.New(), // 不设则零值UUID, 第二次插入必撞主键
		TenantID:  tenantID,
		AccountID: accountID,
		EventType: eventType,
		Severity:  severity,
		Details:   map[string]any{"detail": detail},
	}); err != nil {
		log.Printf("[RISK] 记录账号 %s 的事件 %s 失败: %v\n", eventType, accountID, err)
	}
}

func extractFloodSeconds(errStr string) int {
	// P2-55 核实: floodWaitRe 本身带 (?i) 不区分大小写, 此函数无大小写漏洞。
	matches := floodWaitRe.FindStringSubmatch(errStr)
	if len(matches) > 1 {
		if sec, err := strconv.Atoi(matches[1]); err == nil {
			return sec
		}
	}
	return 0
}

func (e *Engine) markAccountRestricted(ctx context.Context, tenantID, accountID uuid.UUID, reason string, detail string) {
	e.recordRiskEvent(ctx, tenantID, accountID, "account_restricted", "high", reason+": "+detail)
	// P1-20 审计修复: 状态+封禁截止时间必须同一条 UPDATE 原子完成。
	// 原来分两次写, 第二步失败时账号已 restricted 但无 blocked_until——
	// unblockExpiredLoop 依赖 blocked_until 恢复, 缺失即永久封禁。
	// SSE 广播(notifyAccountStatus)放在写库成功后, 与 updateStatus 的分层一致:
	// 广播可重试可丢失, 状态数据不允许中间态。
	blockedUntil := time.Now().UTC().Add(24 * time.Hour)
	if err := e.repo.UpdateStatusAndBlockedUntil(ctx, tenantID, accountID, domain.AccountStatusRestricted, blockedUntil); err != nil {
		log.Printf("[risk] 标记账号受限时原子更新状态+封禁时间错误: %v", err)
		return
	}
	e.notifyAccountStatus(tenantID, accountID, domain.AccountStatusRestricted)
}
