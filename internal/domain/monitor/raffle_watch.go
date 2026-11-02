package monitor

import (
	"context"
	"log"
	"regexp"
	"strings"
	"sync"
	"time"
	"unicode"

	"github.com/google/uuid"
	"github.com/mtgo-labs/mtgo/telegram"
	"github.com/mtgo-labs/mtgo/telegram/types"
	"github.com/mtgo-labs/mtgo/tg"

	"tg-sender/internal/safego"
)

// ===== 抽奖结果追踪 + 多号抢答并行(2026-10-20) =====
// 真实抽奖机器人交互(TG 群):
//   1) 用户发关键词 → 机器人 reply "您已参与成功, 请等待开奖通知!"
//   2) 开奖 → 机器人发 "活动开奖啦!...恭喜以下中奖用户: <蓝色字体=text_mention实体>"
//
// 并行模型: dispatch 是每账号独立的 OnMessage 回调——各号各自监听各自秒发,
// 互不等候。本模块负责: 开奖解析(text_mention 实体优先)、参与回执确认、
// 同号同活动持久化防重(service 注入查询, 重启不丢)。

var (
	// 开奖特征: 覆盖常见抽奖机器人文案(中文为主, 兼容部分英文 bot)
	raffleDrawRe = regexp.MustCompile(`(?i)开奖啦|已开奖|中奖名单|恭喜.{0,4}(中奖|获奖)|获得奖品|获奖者|winners?|congratulations`)
	// R142: 私聊中奖语义(严格层): 真私聊中奖通知必含"中奖/获奖"; 再叠加 奖品/抽奖群 结构化字段双重确认
	rafflePrivateWinRe = regexp.MustCompile(`(?i)(中奖|获奖)`)
	// 参与成功回执
	raffleJoinAckRe = regexp.MustCompile(`(?i)已参与成功|参与成功|报名成功|joined|entry confirmed`)
	// 中奖行提取: "🎉<名字> 获得奖品:xxx" 整行抓取(名字可含『』/空白符/emoji, 规范化后再比对)
	rafflePrizeTextRe = regexp.MustCompile(`🎉\s*([^\n:：,，]{1,40}?)\s*获得奖品[:：]?\s*([^\n,，]{1,40})`)
	// 私聊中奖通知字段提取(061): "获得奖品: 1USDT" / "抽奖群: 测试"
	rafflePrivatePrizeRe = regexp.MustCompile(`获得奖品[::]\s*([^\n]+)`)
	rafflePrivateGroupRe = regexp.MustCompile(`抽奖群[::]\s*([^\n]+)`)
	// R144: kkpay 话术"中奖奖品: 1USDT"等未带"获得"前缀的场景 —— 按"奖品"泛匹配, 取 金额+单位
	rafflePrivatePrizeLooseRe = regexp.MustCompile(`奖品[::]?\s*([0-9][0-9.,]*(?:\s*[A-Za-z一-龥]{1,6})?)`)
	// R144: "群组: xxx"/"群名: xxx" 等不带"抽奖"前缀的场景
	rafflePrivateGroupLooseRe = regexp.MustCompile(`(?:抽奖群|群组|群名|来自群组)[::]\s*([^\n,，]+)`)
	// 无🎉前缀的兜底: "XX 获得/中得 奖品:xxx"
	rafflePrizeFallbackRe = regexp.MustCompile(`([^\n🎉:：,，]{2,40}?)\s*(?:获得|中得)\s*奖品[:：]?\s*([^\n,，]{1,40})`)
)

// normalizeRaffleName 名字规范化: 剥掉『』/空白/U+3164占位符/emoji, 只留字母数字汉字后小写。
// 抽奖公告里的名字装饰(如 晚安『专业接码代实名』/ㅤㅤ季博达)规范化后才能和账号资料对上。
func normalizeRaffleName(s string) string {
	r := strings.NewReplacer("『", "", "』", "", " ", "", "　", "", "ㅤ", "", "\t", "")
	s = r.Replace(s)
	var b strings.Builder
	for _, ch := range s {
		if unicode.IsLetter(ch) || unicode.IsNumber(ch) {
			b.WriteRune(ch)
		}
	}
	return strings.ToLower(b.String())
}

// raffleWatcher 每引擎实例一个
type raffleWatcher struct {
	mu         sync.Mutex
	pendingAck map[string]time.Time // 参与回执等待: key=tenant:account:chat:keyword → 截止时间
	notified   map[string]time.Time // 开奖通知去重(同活动多条开奖消息)

	// service 注入的回调:
	// 结果落库 + SSE 推送
	onResult func(tenantID, accountID uuid.UUID, chatID int64, kind, keyword, prize, detail string)
	// 账号 TG 身份信息(tg_user_id 优先 / username / first_name 兜底判中奖)
	selfInfo func(tenantID, accountID uuid.UUID) (tgUserID int64, username, firstName string)
	// 持久化防重查询: 该号该群该关键词是否已参与过(raffle_results 表)
	joinedQuery func(tenantID, accountID uuid.UUID, chatID int64, keyword string) bool
}

func newRaffleWatcher() *raffleWatcher {
	w := &raffleWatcher{
		pendingAck: map[string]time.Time{},
		notified:   map[string]time.Time{},
	}
	// 定期清理过期等待与通知去重
	safego.Go(func() {
		t := time.NewTicker(5 * time.Minute)
		defer t.Stop()
		for range t.C {
			now := time.Now()
			w.mu.Lock()
			for k, exp := range w.pendingAck {
				if now.After(exp) {
					delete(w.pendingAck, k)
				}
			}
			for k, at := range w.notified {
				if now.Sub(at) > 30*time.Minute {
					delete(w.notified, k)
				}
			}
			w.mu.Unlock()
		}
	})
	return w
}

// SetRaffleResultHandler 注入结果处理(落 raffle_results + SSE)
func (e *Engine) SetRaffleResultHandler(fn func(tenantID, accountID uuid.UUID, chatID int64, kind, keyword, prize, detail string)) {
	e.raffleWatch.onResult = fn
}

// SetRaffleSelfInfo 注入账号身份(判中奖)
func (e *Engine) SetRaffleSelfInfo(fn func(tenantID, accountID uuid.UUID) (int64, string, string)) {
	e.raffleWatch.selfInfo = fn
}

// SetRaffleJoinedQuery 注入持久化防重查询
func (e *Engine) SetRaffleJoinedQuery(fn func(tenantID, accountID uuid.UUID, chatID int64, keyword string) bool) {
	e.raffleWatch.joinedQuery = fn
}

// MarkRafflePendingAck 发送参与关键词后登记回执等待(raffle.go 调)
func (e *Engine) MarkRafflePendingAck(tenantID, accountID uuid.UUID, chatID int64, keyword string) {
	k := ackKey(tenantID, accountID, chatID, keyword)
	e.raffleWatch.mu.Lock()
	e.raffleWatch.pendingAck[k] = time.Now().Add(10 * time.Minute)
	e.raffleWatch.mu.Unlock()
}

// AlreadyJoined 持久化防重(raffle.go 发送前调; 未注入时退回 false 不拦截)
func (e *Engine) AlreadyJoined(tenantID, accountID uuid.UUID, chatID int64, keyword string) bool {
	if e.raffleWatch.joinedQuery == nil {
		return false
	}
	return e.raffleWatch.joinedQuery(tenantID, accountID, chatID, keyword)
}

func ackKey(tenantID, accountID uuid.UUID, chatID int64, keyword string) string {
	return tenantID.String() + ":" + accountID.String() + ":" + itoa(chatID) + ":" + keyword
}

func itoa(v int64) string {
	if v == 0 {
		return "0"
	}
	neg := v < 0
	if neg {
		v = -v
	}
	var b [20]byte
	i := len(b)
	for v > 0 {
		i--
		b[i] = byte('0' + v%10)
		v /= 10
	}
	if neg {
		i--
		b[i] = '-'
	}
	return string(b[i:])
}

// watchRaffleMessage 在 dispatch 的消息回调里调用(每账号并行, 互不阻塞)
func (e *Engine) watchRaffleMessage(tenantID, accountID uuid.UUID, chatID int64, msg *types.Message, cl *telegram.Client) {
	if msg == nil {
		return
	}
	text := msg.Text
	w := e.raffleWatch

	// --- 0) 私聊中奖通知(061 双重保险·铁证层): 机器人私聊"恭喜中奖啦"只发本人——
	// 无需任何名字比对, 收到即中奖。私聊 chatID > 0(mtgo 约定)。
	// R142 修复(用户反馈"和机器人对话就误报中奖"): 三重收紧 —
	//   ① 排除自己发出的消息(激活私聊/中奖自动回复/手测消息不触发)
	//   ② 排除转发消息(转发领奖通知去兑奖不重复提醒)
	//   ③ 须同时含"中奖语义"(中奖/获奖)与结构化字段("奖品"或"抽奖群") —— 真中奖通知必带奖品字段,
	//      诈骗私聊"恭喜您中奖"极少同时具备
	rawPM, _ := msg.Raw.(*tg.Message)
	selfOrFwd := rawPM != nil && (rawPM.Out || rawPM.FwdFrom != nil)
	if chatID > 0 && w.onResult != nil && !selfOrFwd &&
		rafflePrivateWinRe.MatchString(text) &&
		(strings.Contains(text, "奖品") || strings.Contains(text, "抽奖群")) {
		// R144: 奖品/群组名双路提取 —— 严格话术(获得奖品/抽奖群)优先, 宽松兜底(kkpay 话术)补充。
		// 严格正则捕获到行尾, 需按空白/逗号截断, 避免把整行塞进邮件。
		cutToken := func(s string) string {
			s = strings.TrimSpace(s)
			for i, r := range s {
				if r == ' ' || r == ',' || r == '，' || r == '；' || r == ';' || r == '·' {
					return s[:i]
				}
			}
			return s
		}
		prize := ""
		if m := rafflePrivatePrizeRe.FindStringSubmatch(text); m != nil {
			prize = cutToken(m[1])
		}
		if prize == "" {
			if m := rafflePrivatePrizeLooseRe.FindStringSubmatch(text); m != nil {
				prize = cutToken(m[1])
			}
		}
		groupTitle := ""
		if m := rafflePrivateGroupRe.FindStringSubmatch(text); m != nil {
			groupTitle = cutToken(m[1])
		}
		if groupTitle == "" {
			if m := rafflePrivateGroupLooseRe.FindStringSubmatch(text); m != nil {
				groupTitle = cutToken(m[1])
			}
		}
		// V12 审计修复: 去重 key 原来只有 prize——不同活动同奖品金额(都中 1USDT)
		// 被误判重复而漏报。加 groupTitle 区分活动。
		nk := tenantID.String() + ":pm:" + itoa(chatID) + ":" + groupTitle + ":" + prize
		w.mu.Lock()
		if _, dup := w.notified[nk]; dup {
			w.mu.Unlock()
			return
		}
		w.notified[nk] = time.Now()
		w.mu.Unlock()
		safego.Go(func() {
			// R144: 群组名用书名号包裹; 未解析到时详情不再留空尾巴
			detail := "私聊通知"
			if groupTitle != "" {
				detail = "私聊通知·群组《" + groupTitle + "》"
			}
			// R149: 私聊通知带"活动名称"字段, 提取后作为开奖聚合键(与群公告路径合并为一封)
			w.onResult(tenantID, accountID, chatID, "winner", extractActivityName(text), prize, detail)
		})
		log.Printf("[raffle] 私聊中奖通知 acc=%s prize=%q group=%q", accountID, prize, groupTitle)
		return
	}

	// --- 1) 开奖解析 ---
	// 仅群聊: 私聊(bot私信通知)格式杂且名单不可靠, 2026-10-21 日志实证其名单会混入" 测试"等垃圾
	if chatID < 0 && raffleDrawRe.MatchString(text) && w.selfInfo != nil && w.onResult != nil {
		// R150: 开奖原文转发给租户邮箱(完整中奖名单, 不按自己号过滤)。多账号会话会重复回调,
		// 由 Batcher 按 群+活动名 5s 窗口去重成一封。detail 截 2000 字, 防止长名单被切断。
		w.onResult(tenantID, accountID, chatID, "draw", extractActivityName(text), "", truncateR(text, 2000))
		tgUID, username, firstName := w.selfInfo(tenantID, accountID)
		winners := extractWinners(msg)
		prize := extractPrize(text, winners)

		kind := "mentioned"
		if winners.contains(tgUID, username, firstName) {
			kind = "winner"
		}
		// 原文转储(2026-10-21 实证适配用): %q 显形不可见字符, 实体数/中奖名单/判定结果全记录,
		// 拿到真实开奖消息后按此日志做格式适配, 不再纸上谈兵。
		entCount := 0
		if raw, ok := msg.Raw.(*tg.Message); ok && raw != nil {
			entCount = len(raw.Entities)
		}
		log.Printf("[raffle] 开奖解析 tenant=%s acc=%s chat=%d 实体数=%d 中奖名单=%q 判定=%s 自己=(uid=%d uname=%q name=%q) 原文=%.500q",
			tenantID, accountID, chatID, entCount, winners.names, kind, tgUID, username, firstName, text)
		// 同账号同活动 30 分钟只报一次(开奖消息可能多条/编辑)。
		// 2026-10-21 实锤: 原键缺 accountID, 三账号共享 watcher 去重——
		// 第一个号标记后其余号全被判重复, 中奖回复只剩一个号发。
		nk := tenantID.String() + ":" + accountID.String() + ":" + itoa(chatID) + ":" + strings.Join(winners.names, ",")
		w.mu.Lock()
		if _, dup := w.notified[nk]; dup {
			w.mu.Unlock()
			return
		}
		w.notified[nk] = time.Now()
		w.mu.Unlock()

		safego.Go(func() {
			// R149: 开奖公告标题(emoji前文字)作为活动名, 与私聊通知路径聚合
			w.onResult(tenantID, accountID, chatID, kind, extractActivityName(text), prize, truncateR(text, 300))
		})

		// --- 1b) 中奖自动回复(060): 确认中奖才回, 群消息非回复, 拟人延迟在 sendRaffleReply 内 ---
		if kind == "winner" && cl != nil && e.raffleReply != nil {
			if replyOn, replyTpl := e.raffleReply(tenantID); replyOn && strings.TrimSpace(replyTpl) != "" {
				rp := strings.NewReplacer("{奖品}", prize, "{prize}", prize).Replace(replyTpl)
				rp = strings.TrimSpace(strings.ReplaceAll(rp, "\n", " "))
				if rr := []rune(rp); len(rr) > 200 {
					rp = string(rr[:200])
				}
				if rp != "" {
					safego.Go(func() {
						e.sendRaffleReply(context.Background(), cl, chatID, rp)
					})
				}
			}
		}
		return
	}

	// --- 2) 参与回执确认 ---
	if raffleJoinAckRe.MatchString(text) {
		w.mu.Lock()
		var hitKey string
		prefix := tenantID.String() + ":" + accountID.String() + ":" + itoa(chatID) + ":"
		for k := range w.pendingAck {
			if strings.HasPrefix(k, prefix) {
				hitKey = k
				break
			}
		}
		if hitKey != "" {
			delete(w.pendingAck, hitKey)
		}
		w.mu.Unlock()
		if hitKey != "" {
			// R154: joined 事件已改为"发送成功即触发"(raffle.go), 回执不再触发, 防重复邮件。
			// 回执只清除 pendingAck 记录(确认到达, 避免后续误判)。
		}
	}
}

// R149: 活动名提取(开奖聚合键用) —— 优先"活动名称:"字段(私聊通知话术),
// 否则取开奖标题 emoji 前的文字(如 "牛逼 🎁活动开奖啦!" → "牛逼"); 让群公告与私聊两条路径合并到同一封开奖邮件。
var raffleActivityNameRe = regexp.MustCompile(`活动名称[::]\s*([^\n]+)`)

func extractActivityName(text string) string {
	if m := raffleActivityNameRe.FindStringSubmatch(text); m != nil {
		if name := strings.TrimSpace(m[1]); name != "" {
			return name
		}
	}
	if i := strings.Index(text, "🎁"); i > 0 {
		name := strings.TrimSpace(text[:i])
		if name != "" && len([]rune(name)) <= 20 && !strings.Contains(name, "\n") {
			return name
		}
	}
	return ""
}

func truncateR(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n]) + "…"
}

// ===== 中奖名单提取(text_mention 实体优先, 文本正则兜底) =====

type winnerSet struct {
	ids       map[int64]bool
	names     []string
	usernames map[string]bool // 实体携带的 username(全球唯一, 精确判定层)
}

func (s winnerSet) contains(tgUID int64, username, firstName string) bool {
	// L1: user_id 精确(text_mention 实体)
	if tgUID != 0 && s.ids[tgUID] {
		return true
	}
	u := normalizeRaffleName(strings.TrimPrefix(username, "@"))
	// L2: username 精确(全球唯一, 实体层/自己资料层任一对上即铁证, 同名用户也不会误判)
	if u != "" && s.usernames[u] {
		return true
	}
	fn := normalizeRaffleName(firstName)
	for _, n := range s.names {
		nn := normalizeRaffleName(strings.TrimPrefix(strings.TrimSpace(n), "@"))
		if nn == "" {
			continue
		}
		if u != "" && nn == u {
			return true
		}
		// 全等 > 前缀(名字带后缀装饰如『专业接码代实名』) > 子串(>=3字防误伤)
		// V13 审计修复: len() 是字节数——中文名 2 字=6 字节, 单字中文名(3字节)会
		// 误过 >=2 门槛造成同名误命中。改用 rune 数判断名字长度。
		fnRunes := len([]rune(fn))
		if fn != "" && (nn == fn || (fnRunes >= 2 && strings.HasPrefix(nn, fn)) || (fnRunes >= 3 && strings.Contains(nn, fn))) {
			return true
		}
	}
	return false
}

// extractWinners 中奖名单提取: 文本正则为主("XX 获得奖品:xxx" 覆盖 text_mention 的纯文本形态)。
// 注: mtgo 的 Entities 元素是 MessageEntityClass 接口需逐类断言, 为免猜具体类型名
// 引入编译循环, 采用文本方案——开奖消息文本里提及的名字与实体一一对应, 判中奖精度足够。
// utf16OffsetToRune 把 TG 实体的 UTF-16 码元偏移换算成 Go rune 索引(V10 修复用):
// TG 协议中实体 Offset/Length 按 UTF-16 码元计, BMP 外字符(emoji 等)占 2 个码元。
func utf16OffsetToRune(rs []rune, utf16Off int) int {
	cnt := 0
	for i, r := range rs {
		if cnt >= utf16Off {
			return i
		}
		if r > 0xFFFF {
			cnt += 2
		} else {
			cnt++
		}
	}
	return len(rs)
}

func extractWinners(msg *types.Message) winnerSet {
	s := winnerSet{ids: map[int64]bool{}, usernames: map[string]bool{}}
	collect := func(name string) {
		if n := normalizeRaffleName(name); n != "" {
			s.names = append(s.names, name)
		}
	}
	// L1 实体层: text_mention 自带 UserID(蓝色字体=实体, 同名用户的终极判据)。
	// mtgo 实体元素是指针接收者类型(2026-10-21 两轮编译实证): 断言 *tg.MessageEntityMentionName,
	// 该结构体只有 UserID 字段; 断言失败自动降级文本层, 不影响主流程。
	if raw, ok := msg.Raw.(*tg.Message); ok && raw != nil {
		for _, ent := range raw.Entities {
			switch te := ent.(type) {
			case *tg.MessageEntityMentionName:
				if te != nil && te.UserID != 0 {
					s.ids[te.UserID] = true
				}
			case *tg.MessageEntityMention:
				// V10 审计修复: @username 实体(Mention)原被忽略——usernames 集合
				// (L2 精确判定层)拿不到它, 同名中奖人只能靠 L3 文本名单匹配, 误判。
				// 注意 TG 实体偏移是 UTF-16 码元, 直接按 rune 切含 emoji 名字会切错。
				if te == nil || msg.Text == "" || te.Offset < 0 || te.Length <= 0 {
					continue
				}
				rs := []rune(msg.Text)
				// te.Offset/Length 是 int32(TG 协议), helper 收 int——显式转换
				start := utf16OffsetToRune(rs, int(te.Offset))
				end := utf16OffsetToRune(rs, int(te.Offset+te.Length))
				if start < 0 || end > len(rs) || start >= end {
					continue
				}
				if u := normalizeRaffleName(string(rs[start:end])); u != "" {
					s.usernames[u] = true
				}
			}
		}
	}
	for _, m := range rafflePrizeTextRe.FindAllStringSubmatch(msg.Text, -1) {
		if len(m) >= 2 {
			collect(m[1])
		}
	}
	// 兜底: 无🎉前缀时按通用格式抓
	if len(s.names) == 0 {
		for _, m := range rafflePrizeFallbackRe.FindAllStringSubmatch(msg.Text, -1) {
			if len(m) >= 2 {
				collect(m[1])
			}
		}
	}
	// 通用兜底: "恭喜以下中奖用户:" 之后的庆祝行取首词。
	// 2026-10-21 按真实开奖日志实证收紧: 原实现"每行首词全收"会把
	// "中奖用户:"/"👮🏼"/"『联系...领取奖品』" 等垃圾混进名单, "🎉尊"碎片
	// 规范化后仅1字, 可能误伤同名短名账号——现在只认 🎉/🎊 开头的行,
	// 剥掉庆祝emoji后规范化名字 ≥2 字才收。
	if i := strings.Index(msg.Text, "中奖用户"); i >= 0 {
		rest := msg.Text[i:]
		for _, line := range strings.Split(rest, "\n") {
			line = strings.TrimSpace(strings.TrimPrefix(line, "├"))
			if !strings.HasPrefix(line, "🎉") && !strings.HasPrefix(line, "🎊") {
				continue // 只认庆祝emoji开头的行, 表头/管理员/联系语整行跳过
			}
			fields := strings.Fields(line)
			if len(fields) == 0 {
				continue
			}
			w := strings.TrimLeft(fields[0], "🎉🎊")
			if n := normalizeRaffleName(w); len(n) >= 2 {
				s.names = append(s.names, w)
			}
		}
	}
	return s
}

// extractPrize 从自己命中的行提取奖品描述
func extractPrize(text string, winners winnerSet) string {
	for _, m := range rafflePrizeTextRe.FindAllStringSubmatch(text, -1) {
		if len(m) >= 3 {
			mn := normalizeRaffleName(m[1])
			for _, n := range winners.names {
				nn := normalizeRaffleName(n)
				if mn != "" && nn != "" && (strings.Contains(mn, nn) || strings.Contains(nn, mn)) {
					return strings.TrimSpace(m[2])
				}
			}
		}
	}
	if i := strings.Index(text, "奖品"); i >= 0 && i+6 <= len(text) {
		rest := strings.TrimLeft(text[i+6:], " :：\n")
		if rest != "" {
			r := []rune(rest)
			if len(r) > 40 {
				rest = string(r[:40])
			}
			return rest
		}
	}
	return ""
}

