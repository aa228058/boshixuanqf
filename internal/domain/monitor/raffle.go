package monitor

import (
	"context"
	"log"
	"math/rand/v2"
	"reflect"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/mtgo-labs/mtgo/telegram"
	"github.com/mtgo-labs/mtgo/telegram/types"
	"github.com/mtgo-labs/mtgo/tg"
	"tg-sender/internal/safego"
)

// 抽奖助手: 群内抽奖机器人发布"参与关键词: X"类消息时, 自动用本账号在群内
// 发送该关键词完成参与。检测纯文本正则, 无需 AI; 发送带抖动/冷却/去重防风控。

// 正则统一用解释型字符串, 反引号用 \x60 表示(反引号不能出现在反引号 raw string 里)
var raffleKeywordRes = []*regexp.Regexp{
	// 抓到行尾/闭引号(多关键词逗号分隔的场景不再截断, 由 extractRaffleKeyword 取首个)
	regexp.MustCompile("参与关键词[::]\\s*[「『'\"\\x60]?([^「』'\"\\x60\\n]+)"),
	regexp.MustCompile("回复关键词[「『'\"\\x60]?([^「』'\"\\x60\\n]+)"),
	// kkpay 格式(2026-10-28 实测): "├关键词: 老板牛逼" —— 无"参与/回复"前缀, 上面两条均不命中
	regexp.MustCompile("关键词[::]\\s*[「『'\"\\x60]?([^「』'\"\\x60\\n]+)"),
	regexp.MustCompile("发送[『「'\"\\x60]([^』」'\"\\x60]+)[』」'\"\\x60]参与"),
	regexp.MustCompile("回复[『「'\"\\x60]([^』」'\"\\x60]+)[』」'\"\\x60]即可参与"),
}

// raffleGuard 去重+冷却(内存态)
type raffleGuard struct {
	mu         sync.Mutex
	lastJoin   map[string]time.Time // 60 秒短冷却: 同公告极速重推防护
	activities map[string]bool      // 活动级参与标记(2026-10-21): 群+关键词维度, 创建公告到达时重置
	// (抢答化改造已删) 原 lastAny 跨活动冷却字段保留定义兼容, 不再读写
}

var raffleG = &raffleGuard{
	lastJoin:   map[string]time.Time{},
	activities: map[string]bool{}, // (2026-10-21) 活动级参与标记: 群+关键词 维度, 创建公告重置
}

// ===== 活动级防重(2026-10-21 重设计) =====
// 背景: 同一活动机器人会发两种消息——
//   创建公告 "发起了抽奖活动"   → 新活动开始的信号
//   查询回复 "已参与/正在进行的抽奖" → 同活动的重复曝光(msgID 每次都不同, 不能按消息去重)
// 旧设计(10分钟窗+DB永久按 chat+keyword)的两个缺陷:
//   ① 同群同关键词【新活动】被"已参与过"永久挡死
//   ② 同一公告编辑重推超过 10 分钟会重复参与
// 新设计: 活动键 = 群+关键词; 创建公告到达=新活动→重置; 查询回复=同活动→查标记。
// 60 秒短冷却仅挡同一公告的极速重推, 不再误伤新活动。

// activityKey 活动键
func activityKey(tenantID, accountID uuid.UUID, chatID int64, kw string) string {
	return tenantID.String() + ":" + accountID.String() + ":" + fmt_Sprint(chatID) + ":" + kw
}

func (g *raffleGuard) activityJoined(key string) bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.activities[key]
}

func (g *raffleGuard) markActivity(key string) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.activities[key] = true
}

// resetActivity 创建公告到达: 新活动开始, 旧参与标记作废(同关键词可再次参与)
func (g *raffleGuard) resetActivity(key string) {
	g.mu.Lock()
	defer g.mu.Unlock()
	delete(g.activities, key)
}

// SetRaffleProvider 注入开关判定(container 接线)
func (e *Engine) SetRaffleProvider(p func(tenantID uuid.UUID) bool) {
	e.raffleEnabled = p
}

// SetAccountRaffleProvider 注入账号级开关判定(改进#1: 单账号可关闭抽奖)
func (e *Engine) SetAccountRaffleProvider(p func(tenantID, accountID uuid.UUID) bool) {
	e.accountRaffleEnabled = p
}

// SetRaffleReplyProvider 注入中奖回复文案判定(container 接线, 060 迁移新增)
func (e *Engine) SetRaffleReplyProvider(p func(tenantID uuid.UUID) (bool, string)) {
	e.raffleReply = p
}

// SetRaffleBotPingProvider 注入私聊激活判定(container 接线, 061 迁移新增)
func (e *Engine) SetRaffleBotPingProvider(p func(tenantID uuid.UUID) (bool, string)) {
	e.raffleBotPing = p
}

// extractRaffleKeyword 从抽奖公告文本提取参与关键词
func extractRaffleKeyword(text string) string {
	for _, re := range raffleKeywordRes {
		if m := re.FindStringSubmatch(text); m != nil {
			// 整串原样抓取(2026-10-21 按租户确认): 参与关键词后面的内容
			// 不管是符号/emoji/逗号都是同一个关键词, 机器人按精确串匹配(长按复制的那个),
			// 此前"逗号切分取第一个"会导致带符号的关键词参与失败。
			kw := strings.TrimSpace(m[1])
			// R145 修复: 无书名号的话术(如 kkpay "回复关键词 老板发财 网上 参与活动。")中,
			// 捕获组会一直吞到行尾, 把"参与活动"引导语错当关键词的一部分发出去。
			// 关键词本身允许含空格(TG 支持, 长按复制的精确串), 只截断固定引导语尾巴, 不按空格切。
			for _, tail := range []string{"参与活动", "即可参与"} {
				if idx := strings.LastIndex(kw, tail); idx > 0 {
					kw = kw[:idx]
				}
			}
			if idx := strings.LastIndex(kw, " 参与"); idx > 0 {
				kw = kw[:idx]
			}
			// 仅剥句尾句号(标点收尾是公告排版, 不属于关键词本体); 其余符号/emoji 全部保留
			kw = strings.TrimSpace(kw)
			kw = strings.TrimRight(kw, "。.！!？?")
			// 过滤明显是命令/查询的词(如 /lottery /抽奖 查询)
			if kw != "" && !strings.HasPrefix(kw, "/") && kw != "抽奖" {
				return kw
			}
		}
	}
	return ""
}

// structFieldNames 调试辅助: 返回结构体字段名列表(私聊激活反射失败时打印真实结构用)
func structFieldNames(v interface{}) []string {
	defer func() { recover() }()
	rv := reflect.ValueOf(v)
	if rv.Kind() == reflect.Ptr {
		rv = rv.Elem()
	}
	if rv.Kind() != reflect.Struct {
		return nil
	}
	names := make([]string, 0, rv.NumField())
	for i := 0; i < rv.NumField(); i++ {
		names = append(names, rv.Type().Field(i).Name)
	}
	return names
}

// extractBotViaReflection 从 UsersGetUsers 的 TLObject 返回里按字段名提取 bot 资料
// (ID/AccessHash/Username/Bot)。mtgo Raw() 返回通用 tg.TLObject, 具体包装类型名不固定,
// 反射按字段名取值——不猜类型名, 彻底规避编译循环(2026-10-21 实证方案)。
// R119: 追加返回 isBot(mtgo User 的 Bot 字段), 供发送者判定用。
func extractBotViaReflection(res tg.TLObject) (userID, accessHash int64, username string, isBot, ok bool) {
	defer func() { recover() }() // 反射异常兜底
	v := reflect.ValueOf(res)
	if v.Kind() == reflect.Ptr {
		if v.IsNil() {
			return 0, 0, "", false, false
		}
		v = v.Elem()
	}
	// 找 Users 字段(vector<User>) 或直接是 User 结构
	findUser := func(uv reflect.Value) (int64, int64, string, bool, bool) {
		if uv.Kind() == reflect.Interface {
			uv = uv.Elem()
		}
		if uv.Kind() == reflect.Ptr {
			uv = uv.Elem()
		}
		if uv.Kind() != reflect.Struct {
			return 0, 0, "", false, false
		}
		var id, hash int64
		var uname string
		var isBot bool
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
			case "Username":
				if f.Kind() == reflect.String {
					uname = f.String()
				}
			case "Bot": // R119: 发送者判定用
				if f.Kind() == reflect.Bool {
					isBot = f.Bool()
				}
			}
		}
		return id, hash, uname, isBot, id != 0
	}
	if v.Kind() != reflect.Struct {
		return 0, 0, "", false, false
	}
	// 2026-10-21 加固: UsersGetUsers 返回壳的字段名按 gotd 惯例是 Users,
	// 但实测日志(fields=[])显示未命中——扩大候选名单逐个尝试
	// 2026-10-28 R143 实测: mtgo v0.19 返回 *tg.GenericVector, 字段名是 Items ([]tg.TLObject),
	// 用户对象在 Items[0](接口元素, findUser 会解引用)。放首位优先命中。
	for _, name := range []string{"Items", "Users", "User", "Contacts", "Chats", "Peer"} {
		f := v.FieldByName(name)
		if !f.IsValid() {
			continue
		}
		if f.Kind() == reflect.Slice && f.Len() > 0 {
			if id, hash, uname, isBot, ok2 := findUser(f.Index(0)); ok2 {
				return id, hash, uname, isBot, true
			}
		}
	}
	// 最后兜底: 结构体里直接找 User 形状(扁平返回)
	if id, hash, uname, isBot, ok3 := findUser(v); ok3 {
		return id, hash, uname, isBot, true
	}
	return 0, 0, "", false, false
}

// pingRaffleBot 私聊激活(061): 参与成功后私聊发公告的机器人, 建立私聊通道——
// 中奖时机器人会私聊"恭喜中奖啦", 这是无同名误判的最可靠中奖信号。
// 用 InputUserFromMessage 从群公告消息反解 bot 的 access_hash(不需要提前知道 bot 用户名)。
// 失败静默(不影响主流程), 10 分钟防重由 join guard 天然保证(同一活动不会重复参与)。
func (e *Engine) pingRaffleBot(ctx context.Context, cl *telegram.Client, chatID int64, msgID int32, botUserID int64, pingText string) {
	peer := e.resolveGroupPeer(ctx, cl, chatID)
	if peer == nil {
		return // 私聊公告不触发
	}
	cctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	res, err := cl.Raw().UsersGetUsers(cctx, &tg.UsersGetUsersRequest{
		ID: []tg.InputUserClass{&tg.InputUserFromMessage{Peer: peer, MsgID: msgID, UserID: botUserID}},
	})
	if err != nil {
		log.Printf("[raffle] 私聊激活: 反解发送者失败 chat=%d bot=%d err=%v", chatID, botUserID, err)
		return
	}
	uID, uHash, uName, _, ok := extractBotViaReflection(res) // R119: 第4返回值 isBot 此处不需要
	if !ok || uHash == 0 {
		// 调试: 打印真实返回结构(2026-10-21 实证——上一轮反射没找到 Users 字段,
		// 靠 %T+字段名日志看清 mtgo 的壳, 下轮按真实结构精准适配)
		log.Printf("[raffle] 私聊激活: 发送者资料缺失 bot=%d resType=%T fields=%v", botUserID, res, structFieldNames(res))
		return
	}
	sctx, scancel := context.WithTimeout(ctx, 15*time.Second)
	defer scancel()
	// 拟人延迟 3~8 秒(激活不必抢快, 但要像随手一点)
	select {
	case <-ctx.Done():
		return
	case <-time.After(time.Duration(3+rand.IntN(6)) * time.Second):
	}
	if err := sendGroupText(sctx, cl, &tg.InputPeerUser{UserID: uID, AccessHash: uHash}, pingText); err != nil {
		log.Printf("[raffle] 私聊激活发送失败 bot=@%s err=%v", uName, err)
		return
	}
	log.Printf("[raffle] 私聊激活成功 bot=@%s ping=%q", uName, pingText)
}

// tapRaffle 消息入口: 命中抽奖公告 → 自动参与。挂在 monitor 消息分发处。
// 061 起入参改为整条消息(私聊激活需要 FromID/MsgID 反解 bot)
func (e *Engine) tapRaffle(ctx context.Context, cl *telegram.Client, tenantID, accountID uuid.UUID, msg *types.Message) {
	chatID := msg.ChatID
	text := msg.Text
	if e.raffleEnabled == nil || !e.raffleEnabled(tenantID) {
		// R142: 开关关闭不再完全静默 —— 遇到像抽奖公告的消息打日志, 便于排查"助手为何不参与"
		if strings.Contains(text, "抽奖") || strings.Contains(text, "关键词") {
			log.Printf("[raffle] 跳过: 租户抽奖总开关未开启 tenant=%s", tenantID)
		}
		return
	}
	// 账号级双闸(改进#1): 该账号被单独关闭抽奖时直接跳过, 即使在线也不参与
	if e.accountRaffleEnabled != nil && !e.accountRaffleEnabled(tenantID, accountID) {
		if strings.Contains(text, "抽奖") || strings.Contains(text, "关键词") {
			log.Printf("[raffle] 跳过: 账号级抽奖开关关闭 acc=%s", accountID)
		}
		return
	}
	kw := extractRaffleKeyword(text)
	if kw == "" {
		return
	}
	// R119: 发送者判定 —— 只有【机器人】发的抽奖公告才参与;
	// 真人复制粘贴机器人消息的假冒公告直接跳过(用户实证: 有人复制机器人消息在群内发,
	// 助手误当成新活动)。反解失败按真人处理(保守跳过, 日志可追查)。
	raw, ok := msg.Raw.(*tg.Message)
	if !ok || raw == nil || raw.FromID == nil {
		log.Printf("[raffle] 跳过: 无法反解发送者 chat=%d", chatID)
		return
	}
	// R120: 转发消息一律跳过 —— 只认准机器人【原发】的活动公告。
	// 他人转发 bot 公告(FwdFrom 指向原消息)不算新活动。
	if raw.FwdFrom != nil {
		log.Printf("[raffle] 跳过: 转发消息(FwdFrom 非空), 只认机器人原发公告 chat=%d", chatID)
		return
	}
	pu, isUser := raw.FromID.(*tg.PeerUser)
	if !isUser || pu.UserID == 0 {
		// R142: 关联频道/匿名管理员以频道身份发公告 —— 非"真人伪造"场景, 放行(免 bot 验证)
		if _, isChan := raw.FromID.(*tg.PeerChannel); isChan {
			log.Printf("[raffle] 发送者: 频道身份发布, 免 bot 验证放行 chat=%d kw=%q", chatID, kw)
		} else {
			log.Printf("[raffle] 跳过: 发送者非用户实体(匿名发布等) chat=%d", chatID)
			return
		}
	} else if !e.isBotSender(ctx, cl, chatID, raw.ID, pu.UserID) {
		log.Printf("[raffle] 跳过: 发送者 %d 非机器人, 真人复制的消息不触发 chat=%d kw=%q", pu.UserID, chatID, kw)
		return
	}
	// 实证日志(2026-10-21): %q 把不可见字符显形(\ufffc 占位符/\u3164 填充符/零宽字符无所遁形),
	// 用于验证"文本实体在纯文本层是否引入特殊符号"。排查期保留, 稳定后可降 debug 级。
	log.Printf("[raffle] 关键词提取 tenant=%s acc=%s chat=%d kw=%q", tenantID, accountID, chatID, kw)
	// ===== 活动级防重(2026-10-21 重设计) =====
	// 两种消息形态按内容区分(见 raffleGuard 注释):
	//   创建公告 "发起了抽奖活动" → 新活动: 重置参与状态 → 参与
	//   查询回复 "已参与/正在进行的抽奖" → 同活动重复曝光: 已参与跳过, 未参与补参
	ak := activityKey(tenantID, accountID, chatID, kw)
	isCreation := strings.Contains(text, "发起了抽奖活动")
	isQueryReply := !isCreation && (strings.Contains(text, "已参与") || strings.Contains(text, "正在进行的抽奖"))
	if isQueryReply && raffleG.activityJoined(ak) {
		log.Printf("[raffle] 账号 %s 群 %d 活动(%s)已参与(查询回复跳过)", accountID, chatID, kw)
		return
	}
	if isCreation {
		raffleG.resetActivity(ak) // 新活动开始, 旧标记作废(同关键词新活动可参与)
	}
	// 60 秒短冷却: 同一公告极速重推(网络重发/多端同步)不重复参与
	now := time.Now()
	raffleG.mu.Lock()
	if t, ok := raffleG.lastJoin[ak]; ok && now.Sub(t) < 60*time.Second {
		raffleG.mu.Unlock()
		return
	}
	raffleG.lastJoin[ak] = now
	raffleG.mu.Unlock()

	safego.Go(func() {
		rctx, cancel := context.WithTimeout(context.Background(), 40*time.Second)
		defer cancel()
		// 持久化防重已移除(2026-10-21): 旧"chat+keyword 永久跳过"会把同关键词新活动挡死,
		// 活动级判定全部由 activities 标记承担(创建公告重置), 重启后旧活动不会重推, 无重复风险
		if err := e.joinRaffle(rctx, cl, chatID, kw); err != nil {
			log.Printf("[raffle] 账号 %s 群 %d 参与抽奖(%s)失败: %v", accountID, chatID, kw, err)
			return
		}
		// 登记回执等待: 机器人回复"已参与成功"时确认(10 分钟窗口)
		e.MarkRafflePendingAck(tenantID, accountID, chatID, kw)
		raffleG.markActivity(ak) // 活动级标记: 同活动后续查询回复不再重复参与

		// R154: 发送成功即触发 joined 事件(不等回执) —— 回执只做确认落库。
		// 原实现等 bot "已参与成功" 回执才触发, 回执丢失会"参与了但邮件漏发"。
		// 活动标记已在上面打上, 回执到达时因 activityJoined 去重会跳过, 不会重复触发。
		if e.raffleWatch != nil && e.raffleWatch.onResult != nil {
			safego.Go(func() { e.raffleWatch.onResult(tenantID, accountID, chatID, "joined", kw, "", "") })
		}
		log.Printf("[raffle] 账号 %s 群 %d 已自动参与抽奖, 关键词: %q (创建公告=%v)", accountID, chatID, kw, isCreation)

		// 私聊激活(061 双重保险): 参与成功后私聊发公告的机器人, 建立私聊通道——
		// 中奖时机器人私聊"恭喜中奖啦"即为铁证(私聊只发本人, 无同名误判)。
		// 从公告消息反解 bot(FromID), 任何机器人的公告都能激活, 无需预配置。
		if e.raffleBotPing != nil {
			if pingOn, pingText := e.raffleBotPing(tenantID); pingOn && strings.TrimSpace(pingText) != "" {
				if raw, ok := msg.Raw.(*tg.Message); ok && raw != nil && raw.FromID != nil {
					if pu, ok := raw.FromID.(*tg.PeerUser); ok && pu.UserID != 0 {
						safego.Go(func() {
							// 不可用 rctx: dispatch 返回即取消, 激活是异步长任务(2026-10-21 context canceled 实锤)
							e.pingRaffleBot(context.Background(), cl, chatID, raw.ID, pu.UserID, strings.TrimSpace(pingText))
						})
					}
				}
			}
		}
	})
}

// R119: bot 身份缓存(userID → isBot), 避免每条公告都反解调用
var botSenderCache sync.Map

// isBotSender 反解消息发送者并判定是否为机器人(与 pingRaffleBot 同款 InputUserFromMessage 反解)。
// 反解失败/非 bot 返回 false —— 保守策略: 拿不准就当真人, 不触发参与。
func (e *Engine) isBotSender(ctx context.Context, cl *telegram.Client, chatID int64, msgID int32, userID int64) bool {
	if v, ok := botSenderCache.Load(userID); ok {
		return v.(bool)
	}
	// resolveGroupPeer 单返回值(mtgo 真实签名)
	peer := e.resolveGroupPeer(ctx, cl, chatID)
	if peer == nil {
		// R142 修复: 反解失败≠不是 bot。静默 return false 会让整个抽奖助手失灵(r140 实测全跳过),
		// 回退放行(= R119 之前行为) + 告警日志, 仅"明确验证为非 bot"才跳过。
		log.Printf("[raffle] 发送者判定: 反解 peer 失败, 回退放行 chat=%d user=%d", chatID, userID)
		return true
	}
	// UsersGetUsers 参数是 *tg.UsersGetUsersRequest(与 pingRaffleBot 一致),
	// 返回 TLObject 用 extractBotViaReflection 提取(含 Bot 字段)
	res, err := cl.Raw().UsersGetUsers(ctx, &tg.UsersGetUsersRequest{ID: []tg.InputUserClass{&tg.InputUserFromMessage{Peer: peer, MsgID: msgID, UserID: userID}}})
	if err != nil {
		// R142 修复: API 失败(网络抖动/被限流)同样回退放行, 不因瞬时故障错过抽奖
		log.Printf("[raffle] 发送者判定: 反解用户失败, 回退放行 chat=%d user=%d err=%v", chatID, userID, err)
		return true
	}
	_, _, _, isBot, ok := extractBotViaReflection(res)
	if !ok {
		// R142 修复: mtgo 各版本返回壳结构不一(2026-10-21 调试: fields=[] 未命中), 解析失败≠非 bot。
		// 回退放行 + 附带结构信息便于后续精准适配; 仅明确验证为非 bot 才跳过。
		log.Printf("[raffle] 发送者判定: 提取用户资料失败, 回退放行 chat=%d user=%d (res=%T fields=%v)",
			chatID, userID, res, structFieldNames(res))
		return true
	}
	botSenderCache.Store(userID, isBot)
	return isBot
}

// joinRaffle 解析群 peer 并发送关键词
// resolveGroupPeer 解析群 chatID → InputPeer(频道取真实 AccessHash, 普通群兜底)
func (e *Engine) resolveGroupPeer(ctx context.Context, cl *telegram.Client, chatID int64) tg.InputPeerClass {
	var peer tg.InputPeerClass
	cctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	if chatID < 0 {
		// 超级群/频道: -100xxxxxxxxxx
		id := int64(-(chatID + 1000000000000))
		res, err := cl.Raw().ChannelsGetChannels(cctx, &tg.ChannelsGetChannelsRequest{
			ID: []tg.InputChannelClass{&tg.InputChannel{ChannelID: id, AccessHash: 0}},
		})
		if err == nil {
			var chats []tg.ChatClass
			switch cv := res.(type) {
			case *tg.MessagesChats:
				chats = cv.Chats
			case *tg.MessagesChatsSlice:
				chats = cv.Chats
			}
			for _, ch := range chats {
				if c, ok := ch.(*tg.Channel); ok && c.ID == id {
					peer = &tg.InputPeerChannel{ChannelID: c.ID, AccessHash: c.AccessHash}
					break
				}
			}
		}
		if peer == nil {
			peer = &tg.InputPeerChat{ChatID: -chatID} // 兜底普通群
		}
	}
	return peer
}

// sendGroupText 向群 peer 发送一条普通文本(抢答关键词/中奖回复共用)
func sendGroupText(ctx context.Context, cl *telegram.Client, peer tg.InputPeerClass, text string) error {
	if peer == nil {
		return nil
	}
	cctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	_, err := cl.Raw().MessagesSendMessage(cctx, &tg.MessagesSendMessageRequest{
		Peer:     peer,
		Message:  text,
		RandomID: rand.Int64(), // TG 强制要求, 缺了报 RANDOM_ID_EMPTY (2026-10-21 实锤)
	})
	return err
}

// sendRaffleReply 中奖后往抽奖群发感谢语(普通群消息, 非回复)——拟人延迟 5~15 秒
func (e *Engine) sendRaffleReply(ctx context.Context, cl *telegram.Client, chatID int64, text string) {
	peer := e.resolveGroupPeer(ctx, cl, chatID)
	if peer == nil {
		return // 私聊不发
	}
	select {
	case <-ctx.Done():
		return
	case <-time.After(time.Duration(5+rand.IntN(11)) * time.Second):
	}
	if err := sendGroupText(ctx, cl, peer, text); err != nil {
		log.Printf("[raffle] 中奖回复发送失败: %v", err)
	}
}

func (e *Engine) joinRaffle(ctx context.Context, cl *telegram.Client, chatID int64, keyword string) error {
	peer := e.resolveGroupPeer(ctx, cl, chatID)
	if peer == nil {
		return nil // 私聊不参与
	}

	// 拟人延迟 2~6 秒
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-time.After(time.Duration(2+rand.IntN(5)) * time.Second):
	}

	sctx, scancel := context.WithTimeout(ctx, 15*time.Second)
	defer scancel()
	_, err := cl.Raw().MessagesSendMessage(sctx, &tg.MessagesSendMessageRequest{
		Peer: peer, Message: keyword, RandomID: rand.Int64(),
	})
	return err
}

// fmt_Sprint 极简 itoa(避免引 fmt 整个包仅为一处)
func fmt_Sprint(i int64) string {
	if i == 0 {
		return "0"
	}
	neg := i < 0
	if neg {
		i = -i
	}
	var buf [20]byte
	p := len(buf)
	for i > 0 {
		p--
		buf[p] = byte('0' + i%10)
		i /= 10
	}
	if neg {
		p--
		buf[p] = '-'
	}
	return string(buf[p:])
}
