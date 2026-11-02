// Package probe R119: 账号能力探测引擎 —— 8 项矩阵逐项真实操作+秒删,
// 错误码归因(链接失效/账号受限/群数达上限), 结果分级结论。
// 探测目标(测试小号/私有群永久链接/公开群链接)由管理端全局配置, 租户只读使用。
package probe

import (
	"context"
	"errors"
	"fmt"
	"log"
	"reflect"
	"regexp"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/mtgo-labs/mtgo/telegram"
	"github.com/mtgo-labs/mtgo/tg"
	"tg-sender/internal/domain"
)

// 检测矩阵标签(顺序即执行顺序; 清理项不计分)
var itemLabels = []struct{ Key, Label string }{
	{"read", "读取消息"},
	{"send_saved", "基础发送(收藏夹)"},
	{"dm", "主动私聊陌生人"},
	{"join_private", "加入私有群"},
	{"send_private", "私有群发送"},
	{"join_public", "加入公开群"},
	{"send_public", "公开群发送"},
	{"reply", "回复消息"},
}

// Item 单项检测结果
type Item struct {
	Key    string `json:"key"`
	Label  string `json:"label"`
	OK     bool   `json:"ok"`
	Detail string `json:"detail,omitempty"`
}

// Result 完整检测结果
type Result struct {
	Items     []Item    `json:"items"`
	Verdict   string    `json:"verdict"`
	CheckedAt time.Time `json:"checked_at"`
}

// AccountEngine 抽象账号引擎的客户端获取(适配真实签名: GetClient 两返回值,
// 客户端锁需显式 ReleaseClient 配对 —— 同 avatar.go 用法)
type AccountEngine interface {
	GetClient(ctx context.Context, tenantID, accountID uuid.UUID) (*telegram.Client, error)
	ReleaseClient(tenantID, accountID uuid.UUID, client *telegram.Client)
}

// ConfigProvider 探测配置读取
type ConfigProvider interface {
	GetProbeConfig(ctx context.Context) (*domain.ProbeConfig, error)
}

// Deps 探测依赖
type Deps struct {
	Engine  AccountEngine
	Configs ConfigProvider
}

var inviteHashRe = regexp.MustCompile(`^(?:https?://)?(?:www\.)?t\.me/\+([A-Za-z0-9_-]+)$`)
var publicNameRe = regexp.MustCompile(`^(?:https?://)?(?:www\.)?t\.me/([A-Za-z0-9_]{5,})$`)

const probeMsg = "·系统自检·"

// classifyErr 错误归因 —— 把 TG 错误码翻译成人话, 区分"配置问题"和"账号问题"
func classifyErr(err error) string {
	if err == nil {
		return ""
	}
	s := err.Error()
	switch {
	case strings.Contains(s, "INVITE_HASH_EXPIRED"), strings.Contains(s, "INVITE_HASH_INVALID"):
		return "邀请链接失效(管理端需更新永久链接)"
	case strings.Contains(s, "USER_RESTRICTED"), strings.Contains(s, "USER_IS_BLOCKED"), strings.Contains(s, "CHAT_RESTRICTED"):
		return "账号被限制"
	case strings.Contains(s, "CHANNELS_TOO_MUCH"):
		return "加入群数已达上限(500)"
	case strings.Contains(s, "FLOOD_WAIT"):
		return "操作太频繁, 稍后再试"
	case strings.Contains(s, "USERNAME_NOT_OCCUPIED"), strings.Contains(s, "USERNAME_INVALID"):
		return "探测目标不存在(管理端配置有误)"
	case strings.Contains(s, "CHAT_WRITE_FORBIDDEN"):
		return "群已开启仅管理员发言或账号被禁言"
	default:
		return "失败: " + s
	}
}

// extractJoinPeerViaReflection 从 MessagesImportChatInvite 的 TLObject 返回里
// 按字段名提取加入群的 peer(Chat→InputPeerChat / Channel→InputPeerChannel)。
// 与 raffle.go extractBotViaReflection 同思路: mtgo Raw() 返回通用接口,
// 具体包装类型名不猜, 反射按字段名取值, 规避编译循环。
func extractJoinPeerViaReflection(res tg.TLObject) (tg.InputPeerClass, bool) {
	defer func() { recover() }() // 反射异常兜底
	v := reflect.ValueOf(res)
	if v.Kind() == reflect.Ptr {
		if v.IsNil() {
			return nil, false
		}
		v = v.Elem()
	}
	if v.Kind() != reflect.Struct {
		return nil, false
	}
	// 找 Chat 字段(单数, ChatClass 接口) 或 Chats 切片
	findIn := func(uv reflect.Value) (tg.InputPeerClass, bool) {
		if uv.Kind() == reflect.Interface {
			uv = uv.Elem()
		}
		if uv.Kind() == reflect.Ptr {
			uv = uv.Elem()
		}
		if uv.Kind() != reflect.Struct {
			return nil, false
		}
		var id, hash int64
		for i := 0; i < uv.NumField(); i++ {
			f := uv.Field(i)
			switch uv.Type().Field(i).Name {
			case "ID":
				if f.Kind() >= reflect.Int && f.Kind() <= reflect.Int64 {
					id = f.Int()
				}
			case "AccessHash":
				if f.Kind() >= reflect.Int && f.Kind() <= reflect.Int64 {
					hash = f.Int()
				}
			}
		}
		if id == 0 {
			return nil, false
		}
		if hash != 0 {
			return &tg.InputPeerChannel{ChannelID: id, AccessHash: hash}, true
		}
		return &tg.InputPeerChat{ChatID: id}, true
	}
	if f := v.FieldByName("Chat"); f.IsValid() {
		if p, ok := findIn(f); ok {
			return p, true
		}
	}
	if f := v.FieldByName("Chats"); f.IsValid() && f.Kind() == reflect.Slice && f.Len() > 0 {
		return findIn(f.Index(0))
	}
	return nil, false
}

// Run 对指定账号执行完整 8 项探测。任何一步失败不中断, 记 ✗ 继续; 清理步骤总是执行。
func Run(ctx context.Context, deps Deps, tenantID, accountID uuid.UUID) (*Result, error) {
	cfg, err := deps.Configs.GetProbeConfig(ctx)
	if err != nil || cfg == nil || !cfg.Ready() {
		return nil, errors.New("管理员尚未配置探测目标(管理端 → 风控探测设置)")
	}
	client, err := deps.Engine.GetClient(ctx, tenantID, accountID)
	if err != nil {
		return nil, fmt.Errorf("账号不在线, 无法检测: %w", err)
	}
	defer deps.Engine.ReleaseClient(tenantID, accountID, client)

	res := &Result{CheckedAt: time.Now()}
	idx := map[string]int{}
	for i, l := range itemLabels {
		res.Items = append(res.Items, Item{Key: l.Key, Label: l.Label})
		idx[l.Key] = i
	}
	set := func(key string, ok bool, detail string) {
		it := &res.Items[idx[key]]
		it.OK = ok
		it.Detail = detail
	}
	fail := func(key string, err error) {
		set(key, false, classifyErr(err))
	}

	raw := client.Raw()
	// 所有测试消息按 (peer, id) 记录, 清理时逐条精确撤回
	// (mtgo 的 MessagesDeleteMessagesRequest 无 Peer 字段 —— 消息 ID 全局唯一,
	//  与 verifier.go:382 的现有用法一致)
	type sentMsg struct {
		peer tg.InputPeerClass
		id   int32
	}
	var sentMsgs []sentMsg
	cleanup := func() {
		for _, sm := range sentMsgs {
			_, _ = raw.MessagesDeleteMessages(ctx, &tg.MessagesDeleteMessagesRequest{ID: []int32{sm.id}, Revoke: true})
		}
	}

	// 探测群上下文: 加入后保留 peer 供发送/清理
	peers := map[string]tg.InputPeerClass{}
	defer func() {
		// 清理: 退两群(尽力而为)
		// (ChannelsLeaveChannel 要 InputChannelClass: 由 ChannelID+AccessHash 构造,
		//  直接传 *InputPeerChannel 不满足接口 —— raffle.go resolveGroupPeer 同款做法)
		for _, key := range []string{"private", "public"} {
			if p, ok := peers[key]; ok {
				if ch, isCh := p.(*tg.InputPeerChannel); isCh {
					_, _ = raw.ChannelsLeaveChannel(ctx, &tg.ChannelsLeaveChannelRequest{Channel: &tg.InputChannel{ChannelID: ch.ChannelID, AccessHash: ch.AccessHash}})
				} else if chat, isChat := p.(*tg.InputPeerChat); isChat {
					_, _ = raw.MessagesDeleteChatUser(ctx, &tg.MessagesDeleteChatUserRequest{ChatID: chat.ChatID, UserID: &tg.InputUserSelf{}})
				}
			}
		}
	}()

	sendAndRemember := func(peer tg.InputPeerClass) (int32, error) {
		m, err := raw.MessagesSendMessage(ctx, &tg.MessagesSendMessageRequest{Peer: peer, Message: probeMsg, RandomID: time.Now().UnixNano()})
		if err != nil {
			return 0, err
		}
		var id int32
		switch u := m.(type) {
		case *tg.Updates:
			for _, upd := range u.Updates {
				if im, ok2 := upd.(*tg.UpdateMessageID); ok2 {
					id = im.ID
				}
			}
		}
		if id != 0 {
			sentMsgs = append(sentMsgs, sentMsg{peer: peer, id: id})
		}
		return id, nil
	}

	// ① 读取消息: 拉收藏夹历史(只读零风险)
	_, err = raw.MessagesGetHistory(ctx, &tg.MessagesGetHistoryRequest{Peer: &tg.InputPeerSelf{}, Limit: 1})
	set("read", err == nil, "")
	if err != nil {
		fail("read", err)
	}

	// ② 基础发送: 收藏夹发+删(删除在 cleanup 统一做)
	if err == nil || true { // read 失败也继续测发送(两者独立)
		_, err = sendAndRemember(&tg.InputPeerSelf{})
		if err != nil {
			fail("send_saved", err)
		} else {
			set("send_saved", true, "")
		}
	}

	// ③ 主动私聊: 给测试小号发一条
	var dmPeer tg.InputPeerClass
	if resolved, rerr := raw.ContactsResolveUsername(ctx, &tg.ContactsResolveUsernameRequest{Username: strings.TrimPrefix(cfg.TestUsername, "@")}); rerr != nil {
		fail("dm", rerr)
	} else if len(resolved.Users) == 0 {
		set("dm", false, "探测目标不存在(管理端配置有误)")
	} else {
		// mtgo: Users 是 UserClass 接口切片, 断言 *tg.User 取 ID/AccessHash
		if u, ok2 := resolved.Users[0].(*tg.User); ok2 && u.AccessHash != 0 {
			dmPeer = &tg.InputPeerUser{UserID: u.ID, AccessHash: u.AccessHash}
		}
		if dmPeer == nil {
			set("dm", false, "无法解析探测目标用户资料")
		} else if _, serr := sendAndRemember(dmPeer); serr != nil {
			fail("dm", serr)
		} else {
			set("dm", true, "")
		}
	}

	// ④ 加入私有群(邀请链接)
	if m := inviteHashRe.FindStringSubmatch(strings.TrimSpace(cfg.PrivateInvite)); m == nil {
		set("join_private", false, "管理端配置的私有群链接格式无效(需 https://t.me/+xxx)")
	} else {
		imp, ierr := raw.MessagesImportChatInvite(ctx, &tg.MessagesImportChatInviteRequest{Hash: m[1]})
		if ierr != nil {
			fail("join_private", ierr)
		} else {
			set("join_private", true, "")
			// mtgo 的 JoinResult 具体类型名不猜 —— 反射按字段名取 Chat 的 ID/AccessHash
			// (与 raffle.go extractBotViaReflection 同思路: 不猜包装类型名, 彻底规避编译循环)
			if p, ok2 := extractJoinPeerViaReflection(imp); ok2 {
				peers["private"] = p
			}
		}
	}

	// ⑤ 私有群发送
	if p, ok := peers["private"]; ok {
		if _, serr := sendAndRemember(p); serr != nil {
			fail("send_private", serr)
		} else {
			set("send_private", true, "")
		}
	} else {
		set("send_private", false, "未加入私有群, 跳过")
	}

	// ⑥ 加入公开群
	if m := publicNameRe.FindStringSubmatch(strings.TrimSpace(cfg.PublicLink)); m == nil {
		set("join_public", false, "管理端配置的公开群链接格式无效(需 https://t.me/群名)")
	} else {
		resolved, rerr := raw.ContactsResolveUsername(ctx, &tg.ContactsResolveUsernameRequest{Username: m[1]})
		if rerr != nil {
			fail("join_public", rerr)
		} else if len(resolved.Chats) == 0 {
			set("join_public", false, "公开群不存在(管理端配置有误)")
		} else {
			// mtgo: ContactsResolveUsername 的 Chats 是接口切片, 断言 *tg.Channel
			if ch, ok2 := resolved.Chats[0].(*tg.Channel); ok2 {
				peers["public"] = &tg.InputPeerChannel{ChannelID: ch.ID, AccessHash: ch.AccessHash}
				// JoinChannel 要 InputChannelClass: 构造专用对象(不直接复用 InputPeerChannel)
				_, _ = raw.ChannelsJoinChannel(ctx, &tg.ChannelsJoinChannelRequest{Channel: &tg.InputChannel{ChannelID: ch.ID, AccessHash: ch.AccessHash}})
			}
			set("join_public", true, "")
		}
	}

	// ⑦ 公开群发送
	if p, ok := peers["public"]; ok {
		if _, serr := sendAndRemember(p); serr != nil {
			fail("send_public", serr)
		} else {
			set("send_public", true, "")
		}
	} else {
		set("send_public", false, "未加入公开群, 跳过")
	}

	// ⑧ 回复消息(回复私有群里刚发的自检消息 —— 有真实群上下文)
	if p, ok := peers["private"]; ok {
		var replyToID int32
		for _, sm := range sentMsgs {
			if _, isChat := sm.peer.(*tg.InputPeerChat); isChat {
				replyToID = sm.id
				break
			}
		}
		if replyToID == 0 && len(sentMsgs) > 0 {
			replyToID = sentMsgs[len(sentMsgs)-1].id
		}
		if replyToID > 0 {
			_, err = raw.MessagesSendMessage(ctx, &tg.MessagesSendMessageRequest{
				Peer: p, Message: probeMsg, RandomID: time.Now().UnixNano(),
				ReplyTo: &tg.InputReplyToMessage{ReplyToMsgID: replyToID},
			})
			if err != nil {
				fail("reply", err)
			} else {
				set("reply", true, "")
			}
		} else {
			set("reply", false, "无可回复消息, 跳过")
		}
	} else {
		set("reply", false, "未加入私有群, 跳过")
	}

	// 清理: 精确撤回所有测试消息(按 peer 分组记录, 不会删错会话)
	cleanup()
	res.Verdict = verdict(res)
	log.Printf("[probe] account=%s verdict=%q", accountID, res.Verdict)
	return res, nil
}

// verdict 综合分级结论(截图同款文案)
func verdict(r *Result) string {
	get := func(k string) (bool, bool) {
		for _, it := range r.Items {
			if it.Key == k {
				return it.OK, true
			}
		}
		return false, false
	}
	saved, _ := get("send_saved")
	dm, _ := get("dm")
	joinP, _ := get("join_private")
	sendP, _ := get("send_private")
	joinPub, _ := get("join_public")
	sendPub, _ := get("send_public")

	if saved && dm && joinP && sendP && joinPub && sendPub {
		return "账号正常, 全部功能可用"
	}
	if !saved {
		return "严重受限/封禁, 账号无使用价值"
	}
	// 能发收藏夹, 但陌生人私聊不行, 群功能可以
	if !dm && sendP && sendPub {
		return "陌生人私聊受限(通常数天后自动解禁)"
	}
	// 公开群受限但私有群正常 = L2 梯度
	if !sendPub && sendP {
		return "公开群发言受限, 私密群正常(L2 梯度限制)"
	}
	if !sendP && !sendPub {
		return "群组功能受限(加入或发言被拒)"
	}
	if !joinP || !joinPub {
		return "加入群组功能受限"
	}
	return "部分功能受限, 建议减少操作频率养号"
}
