package account

import (
	"context"
	"fmt"
	"log"
	"time"

	"strings"

	"github.com/google/uuid"
	"github.com/mtgo-labs/mtgo/telegram"
	"github.com/mtgo-labs/mtgo/telegram/types"
	"github.com/mtgo-labs/mtgo/tg"
)

// buildChatResult converts a *types.Chat to a map result.
func buildChatResult(chat *types.Chat) map[string]any {
	// Determine chat type
	var chatType string
	switch chat.Type {
	case types.ChatTypeGroup:
		chatType = "group"
	case types.ChatTypeSupergroup:
		chatType = "supergroup"
	case types.ChatTypeChannel, types.ChatTypeForum:
		chatType = "channel"
	case types.ChatTypeBot:
		return nil // Skip bots
	default:
		// Private chats (including unknown/empty types with positive ID)
		chatType = "user"
	}

	// Build display title
	title := chat.Title
	if title == "" && (chat.FirstName != "" || chat.LastName != "") {
		title = chat.FirstName
		if chat.LastName != "" {
			if title != "" {
				title += " "
			}
			title += chat.LastName
		}
	}
	if title == "" {
		title = "未知"
	}

	return map[string]any{
		"tg_chat_id":   chat.ID,
		"title":        title,
		"first_name":   chat.FirstName,
		"last_name":    chat.LastName,
		"username":     chat.Username,
		"type":         chatType,
		"member_count": chat.MembersCount,
		"is_member":    !chat.IsLeft,
		"is_verified":  chat.IsVerified,
		"is_scam":      chat.IsScam,
	}
}

// getAllDialogs fetches all dialogs using time-window pagination.
// Telegram's GetDialogs returns max 100 per call, so we paginate by offsetDate.
func (e *Engine) getAllDialogs(ctx context.Context, client *telegram.Client) ([]map[string]any, error) {
	var allResults []map[string]any
	seen := make(map[int64]bool)

	// 私聊必须先拉: 深度分页容易触发 FLOOD_WAIT, 把私聊请求放在分页之前
	for _, u := range e.getPrivateDialogs(ctx, client) {
		if id, ok := u["id"].(int64); ok && !seen[id] {
			seen[id] = true
			allResults = append(allResults, u)
		}
	}

	const batchSize = 100
	const windowDays = 7     // 7-day time window per page
	const maxPages = 8       // 只看最近 ~2 个月, 降低限流风险(群组有本地同步表兜底)
	const emptyThreshold = 2 // stop after 2 consecutive empty pages

	emptyStreak := 0
	now := time.Now()

	for page := 0; page < maxPages; page++ {
		// 节流: 私聊分页之后紧跟, 必须间隔避免FLOOD_WAIT
		if page > 0 {
			select {
			case <-ctx.Done():
				return allResults, nil
			case <-time.After(1500 * time.Millisecond):
			}
		}
		var offsetDate int32
		if page == 0 {
			offsetDate = 0 // First call: get latest
		} else {
			// Use progressively older dates
			offsetDate = int32(now.AddDate(0, 0, -page*windowDays).Unix())
		}

		chats, err := client.GetDialogs(ctx, batchSize, offsetDate)
		if err != nil {
			// FLOOD_WAIT 等限流: 不整盘失败, 返回已获取的部分结果
			if len(allResults) > 0 {
				log.Printf("[dialogs] page %d hit rate limit (%v), returning %d partial results", page, err, len(allResults))
				return allResults, nil
			}
			return nil, fmt.Errorf("get dialogs page %d: %w", page, err)
		}

		if len(chats) == 0 {
			emptyStreak++
			if emptyStreak >= emptyThreshold {
				break
			}
			continue
		}
		emptyStreak = 0

		newCount := 0
		for _, chat := range chats {
			if chat == nil || seen[chat.ID] {
				continue
			}
			seen[chat.ID] = true
			newCount++

			result := buildChatResult(chat)
			if result != nil {
				allResults = append(allResults, result)
			}
		}

		// If first page has fewer than batchSize, we got everything
		if page == 0 && len(chats) < batchSize {
			break
		}

		// If no new chats in this window, increment empty streak
		if page > 0 && newCount == 0 {
			emptyStreak++
			if emptyStreak >= emptyThreshold {
				break
			}
		}
	}

	privateCount := 0
	for _, r := range allResults {
		if r["type"] == "user" {
			privateCount++
		}
	}
	log.Printf("[account] 从Telegram获取了 %d 个唯一对话 (群组/频道 %d, 私聊 %d)", len(allResults), len(allResults)-privateCount, privateCount)
	return allResults, nil
}

func (e *Engine) GetTelegramDialogs(ctx context.Context, tenantID, accountID uuid.UUID) ([]map[string]any, error) {
	client, err := e.GetClient(ctx, tenantID, accountID)
	if err != nil {
		return nil, fmt.Errorf("get client: %w", err)
	}
	defer e.ReleaseClient(tenantID, accountID, client)

	return e.getAllDialogs(ctx, client)
}

// GetTelegramContacts returns the account's Telegram contact list.
func (e *Engine) GetTelegramContacts(ctx context.Context, tenantID, accountID uuid.UUID) ([]map[string]any, error) {
	client, err := e.GetClient(ctx, tenantID, accountID)
	if err != nil {
		return nil, fmt.Errorf("get client: %w", err)
	}
	defer e.ReleaseClient(tenantID, accountID, client)

	contactsRaw, err := client.GetContacts(ctx, 0)
	if err != nil {
		return nil, fmt.Errorf("get contacts: %w", err)
	}

	var result []map[string]any
	switch contacts := contactsRaw.(type) {
	case *tg.ContactsContacts:
		for _, u := range contacts.Users {
			user, ok := u.(*tg.User)
			if !ok || user == nil || user.Bot {
				continue
			}
			result = append(result, map[string]any{
				"id":         user.ID,
				"tg_user_id": user.ID,
				"username":   user.Username,
				"first_name": user.FirstName,
				"last_name":  user.LastName,
				"phone":      user.Phone,
				"is_premium": user.Premium,
			})
		}
	default:
		// Unknown contacts type, return empty
	}
	return result, nil
}

// ResolvePeer resolves a Telegram username, invite link, or ID to chat info.
func (e *Engine) ResolvePeer(ctx context.Context, tenantID, accountID uuid.UUID, identifier string) (map[string]any, error) {
	client, err := e.GetClient(ctx, tenantID, accountID)
	if err != nil {
		return nil, fmt.Errorf("get client: %w", err)
	}
	defer e.ReleaseClient(tenantID, accountID, client)

	peer, err := client.ResolvePeer(ctx, identifier)
	if err != nil {
		return nil, fmt.Errorf("resolve peer: %w", err)
	}

	var chatID int64
	var peerType string
	switch p := peer.(type) {
	case *tg.InputPeerUser:
		chatID = p.UserID
		peerType = "user"
	case *tg.InputPeerChat:
		chatID = p.ChatID
		peerType = "group"
	case *tg.InputPeerChannel:
		chatID = p.ChannelID
		peerType = "channel"
	default:
		return nil, fmt.Errorf("unknown peer type")
	}

	return map[string]any{
		"tg_chat_id": chatID,
		"type":       peerType,
		"identifier": identifier,
	}, nil
}

// getPrivateDialogs 通过原始 Invoke 分页拉取全部私聊会话。
// 私聊(PeerUser)的对方资料在响应的 Users 数组中, 封装层 GetDialogs 只返回 Chats。
// 翻页偏移三件套必须真实: offset_date=最后一条消息的日期(从Messages里查),
// offset_id=TopMessage, offset_peer=最后对话框的Peer(用户/群/频道各自带AccessHash)。
func (e *Engine) getPrivateDialogs(ctx context.Context, client *telegram.Client) []map[string]any {
	var all []map[string]any
	seen := make(map[int64]bool)
	var offsetDate, offsetID, prevLastMsg int32
	var offsetPeer tg.InputPeerClass = &tg.InputPeerEmpty{}

	for page := 0; page < 15; page++ {
		// 节流: getDialogs 配额约1次/秒, 连续翻页必触发FLOOD_WAIT
		if page > 0 {
			select {
			case <-ctx.Done():
				return all
			case <-time.After(1500 * time.Millisecond):
			}
		}
		resp, err := client.Invoke(ctx, &tg.MessagesGetDialogsRequest{
			ExcludePinned: false,
			OffsetDate:    offsetDate,
			OffsetID:      offsetID,
			OffsetPeer:    offsetPeer,
			Limit:         100,
			Hash:          0,
		})
		if err != nil {
			if len(all) > 0 {
				log.Printf("[dialogs] private page %d rate limited, returning %d partial", page, len(all))
				break
			}
			log.Printf("[dialogs] get private dialogs failed: %v", err)
			return nil
		}

		var dialogs *tg.MessagesDialogs
		switch d := resp.(type) {
		case *tg.MessagesDialogs:
			dialogs = d
		case *tg.MessagesDialogsSlice:
			dialogs = &tg.MessagesDialogs{Dialogs: d.Dialogs, Users: d.Users, Chats: d.Chats, Messages: d.Messages}
		default:
			log.Printf("[dialogs] unexpected response type: %T", resp)
			return all
		}
		if len(dialogs.Dialogs) == 0 {
			break
		}

		userMap := make(map[int64]*tg.User)
		for _, u := range dialogs.Users {
			if user, ok := u.(*tg.User); ok {
				userMap[user.ID] = user
			}
		}
		channelMap := make(map[int64]*tg.Channel)
		for _, c := range dialogs.Chats {
			if ch, ok := c.(*tg.Channel); ok {
				channelMap[ch.ID] = ch
			}
		}

		pagePrivate := 0
		for _, dc := range dialogs.Dialogs {
			d, ok := dc.(*tg.Dialog)
			if !ok {
				continue
			}
			peer, ok := d.Peer.(*tg.PeerUser)
			if !ok {
				continue
			}
			user, exists := userMap[peer.UserID]
			if !exists || seen[user.ID] {
				continue
			}
			if user.Bot {
				continue // 排除机器人账号
			}
			seen[user.ID] = true
			name := strings.TrimSpace(user.FirstName + " " + user.LastName)
			if name == "" {
				name = user.Username
			}
			if name == "" {
				name = fmt.Sprintf("用户%d", user.ID)
			}
			all = append(all, map[string]any{
				"id":       user.ID,
				"title":    name,
				"username": user.Username,
				"type":     "user",
			})
			pagePrivate++
		}
		log.Printf("[dialogs] private page %d: %d dialogs, %d new private (total %d)", page, len(dialogs.Dialogs), pagePrivate, len(all))

		// 续翻锚点
		last, ok := dialogs.Dialogs[len(dialogs.Dialogs)-1].(*tg.Dialog)
		if !ok || last.TopMessage == 0 {
			break
		}
		if last.TopMessage == prevLastMsg {
			log.Printf("[dialogs] private pagination stalled at page %d (total %d)", page, len(all))
			break // 翻页不再前进, 提前结束, 省掉后续空等
		}
		prevLastMsg = last.TopMessage
		// offset_date 必须是最后一条消息的真实日期, 从 Messages 里按 ID 反查
		var lastDate int32
		for _, m := range dialogs.Messages {
			if msg, ok := m.(*tg.Message); ok && msg.ID == last.TopMessage {
				lastDate = msg.Date
				break
			}
		}
		offsetDate = lastDate
		offsetID = last.TopMessage
		switch lp := last.Peer.(type) {
		case *tg.PeerUser:
			np := &tg.InputPeerUser{UserID: lp.UserID}
			if u, ok2 := userMap[lp.UserID]; ok2 {
				np.AccessHash = u.AccessHash
			}
			offsetPeer = np
		case *tg.PeerChat:
			offsetPeer = &tg.InputPeerChat{ChatID: lp.ChatID}
		case *tg.PeerChannel:
			np := &tg.InputPeerChannel{ChannelID: lp.ChannelID}
			if ch, ok2 := channelMap[lp.ChannelID]; ok2 {
				np.AccessHash = ch.AccessHash
			}
			offsetPeer = np
		default:
			return all
		}
	}

	return all
}
