package task

import (
	"context"
	"fmt"
	"log"
	"math/rand"
	"strings"
	"time"

	"tg-sender/internal/domain"

	"github.com/google/uuid"
	"github.com/mtgo-labs/mtgo/telegram"
	"github.com/mtgo-labs/mtgo/tg"
)


// classifyJoinError 把 Telegram 拒绝原因细化成可读错误码（原 join_failed 吞掉真因）
func classifyJoinError(err error) string {
	if err == nil {
		return ""
	}
	s := strings.ToLower(err.Error())
	switch {
	case strings.Contains(s, "invite_hash_expired"):
		return "invite_expired"
	case strings.Contains(s, "invite_hash_empty"), strings.Contains(s, "invite_hash_invalid"):
		return "invite_invalid"
	case strings.Contains(s, "username_invalid"), strings.Contains(s, "username_occupied"):
		return "username_invalid"
	case strings.Contains(s, "peer_flood"):
		return "peer_flood"
	case strings.Contains(s, "too much"), strings.Contains(s, "channels_too_much"):
		return "too_many_groups"
	case strings.Contains(s, "channel_private"), strings.Contains(s, "chat_private"):
		return "group_private"
	case strings.Contains(s, "user_not_participant"):
		return "not_invited"
	case strings.Contains(s, "banned"), strings.Contains(s, "kicked"):
		return "banned_from_group"
	case strings.Contains(s, "flood"):
		return "join_flood_wait"
	default:
		return "join_failed"
	}
}

func (e *Engine) executeJoinGroups(ctx context.Context, task *domain.Task) {
	total := len(task.TargetIDs)
	done := 0
	failed := 0

	if len(task.AccountIDs) == 0 {
		if err := e.repo.UpdateStatus(ctx, task.TenantID, task.TaskID, domain.TaskStatusFailed); err != nil {
			taskLog.Printf("[task] UpdateStatus failed: %v", err)
		}
		return
	}

	// 加群间隔: 租户可在创建任务时设置 最小/最大 间隔(分钟), 每次在区间内随机取值;
	// 未设置时退回全局配置 JoinIntervalMinutes(默认2分钟) + 0~50% 抖动
	intervalMin := time.Duration(0)
	intervalMax := time.Duration(0)
	if v, ok := task.Payload["interval_min_minutes"].(float64); ok && v > 0 {
		intervalMin = time.Duration(v * float64(time.Minute))
	}
	if v, ok := task.Payload["interval_max_minutes"].(float64); ok && v > 0 {
		intervalMax = time.Duration(v * float64(time.Minute))
	}
	if intervalMax > 0 && intervalMax < intervalMin {
		intervalMax = intervalMin
	}

	joinIntervalMinutes := e.cfg.JoinIntervalMinutes
	if joinIntervalMinutes <= 0 {
		joinIntervalMinutes = 2
	}
	joinInterval := time.Duration(joinIntervalMinutes) * time.Minute

	// R108 修复: accID 声明(此前被误删, join 全程单账号执行)
	accID := task.AccountIDs[0]

	// Check account status before using it
	acc, err := e.accEngine.GetByID(ctx, task.TenantID, accID)
	if err != nil || acc == nil || (acc.Status == domain.AccountStatusBanned || acc.Status == domain.AccountStatusRestricted || acc.Status == domain.AccountStatusPendingAuth) {
		status := "unknown"
		if acc != nil {
			status = string(acc.Status)
		}
		taskLog.Printf("[join] skip account %s: status=%s (not active)", accID, status)
		if err := e.repo.UpdateStatus(ctx, task.TenantID, task.TaskID, domain.TaskStatusFailed); err != nil {
			taskLog.Printf("[task] UpdateStatus failed: %v", err)
		}
		return
	}
	if acc.BlockedUntil != nil && acc.BlockedUntil.After(time.Now().UTC()) {
		taskLog.Printf("[join] skip account %s: blocked_until=%s", accID, acc.BlockedUntil.Format(time.RFC3339))
		if err := e.repo.UpdateStatus(ctx, task.TenantID, task.TaskID, domain.TaskStatusFailed); err != nil {
			taskLog.Printf("[task] UpdateStatus failed: %v", err)
		}
		return
	}

	client, err := e.accEngine.GetClient(ctx, task.TenantID, accID)
	if err != nil {
		if err := e.repo.UpdateStatus(ctx, task.TenantID, task.TaskID, domain.TaskStatusFailed); err != nil {
			taskLog.Printf("[task] UpdateStatus failed: %v", err)
		}
		return
	}
	defer e.accEngine.ReleaseClient(task.TenantID, accID, client)

	for _, link := range task.TargetIDs {
		select {
		case <-e.stopCh:
			if err := e.repo.UpdateStatus(ctx, task.TenantID, task.TaskID, domain.TaskStatusPaused); err != nil {
				taskLog.Printf("[task] UpdateStatus failed: %v", err)
			}
			return
		default:
		}

		status := "success"
		var errCode string
		var errMsg string // 2026-09-22: Telegram 拒绝原文, 落 task_logs.message, 前端详情可见
		floodHit := false
		floodWaitSec := 0 // 命中限流时记录 Telegram 要求的等待秒数

		// Distinguish private invite link vs public group link
		if strings.Contains(link, "+") || strings.Contains(link, "joinchat") {
			// Private group: use ImportChatInvite with hash
			// Deduplication: skip if already pending approval for this link
			if e.approvalRepo != nil {
				if existing, _ := e.approvalRepo.GetByAccountAndLink(ctx, accID, link); existing != nil {
					status = "pending_approval"
					errCode = "already_pending"
					goto logAndContinue
				}
			}
			hash := extractInviteHash(link)
			req := &tg.MessagesImportChatInviteRequest{Hash: hash}
			// 协议号执行任务可能在此永久阻塞（网络层卡死），加超时让错误可见
			invokeCtx, invokeCancel := context.WithTimeout(ctx, 90*time.Second)
			result, joinErr := client.Invoke(invokeCtx, req)
			invokeCancel()
			if joinErr != nil {
				if isInviteRequestSent(joinErr) {
					// Join request automatically sent by Telegram server
					status = "pending_approval"
					// Write to pending_approvals for background checking
					if e.approvalRepo != nil {
						// For private links, chatID is unknown until approved
						// AccessHash will be resolved later when checking
						if err := e.approvalRepo.Create(ctx, &domain.PendingApproval{
							TaskID:     task.TaskID,
							TenantID:   task.TenantID,
							AccountID:  accID,
							ChatID:     0, // private link, chat_id unknown yet
							AccessHash: 0,
							TargetLink: link,
							Status:     "pending",
							MaxChecks:  12,
						}); err != nil {
							log.Printf("[join] 创建待审批记录失败: %v", err)
						}
					}
					// Create temporary group record for pending approval
					if e.groupRepo != nil {
						now := time.Now()
						g := &domain.Group{
							GroupID:  uuid.New(),
							TenantID: task.TenantID,
							Title:    link,
							Type:     "pending",
							IsJoined: false,
							JoinedAt: &now,
						}
						if err := e.groupRepo.Create(ctx, g); err != nil {
							log.Printf("[join] 创建群组失败: %v", err)
						}
					}
				} else {
					status = "failed"
					errCode = "join_failed"
					if isGlobalFlood(joinErr) {
						floodHit = true
						floodWaitSec = extractFloodSeconds(strings.ToLower(joinErr.Error()))
						e.markAccountRestricted(ctx, task.TenantID, accID, "join_flood_wait", joinErr.Error(), floodWaitSec)
					} else {
						e.recordRiskEvent(ctx, task.TenantID, accID, "join_fail", "medium", joinErr.Error())
					}
				}
			} else {
				// Direct join success: extract group info from Updates
				if updates, ok := result.(*tg.Updates); ok && e.groupRepo != nil {
					e.extractAndSaveGroupFromUpdates(ctx, task, updates)
				} else if !ok {
					taskLog.Printf("[join] ImportChatInvite result is not *tg.Updates, type: %T", result)
				}
			}
		} else {
			// Public group: ResolvePeer → get chat ID → JoinChannel
			peer, resolveErr := client.ResolvePeer(ctx, link)
			if resolveErr != nil {
				status = "failed"
				errCode = "resolve_failed"
			} else {
				var chatID int64
				var accessHash int64
				var gtype string
				var username string
				switch p := peer.(type) {
				case *tg.InputPeerChat:
					chatID = p.ChatID
					gtype = "group"
				case *tg.InputPeerChannel:
					chatID = p.ChannelID
					accessHash = p.AccessHash
					gtype = "channel"
				default:
					status = "failed"
					errCode = "not a group"
				}
				if chatID != 0 {
					// Deduplication: skip if already joined this group
					if e.groupRepo != nil {
						if existing, _ := e.groupRepo.GetByTGChatID(ctx, task.TenantID, chatID); existing != nil && existing.IsJoined {
							status = "success"
							errCode = "already_joined"
							goto logAndContinue
						}
					}
					var joinErr error
					switch p := peer.(type) {
					case *tg.InputPeerChat:
						// Regular group: treat as channel (public groups are usually channels/supergroups)
						_, joinErr = client.Invoke(ctx, &tg.ChannelsJoinChannelRequest{
							Channel: &tg.InputChannel{
								ChannelID:  p.ChatID,
								AccessHash: 0,
							},
						})
					case *tg.InputPeerChannel:
						// Channel/supergroup: use channels.joinChannel
						_, joinErr = client.Invoke(ctx, &tg.ChannelsJoinChannelRequest{
							Channel: &tg.InputChannel{
								ChannelID:  p.ChannelID,
								AccessHash: p.AccessHash,
							},
						})
					}
					if joinErr != nil {
						if isInviteRequestSent(joinErr) {
							status = "pending_approval"
							if e.approvalRepo != nil {
								if err := e.approvalRepo.Create(ctx, &domain.PendingApproval{
									TaskID:     task.TaskID,
									TenantID:   task.TenantID,
									AccountID:  accID,
									ChatID:     chatID,
									TargetLink: link,
									Status:     "pending",
									MaxChecks:  12,
								}); err != nil {
									log.Printf("[join] 创建待审批记录失败: %v", err)
								}
							}
						} else if isAlreadyMember(joinErr) {
							status = "success"
							errCode = "already_member"
						} else {
							status = "failed"
							errMsg = joinErr.Error()
							errCode = classifyJoinError(joinErr)
							errStr := strings.ToLower(joinErr.Error())
							switch {
							case isGlobalFlood(joinErr):
								waitSec := extractFloodSeconds(errStr)
								if waitSec >= 60 {
									e.markAccountRestricted(ctx, task.TenantID, accID, "join_flood_wait", joinErr.Error(), extractFloodSeconds(strings.ToLower(joinErr.Error())))
								}
							case isAccountBanned(joinErr):
								e.markAccountBanned(ctx, task.TenantID, accID, "account_banned", joinErr.Error())
							default:
								e.recordRiskEvent(ctx, task.TenantID, accID, "join_fail", "medium", joinErr.Error())
							}
						}
					} else {
						// Join success: save to groups table immediately
						if e.groupRepo != nil {
							now := time.Now()
							// Extract username from public link
							if strings.HasPrefix(link, "@") {
								username = link[1:]
							} else if strings.HasPrefix(link, "https://t.me/") {
								username = strings.TrimPrefix(link, "https://t.me/")
							} else if strings.HasPrefix(link, "http://t.me/") {
								username = strings.TrimPrefix(link, "http://t.me/")
							} else if !strings.Contains(link, "/") {
								username = link
							}
							g := &domain.Group{
								GroupID:    uuid.New(),
								TenantID:   task.TenantID,
								TGChatID:   chatID,
								Title:      link,
								Username:   username,
								Type:       gtype,
								AccessHash: accessHash,
								IsJoined:   true,
								JoinedAt:   &now,
							}
							existing, err := e.groupRepo.GetByTGChatID(ctx, task.TenantID, chatID)
							if err == nil && existing != nil {
								existing.AccessHash = accessHash
								existing.IsJoined = true
								if g.Username != "" {
									existing.Username = g.Username
								}
								if err := e.groupRepo.Update(ctx, existing); err != nil {
									log.Printf("[join] 更新群组失败: %v", err)
								}
							} else {
								if err := e.groupRepo.Create(ctx, g); err != nil {
									log.Printf("[join] 创建群组失败: %v", err)
								}
							}
						}
					}
				}
			}
		}

	logAndContinue:
		joinLog := &domain.TaskLog{
			LogID:      uuid.New(),
			TaskID:     task.TaskID,
			TenantID:   task.TenantID,
			AccountID:  accID,
			TargetID:   link,
			Action:     "join_group",
			Status:     status,
			ErrorCode:  errCode,
			Message:    errMsg,
			ExecutedAt: time.Now(),
		}
		if err := e.logRepo.Create(ctx, joinLog); err != nil {
			log.Printf("[join] 创建日志失败: %v", err)
		}
		e.publishLog(task.TenantID, task.TaskID, joinLog)

		// R87-M10: 加入失败 → 回写群库"已失效"状态(按 dedup_key 匹配, 已加入的群不降级)
		if status == "failed" && e.systemGroupRepo != nil {
			key := link
			if strings.HasPrefix(key, "+") || strings.HasPrefix(key, "joinchat/") {
				key = "l:t.me/" + strings.TrimPrefix(strings.TrimPrefix(key, "+"), "joinchat/")
			} else if !strings.HasPrefix(key, "u:") && !strings.HasPrefix(key, "l:") {
				key = "u:" + strings.ToLower(key)
			}
			if merr := e.systemGroupRepo.MarkInvalidByDedupKey(ctx, key); merr != nil {
				log.Printf("[join] 标记群失效失败 %s: %v", key, merr)
			}
		}

		// Accumulate progress locally, flush at end of loop
		progressDelta := domain.TaskProgress{Done: 0, Failed: 0, CurrentTarget: link}
		if status == "success" {
			progressDelta.Done = 1
		} else if status == "pending_approval" {
			// pending_approval: join request sent successfully, awaiting admin approval
			// Count as done since the user's action (sending request) is complete
			progressDelta.Done = 1
			progressDelta.Failed = 0
		} else {
			progressDelta.Failed = 1
		}
		done += progressDelta.Done
		failed += progressDelta.Failed

		// 实时进度: 每个目标处理完立即上报(前端进度面板实时可见)
		liveProgress := domain.TaskProgress{Total: total, Done: done, Failed: failed, CurrentTarget: link}
		if err := e.repo.UpdateProgressExact(ctx, task.TenantID, task.TaskID, liveProgress); err != nil {
			log.Printf("[join] 更新进度失败: %v", err)
		}
		e.publishProgress(task.TenantID, task.TaskID, liveProgress)

		// 触发频繁限制(FloodWait): 不原地睡 1 小时（会占死 worker 槽位和 client
		// 连接，3 个 worker 撞 3 次 flood 队列即停摆）。改为等 Telegram 要求的时长
		// （上限 10 分钟）后继续；期间任务被取消则挂起由超时回收。
		if floodHit {
			waitDur := time.Duration(floodWaitSec) * time.Second
			if waitDur <= 0 || waitDur > 10*time.Minute {
				waitDur = 10 * time.Minute
			}
			waitLog := &domain.TaskLog{LogID: uuid.New(), TaskID: task.TaskID, TenantID: task.TenantID, TargetID: link, Action: "join_group", Status: "info", ErrorCode: "flood_wait", Message: fmt.Sprintf("触发频繁限制，等待 %d 秒后继续", int(waitDur.Seconds())), ExecutedAt: time.Now()}
			if err := e.logRepo.Create(ctx, waitLog); err != nil {
				log.Printf("[join] 创建日志失败: %v", err)
			}
			e.publishLog(task.TenantID, task.TaskID, waitLog)
			select {
			case <-ctx.Done():
				return
			case <-time.After(waitDur):
			}
		} else {
			waitDur := joinInterval
			if intervalMax > 0 {
				// 租户自定义区间: [intervalMin, intervalMax] 随机
				span := intervalMax - intervalMin
				if span < 0 {
					span = 0
				}
				waitDur = intervalMin + time.Duration(rand.Int63n(int64(span+1)))
			} else if joinInterval > 0 {
				waitDur = joinInterval + time.Duration(rand.Int63n(int64(joinInterval/2)))
			}
			select {
			case <-ctx.Done():
				return
			case <-time.After(waitDur):
			}
		}
	}

	// Set final exact progress (single DB write instead of per-target)
	// Use actual 'done' count (not total-failed) to correctly reflect pending_approval states
	finalProgress := domain.TaskProgress{Total: total, Done: done, Failed: failed}
	if err := e.repo.UpdateProgressExact(ctx, task.TenantID, task.TaskID, finalProgress); err != nil {
		log.Printf("[join] 更新进度失败: %v", err)
	}
	e.publishProgress(task.TenantID, task.TaskID, finalProgress)

	// Sync joined groups to groups table (always try, even if already_member or failed)
	if e.groupRepo != nil && total > 0 {
		e.syncJoinedGroups(ctx, task, accID, client)
	}

	if failed == total && total > 0 {
		if err := e.repo.UpdateStatus(ctx, task.TenantID, task.TaskID, domain.TaskStatusFailed); err != nil {
			taskLog.Printf("[task] UpdateStatus failed: %v", err)
		}
	} else {
		if err := e.repo.UpdateStatus(ctx, task.TenantID, task.TaskID, domain.TaskStatusCompleted); err != nil {
			taskLog.Printf("[task] UpdateStatus failed: %v", err)
		}
	}
}

// syncJoinedGroups resolves joined group info and upserts to groups table.
// Saves access_hash for channels/supergroups to enable manual peer construction later.
func (e *Engine) syncJoinedGroups(ctx context.Context, task *domain.Task, accID uuid.UUID, client *telegram.Client) {
	for _, link := range task.TargetIDs {
		var chatID int64
		var title string
		var username string
		var gtype string
		var accessHash int64

		if strings.Contains(link, "+") || strings.Contains(link, "joinchat") {
			// Private link: try to resolve by hash after joining
			// Retry up to 3 times with backoff because cache may not sync immediately
			hash := extractInviteHash(link)
			var peer any
			var resolveErr error
			for attempt := 0; attempt < 3; attempt++ {
				if attempt > 0 {
					select {
					case <-ctx.Done():
						return
					case <-time.After(time.Duration(attempt) * 2 * time.Second):
					}
				}
				peer, resolveErr = client.ResolvePeer(ctx, "t.me/+"+hash)
				if resolveErr == nil {
					break
				}
			}
			if resolveErr != nil {
				continue
			}
			switch p := peer.(type) {
			case *tg.InputPeerChat:
				chatID = p.ChatID
				gtype = "group"
			case *tg.InputPeerChannel:
				chatID = p.ChannelID
				accessHash = p.AccessHash
				gtype = "channel"
			}
		} else {
			// Public link: resolve to get chat ID and extract username
			peer, err := client.ResolvePeer(ctx, link)
			if err != nil {
				continue
			}
			switch p := peer.(type) {
			case *tg.InputPeerChat:
				chatID = p.ChatID
				gtype = "group"
			case *tg.InputPeerChannel:
				chatID = p.ChannelID
				accessHash = p.AccessHash
				gtype = "channel"
			}
			// Extract username from public link (t.me/username or @username)
			if strings.HasPrefix(link, "@") {
				username = link[1:]
			} else if strings.HasPrefix(link, "https://t.me/") {
				username = strings.TrimPrefix(link, "https://t.me/")
			} else if strings.HasPrefix(link, "http://t.me/") {
				username = strings.TrimPrefix(link, "http://t.me/")
			} else if !strings.Contains(link, "/") {
				username = link
			}
		}

		if chatID == 0 {
			continue
		}

		// Try to get real title from Telegram (best-effort)
		if gtype == "channel" && accessHash != 0 {
			fullReq := &tg.ChannelsGetFullChannelRequest{
				Channel: &tg.InputChannel{ChannelID: chatID, AccessHash: accessHash},
			}
			if fullResult, err := client.Invoke(ctx, fullReq); err == nil {
				if full, ok := fullResult.(*tg.MessagesChatFull); ok {
					if _, ok := full.FullChat.(*tg.ChannelFull); ok {
						// Title is in the Channel object, not ChannelFull
						for _, chat := range full.Chats {
							if ch, ok := chat.(*tg.Channel); ok && ch.ID == chatID {
								title = ch.Title
								break
							}
						}
					}
				}
			}
		} else if gtype == "group" {
			fullReq := &tg.MessagesGetFullChatRequest{ChatID: chatID}
			if fullResult, err := client.Invoke(ctx, fullReq); err == nil {
				if full, ok := fullResult.(*tg.MessagesChatFull); ok {
					if _, ok := full.FullChat.(*tg.ChatFull); ok {
						for _, chat := range full.Chats {
							if ch, ok := chat.(*tg.Chat); ok && ch.ID == chatID {
								title = ch.Title
								break
							}
						}
					}
				}
			}
		}
		// Fallback to link if title still empty
		if title == "" {
			title = link
		}

		// Upsert group record with access_hash
		now := time.Now()
		g := &domain.Group{
			GroupID:    uuid.New(),
			TenantID:   task.TenantID,
			TGChatID:   chatID,
			Title:      title,
			Username:   username,
			Type:       gtype,
			AccessHash: accessHash,
			IsJoined:   true,
			JoinedAt:   &now,
		}
		// Check if group already exists by tg_chat_id + tenant_id
		existing, err := e.groupRepo.GetByTGChatID(ctx, task.TenantID, chatID)
		if err == nil && existing != nil {
			// Update existing with latest info
			existing.AccessHash = accessHash
			existing.IsJoined = true
			if username != "" {
				existing.Username = username
			}
			if err := e.groupRepo.Update(ctx, existing); err != nil {
				log.Printf("[join] 更新群组失败: %v", err)
			}
		} else {
			if err := e.groupRepo.Create(ctx, g); err != nil {
				log.Printf("[join] 创建群组失败: %v", err)
			}
		}
	}
}

// extractAndSaveGroupFromUpdates extracts group info from Updates (ImportChatInvite result)
// and saves/updates the groups table. Handles both *tg.Chat and *tg.Channel.
func (e *Engine) extractAndSaveGroupFromUpdates(ctx context.Context, task *domain.Task, updates *tg.Updates) {
	for _, chat := range updates.Chats {
		var chatID int64
		var title string
		var gtype string
		var accessHash int64

		switch c := chat.(type) {
		case *tg.Chat:
			chatID = c.ID
			title = c.Title
			gtype = "group"
		case *tg.Channel:
			// Convert internal channel ID to external format (-100{channel_id})
			chatID = -(1000000000000 + c.ID)
			title = c.Title
			accessHash = c.AccessHash
			if c.Megagroup {
				gtype = "supergroup"
			} else {
				gtype = "channel"
			}
		default:
			continue
		}

		if chatID == 0 {
			continue
		}

		now := time.Now()
		g := &domain.Group{
			GroupID:    uuid.New(),
			TenantID:   task.TenantID,
			TGChatID:   chatID,
			Title:      title,
			Type:       gtype,
			AccessHash: accessHash,
			IsJoined:   true,
			JoinedAt:   &now,
		}

		existing, err := e.groupRepo.GetByTGChatID(ctx, task.TenantID, chatID)
		if err == nil && existing != nil {
			existing.AccessHash = accessHash
			existing.IsJoined = true
			existing.Title = title
			if err := e.groupRepo.Update(ctx, existing); err != nil {
				log.Printf("[join] 更新群组失败: %v", err)
			}
		} else {
			if err := e.groupRepo.Create(ctx, g); err != nil {
				log.Printf("[join] 创建群组失败: %v", err)
			}
		}
	}
}
