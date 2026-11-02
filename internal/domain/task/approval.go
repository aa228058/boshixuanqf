package task

import (
	"context"
	"log"
	"strings"
	"tg-sender/internal/safego"
	"time"

	"github.com/mtgo-labs/mtgo/telegram"
	"github.com/mtgo-labs/mtgo/tg"
	"tg-sender/internal/domain"
)

func (e *Engine) StartApprovalChecker(ctx context.Context) {
	safego.Go(func() { e.runApprovalChecker(ctx) })
}

func (e *Engine) runApprovalChecker(ctx context.Context) {
	ticker := time.NewTicker(e.cfg.ApprovalCheckInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			e.checkPendingApprovals(ctx)
		case <-e.stopCh:
			return
		case <-ctx.Done():
			return
		}
	}
}

func (e *Engine) checkPendingApprovals(ctx context.Context) {
	if e.approvalRepo == nil {
		return
	}

	pendings, err := e.approvalRepo.GetPendingAll(ctx, e.cfg.ApprovalCheckLimit)
	if err != nil {
		return
	}

	for _, p := range pendings {
		if p.CheckCount >= p.MaxChecks {
			if err := e.approvalRepo.UpdateStatus(ctx, p.TenantID, p.TaskID, "timeout"); err != nil {
				log.Printf("[approval] 更新状态失败: %v", err)
			}
			if err := e.repo.UpdateStatus(ctx, p.TenantID, p.TaskID, domain.TaskStatusFailed); err != nil {
				taskLog.Printf("[task] UpdateStatus failed: %v", err)
			}
			continue
		}

		safego.Go(func() {
			defer func() {
				if r := recover(); r != nil {
					taskLog.Printf("[task] approval check panic recovered: %v", r)
				}
			}()
			client, err := e.accEngine.GetClient(ctx, p.TenantID, p.AccountID)
			if err != nil {
				taskLog.Printf("[task] GetClient failed for approval check: %v", err)
				return
			}
			defer e.accEngine.ReleaseClient(p.TenantID, p.AccountID, client)

			inGroup := e.isAccountInGroup(ctx, client, p.ChatID, p.AccessHash, p.TargetLink)
			if inGroup {
				if err := e.approvalRepo.UpdateStatus(ctx, p.TenantID, p.TaskID, "approved"); err != nil {
					log.Printf("[approval] 更新状态失败: %v", err)
				}
				if err := e.repo.UpdateStatus(ctx, p.TenantID, p.TaskID, domain.TaskStatusCompleted); err != nil {
					taskLog.Printf("[task] UpdateStatus failed: %v", err)
				}
			} else {
				if err := e.approvalRepo.IncrementCheck(ctx, p.TenantID, p.TaskID); err != nil {
					log.Printf("[approval] 增加检查次数失败: %v", err)
				}
			}
		})
	}
}

func (e *Engine) isAccountInGroup(ctx context.Context, client *telegram.Client, chatID int64, accessHash int64, targetLink string) bool {
	// Method 1: Check via GetDialogs (safest, no side effects)
	// GetDialogs returns []*types.Chat, not []*types.Dialog
	dialogs, err := client.GetDialogs(ctx, e.cfg.DialogsLimit, 0)
	if err == nil {
		for _, chat := range dialogs {
			if chat == nil {
				continue
			}
			// Match by chatID (normalize both to positive for comparison)
			if chatID != 0 {
				dialogID := chat.ID
				if dialogID < 0 {
					dialogID = -dialogID
				}
				checkID := chatID
				if checkID < 0 {
					checkID = -checkID
				}
				if dialogID == checkID {
					return true
				}
			}
			// Match by invite link in title/username (fallback for private groups)
			if targetLink != "" && (strings.Contains(chat.Title, targetLink) || strings.Contains(chat.Username, targetLink)) {
				return true
			}
		}
	}

	// Method 2: Try by chatID via Telegram API (read-only, no side effects)
	// Use AccessHash if available for channel queries
	if chatID != 0 {
		// P0-4 审计修复: chatID 是 -100xxx 全格式时转裸 ID, 否则审批检查对超级群必失败
		chID := chatID
		if chID <= -1000000000000 {
			chID = -chID - 1000000000000
		}
		req := &tg.ChannelsGetFullChannelRequest{
			Channel: &tg.InputChannel{
				ChannelID:  chID,
				AccessHash: accessHash,
			},
		}
		_, err := client.Invoke(ctx, req)
		if err == nil {
			return true
		}
		// Fallback: try as regular chat
		_, err = client.Invoke(ctx, &tg.MessagesGetFullChatRequest{ChatID: chatID})
		if err == nil {
			return true
		}
	}

	// Method 3: No chatID and not in dialogs - try re-importing invite link
	// This is last resort and may re-send request if admin rejected before
	if targetLink != "" && (strings.Contains(targetLink, "+") || strings.Contains(targetLink, "joinchat")) {
		hash := extractInviteHash(targetLink)
		req := &tg.MessagesImportChatInviteRequest{Hash: hash}
		_, err := client.Invoke(ctx, req)
		if err != nil {
			errStr := strings.ToLower(err.Error())
			if strings.Contains(errStr, "already_participant") || strings.Contains(errStr, "user_already") {
				return true
			}
		} else {
			// Import succeeded, we are now in the group
			return true
		}
	}

	return false
}

// StartResourceMonitor starts a background goroutine that monitors system resources
// and pushes alerts via SSE when thresholds are exceeded.
