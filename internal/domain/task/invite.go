package task

import (
	"context"
	"fmt"
	"math/rand/v2"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/mtgo-labs/mtgo/telegram"
	"github.com/mtgo-labs/mtgo/tg"
	"tg-sender/internal/domain"
)

// executeInviteMembers 邀请入群: 把来源群已采集成员逐个邀请进本账号的目标群。
// 成员带 AccessHash(采集历史消息/成员时已落库), 无需按用户名解析, 直连邀请。
func (e *Engine) executeInviteMembers(ctx context.Context, task *domain.Task) error {
	targetID, err := uuid.Parse(fmt.Sprint(task.Payload["target_group_id"]))
	if err != nil {
		return fmt.Errorf("目标群ID缺失: %w", err)
	}
	sourceID, err := uuid.Parse(fmt.Sprint(task.Payload["source_group_id"]))
	if err != nil {
		return fmt.Errorf("来源群ID缺失: %w", err)
	}
	target, err := e.groupRepo.GetByID(ctx, task.TenantID, targetID)
	if err != nil || target == nil {
		return fmt.Errorf("目标群不存在")
	}
	if len(task.AccountIDs) == 0 {
		return fmt.Errorf("无执行账号")
	}
	accID := task.AccountIDs[0]
	joined, err := e.groupRepo.IsJoinedByAccount(ctx, task.TenantID, accID, targetID)
	if err != nil {
		return fmt.Errorf("检查执行账号是否已加入目标群失败: %w", err)
	}
	if !joined {
		return fmt.Errorf("执行账号尚未加入目标群")
	}
	source, err := e.groupRepo.GetByID(ctx, task.TenantID, sourceID)
	if err != nil || source == nil {
		return fmt.Errorf("来源群不存在")
	}
	if e.memberRepo == nil {
		return fmt.Errorf("成员仓库未就绪")
	}
	total, err := e.memberRepo.CountByGroup(ctx, task.TenantID, sourceID)
	if err != nil {
		return fmt.Errorf("统计成员失败: %w", err)
	}
	initProg := domain.TaskProgress{Total: total, Done: 0, Failed: 0}
	if updated, err := e.repo.UpdateProgressExactIfOwned(ctx, task.TenantID, task.TaskID, e.workerID, initProg); err != nil {
		return fmt.Errorf("初始化邀请进度失败: %w", err)
	} else if updated {
		e.publishProgress(task.TenantID, task.TaskID, initProg)
	}
	taskLog.Printf("[invite] 任务 %s: 邀请 %s 的 %d 名成员进 %s", task.TaskID, source.Title, total, target.Title)

	acc, err := e.accEngine.GetByID(ctx, task.TenantID, accID)
	if err != nil || acc == nil {
		return fmt.Errorf("执行账号不存在")
	}
	if acc.PoolStatus == "standby" || acc.Status == domain.AccountStatusBanned || acc.Status == domain.AccountStatusRestricted || acc.Status == domain.AccountStatusPendingAuth {
		return fmt.Errorf("执行账号不可用于邀请(当前状态: %s, 池状态: %s)", acc.Status, acc.PoolStatus)
	}
	client, err := e.accEngine.GetClient(ctx, task.TenantID, accID)
	if err != nil {
		return fmt.Errorf("取客户端失败: %w", err)
	}
	defer e.accEngine.ReleaseClient(task.TenantID, accID, client)

	channel := &tg.InputChannel{ChannelID: target.TGChatID, AccessHash: target.AccessHash}

	invited, failed := 0, 0
	const pageSize = 200
	saveProgress := func() {
		prog := domain.TaskProgress{Total: total, Done: invited, Failed: failed, CurrentTarget: fmt.Sprintf("已邀 %d / 失败 %d", invited, failed)}
		progressCtx := ctx
		var cancel context.CancelFunc
		if ctx.Err() != nil {
			progressCtx, cancel = context.WithTimeout(context.Background(), 10*time.Second)
		}
		if cancel != nil {
			defer cancel()
		}
		if updated, err := e.repo.UpdateProgressExactIfOwned(progressCtx, task.TenantID, task.TaskID, e.workerID, prog); err == nil && updated {
			e.publishProgress(task.TenantID, task.TaskID, prog)
		} else if err != nil {
			taskLog.Printf("[invite] 保存进度失败: %v", err)
		}
	}

	for offset := 0; ; offset += pageSize {
		select {
		case <-ctx.Done():
			saveProgress()
			return ctx.Err()
		default:
		}
		listCtx, listCancel := context.WithTimeout(ctx, 15*time.Second)
		members, err := e.memberRepo.ListByGroup(listCtx, task.TenantID, sourceID, pageSize, offset)
		listCancel()
		if err != nil {
			return fmt.Errorf("读取来源群成员失败: %w", err)
		}
		if len(members) == 0 {
			break
		}
		for _, m := range members {
			select {
			case <-ctx.Done():
				saveProgress()
				return ctx.Err()
			default:
			}
			if m.TGUserID == 0 {
				failed++
				continue
			}
			if m.AccessHash == 0 {
				failed++ // 无 access_hash 无法定向邀请
				continue
			}
			invokeCtx, invokeCancel := context.WithTimeout(ctx, 60*time.Second)
			_, err := client.Raw().ChannelsInviteToChannel(invokeCtx, &tg.ChannelsInviteToChannelRequest{
				Channel: channel,
				Users:   []tg.InputUserClass{&tg.InputUser{UserID: m.TGUserID, AccessHash: m.AccessHash}},
			})
			invokeErr := invokeCtx.Err()
			invokeCancel()
			if invokeErr != nil {
				saveProgress()
				return fmt.Errorf("邀请请求超时或已取消: %w", invokeErr)
			}
			if err != nil {
				errStr := strings.ToLower(err.Error())
				// 账号级风险: 邀请过频/被封 → 停止任务并冷却账号
				if strings.Contains(errStr, "flood") || strings.Contains(errStr, "ban") || strings.Contains(errStr, "restricted") {
					waitSec := extractFloodSeconds(errStr)
					e.markAccountRestricted(ctx, task.TenantID, accID, "invite_flood", err.Error(), waitSec)
					saveProgress()
					return fmt.Errorf("账号被限流/封禁, 已冷却: %w", err)
				}
				failed++ // 个体原因(已是群员/隐私拒绝等), 记失败继续
			} else {
				invited++
			}
			if (invited+failed)%20 == 0 {
				saveProgress()
			}
			// 邀请比发消息敏感, 间隔放宽: 1.5~3.5s 抖动
			select {
			case <-ctx.Done():
				saveProgress()
				return ctx.Err()
			case <-time.After(time.Duration(1500+rand.IntN(2000)) * time.Millisecond):
			}
		}
	}
	saveProgress()
	taskLog.Printf("[invite] 任务 %s 完成: 成功 %d / 失败 %d", task.TaskID, invited, failed)
	return nil
}

var _ = telegram.Client{} // 保留包引用一致性
