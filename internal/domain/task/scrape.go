package task

import (
	"context"
	"fmt"
	"runtime/debug"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"tg-sender/internal/domain"

	"github.com/google/uuid"
	"github.com/mtgo-labs/mtgo/telegram"
	"github.com/mtgo-labs/mtgo/telegram/types"
	"github.com/mtgo-labs/mtgo/tg"
)

func (e *Engine) executeScrapeMembers(ctx context.Context, task *domain.Task) {
	// 整体超时: 8分钟, 防止TG请求石沉大海导致任务永远卡运行中
	ctx, cancel := context.WithTimeout(ctx, 8*time.Minute)
	defer cancel()
	taskLog.Printf("[scrape] 成员采集开始: task=%s group=%v", task.TaskID, task.TargetIDs)
	defer func() {
		if r := recover(); r != nil {
			taskLog.Printf("executeScrapeMembers panic: %v\n%s\n", r, debug.Stack())
			if err := e.repo.UpdateStatus(ctx, task.TenantID, task.TaskID, domain.TaskStatusFailed); err != nil {
				taskLog.Printf("[task] UpdateStatus failed: %v", err)
			}
		}
	}()
	groupIDStr := ""
	if len(task.TargetIDs) > 0 {
		groupIDStr = task.TargetIDs[0]
	}
	if groupIDStr == "" || len(task.AccountIDs) == 0 {
		if err := e.repo.UpdateStatus(ctx, task.TenantID, task.TaskID, domain.TaskStatusFailed); err != nil {
			taskLog.Printf("[task] UpdateStatus failed: %v", err)
		}
		return
	}

	var groupID uuid.UUID
	var payloadGroupID string
	// 采集过滤条件: recent_days=N (仅最近N天上线), has_photo=true (仅带头像)
	filterRecentDays, filterHasPhoto := parseScrapeFilter(task.Payload)

	if v, ok := task.Payload["group_id"]; ok {
		payloadGroupID = fmt.Sprint(v)
	}
	if payloadGroupID != "" && payloadGroupID != "<nil>" {
		var parseErr error
		groupID, parseErr = uuid.Parse(payloadGroupID)
		if parseErr != nil {
			if err := e.repo.UpdateStatus(ctx, task.TenantID, task.TaskID, domain.TaskStatusFailed); err != nil {
				taskLog.Printf("[task] UpdateStatus failed: %v", err)
			}
			return
		}
	}
	if groupID == uuid.Nil {
		if err := e.repo.UpdateStatus(ctx, task.TenantID, task.TaskID, domain.TaskStatusFailed); err != nil {
			taskLog.Printf("[task] UpdateStatus failed: %v", err)
		}
		return
	}

	accID := task.AccountIDs[0]

	// Check account status before using it
	acc, err := e.accEngine.GetByID(ctx, task.TenantID, accID)
	if err != nil || acc == nil || (acc.Status == domain.AccountStatusBanned || acc.Status == domain.AccountStatusRestricted || acc.Status == domain.AccountStatusPendingAuth) {
		status := "unknown"
		if acc != nil {
			status = string(acc.Status)
		}
		taskLog.Printf("[scrape] skip account %s: status=%s (not active)", accID, status)
		if err := e.repo.UpdateStatus(ctx, task.TenantID, task.TaskID, domain.TaskStatusFailed); err != nil {
			taskLog.Printf("[task] UpdateStatus failed: %v", err)
		}
		return
	}
	if acc.BlockedUntil != nil && acc.BlockedUntil.After(time.Now().UTC()) {
		taskLog.Printf("[scrape] skip account %s: blocked_until=%s", accID, acc.BlockedUntil.Format(time.RFC3339))
		if err := e.repo.UpdateStatus(ctx, task.TenantID, task.TaskID, domain.TaskStatusFailed); err != nil {
			taskLog.Printf("[task] UpdateStatus failed: %v", err)
		}
		return
	}

	client, err := e.accEngine.GetClient(ctx, task.TenantID, accID)
	if err != nil {
		// 必须记录具体错误, 否则任务静默失败无法排查
		taskLog.Printf("[scrape] 获取客户端失败 account=%s: %v", accID, err)
		if err := e.repo.UpdateStatus(ctx, task.TenantID, task.TaskID, domain.TaskStatusFailed); err != nil {
			taskLog.Printf("[task] UpdateStatus failed: %v", err)
		}
		return
	}
	defer e.accEngine.ReleaseClient(task.TenantID, accID, client)

	// Try to resolve peer using tg_chat_id from database first (UUID cannot be resolved by Telegram)
	var peer any
	var resolveErr error
	if groupID != uuid.Nil && e.groupRepo != nil {
		if g, getErr := e.groupRepo.GetByID(ctx, task.TenantID, groupID); getErr == nil && g != nil {
			// Use tg_chat_id directly — this is the Telegram-native ID, not our UUID
			peer, resolveErr = client.ResolvePeer(ctx, strconv.FormatInt(g.TGChatID, 10))
			if resolveErr != nil && g.AccessHash != 0 {
				// Fallback: construct peer manually with access_hash (more reliable for channels)
				if g.TGChatID < 0 {
					var channelID int64
					absID := -g.TGChatID
					if absID >= 1000000000000 {
						channelID = absID - 1000000000000
					} else {
						channelID = absID
					}
					peer = &tg.InputPeerChannel{
						ChannelID:  channelID,
						AccessHash: g.AccessHash,
					}
					err = nil
				} else {
					peer = &tg.InputPeerChat{ChatID: g.TGChatID}
					err = nil
				}
			}
		}
	}
	// Last resort: try groupIDStr (UUID) — almost always fails but kept for edge cases
	if err != nil || peer == nil {
		peer, err = client.ResolvePeer(ctx, groupIDStr)
	}
	if err != nil {
		taskLog.Printf("[scrape] 解析群 peer 失败 group=%s: %v", groupIDStr, err)
		e.recordRiskEvent(ctx, task.TenantID, accID, "broadcast_fail", "medium", "resolve: "+err.Error())
		if err := e.repo.UpdateStatus(ctx, task.TenantID, task.TaskID, domain.TaskStatusFailed); err != nil {
			taskLog.Printf("[task] UpdateStatus failed: %v", err)
		}
		return
	}

	var allMembers []domain.Member
	// totalScraped 用独立计数器: allMembers 每满一批会 flush 并归零,
	// 不能用 len(allMembers) 判断成败(否则整千数会被误判为失败)。
	totalScraped := 0
	const memberBatchSize = 1000
	flushMembers := func(batch []domain.Member) {
		if len(batch) == 0 || e.memberRepo == nil {
			return
		}
		membersPtr := make([]*domain.Member, len(batch))
		for i := range batch {
			membersPtr[i] = &batch[i]
		}
		if err := e.memberRepo.BatchCreate(ctx, membersPtr); err != nil {
			taskLog.Printf("[task] BatchCreate members failed: %v", err)
		}
	}

	switch p := peer.(type) {
	case *tg.InputPeerChannel:
		offset := int32(0)
		limit := int32(200)
		for {
			req := &tg.ChannelsGetParticipantsRequest{
				Channel: &tg.InputChannel{
					ChannelID:  p.ChannelID,
					AccessHash: p.AccessHash,
				},
				Filter: &tg.ChannelParticipantsRecent{},
				Offset: offset,
				Limit:  limit,
			}
			invokeCtx, invokeCancel := context.WithTimeout(ctx, 60*time.Second)
			result, err := client.Invoke(invokeCtx, req)
			invokeCancel()
			if err != nil {
				e.recordRiskEvent(ctx, task.TenantID, accID, "broadcast_fail", "medium", "fetch: "+err.Error())
				break
			}
			participants, ok := result.(*tg.ChannelsChannelParticipants)
			if !ok {
				break
			}
			userMap := make(map[int64]*tg.User)
			for _, u := range participants.Users {
				if user, ok := u.(*tg.User); ok && user != nil {
					userMap[user.ID] = user
				}
			}
			for _, part := range participants.Participants {
				var userID int64
				switch participant := part.(type) {
				case *tg.ChannelParticipant:
					userID = participant.UserID
				case *tg.ChannelParticipantAdmin:
					userID = participant.UserID
				case *tg.ChannelParticipantCreator:
					userID = participant.UserID
				case *tg.ChannelParticipantSelf:
					userID = participant.UserID
				default:
					continue
				}
				if userID == 0 {
					continue
				}
				user := userMap[userID]
				// 自动跳过机器人账号: 采集结果不保存 bot
				if user != nil && user.Bot {
					continue
				}
				username, lastSeen, hasPhoto, memberBio := memberActivity(user)
				// 采集过滤: 死号/无头像不入库（与下方 Chat 分支保持一致，
				// 此前 Channel 分支漏了过滤，死号浪费发送配额）
				if filterRecentDays > 0 && (lastSeen == nil || time.Since(*lastSeen) > time.Duration(filterRecentDays)*24*time.Hour) {
					continue
				}
				if filterHasPhoto && !hasPhoto {
					continue
				}
				allMembers = append(allMembers, domain.Member{
					TenantID:  task.TenantID,
					GroupID:   groupID,
					TGUserID:  userID,
					Username:  username,
					Bio:       memberBio,
					LastSeen:  lastSeen,
					HasPhoto:  hasPhoto,
					ScrapedAt: time.Now(),
				})
				totalScraped++
				if len(allMembers) >= memberBatchSize {
					flushMembers(allMembers)
					allMembers = allMembers[:0]
				}
			}
			if int32(len(participants.Participants)) < limit {
				break
			}
			offset += int32(len(participants.Participants))

			select {
			case <-e.stopCh:
				// P2-42 审计修复: 原来直接 return——采集缓冲(allMembers)里剩余的
				// 成员(不满一批的)丢失。先 flush 再退出。
				if len(allMembers) > 0 {
					flushMembers(allMembers)
				}
				if err := e.repo.UpdateStatus(ctx, task.TenantID, task.TaskID, domain.TaskStatusPaused); err != nil {
					taskLog.Printf("[task] UpdateStatus failed: %v", err)
				}
				return
			case <-ctx.Done():
				// P2-42 审计修复: 同 stopCh——取消前 flush 剩余成员, 不丢数据
				if len(allMembers) > 0 {
					flushMembers(allMembers)
				}
				return
			default:
			}
		}

	case *tg.InputPeerChat:
		req := &tg.MessagesGetFullChatRequest{ChatID: p.ChatID}
		invokeCtx, invokeCancel := context.WithTimeout(ctx, 60*time.Second)
		result, err := client.Invoke(invokeCtx, req)
		invokeCancel()
		if err != nil {
			e.recordRiskEvent(ctx, task.TenantID, accID, "broadcast_fail", "medium", "fetch: "+err.Error())
			break
		}
		chatFullResult, ok := result.(*tg.MessagesChatFull)
		if !ok {
			break
		}
		if chatFull, ok := chatFullResult.FullChat.(*tg.ChatFull); ok && chatFull.Participants != nil {
			if chatParticipants, ok := chatFull.Participants.(*tg.ChatParticipants); ok {
				userMap := make(map[int64]*tg.User)
				for _, u := range chatFullResult.Users {
					if user, ok := u.(*tg.User); ok && user != nil {
						userMap[user.ID] = user
					}
				}
				for _, part := range chatParticipants.Participants {
					var userID int64
					switch participant := part.(type) {
					case *tg.ChatParticipant:
						userID = participant.UserID
					case *tg.ChatParticipantAdmin:
						userID = participant.UserID
					case *tg.ChatParticipantCreator:
						userID = participant.UserID
					default:
						continue
					}
					if userID == 0 {
						continue
					}
					user := userMap[userID]
					// 自动跳过机器人账号: 采集结果不保存 bot
					if user != nil && user.Bot {
						continue
					}
					username, lastSeen, hasPhoto, memberBio := memberActivity(user)
					// 采集过滤: 死号/无头像不入库, 不浪费发送配额
					if filterRecentDays > 0 && (lastSeen == nil || time.Since(*lastSeen) > time.Duration(filterRecentDays)*24*time.Hour) {
						continue
					}
					if filterHasPhoto && !hasPhoto {
						continue
					}
					allMembers = append(allMembers, domain.Member{
						TenantID:  task.TenantID,
						GroupID:   groupID,
						TGUserID:  userID,
						Username:  username,
						Bio:       memberBio,
						LastSeen:  lastSeen,
						HasPhoto:  hasPhoto,
						ScrapedAt: time.Now(),
					})
					totalScraped++
					if len(allMembers) >= memberBatchSize {
						flushMembers(allMembers)
						allMembers = allMembers[:0]
					}
				}
			}
		}

	default:
		e.recordRiskEvent(ctx, task.TenantID, accID, "broadcast_fail", "medium", "peer is not a group or channel")
	}

	memberCount := totalScraped
	flushMembers(allMembers)

	if memberCount == 0 {
		// P2-43 审计修复: 空群(0 成员)是合法采集结果——原来标 failed, 空群永远显示失败。
		// 空群/全员隐私设置的群按"成功完成(0 结果)"处理。
		finalProgress := domain.TaskProgress{Total: 0, Done: 0, Failed: 0}
		if err := e.repo.UpdateProgressExact(ctx, task.TenantID, task.TaskID, finalProgress); err != nil {
			taskLog.Printf("[task] UpdateProgressExact failed: %v", err)
		}
		e.publishProgress(task.TenantID, task.TaskID, finalProgress)
		if err := e.repo.UpdateStatus(ctx, task.TenantID, task.TaskID, domain.TaskStatusCompleted); err != nil {
			taskLog.Printf("[task] UpdateStatus failed: %v", err)
		}
		if err := e.logRepo.Create(ctx, &domain.TaskLog{
			TaskID:     task.TaskID,
			TenantID:   task.TenantID,
			AccountID:  accID,
			TargetID:   "",
			Action:     "scrape_members",
			Status:     "not_supported",
			ErrorCode:  "channel_members_not_found",
			ExecutedAt: time.Now(),
		}); err != nil {
			taskLog.Printf("[scrape] log create failed: %v", err)
		}
		e.publishLog(task.TenantID, task.TaskID, &domain.TaskLog{
			TaskID: task.TaskID, TenantID: task.TenantID, AccountID: accID,
			Action: "scrape_members", Status: "not_supported",
			ErrorCode: "channel_members_not_found", ExecutedAt: time.Now(),
		})
		return
	}

	finalProgress := domain.TaskProgress{Total: memberCount, Done: memberCount, Failed: 0}
	if err := e.repo.UpdateProgressExact(ctx, task.TenantID, task.TaskID, finalProgress); err != nil {
		taskLog.Printf("[task] UpdateProgressExact failed: %v", err)
	}
	e.publishProgress(task.TenantID, task.TaskID, finalProgress)
	if err := e.repo.UpdateStatus(ctx, task.TenantID, task.TaskID, domain.TaskStatusCompleted); err != nil {
		taskLog.Printf("[task] UpdateStatus failed: %v", err)
	}
}

func (e *Engine) executeScrapeLinks(ctx context.Context, task *domain.Task) {
	defer func() {
		if r := recover(); r != nil {
			taskLog.Printf("executeScrapeLinks panic: %v\n%s\n", r, debug.Stack())
			if err := e.repo.UpdateStatus(ctx, task.TenantID, task.TaskID, domain.TaskStatusFailed); err != nil {
				taskLog.Printf("[task] UpdateStatus failed: %v", err)
			}
		}
	}()
	if len(task.AccountIDs) == 0 {
		if err := e.repo.UpdateStatus(ctx, task.TenantID, task.TaskID, domain.TaskStatusFailed); err != nil {
			taskLog.Printf("[task] UpdateStatus failed: %v", err)
		}
		return
	}

	accID := task.AccountIDs[0]

	// Check account status before using it
	acc, err := e.accEngine.GetByID(ctx, task.TenantID, accID)
	if err != nil || acc == nil || (acc.Status == domain.AccountStatusBanned || acc.Status == domain.AccountStatusRestricted || acc.Status == domain.AccountStatusPendingAuth) {
		status := "unknown"
		if acc != nil {
			status = string(acc.Status)
		}
		taskLog.Printf("[scrape] skip account %s: status=%s (not active)", accID, status)
		if err := e.repo.UpdateStatus(ctx, task.TenantID, task.TaskID, domain.TaskStatusFailed); err != nil {
			taskLog.Printf("[task] UpdateStatus failed: %v", err)
		}
		return
	}
	if acc.BlockedUntil != nil && acc.BlockedUntil.After(time.Now().UTC()) {
		taskLog.Printf("[scrape] skip account %s: blocked_until=%s", accID, acc.BlockedUntil.Format(time.RFC3339))
		if err := e.repo.UpdateStatus(ctx, task.TenantID, task.TaskID, domain.TaskStatusFailed); err != nil {
			taskLog.Printf("[task] UpdateStatus failed: %v", err)
		}
		return
	}

	client, err := e.accEngine.GetClient(ctx, task.TenantID, accID)
	if err != nil {
		// 必须记录具体错误, 否则任务静默失败无法排查
		taskLog.Printf("[scrape] 获取客户端失败 account=%s: %v", accID, err)
		if err := e.repo.UpdateStatus(ctx, task.TenantID, task.TaskID, domain.TaskStatusFailed); err != nil {
			taskLog.Printf("[task] UpdateStatus failed: %v", err)
		}
		return
	}
	defer e.accEngine.ReleaseClient(task.TenantID, accID, client)

	var privateHashes []string
	var publicGroups []map[string]interface{}
	var mu sync.Mutex

	var batchLogs []*domain.TaskLog
	var logMu sync.Mutex

	handlerID := uuid.New().String()
	entry := &scrapeHandlerEntry{active: 1}
	scrapeHandlerRegistry.Store(handlerID, entry)

	// P2止血(精读发现): mtgo OnMessage 无摘除API, 本闭包会永久挂在池化客户端上
	// 并被后续任务复用。任务结束后把拽住的大状态置 nil, 把每个残留 handler
	// 的内存压到最小, 阻止内存随完成任务数无限增长。
	defer func() {
		// 持锁置 nil: handler 可能迟到触发, 裸置会与它竞争
		mu.Lock()
		privateHashes = nil
		publicGroups = nil
		mu.Unlock()
		logMu.Lock()
		batchLogs = nil
		logMu.Unlock()
	}()

	client.OnMessage(func(c *telegram.Client, msg *types.Message) {
		val, ok := scrapeHandlerRegistry.Load(handlerID)
		if !ok || atomic.LoadInt32(&val.(*scrapeHandlerEntry).active) == 0 {
			return
		}

		select {
		case <-e.stopCh:
			return
		case <-ctx.Done():
			return
		default:
		}

		if msg == nil || msg.Text == "" {
			return
		}

		if msg.ChatID >= 0 {
			return
		}

		matches := tmeLinkRe.FindAllStringSubmatch(msg.Text, -1)
		for _, match := range matches {
			if len(match) < 5 {
				continue
			}
			path := match[4]
			// Preserve joinchat/ prefix for private links
			if match[3] != "" {
				path = match[3] + path
			}
			fullLink := "https://t.me/" + path

			privateMatch := privateLinkRe.FindStringSubmatch(fullLink)
			if len(privateMatch) > 1 {
				hash := privateMatch[len(privateMatch)-1]

				mu.Lock()
				exists := false
				for _, existing := range privateHashes {
					if existing == hash {
						exists = true
						break
					}
				}
				if !exists {
					privateHashes = append(privateHashes, hash)
				}
				mu.Unlock()

				logMu.Lock()
				batchLogs = append(batchLogs, &domain.TaskLog{
					LogID:      uuid.New(),
					TaskID:     task.TaskID,
					TenantID:   task.TenantID,
					AccountID:  accID, // 修复(2026-09-22): 缺失会导致 task_logs FK 冲突, 整批日志丢失
					TargetID:   hash,
					Action:     "scrape_private_link",
					Status:     "success",
					ExecutedAt: time.Now(),
				})
				logMu.Unlock()
			} else {
				username := path
				if strings.HasPrefix(username, "@") {
					username = username[1:]
				}

				peer, resolveErr := client.ResolvePeer(ctx, username)
				if resolveErr != nil {
					taskLog.Printf("[scrape] ResolvePeer failed for %s: %v", username, resolveErr)
					continue
				}

				var chatID int64
				switch p := peer.(type) {
				case *tg.InputPeerChat:
					chatID = p.ChatID
				case *tg.InputPeerChannel:
					chatID = p.ChannelID
				default:
					continue
				}
				if chatID == 0 {
					continue
				}

				// Database deduplication: skip if already in groups table
				if e.groupRepo != nil {
					if existing, _ := e.groupRepo.GetByTGChatID(ctx, task.TenantID, chatID); existing != nil {
						continue
					}
				}

				mu.Lock()
				exists := false
				for _, existing := range publicGroups {
					if existing["username"] == username {
						exists = true
						break
					}
				}
				if !exists {
					publicGroups = append(publicGroups, map[string]interface{}{
						"username": username,
						"chat_id":  chatID,
					})
				}
				mu.Unlock()

				logMu.Lock()
				batchLogs = append(batchLogs, &domain.TaskLog{
					LogID:      uuid.New(),
					TaskID:     task.TaskID,
					TenantID:   task.TenantID,
					AccountID:  accID,
					TargetID:   username,
					Action:     "scrape_public_group",
					Status:     "success",
					ExecutedAt: time.Now(),
				})
				logMu.Unlock()
			}
		}
	})

	timeout := e.cfg.ScrapeLinksTimeout
	var d float64
	if v, ok := task.Payload["duration_minutes"]; ok {
		switch val := v.(type) {
		case float64:
			d = val
		case int:
			d = float64(val)
		case string:
			var parseErr error
			d, parseErr = strconv.ParseFloat(val, 64)
			if parseErr != nil {
				d = 0
			}
		}
	}
	if d > 0 {
		if d > 1440 { // 1440 分钟 = 24 小时，防"监听一个月"式资源滥用
			d = 1440
		}
		timeout = time.Duration(d) * time.Minute
	}

	select {
	case <-e.stopCh:
		atomic.StoreInt32(&entry.active, 0)
		scrapeHandlerRegistry.Delete(handlerID)
		goto storeResults
	case <-ctx.Done():
		atomic.StoreInt32(&entry.active, 0)
		scrapeHandlerRegistry.Delete(handlerID)
		goto storeResults
	case <-time.After(timeout):
		atomic.StoreInt32(&entry.active, 0)
		scrapeHandlerRegistry.Delete(handlerID)
		goto storeResults
	}

storeResults:
	logMu.Lock()
	if len(batchLogs) > 0 {
		if err := e.logRepo.CreateBatch(ctx, batchLogs); err != nil {
			taskLog.Printf("[task] CreateBatch logs failed: %v", err)
		}
		batchLogs = nil
	}
	logMu.Unlock()

	mu.Lock()
	privates := make([]string, len(privateHashes))
	copy(privates, privateHashes)
	publics := make([]map[string]interface{}, len(publicGroups))
	copy(publics, publicGroups)
	mu.Unlock()

	if len(privates) > 0 {
		task.Payload["private_hashes"] = privates
	}
	if len(publics) > 0 {
		task.Payload["public_groups"] = publics
	}

	totalFound := len(privates) + len(publics)
	finalProgress := domain.TaskProgress{
		Total: totalFound,
		Done:  totalFound,
	}
	if err := e.repo.UpdateProgressExact(ctx, task.TenantID, task.TaskID, finalProgress); err != nil {
		taskLog.Printf("[task] UpdateProgressExact failed: %v", err)
	}
	e.publishProgress(task.TenantID, task.TaskID, finalProgress)

	if totalFound == 0 {
		if err := e.repo.UpdateStatus(ctx, task.TenantID, task.TaskID, domain.TaskStatusFailed); err != nil {
			taskLog.Printf("[task] UpdateStatus failed: %v", err)
		}
	} else {
		if err := e.repo.UpdateStatus(ctx, task.TenantID, task.TaskID, domain.TaskStatusCompleted); err != nil {
			taskLog.Printf("[task] UpdateStatus failed: %v", err)
		}
	}
}

// executeScrapeLinksReport runs as a reporting task in persistent scraping mode.
// The actual listening is done by LinkScraper; this task just reports progress.
func (e *Engine) executeScrapeLinksReport(ctx context.Context, task *domain.Task) {
	defer func() {
		if r := recover(); r != nil {
			taskLog.Printf("executeScrapeLinksReport panic: %v\n%s\n", r, debug.Stack())
		}
	}()

	if len(task.AccountIDs) == 0 {
		e.repo.UpdateStatus(ctx, task.TenantID, task.TaskID, domain.TaskStatusFailed)
		return
	}

	accID := task.AccountIDs[0]
	if e.linkScraper != nil {
		e.linkScraper.RegisterAccount(ctx, task.TenantID, accID)
	}

	// 任务窗口起点: 进度/结果只统计本任务期间采集到的链接,
	// 不能用租户累计总量(CountByTenant)。
	taskStart := time.Now()

	duration := 10 * time.Minute
	if d, ok := task.Payload["duration_minutes"]; ok {
		switch v := d.(type) {
		case float64:
			duration = time.Duration(v) * time.Minute
		case int:
			duration = time.Duration(v) * time.Minute
		}
	}
	if duration > 24*time.Hour { // 上限 24 小时，防监听任务无限期挂占资源
		duration = 24 * time.Hour
	}

	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()
	timeout := time.After(duration)

	for {
		select {
		case <-e.stopCh:
			goto finish
		case <-ctx.Done():
			goto finish
		case <-timeout:
			goto finish
		case <-ticker.C:
			// 统计本任务窗口内实际入库的链接数
			total := 0
			if e.scrapedLinkRepo != nil {
				if cnt, err := e.scrapedLinkRepo.CountByTenantSince(ctx, task.TenantID, taskStart); err == nil {
					total = cnt
				}
			}
			progress := domain.TaskProgress{Total: total, Done: total}
			e.repo.UpdateProgressExact(ctx, task.TenantID, task.TaskID, progress)
			e.publishProgress(task.TenantID, task.TaskID, progress)
		}
	}

finish:
	var finalCount int
	if e.scrapedLinkRepo != nil {
		if cnt, err := e.scrapedLinkRepo.CountByTenantSince(ctx, task.TenantID, taskStart); err == nil {
			finalCount = cnt
		}
	}

	finalProgress := domain.TaskProgress{Total: finalCount, Done: finalCount}
	e.repo.UpdateProgressExact(ctx, task.TenantID, task.TaskID, finalProgress)
	e.publishProgress(task.TenantID, task.TaskID, finalProgress)

	if err := e.repo.UpdateStatus(ctx, task.TenantID, task.TaskID, domain.TaskStatusCompleted); err != nil {
		taskLog.Printf("[task] UpdateStatus failed: %v", err)
	}
	// 注意: 这里【不】注销常驻监听。常驻 LinkScraper 由 accountWatcherLoop
	// 统一管理(每30秒同步), 手动任务结束后把它注销会中断常驻采集,
	// 且残留 handler 条目会导致 watcher 永远无法重新注册。
}

// memberActivity 从用户资料提取 用户名/最近上线时间/是否有头像 (用于死号过滤)
// memberActivity 提取用户名/最后上线/头像标记; bio 为成员简介(简介挖掘的数据源)。
func memberActivity(user *tg.User) (string, *time.Time, bool, string) {
	if user == nil {
		return "", nil, false, ""
	}
	// v0.19 起 tg.User 不再携带 bio(需逐用户 GetFullUser, 采集场景成本过高)。
	// 简介挖掘对空 bio 自动跳过, 这里恒空串优雅降级。
	bio := ""
	var lastSeen *time.Time
	now := time.Now()
	switch st := user.Status.(type) {
	case *tg.UserStatusOnline:
		lastSeen = &now
	case *tg.UserStatusOffline:
		t := time.Unix(int64(st.WasOnline), 0)
		lastSeen = &t
	case *tg.UserStatusRecently:
		t := now.Add(-48 * time.Hour)
		lastSeen = &t
	case *tg.UserStatusLastWeek:
		t := now.Add(-7 * 24 * time.Hour)
		lastSeen = &t
	case *tg.UserStatusLastMonth:
		t := now.Add(-30 * 24 * time.Hour)
		lastSeen = &t
	}
	return user.Username, lastSeen, user.Photo != nil, bio
}

// parseScrapeFilter 解析采集过滤条件 (payload.filter): recent_days=N, has_photo=true
func parseScrapeFilter(payload map[string]any) (int, bool) {
	f, ok := payload["filter"].(map[string]any)
	if !ok {
		return 0, false
	}
	days := 0
	// P2-46 审计修复: 原来只断言 float64——前端传字符串("7")或整数时断言失败,
	// days 静默归 0, "仅最近N天"过滤完全不生效。兼容三种类型。
	switch v := f["recent_days"].(type) {
	case float64:
		days = int(v)
	case string:
		if n, err := strconv.Atoi(v); err == nil {
			days = n
		}
	case int:
		days = v
	}
	photo := false
	if v, ok := f["has_photo"].(bool); ok {
		photo = v
	}
	return days, photo
}

// executeScrapeActiveUsers 历史消息活跃用户采集(隐私群 Participants 只暴露管理员时的替代方案):
// 通过 MessagesGetHistory 翻群历史消息, 从返回的 Users 提取所有发过言的用户(带 AccessHash, 采完即可私聊直发).
func (e *Engine) executeScrapeActiveUsers(ctx context.Context, task *domain.Task) {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Minute)
	defer cancel()
	if len(task.AccountIDs) == 0 {
		e.repo.UpdateStatus(ctx, task.TenantID, task.TaskID, domain.TaskStatusFailed)
		return
	}
	groupID, err := uuid.Parse(fmt.Sprint(task.Payload["group_id"]))
	if err != nil {
		e.repo.UpdateStatus(ctx, task.TenantID, task.TaskID, domain.TaskStatusFailed)
		return
	}
	accID := task.AccountIDs[0]
	maxMessages := 1000
	if v, ok := task.Payload["max_messages"].(float64); ok && v > 0 {
		maxMessages = int(v)
	}

	acc, err := e.accEngine.GetByID(ctx, task.TenantID, accID)
	// 待机号也执行：GetClient 现场建连。只排除真正不可用的状态。
	if err != nil || acc == nil || acc.Status == domain.AccountStatusBanned || acc.Status == domain.AccountStatusRestricted || acc.Status == domain.AccountStatusPendingAuth {
		e.repo.UpdateStatus(ctx, task.TenantID, task.TaskID, domain.TaskStatusFailed)
		return
	}
	client, err := e.accEngine.GetClient(ctx, task.TenantID, accID)
	if err != nil || client == nil {
		e.repo.UpdateStatus(ctx, task.TenantID, task.TaskID, domain.TaskStatusFailed)
		return
	}
	defer e.accEngine.ReleaseClient(task.TenantID, accID, client)

	g, _ := e.groupRepo.GetByID(ctx, task.TenantID, groupID)
	if g == nil {
		e.repo.UpdateStatus(ctx, task.TenantID, task.TaskID, domain.TaskStatusFailed)
		return
	}
	var peer tg.InputPeerClass
	if g.TGChatID < 0 {
		channelID := -g.TGChatID
		if channelID >= 1000000000000 {
			channelID -= 1000000000000
		}
		peer = &tg.InputPeerChannel{ChannelID: channelID, AccessHash: g.AccessHash}
	} else {
		peer = &tg.InputPeerChat{ChatID: g.TGChatID}
	}

	seen := map[int64]bool{}
	var collected []*domain.Member
	lastID, scanned := int32(0), 0
	for scanned < maxMessages {
		req := &tg.MessagesGetHistoryRequest{Peer: peer, Limit: 100, OffsetID: lastID}
		var res tg.MessagesClass
		var err error
		if res, err = client.Raw().MessagesGetHistory(ctx, req); err != nil {
			taskLog.Printf("[scrape] GetHistory 失败(offset=%d): %v", lastID, err)
			break
		}
		// P0-15 审计修复: 原来只断言 *tg.MessagesMessages——频道/超级群的 GetHistory
		// 返回 *tg.MessagesChannelMessages, 断言失败直接 break, 频道历史采集恒为 0。
		var msgs []tg.MessageClass
		var users []tg.UserClass
		switch m := res.(type) {
		case *tg.MessagesMessages:
			msgs, users = m.Messages, m.Users
		case *tg.MessagesChannelMessages:
			msgs, users = m.Messages, m.Users
		case *tg.MessagesMessagesSlice:
			msgs, users = m.Messages, m.Users
		default:
			taskLog.Printf("[scrape] GetHistory 返回未知类型 %T, 停止采集", res)
			break
		}
		mm := struct {
			Messages []tg.MessageClass
			Users    []tg.UserClass
		}{Messages: msgs, Users: users}
		if len(msgs) == 0 {
			break
		}
		for _, u := range mm.Users {
			user, ok := u.(*tg.User)
			if !ok || user.Bot || user.ID == 0 || seen[user.ID] {
				continue
			}
			seen[user.ID] = true
			collected = append(collected, &domain.Member{
				TenantID:   task.TenantID,
				GroupID:    groupID,
				TGUserID:   user.ID,
				Username:   user.Username,
				FirstName:  user.FirstName,
				LastName:   user.LastName,
				AccessHash: user.AccessHash,
			})
		}
		scanned += len(mm.Messages)
		lastID = 0
		for i := len(mm.Messages) - 1; i >= 0; i-- {
			if m, ok := mm.Messages[i].(*tg.Message); ok {
				lastID = int32(m.ID)
				break
			}
		}
		if lastID == 0 {
			break
		}
		time.Sleep(500 * time.Millisecond)
	}

	if len(collected) > 0 {
		if err := e.memberRepo.BatchCreate(ctx, collected); err != nil {
			taskLog.Printf("[scrape] 活跃用户写库失败: %v", err)
		}
	}
	taskLog.Printf("[scrape] 历史消息采集完成: 扫描 %d 条消息, %d 个用户", scanned, len(collected))
	e.repo.UpdateStatus(ctx, task.TenantID, task.TaskID, domain.TaskStatusCompleted)
}
