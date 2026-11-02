package task

import (
	"context"
	"fmt"
	"log"

	"github.com/google/uuid"
	"tg-sender/internal/domain"
)

// SyncJoinedGroups fetches the account's Telegram dialogs and upserts
// all groups/channels into the local groups table.
func (e *Engine) SyncJoinedGroups(ctx context.Context, tenantID, accountID uuid.UUID) error {
	log.Printf("[sync] 开始同步账号 %s 的已加入群组", accountID)
	if e.accEngine == nil {
		return fmt.Errorf("account engine not set")
	}
	client, err := e.accEngine.GetClient(ctx, tenantID, accountID)
	if err != nil {
		return fmt.Errorf("get client: %w", err)
	}
	defer e.accEngine.ReleaseClient(tenantID, accountID, client)

	log.Printf("[sync] 调用GetDialogs，限制=%d", e.cfg.DialogsLimit)

	// Paginate through all dialogs
	offset := int32(0)
	totalProcessed := 0
	totalSkipped := 0
	batchCount := 0
	seenIDs := make(map[int64]bool) // dedup: same dialog may appear across pages

	for {
		batchCount++
		log.Printf("[sync] GetDialogs批次#%d，偏移=%d，限制=%d", batchCount, offset, e.cfg.DialogsLimit)
		dialogs, err := client.GetDialogs(ctx, e.cfg.DialogsLimit, offset)
		if err != nil {
			return fmt.Errorf("get dialogs (batch %d): %w", batchCount, err)
		}
		log.Printf("[sync] GetDialogs批次#%d返回了%d个对话", batchCount, len(dialogs))

		if len(dialogs) == 0 {
			log.Printf("[sync] 没有更多对话，停止分页")
			break
		}

		if e.groupRepo == nil {
			return fmt.Errorf("group repository not initialized")
		}

		processed := 0
		skipped := 0
		newInBatch := 0
		for _, chat := range dialogs {
			if chat == nil {
				skipped++
				continue
			}

			// Dedup: skip if we've already seen this chat ID in a previous batch
			if seenIDs[chat.ID] {
				skipped++
				continue
			}
			seenIDs[chat.ID] = true
			newInBatch++
			// Log raw type for debugging
			rawType := fmt.Sprintf("%v", chat.Type)
			log.Printf("[sync] 对话 id=%d 标题=%q 原始类型=%q 是否离开=%v", chat.ID, chat.Title, rawType, chat.IsLeft)

			// Map Telegram chat types to our schema
			// mtgo may return string names or numeric enum values
			gtype := "group" // default
			switch rawType {
			// Private chats and bots - skip
			case "private", "user", "bot",
				"ChatTypePrivate", "ChatTypeUser", "ChatTypeBot",
				"0": // numeric enum for private
				// 私聊/机器人: 不入群组库, 但写入联系人缓存(群发反查昵称 + 选择器秒开)
				fname, lname := chat.Title, ""
				if err := e.memberRepo.UpsertContact(ctx, tenantID, accountID, chat.ID, chat.Username, fname, lname, chat.AccessHash); err != nil {
					log.Printf("[sync] 写入联系人 %d 失败: %v", chat.ID, err)
				}
				skipped++
				continue
			// Basic groups
			case "group", "chat",
				"ChatTypeGroup", "ChatTypeChat",
				"1": // numeric enum for group
				gtype = "group"
			// Supergroups
			case "supergroup", "megagroup",
				"ChatTypeSupergroup", "ChatTypeMegagroup",
				"2": // numeric enum for supergroup
				gtype = "supergroup"
			// Channels
			case "channel",
				"ChatTypeChannel",
				"3": // numeric enum for channel
				gtype = "channel"
			// Bots (numeric)
			case "4":
				log.Printf("[sync] 跳过机器人对话 id=%d 类型=%s", chat.ID, rawType)
				skipped++
				continue
			default:
				// Try to infer from chat properties
				if chat.Username != "" {
					gtype = "channel" // channels usually have usernames
				} else {
					gtype = "group"
				}
				log.Printf("[sync] 对话 %d 未知类型 %s，推断为 %s", chat.ID, rawType, gtype)
			}

			// 已退出的群不再跳过: TG 客户端仍显示这些会话, 跳过会导致群组选择器缺群。
			// 照常入库, 仅标记 is_joined=false, 由前端"已加入"标签过滤。
			isJoined := true
			if chat.IsLeft {
				log.Printf("[sync] 标记未加入群组 id=%d 标题=%q", chat.ID, chat.Title)
				isJoined = false
			}

			// Use MembersCount directly from GetDialogs result (mtgo provides it)
			memberCount := chat.MembersCount
			if memberCount < 0 {
				memberCount = 0
			}

			// 频道(channel)也一并保存: 很多"群组"实际是频道, 跳过会导致群组选择器加载不全
			log.Printf("[sync] 处理对话 id=%d 标题=%q 类型=%s 成员=%d", chat.ID, chat.Title, gtype, memberCount)

			// Check if group already exists by TGChatID to avoid duplicate inserts
			existing, _ := e.groupRepo.GetByTGChatID(ctx, tenantID, chat.ID)
			var groupID uuid.UUID
			if existing != nil {
				existing.Title = chat.Title
				existing.Username = chat.Username
				existing.Type = gtype
				// 已退出的群同步标记 is_joined=false(旧版硬编码 true,
				// 导致退出/被踢的群永远显示"已加入")
				existing.IsJoined = isJoined
				existing.MemberCount = memberCount
				// Preserve existing AccessHash (GetDialogs doesn't provide it)
				if err := e.groupRepo.Update(ctx, existing); err != nil {
					log.Printf("[sync] 更新群组 %d 失败: %v", chat.ID, err)
				}
				groupID = existing.GroupID
			} else {
				groupID = uuid.New()
				g := &domain.Group{
					GroupID:     groupID,
					TenantID:    tenantID,
					TGChatID:    chat.ID,
					Title:       chat.Title,
					Username:    chat.Username,
					Type:        gtype,
					MemberCount: memberCount,
					IsJoined:    isJoined,
					// AccessHash will be filled later via ResolvePeer when needed
				}
				if err := e.groupRepo.Create(ctx, g); err != nil {
					log.Printf("[sync] upsert group %d failed: %v", chat.ID, err)
					continue
				}
			}

			// Link group to account
			if err := e.groupRepo.UpsertAccountGroup(ctx, accountID, groupID, isJoined); err != nil {
				log.Printf("[sync] 关联群组 %d 到账号失败: %v", chat.ID, err)
			} else {
				log.Printf("[sync] 已关联群组 %d（group_id=%s）到账号 %s", chat.ID, groupID, accountID)
			}
			processed++
		}

		totalProcessed += processed
		totalSkipped += skipped
		log.Printf("[sync] 批次#%d完成：已处理=%d 已跳过=%d 新增=%d", batchCount, processed, skipped, newInBatch)

		// Stop if no new unique dialogs were found in this batch (all were duplicates)
		if newInBatch == 0 {
			log.Printf("[sync] 批次#%d没有新的唯一对话，停止", batchCount)
			break
		}

		// Also stop if we got fewer dialogs than requested (last page)
		if len(dialogs) < e.cfg.DialogsLimit {
			log.Printf("[sync] 到达最后一页（%d < %d），停止", len(dialogs), e.cfg.DialogsLimit)
			break
		}

		offset += int32(len(dialogs))
		// Safety limit: max 200 batches (约 2 万个会话), 防止极端情况下无限分页
		if batchCount >= 200 {
			log.Printf("[sync] 达到最大批次数量（200），停止")
			break
		}
	}

	log.Printf("[sync] 同步已加入群组完成：总处理=%d 总跳过=%d 批次=%d", totalProcessed, totalSkipped, batchCount)

	// ===== Prune: 本地标记"已加入"但本次同步没出现的群 → 标记为已退出 =====
	// 只增不删的同步会让已退出/被踢的群永远留在选择器里。
	// 反向 diff: 分页遍历本地该账号的全部已加入群, 不在 seenIDs 里的标 left。
	pruned := 0
	const prunePage = 500
	for off := 0; ; off += prunePage {
		localGroups, err := e.groupRepo.ListByAccount(ctx, tenantID, accountID, prunePage, off)
		if err != nil {
			log.Printf("[sync] prune 阶段读取本地群组失败(offset=%d): %v", off, err)
			break
		}
		if len(localGroups) == 0 {
			break
		}
		for _, lg := range localGroups {
			if lg == nil || !lg.IsJoined {
				continue
			}
			if seenIDs[lg.TGChatID] {
				continue // 本次同步见到了, 保持已加入
			}
			if err := e.groupRepo.UpdateJoinStatus(ctx, tenantID, lg.GroupID, false); err != nil {
				log.Printf("[sync] prune 标记群 %d left 失败: %v", lg.TGChatID, err)
				continue
			}
			if err := e.groupRepo.UpsertAccountGroup(ctx, accountID, lg.GroupID, false); err != nil {
				log.Printf("[sync] prune 更新关联表失败 group=%s: %v", lg.GroupID, err)
			}
			pruned++
		}
		if len(localGroups) < prunePage {
			break // 最后一页
		}
	}
	if pruned > 0 {
		log.Printf("[sync] prune 完成：标记 %d 个已退出群组", pruned)
	}
	// R99: 同步完成主动通知 —— 前端全局事件流收到后自动刷新群缓存,
	// 避免"同步完要重开选择器/手动刷新"
	if e.sseBroker != nil {
		e.sseBroker.BroadcastToTenant(tenantID, "events", map[string]interface{}{
			"type":       "groups_synced",
			"account_id": accountID.String(),
		})
	}
	return nil
}
