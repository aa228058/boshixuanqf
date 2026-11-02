package task

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"math/rand/v2"
	"mime"
	"os"
	"path/filepath"
	"regexp"
	"runtime/debug"
	"strconv"
	"strings"
	"sync"
	"tg-sender/internal/safego"
	"time"

	"tg-sender/internal/domain"

	"github.com/google/uuid"
	"github.com/mtgo-labs/mtgo/telegram"
	"github.com/mtgo-labs/mtgo/telegram/params"
	"github.com/mtgo-labs/mtgo/tg"
)

func (e *Engine) executeBroadcast(ctx context.Context, task *domain.Task) {
	// Overall timeout: max 30 minutes for a broadcast task to prevent indefinite hanging
	ctx, cancel := context.WithTimeout(ctx, 30*time.Minute)
	defer cancel()

	// 读取任务自定义发送间隔（秒），未设置则使用全局配置
	var taskMinInterval, taskMaxInterval int
	if task.Payload != nil {
		if v, ok := task.Payload["interval_min"].(float64); ok && v > 0 {
			taskMinInterval = int(v)
		}
		if v, ok := task.Payload["interval_max"].(float64); ok && v > 0 {
			taskMaxInterval = int(v)
		}
	}

	var message string
	if v, ok := task.Payload["message"]; ok {
		message = fmt.Sprint(v)
	}
	if message == "" {
		if err := e.repo.UpdateStatus(ctx, task.TenantID, task.TaskID, domain.TaskStatusFailed); err != nil {
			taskLog.Printf("[task] UpdateStatus failed: %v", err)
		}
		return
	}

	// R119-fix: Markdown 转义移到发送前(所有 spintax/变量/混淆解析之后)统一兜底,
	// 这里先转义会把 { } | 变成 \{ \} 导致 spintax 失效({你好|Hi} 原样发出)。

	// 一键自动变化: 群发创建时勾选"每条消息自动变化"即可, 无需写任何变量语法
	autoVary := false
	if v, ok := task.Payload["auto_vary"]; ok {
		autoVary = fmt.Sprint(v) == "true"
	}

	// 文案变量(数据字典): 预建 {{词典名}} → 词条池, 发送时每个目标随机替换
	dictBag := e.buildDictBag(ctx, task.TenantID, message)

	// 消息混淆开关: 发送前(每个目标独立)加随机emoji+隐形字符, 保证每条消息都不同
	obfEnabled, _ := task.Payload["obfuscate"].(bool)

	// 富媒体与 URL 按钮 (可选, 来自 payload)
	var mediaType, mediaSource string
	if m, ok := task.Payload["media"].(map[string]any); ok {
		mediaType, _ = m["type"].(string)
		mediaSource, _ = m["source"].(string)
	}
	var urlButtons []urlButton
	if bs, ok := task.Payload["buttons"].([]any); ok {
		for _, b := range bs {
			bm, ok := b.(map[string]any)
			if !ok {
				continue
			}
			bt, _ := bm["text"].(string)
			bu, _ := bm["url"].(string)
			if bt != "" && bu != "" {
				urlButtons = append(urlButtons, urlButton{bt, bu})
			}
		}
	}
	if len(urlButtons) > 0 {
		log.Printf("[broadcast] 任务 %s 执行器收到 %d 个按钮", task.TaskID, len(urlButtons))
	}

	// Determine broadcast mode
	modeStr, _ := task.Payload["mode"].(string)
	mode := domain.BroadcastMode(modeStr)
	if mode == "" {
		mode = domain.BroadcastModeCartesian // default to legacy mode
	}

	// Build account -> targets mapping
	accountTargets := make(map[uuid.UUID][]string)
	var total int

	if mode == domain.BroadcastModeMapping {
		// Parse mappings from payload
		var mappingsData []any
		if v, ok := task.Payload["mappings"]; ok {
			switch val := v.(type) {
			case []any:
				mappingsData = val
			}
		}
		for _, m := range mappingsData {
			mMap, ok := m.(map[string]any)
			if !ok {
				continue
			}
			accIDStr, _ := mMap["account_id"].(string)
			accID, err := uuid.Parse(accIDStr)
			if err != nil {
				continue
			}
			targetsRaw, _ := mMap["targets"].([]any)
			var targets []string
			for _, t := range targetsRaw {
				if ts, ok := t.(string); ok {
					targets = append(targets, ts)
				}
			}
			accountTargets[accID] = targets
			total += len(targets)
		}
		// Fallback: if no valid mappings parsed, use cartesian
		if len(accountTargets) == 0 {
			mode = domain.BroadcastModeCartesian
		}
	}

	if mode == domain.BroadcastModeCartesian {
		for _, accID := range task.AccountIDs {
			accountTargets[accID] = task.TargetIDs
		}
		total = len(task.TargetIDs) * len(task.AccountIDs)
	}

	done := 0
	failed := 0
	accountProgress := make(map[uuid.UUID]*domain.AccountProgress)
	var batchLogs []*domain.TaskLog         // collect logs for batch insert
	var mu sync.Mutex                       // protect shared state across goroutines
	var pendingLogs []*domain.TaskLog       // accumulated logs for throttled SSE push
	var pendingProgress domain.TaskProgress // accumulated progress for throttled SSE push

	// Periodic flush: every 5 seconds or 50 logs
	flushTicker := time.NewTicker(5 * time.Second)
	defer flushTicker.Stop()
	flushDone := make(chan struct{})
	safego.Go(func() {
		for {
			select {
			case <-flushTicker.C:
				mu.Lock()
				if len(batchLogs) > 0 {
					logs := batchLogs
					batchLogs = nil
					mu.Unlock()
					if err := e.logRepo.CreateBatch(ctx, logs); err != nil {
						taskLog.Printf("[task] periodic flush logs failed: %v", err)
					}
				} else {
					mu.Unlock()
				}
			case <-ctx.Done():
				return
			case <-e.stopCh:
				return
			case <-flushDone:
				return
			}
		}
	})

	// Throttled SSE aggregator: flushes pending progress/logs every 100ms
	// Prevents memory explosion from per-message SSE channel writes
	sseTicker := time.NewTicker(100 * time.Millisecond)
	defer sseTicker.Stop()
	sseDone := make(chan struct{})
	safego.Go(func() {
		for {
			select {
			case <-sseTicker.C:
				mu.Lock()
				logs := pendingLogs
				prog := pendingProgress
				pendingLogs = nil
				pendingProgress = domain.TaskProgress{}
				mu.Unlock()
				if len(logs) > 0 {
					for _, l := range logs {
						e.publishLog(task.TenantID, task.TaskID, l)
					}
				}
				if prog.Total > 0 || prog.Done > 0 || prog.Failed > 0 {
					e.publishProgress(task.TenantID, task.TaskID, prog)
				}
			case <-ctx.Done():
				return
			case <-e.stopCh:
				return
			case <-sseDone:
				return
			}
		}
	})

	// Worker pool: process up to N accounts concurrently.
	// Use tenant plan quota, dynamically compressed based on system memory pressure.
	maxConcurrentAccounts := e.cfg.BroadcastWorkers
	if e.scheduler != nil {
		tenantLimit := e.scheduler.GetTenantLimit(task.TenantID)
		if tenantLimit > 0 {
			maxConcurrentAccounts = tenantLimit
		}
	}
	if maxConcurrentAccounts <= 0 {
		maxConcurrentAccounts = 5
	}
	semaphore := make(chan struct{}, maxConcurrentAccounts)
	var wg sync.WaitGroup

	// Deduplicate targets globally to prevent multiple accounts sending to the same target
	seenTargets := make(map[string]bool)
	for accID := range accountTargets {
		unique := make([]string, 0, len(accountTargets[accID]))
		for _, t := range accountTargets[accID] {
			if !seenTargets[t] {
				seenTargets[t] = true
				unique = append(unique, t)
			}
		}
		accountTargets[accID] = unique
	}

	// P1-47 审计修复: 续传防重原先是每目标一次 count(*) 查询(N+1), 上千目标时
	// 拖慢发送循环。这里在 worker 扇出前一次性批量查回成功目标集; 运行期新成功
	// 在写日志处本地登记(原逻辑要等周期 flush 落库后才可见, 本地登记更及时)。
	allTargets := []string{}
	seenT := map[string]bool{}
	for _, list := range accountTargets {
		for _, t := range list {
			if !seenT[t] {
				seenT[t] = true
				allTargets = append(allTargets, t)
			}
		}
	}
	successSet := map[string]bool{}
	if ss, err := e.logRepo.HasSuccessForTargets(ctx, task.TenantID, task.TaskID, allTargets); err != nil {
		log.Printf("[broadcast] 批量查询已发目标失败 task=%s: %v(防重降级为不检查)", task.TaskID, err)
	} else {
		successSet = ss
	}

	for _, accID := range task.AccountIDs {
		targets := accountTargets[accID]
		if len(targets) == 0 {
			continue
		}

		wg.Add(1)
		// P0-16 审计修复: 原写法 semaphore 不响应 ctx——任务取消且 worker 全占满时,
		// 主协程永久阻塞在 <-struct{}{}, 任务永远停 running。select 让取消信号可中断。
		select {
		case semaphore <- struct{}{}:
		case <-ctx.Done():
			wg.Done()
			break
		}
		safego.Go(func() {
			// 群名映射: 日志里显示群名而不是 ID
			titleByChatID := map[int64]string{}
			if gl, err := e.groupRepo.ListByAccount(ctx, task.TenantID, accID, 1000, 0); err == nil {
				for _, g := range gl {
					if g.TGChatID != 0 {
						titleByChatID[g.TGChatID] = g.Title
					}
				}
			}
			// 联系人缓存: 私聊目标反查昵称
			contactNameByID := map[int64]string{}
			if cn, err := e.memberRepo.ListContactsByAccount(ctx, task.TenantID, accID); err == nil {
				contactNameByID = cn
			}
			defer func() {
				if r := recover(); r != nil {
					taskLog.Printf("[task] broadcast panic for account %s: %v\n%s", accID, r, debug.Stack())
				}
			}()
			defer wg.Done()
			defer func() { <-semaphore }()

			// 检查账号状态，非 ACTIVE 或已被 block 的账号跳过
			acc, err := e.accEngine.GetByID(ctx, task.TenantID, accID)
			if err != nil || acc == nil || (acc.Status == domain.AccountStatusBanned || acc.Status == domain.AccountStatusRestricted || acc.Status == domain.AccountStatusPendingAuth) {
				status := "unknown"
				if acc != nil {
					status = string(acc.Status)
				}
				taskLog.Printf("[task] skip account %s: status=%s (not active)", accID, status)
				mu.Lock()
				failed += len(targets)
				accountProgress[accID] = &domain.AccountProgress{
					AccountID: accID,
					Done:      0,
					Failed:    len(targets),
					Total:     len(targets),
				}
				mu.Unlock()
				return
			}
			if acc.BlockedUntil != nil && acc.BlockedUntil.After(time.Now().UTC()) {
				taskLog.Printf("[task] skip account %s: blocked_until=%s", accID, acc.BlockedUntil.Format(time.RFC3339))
				mu.Lock()
				failed += len(targets)
				accountProgress[accID] = &domain.AccountProgress{
					AccountID: accID,
					Done:      0,
					Failed:    len(targets),
					Total:     len(targets),
				}
				mu.Unlock()
				return
			}

			client, err := e.accEngine.GetClient(ctx, task.TenantID, accID)
			if err != nil {
				mu.Lock()
				failed += len(targets)
				accountProgress[accID] = &domain.AccountProgress{
					AccountID: accID,
					Done:      0,
					Failed:    len(targets),
					Total:     len(targets),
				}
				mu.Unlock()
				return
			}
			defer e.accEngine.ReleaseClient(task.TenantID, accID, client)

			// P2修复(精读发现): 账号等级原来在逐目标循环内每发一条消息查一次库
			// (万级群发 = 万次 DB 查询, Q族同款)。等级在账号维度不变, 循环外查一次。
			accLevel := e.accEngine.GetAccountLevel(ctx, task.TenantID, accID)

			if len(task.AccountIDs) == 0 || len(targets) == 0 {
				taskLog.Printf("[task] broadcast task %s has empty account_ids or target_ids, marking failed", task.TaskID)
				if err := e.repo.UpdateStatus(ctx, task.TenantID, task.TaskID, domain.TaskStatusFailed); err != nil {
					taskLog.Printf("[task] UpdateStatus failed: %v", err)
				}
				if err := e.logRepo.Create(ctx, &domain.TaskLog{
					TaskID:     task.TaskID,
					TenantID:   task.TenantID,
					AccountID:  uuid.Nil,
					Action:     "broadcast",
					Status:     "failed",
					ErrorCode:  "empty_accounts_or_targets",
					ExecutedAt: time.Now(),
				}); err != nil {
					taskLog.Printf("[task] log create failed: %v", err)
				}
				return
			}

			accDone := 0
			accFailed := 0
			progressAccum := domain.TaskProgress{}
			progressFlushThreshold := 10 // flush progress every N messages to reduce DB writes

			for i, targetID := range targets {
				// 2026-10-21: 目标级耗时统计——结果日志带"耗时 Xs", 用户要的完整 时间-群名-状态-耗时
				targetStart := time.Now()
				select {
				case <-e.stopCh:
					// Flush pending progress before pausing
					if progressAccum.Total > 0 {
						if err := e.repo.UpdateProgressExact(ctx, task.TenantID, task.TaskID, progressAccum); err != nil {
							log.Printf("[broadcast] 更新进度失败: %v", err)
						}
						e.publishProgress(task.TenantID, task.TaskID, progressAccum)
					}
					if err := e.repo.UpdateStatus(ctx, task.TenantID, task.TaskID, domain.TaskStatusPaused); err != nil {
						taskLog.Printf("[task] UpdateStatus failed: %v", err)
					}
					return
				case <-ctx.Done():
					// Flush pending progress before cancel exit
					if progressAccum.Total > 0 {
						if err := e.repo.UpdateProgressExact(ctx, task.TenantID, task.TaskID, progressAccum); err != nil {
							log.Printf("[broadcast] 更新进度失败: %v", err)
						}
						e.publishProgress(task.TenantID, task.TaskID, progressAccum)
					}
					return
				default:
				}

				// 续传防重: 重启后继续任务时跳过已成功发送的目标 (L0: 防止重复发送)
				// P1-47: 改为内存查 successSet(worker 并发, 借 mu 互斥), 消除 N+1 查询
				// P0-12: 按 (账号,目标) 复合键查——按目标全局去重会把 cartesian 模式压成单账号发送
				mu.Lock()
				alreadySent := successSet[accID.String()+"|"+targetID]
				mu.Unlock()
				if alreadySent {
					accDone++
					continue
				}

				// TG 返回的 message_id 与发送错误, 在分支间共享, 供循环尾部写 task_logs
				var sentMsgID int64
				var sendErr error

				// Try numeric chat ID first (e.g. "-5303625358")
				status := "success"
				var errCode string
				var chatID int64
				var peer any
				if parsedID, parseErr := strconv.ParseInt(targetID, 10, 64); parseErr == nil {
					chatID = parsedID
					// 超级群/频道必须用 AccessHash, 否则 PEER_ID_INVALID。
					// 前端选择器现在统一存 TG chat ID, 查库补全 hash 直发, 零解析。
					if g, gErr := e.groupRepo.GetByTGChatID(ctx, task.TenantID, chatID); gErr == nil && g != nil && g.AccessHash != 0 {
						peer = &tg.InputPeerChannel{ChannelID: chatID, AccessHash: g.AccessHash}
					} else if h, mErr := e.memberRepo.GetAccessHashByTGUserID(ctx, task.TenantID, chatID); mErr == nil && h != 0 {
						// 私聊目标: groups 表查不到 → 从 members 表(成员采集时缓存的 AccessHash)补, 直发不报错
						peer = &tg.InputPeerUser{UserID: chatID, AccessHash: h}
					}
				} else if gid, uuidErr := uuid.Parse(targetID); uuidErr == nil {
					// 定时任务等场景: target 是群组 UUID, 查库取 TGChatID 直发
					if g, gErr := e.groupRepo.GetByID(ctx, task.TenantID, gid); gErr == nil && g.TGChatID != 0 {
						chatID = g.TGChatID
						peer = &tg.InputPeerChannel{ChannelID: g.TGChatID, AccessHash: g.AccessHash}
					}
				} else {
					var resolveErr error
					for attempt := 0; attempt < 3; attempt++ {
						if attempt > 0 {
							backoff := time.Duration(attempt*attempt) * 3 * time.Second
							select {
							case <-ctx.Done():
								return
							case <-time.After(backoff):
							}
						}
						peer, resolveErr = client.ResolvePeer(ctx, targetID)
						if resolveErr == nil {
							break
						}
						if isGlobalFlood(resolveErr) {
							break
						}
					}
					if resolveErr != nil {
						status = "failed"
						errCode = "resolve_peer_failed"
						accFailed++
						e.recordRiskEvent(ctx, task.TenantID, accID, "broadcast_fail", "medium", errCode)
						taskLog.Printf("[broadcast] target %s resolve failed for account %s: %s (target may be invalid or account restricted)\n",
							targetID, accID, resolveErr.Error())
						continue
					}
					switch p := peer.(type) {
					case *tg.InputPeerUser:
						chatID = p.UserID
					case *tg.InputPeerChat:
						chatID = p.ChatID
					case *tg.InputPeerChannel:
						chatID = p.ChannelID
					}
				}
				if chatID == 0 {
					status = "failed"
					errCode = "unsupported peer type"
					accFailed++
					e.recordRiskEvent(ctx, task.TenantID, accID, "broadcast_fail", "medium", errCode)
				} else {
					// Replace template variables with target info before sending
					// 先 spintax 随机语法(每个目标独立), 再变量替换
					rawMsg := message
					if dictBag != nil {
						rawMsg = dictBag.fill(message)
					}
					personalizedMsg := replaceMessageVars(spintaxSpin(rawMsg), peer, targetID)
					if obfEnabled {
						// 混淆每条重新随机 — TG按内容查重时每条都是"不同消息"
						if autoVary {
						personalizedMsg = autoVaryMessage(personalizedMsg)
					}
					personalizedMsg = obfuscateMessage(personalizedMsg)
					}
					// R104+R118: 拟人化 —— 发送前模拟"正在输入..." 1~3 秒再发(风控规避,
					// 真实用户行为: 先打字后发送, 机器人则消息凭空出现)
					// 时长从 3~8s 压到 1~3s: 行为特征保留, 随机发送间隔已承担主要防封作用
					if tp, ok := peer.(tg.InputPeerClass); ok {
						_, _ = client.Raw().MessagesSetTyping(ctx, &tg.MessagesSetTypingRequest{Peer: tp, Action: &tg.SendMessageTypingAction{}})
					} else if chatID < 0 {
						_, _ = client.Raw().MessagesSetTyping(ctx, &tg.MessagesSetTypingRequest{Peer: &tg.InputPeerChat{ChatID: chatID}, Action: &tg.SendMessageTypingAction{}})
					}
					time.Sleep(time.Duration(1000+rand.IntN(2000)) * time.Millisecond)

					// R119-fix: 发送前最后一步 —— Markdown 转义兜底(此时 spintax/变量/混淆已全部解析,
					// 转义后的 \{ \} 不再参与语法匹配, 用户写的 {你好|Hi} 能正常随机取值)
					personalizedMsg = escapeTelegramMarkdown(personalizedMsg)

					// 统一发送入口: 富媒体走 SendMedia, 纯文本走 SendMessage, 均支持 URL 按钮
					// trySend 同时提取 TG 返回的 message_id 用于发送结果可核对
					trySend := func() (int64, error) {
						if mediaSource != "" {
							if err := e.sendBroadcastMedia(ctx, client, chatID, personalizedMsg, mediaType, mediaSource, urlButtons); err != nil {
								return 0, err
							}
							return 0, nil // 富媒体路径 message_id 提取待后续增强
						}
						// 协议号网络卡死时 SendMessage 会无限阻塞，包超时让失败可见
						sCtx, sCancel := context.WithTimeout(ctx, 60*time.Second)
						ret, err := client.SendMessage(sCtx, chatID, personalizedMsg, broadcastSendOpts(urlButtons)...)
						sCancel()
						if err != nil {
							return 0, err
						}
						return extractMessageID(ret), nil
					}
					sentMsgID, sendErr = trySend()
					if sendErr != nil {
						status = "failed"
						errCode = "send_failed"
						accFailed++
						errStr := strings.ToLower(sendErr.Error())
						switch {
						case isGlobalFlood(sendErr):
							waitSec := extractFloodSeconds(errStr)
							if waitSec >= 60 {
								e.markAccountRestricted(ctx, task.TenantID, accID, "broadcast_flood_wait", sendErr.Error(), waitSec)
							} else if waitSec > 0 {
								// Light flood: wait and retry once
								select {
								case <-ctx.Done():
									return
								case <-time.After(time.Duration(waitSec) * time.Second):
								}
								retryMsgID, retryErr := trySend()
								if retryErr != nil {
									e.recordRiskEvent(ctx, task.TenantID, accID, "broadcast_fail", "medium", retryErr.Error())
								} else {
									if retryMsgID != 0 {
										sentMsgID = retryMsgID
									}
									accDone++
									// Retry succeeded: decrement failed count, but guard against negative
									if accFailed > 0 {
										accFailed--
									}
									status = "success"
									errCode = ""
								}
							}
						case isGroupLevelRestriction(sendErr):
							// Group-level restriction: skip this target only, do not mark account
							e.recordRiskEvent(ctx, task.TenantID, accID, "broadcast_group_restricted", "low", sendErr.Error())
						case isAccountBanned(sendErr):
							e.markAccountBanned(ctx, task.TenantID, accID, "account_banned", sendErr.Error())
						default:
							e.recordRiskEvent(ctx, task.TenantID, accID, "broadcast_fail", "medium", sendErr.Error())
						}
					} else {
						accDone++
					}
				}

				// 获取目标名称
				targetName := targetID
				if peer != nil {
					switch p := peer.(type) {
					case *tg.InputPeerChannel:
						if p.ChannelID != 0 {
							if t := titleByChatID[p.ChannelID]; t != "" {
								targetName = fmt.Sprintf("频道(%s)", t)
							} else {
								targetName = fmt.Sprintf("频道(%d)", p.ChannelID)
							}
						}
					case *tg.InputPeerChat:
						if p.ChatID != 0 {
							if t := titleByChatID[p.ChatID]; t != "" {
								targetName = fmt.Sprintf("群组(%s)", t)
							} else {
								targetName = fmt.Sprintf("群组(%d)", p.ChatID)
							}
						}
					case *tg.InputPeerUser:
						if p.UserID != 0 {
							// 私聊: 优先显示联系人缓存里的昵称
							if n := contactNameByID[p.UserID]; n != "" {
								targetName = fmt.Sprintf("私聊(%s)", n)
							} else {
								targetName = fmt.Sprintf("用户(%d)", p.UserID)
							}
						}
					}
				}
				tl := &domain.TaskLog{
					LogID:       uuid.New(),
					TaskID:      task.TaskID,
					TenantID:    task.TenantID,
					AccountID:   accID,
					TargetID:    targetID,
					TargetName:  targetName,
					Action:      "send_message",
					Status:      status,
					// 2026-10-21: 目标级耗时——用户要的完整日志 时间-群名-状态-耗时
					Message: fmt.Sprintf("耗时 %.1fs", time.Since(targetStart).Seconds()),
					ErrorCode:   errCode,
					TGMessageID: sentMsgID,
					ExecutedAt:  time.Now(),
				}

				progressDelta := domain.TaskProgress{Done: 0, Failed: 0, CurrentTarget: targetID}
				if status == "success" {
					progressDelta.Done = 1
					// R103: 配额功能整体下架(租户决策: 此类限制无意义), 不再记录
				} else {
					progressDelta.Failed = 1
				}

				mu.Lock()
				batchLogs = append(batchLogs, tl)
				done += progressDelta.Done
				failed += progressDelta.Failed
				progressAccum.Done += progressDelta.Done
				progressAccum.Failed += progressDelta.Failed
				progressAccum.CurrentTarget = targetID
				// P1-47: 成功即本地登记, 不必等 flush 落库。
				// P0-12: 登记同样用复合键——账号维度各自防重, cartesian 语义完整保留
				if status == "success" {
					successSet[accID.String()+"|"+targetID] = true
				}
				mu.Unlock()

				// Throttled SSE: accumulate into pending buffer, flushed by aggregator goroutine
				// Prevents memory explosion from per-message SSE channel writes
				mu.Lock()
				pendingLogs = append(pendingLogs, tl)
				pendingProgress = domain.TaskProgress{
					Total:         total,
					Done:          done,
					Failed:        failed,
					CurrentTarget: targetID,
				}
				mu.Unlock()

				// Batch flush progress to DB every N messages (reduces DB writes)
				if (i+1)%progressFlushThreshold == 0 || i == len(targets)-1 {
					mu.Lock()
					accum := progressAccum
					progressAccum = domain.TaskProgress{} // reset
					mu.Unlock()
					if err := e.repo.UpdateProgress(ctx, task.TenantID, task.TaskID, accum); err != nil {
						taskLog.Printf("progress update failed: %v\n", err)
					}
				}
				// Use account's actual level for send interval (循环外已查好 accLevel)
				waitDur := e.accEngine.SendIntervalWithGuard(accLevel, time.Duration(taskMinInterval)*time.Second, time.Duration(taskMaxInterval)*time.Second)
				// 等待日志降噪(2026-10-21): 只在真的限速等待(>=1s)时写一行,
				// 文案说清"多久后进下一目标"; 0 秒等待是常态不再刷噪音。
				if waitDur >= time.Second {
					waitLog := &domain.TaskLog{LogID: uuid.New(), TaskID: task.TaskID, TenantID: task.TenantID, TargetID: targetID, Action: "send_wait", Status: "info", Message: fmt.Sprintf("⏳ 限速中，%d 秒后进入下一目标", int(waitDur.Seconds())), ExecutedAt: time.Now()}
					mu.Lock()
					batchLogs = append(batchLogs, waitLog)
					mu.Unlock()
					e.publishLog(task.TenantID, task.TaskID, waitLog)
				}
				// R118: 等待期倒计时推送 —— 等待窗口内每秒推一帧 task_wait
				// (走 SSE 即时通道不落库), 前端实时显示"账号X 还剩N秒→下一目标",
				// 等待从死寂变呼吸。防封节奏不变, 只是让等待过程可见。
				if waitDur >= 2*time.Second {
					ticker := time.NewTicker(time.Second)
					remaining := int(waitDur.Seconds())
					for remaining > 0 {
						select {
						case <-ctx.Done():
							ticker.Stop()
							return
						case <-ticker.C:
							remaining--
							e.publishWaitFrame(task.TenantID, task.TaskID, accID.String(), targetID, remaining, i+1, len(targets))
						}
					}
					ticker.Stop()
				} else {
					select {
					case <-ctx.Done():
						return
					case <-time.After(waitDur):
					}
				}
			}

			mu.Lock()
			accountProgress[accID] = &domain.AccountProgress{
				AccountID: accID,
				Done:      accDone,
				Failed:    accFailed,
				Total:     len(targets),
			}
			mu.Unlock()
		})
	}
	wg.Wait()
	close(flushDone)
	close(sseDone)

	// Final SSE flush: push any remaining pending data
	mu.Lock()
	logs := pendingLogs
	prog := pendingProgress
	mu.Unlock()
	if len(logs) > 0 {
		for _, l := range logs {
			e.publishLog(task.TenantID, task.TaskID, l)
		}
	}
	if prog.Total > 0 || prog.Done > 0 || prog.Failed > 0 {
		e.publishProgress(task.TenantID, task.TaskID, prog)
	}

	// Final flush of remaining logs
	mu.Lock()
	if len(batchLogs) > 0 {
		logs := batchLogs
		batchLogs = nil
		mu.Unlock()
		if err := e.logRepo.CreateBatch(ctx, logs); err != nil {
			taskLog.Printf("[task] final flush logs failed: %v", err)
		}
	} else {
		mu.Unlock()
	}

	// Build account_progress slice
	accProgList := make([]domain.AccountProgress, 0, len(accountProgress))
	for _, ap := range accountProgress {
		accProgList = append(accProgList, *ap)
	}

	// Set final exact progress and status
	finalProgress := domain.TaskProgress{
		Total:           total,
		Done:            done,
		Failed:          failed,
		AccountProgress: accProgList,
	}
	if err := e.repo.UpdateProgressExact(ctx, task.TenantID, task.TaskID, finalProgress); err != nil {
		log.Printf("[broadcast] 更新进度失败: %v", err)
	}
	e.publishProgress(task.TenantID, task.TaskID, finalProgress)

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

// replaceMessageVars replaces template variables in the message with actual target info.
// Supported variables: {{first_name}}, {{group_name}}, {{user_id}}, {{now}}
// dictVarRe 匹配 {{词典名}}(与现有 {{first_name}} 同风格; 单花括号留给 spintax)
var dictVarRe = regexp.MustCompile(`\{\{([A-Za-z0-9_\-\x{4e00}-\x{9fa5}]{1,32})\}\}`)

// dictBag 文案变量替换池: 任务开始时一次加载, 发送时每个目标随机取词
type dictBag struct{ pools map[string][]string }

// buildDictBag 解析消息里的 {{词典名}} 并预载词条; 无词典 repo 或无变量时返回 nil
func (e *Engine) buildDictBag(ctx context.Context, tenantID uuid.UUID, message string) *dictBag {
	if e.dictRepo == nil || !strings.Contains(message, "{{") {
		return nil
	}
	names := map[string]bool{}
	for _, m := range dictVarRe.FindAllStringSubmatch(message, -1) {
		names[m[1]] = true
	}
	if len(names) == 0 {
		return nil
	}
	ns := make([]string, 0, len(names))
	for n := range names {
		ns = append(ns, n)
	}
	pools, err := e.dictRepo.WordsByNames(ctx, tenantID, ns)
	if err != nil || len(pools) == 0 {
		return nil
	}
	return &dictBag{pools: pools}
}

// fill 把消息里的 {{词典名}} 随机替换为词条(同一消息多处同名变量各取各的, 最大化差异化)
func (b *dictBag) fill(msg string) string {
	return dictVarRe.ReplaceAllStringFunc(msg, func(m string) string {
		name := dictVarRe.FindStringSubmatch(m)[1]
		ws := b.pools[name]
		if len(ws) == 0 {
			return m // 词典不存在/空 → 保留原文, 不影响发送
		}
		return ws[rand.IntN(len(ws))]
	})
}

// autoVarySynonyms 内置同义变换池: 命中即随机换, 让每条消息措辞不同(无需用户配置)
var autoVarySynonyms = map[string][]string{
	"你好":   {"您好", "你好", "Hi", "哈喽", "嗨"},
	"您好":   {"你好", "您好", "Hi", "哈喽"},
	"欢迎":   {"欢迎", "歡迎", "喜迎"},
	"加入":   {"加入", "进", "加入", "入"},
	"进群":   {"进群", "加群", "加入群聊"},
	"免费":   {"免费", "免費", "无偿"},
	"需要":   {"需要", "要", "想用"},
	"可以":   {"可以", "能", "可"},
	"点击":   {"点击", "点", "戳"},
	"了解":   {"了解", "看下", "看看"},
	"回复":   {"回复", "回", "回信"},
	"咨询":   {"咨询", "问", "询问"},
	"感谢":   {"感谢", "谢谢", "谢啦"},
	"现在":   {"现在", "眼下", "如今"},
}

// varyPuncts 标点抖动: 句末语气词随机后缀
var varySuffixes = []string{"", "", "！", "~", "～", " 😊", " 👍", " 🌟"}

// autoVaryMessage 自动内容变化(一键开关, 面向不懂语法的租户):
// 同义替换 → 标点/表情后缀抖动 → 随机全半角 → 保留 send 侧既有的零宽字符混淆。
func autoVaryMessage(msg string) string {
	// 1. 同义替换(按 key 长度降序, 先长后短避免部分命中)
	s := msg
	for _, k := range varyKeysByLen {
		if v, ok := autoVarySynonyms[k]; ok && strings.Contains(s, k) {
			s = strings.ReplaceAll(s, k, v[rand.IntN(len(v))])
		}
	}
	// 2. 每个换行段落后随机追加语气后缀(部分段不加, 保持自然)
	lines := strings.Split(s, "\n")
	for i, ln := range lines {
		ln = strings.TrimRight(ln, "。！!~～")
		if ln != "" && rand.IntN(3) > 0 {
			lines[i] = ln + "。" + varySuffixes[rand.IntN(len(varySuffixes))]
		} else if ln != "" {
			lines[i] = ln + "。"
		}
	}
	s = strings.Join(lines, "\n")
	// 3. 随机全角化部分标点
	s = varyFullWidth(s)
	return s
}

// varyKeysByLen 同义词 key 按长度降序预排序
var varyKeysByLen = func() []string {
	ks := make([]string, 0, len(autoVarySynonyms))
	for k := range autoVarySynonyms {
		ks = append(ks, k)
	}
	for i := 0; i < len(ks); i++ {
		for j := i + 1; j < len(ks); j++ {
			if len(ks[j]) > len(ks[i]) {
				ks[i], ks[j] = ks[j], ks[i]
			}
		}
	}
	return ks
}()

// varyFullWidth 把约一半的半角标点随机换成全角
func varyFullWidth(s string) string {
	pairs := map[rune]rune{',': '，', '!': '！', '?': '？', ';': '；', ':': '：'}
	return strings.Map(func(r rune) rune {
		if fw, ok := pairs[r]; ok && rand.IntN(2) == 0 {
			return fw
		}
		return r
	}, s)
}

func replaceMessageVars(message string, peer any, targetID string) string {
	var firstName, groupName, userID string

	switch p := peer.(type) {
	case *tg.InputPeerUser:
		userID = strconv.FormatInt(p.UserID, 10)
		firstName = targetID // fallback to targetID (username) if user info not available
	case *tg.InputPeerChat:
		groupName = targetID
		userID = strconv.FormatInt(p.ChatID, 10)
	case *tg.InputPeerChannel:
		groupName = targetID
		userID = strconv.FormatInt(p.ChannelID, 10)
	default:
		firstName = targetID
		groupName = targetID
		userID = targetID
	}

	// If targetID starts with @, use it as first_name fallback for users
	if strings.HasPrefix(targetID, "@") {
		if firstName == "" {
			firstName = targetID[1:] // remove @
		}
		if groupName == "" {
			groupName = targetID
		}
	}

	result := message
	result = strings.ReplaceAll(result, "{{first_name}}", firstName)
	result = strings.ReplaceAll(result, "{{group_name}}", groupName)
	result = strings.ReplaceAll(result, "{{user_id}}", userID)
	result = strings.ReplaceAll(result, "{{now}}", time.Now().Format("2006-01-02 15:04:05"))
	return result
}

// escapeTelegramMarkdown escapes characters that Telegram interprets as Markdown formatting.
// This prevents user messages from being accidentally formatted.
// 混淆素材池: 常用emoji + 零宽字符(肉眼不可见, TG视为不同消息)
var obfEmojis = []string{"😀", "😊", "👍", "🎉", "🔥", "✨", "💯", "🌟", "🚀", "💪", "😄", "🙌", "❤️", "🤝", "👏"}
var obfZeroWidth = []rune{0x200b, 0x200c, 0x200d, 0xfeff}

func obfuscateMessage(text string) string {
	runes := []rune(text)
	n := 1 + rand.IntN(2)
	for i := 0; i < n; i++ {
		e := obfEmojis[rand.IntN(len(obfEmojis))]
		pos := rand.IntN(len(runes) + 1)
		runes = append(runes[:pos], append([]rune(e), runes[pos:]...)...)
	}
	zn := 1 + rand.IntN(4)
	for i := 0; i < zn; i++ {
		runes = append(runes, obfZeroWidth[rand.IntN(len(obfZeroWidth))])
	}
	return string(runes)
}

func escapeTelegramMarkdown(text string) string {
	// Characters that need escaping in Telegram MarkdownV2:
	// _ * [ ] ( ) ~ ` > # + - = | { } . !
	replacer := strings.NewReplacer(
		"_", "\\_",
		"*", "\\*",
		"[", "\\[",
		"]", "\\]",
		"(", "\\(",
		")", "\\)",
		"~", "\\~",
		"`", "\\`",
		">", "\\>",
		"#", "\\#",
		"+", "\\+",
		"-", "\\-",
		"=", "\\=",
		"|", "\\|",
		"{", "\\{",
		"}", "\\}",
		".", "\\.",
		"!", "\\!",
	)
	return replacer.Replace(text)
}

// urlButton 是群发消息附加的纯 URL 跳转按钮(不做回调)
type urlButton struct{ Text, URL string }

// broadcastSendOpts 构建发送选项; 有按钮时附带内联键盘
// extractMessageID 从 mtgo SendMessage 的返回中提取 message_id.
// mtgo 返回值类型随版本变化(int64/指针/结构体), 用 any 接收 + type switch
// + json 兜底, 对任何返回类型编译安全.
func extractMessageID(v any) int64 {
	switch m := v.(type) {
	case nil:
		return 0
	case int64:
		return m
	case int:
		return int64(m)
	case int32:
		return int64(m)
	}
	b, err := json.Marshal(v)
	if err != nil {
		return 0
	}
	var obj struct {
		ID        int64 `json:"id"`
		MessageID int64 `json:"message_id"`
	}
	if err := json.Unmarshal(b, &obj); err != nil {
		return 0
	}
	if obj.ID != 0 {
		return obj.ID
	}
	return obj.MessageID
}

func broadcastSendOpts(buttons []urlButton) []*params.SendMessage {
	if len(buttons) == 0 {
		return []*params.SendMessage{{}}
	}
	rows := make([][]params.KeyboardButton, 0, len(buttons))
	for _, b := range buttons {
		rows = append(rows, []params.KeyboardButton{params.ButtonURL(b.Text, b.URL)})
	}
	return []*params.SendMessage{{ReplyMarkup: params.InlineKeyboard(rows...)}}
}

// sendBroadcastMedia 发送图文/视频消息。
// source 为 http(s) URL 时由 Telegram 远端抓取; 为相对路径时从媒体目录读取上传。
func (e *Engine) sendBroadcastMedia(ctx context.Context, client *telegram.Client, chatID int64, caption, mediaType, mediaSource string, buttons []urlButton) error {
	opts := broadcastSendOpts(buttons)
	var media tg.InputMediaClass
	if strings.HasPrefix(mediaSource, "http://") || strings.HasPrefix(mediaSource, "https://") {
		if mediaType == "video" {
			media = &tg.InputMediaDocumentExternal{URL: mediaSource}
		} else {
			media = &tg.InputMediaPhotoExternal{URL: mediaSource}
		}
	} else {
		// 防路径穿越校验抽成纯函数 validateMediaPath，便于表驱动测试
		path, verr := validateMediaPath(e.cfg.MediaDir, mediaSource)
		if verr != nil {
			return verr
		}
		f, err := os.Open(path)
		if err != nil {
			return fmt.Errorf("打开媒体文件失败: %w", err)
		}
		defer f.Close()
		fi, err := f.Stat()
		if err != nil {
			return err
		}
		up, err := client.UploadFile(ctx, f, filepath.Base(path), fi.Size(), nil)
		if err != nil {
			return fmt.Errorf("上传媒体失败: %w", err)
		}
		if mediaType == "video" {
			media = &tg.InputMediaUploadedDocument{File: up.File, MimeType: mime.TypeByExtension(filepath.Ext(path))}
		} else {
			media = &tg.InputMediaUploadedPhoto{File: up.File}
		}
	}
	_, err := client.SendMedia(ctx, chatID, media, caption, opts...)
	return err
}

// spintaxSpin 处理 {a|b|c} 随机语法, 每个目标独立随机选取, 防文案雷同进垃圾箱。
// 只匹配含 | 的花括号段, 不会误伤 {first_name} 这类变量。
var spintaxRe = regexp.MustCompile(`\{([^{}]*\|[^{}]*)\}`)

func spintaxSpin(s string) string {
	return spintaxRe.ReplaceAllStringFunc(s, func(m string) string {
		inner := m[1 : len(m)-1]
		parts := strings.Split(inner, "|")
		if len(parts) == 0 {
			return m
		}
		return strings.TrimSpace(parts[rand.IntN(len(parts))])
	})
}
