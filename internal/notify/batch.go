package notify

// R148 邮件聚合器: 同一活动的多账号通知合并为一封, 避免"9个号一轮抽奖9封邮件"把服务商限额烧光。
// R149: 开奖键去掉群号(同一开奖常经群公告+私聊两条路径各报一次, 带群号会拆两封), 改用活动名合并;
//       来源展示区分群/私聊。窗口: 发起 40s(各号看公告有先后), 开奖 5s(名单同一条消息, 同时解析)。

import (
	"fmt"
	"log"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"

	"tg-sender/internal/domain"
)

type batchEntry struct {
	tenantID uuid.UUID
	event    string            // EventRaffleActivity / EventRaffleWinner
	kind     string            // joined / winner / draw
	chatID   int64             // 展示用: 负数=群号, 正数=私聊(群内来源优先)
	keyword  string            // 活动关键词/活动名
	accounts map[string]bool   // 账号展示名(去重)
	prizes   map[string]string // 账号 → 奖品(仅 winner 用)
	firstAt  time.Time
	timer    *time.Timer
	text     string // R150: 开奖公告原文(draw 转发用)
}

type Batcher struct {
	mu      sync.Mutex
	mailer  *Mailer
	windows map[string]time.Duration // kind → 聚合窗口
	entries map[string]*batchEntry
	tenants func(uuid.UUID) *domain.Tenant // 租户解析(发送时取最新配置)
}

// NewBatcher windows: kind → 窗口(如 {"joined": 40s, "winner": 5s})
func NewBatcher(mailer *Mailer, tenants func(uuid.UUID) *domain.Tenant, windows map[string]time.Duration) *Batcher {
	return &Batcher{mailer: mailer, tenants: tenants, windows: windows, entries: map[string]*batchEntry{}}
}

// Joined 账号参与了某活动(聚合, 40s 窗口)
func (b *Batcher) Joined(tenantID uuid.UUID, chatID int64, keyword, account string) {
	b.add(EventRaffleActivity, "joined", tenantID, chatID, keyword, account, "")
}

// Draw 开奖公告原文转发(R150: 租户要求直接转发机器人开奖消息+群名, 完整名单一目了然,
// 不再按"自己哪个号中奖"过滤 —— 名单里谁中奖租户自己看)。5s 窗口只为合并多账号会话的重复回调。
func (b *Batcher) Draw(tenantID uuid.UUID, chatID int64, activityName, text string) {
	b.addText(EventRaffleWinner, "draw", tenantID, chatID, activityName, text)
}

// Winner 账号中奖(聚合, 5s 窗口 —— 同一条开奖消息所有账号几乎同时解析)
func (b *Batcher) Winner(tenantID uuid.UUID, chatID int64, keyword, account, prize string) {
	b.add(EventRaffleWinner, "winner", tenantID, chatID, keyword, account, prize)
}

// addText: draw 专用 —— 无账号维度, 键=租户|draw|群|活动名, 攒 5s 发一封原文转发
func (b *Batcher) addText(event, kind string, tenantID uuid.UUID, chatID int64, keyword, text string) {
	if b == nil || b.mailer == nil || text == "" {
		return
	}
	key := fmt.Sprintf("%s|%s|%d|%s", tenantID.String(), kind, chatID, keyword)
	b.mu.Lock()
	e, ok := b.entries[key]
	if !ok {
		e = &batchEntry{
			tenantID: tenantID, event: event, kind: kind, chatID: chatID, keyword: keyword,
			accounts: map[string]bool{}, prizes: map[string]string{}, firstAt: time.Now(), text: text,
		}
		b.entries[key] = e
		window := b.windows[kind]
		if window <= 0 {
			window = 5 * time.Second
		}
		e.timer = time.AfterFunc(window, func() { b.flush(key) })
	}
	// R149: 群内来源(负数ID)优先于私聊(正数ID)
	if chatID < 0 || e.chatID == 0 {
		e.chatID = chatID
	}
	b.mu.Unlock()
	log.Printf("[notify] 聚合邮件 %s 活动=%q 群=%d (原文转发)", kind, keyword, chatID)
}

func (b *Batcher) add(event, kind string, tenantID uuid.UUID, chatID int64, keyword, account, prize string) {
	if b == nil || b.mailer == nil || account == "" {
		return
	}
	key := fmt.Sprintf("%s|%s|%d|%s", tenantID.String(), kind, chatID, keyword)
	if kind == "winner" {
		// R149: 开奖聚合键去掉群号 —— 同一开奖会经"群公告 + 私聊通知"两条路径各报一次中奖,
		// 带群号会被拆成两封邮件; 改用活动名做键, 两条路径自然合并, 账号按展示名去重。
		key = fmt.Sprintf("%s|%s|%s", tenantID.String(), kind, keyword)
	}
	b.mu.Lock()
	e, ok := b.entries[key]
	if !ok {
		e = &batchEntry{
			tenantID: tenantID, event: event, kind: kind, chatID: chatID, keyword: keyword,
			accounts: map[string]bool{}, prizes: map[string]string{}, firstAt: time.Now(),
		}
		b.entries[key] = e
		window := b.windows[kind]
		if window <= 0 {
			window = 40 * time.Second
		}
		e.timer = time.AfterFunc(window, func() { b.flush(key) })
	}
	// R149: 展示来源 —— 群内来源(负数ID)优先于私聊(正数ID)
	if chatID < 0 || e.chatID == 0 {
		e.chatID = chatID
	}
	e.accounts[account] = true
	if prize != "" {
		e.prizes[account] = prize
	}
	n := len(e.accounts)
	b.mu.Unlock()
	log.Printf("[notify] 聚合邮件 %s 活动=%q 群=%d 已凑齐账号数=%d", kind, keyword, chatID, n)
}

func (b *Batcher) flush(key string) {
	b.mu.Lock()
	e, ok := b.entries[key]
	if !ok {
		b.mu.Unlock()
		return
	}
	delete(b.entries, key)
	b.mu.Unlock()
	if len(e.accounts) == 0 {
		return
	}
	t := b.tenants(e.tenantID)
	if t == nil {
		log.Printf("[notify] 聚合邮件刷新失败: 租户不存在 tenant=%s", e.tenantID)
		return
	}
	names := make([]string, 0, len(e.accounts))
	for a := range e.accounts {
		names = append(names, a)
	}
	sort.Strings(names)
	cst := time.FixedZone("CST", 8*3600)
	// R149: 来源展示 —— 负数=群, 正数=私聊
	chatLabel := "-"
	if e.chatID < 0 {
		chatLabel = "群 " + strconv.FormatInt(e.chatID, 10)
	} else if e.chatID > 0 {
		chatLabel = "机器人私聊"
	}
	kwLabel := e.keyword
	if kwLabel == "" {
		kwLabel = "未命名活动"
	}
	if e.kind == "draw" {
		var sb strings.Builder
		sb.WriteString("🎉 开奖通知\n\n活动: " + kwLabel + "\n来源: " + chatLabel + "\n开奖时间: " + e.firstAt.In(cst).Format("2006-01-02 15:04:05") + "（北京时间）\n\n—— 开奖原文 ——\n" + e.text + "\n\n——博士轩群发器通知中心")
		b.mailer.Notify(t, EventRaffleWinner, fmt.Sprintf("【博士轩群发器】%s 开奖啦《%s》", chatLabel, kwLabel), sb.String())
		return
	}
	if e.event == EventRaffleWinner {
		var sb strings.Builder
		sb.WriteString("🎉 开奖结果\n\n活动: " + kwLabel + "\n来源: " + chatLabel + "\n开奖时间: " + e.firstAt.In(cst).Format("2006-01-02 15:04:05") + "（北京时间）\n\n中奖账号(" + strconv.Itoa(len(names)) + "):\n")
		for _, a := range names {
			sb.WriteString("  • " + a)
			if p := e.prizes[a]; p != "" {
				sb.WriteString(" — 奖品: " + p)
			}
			sb.WriteString("\n")
		}
		sb.WriteString("\n请尽快前往兑奖, 逾期不候。\n——博士轩群发器通知中心")
		b.mailer.Notify(t, EventRaffleWinner, fmt.Sprintf("【博士轩群发器】开奖《%s》— %d 个账号中奖", kwLabel, len(names)), sb.String())
		return
	}
	var sb strings.Builder
	sb.WriteString("🎯 新抽奖活动\n\n活动关键词: " + kwLabel + "\n来源: " + chatLabel + "\n发现时间: " + e.firstAt.In(cst).Format("2006-01-02 15:04:05") + "（北京时间）\n\n已参与账号(" + strconv.Itoa(len(names)) + "):\n")
	for _, a := range names {
		sb.WriteString("  • " + a + "\n")
	}
	sb.WriteString("\n系统已自动发送关键词参与, 开奖结果将聚合通知。\n——博士轩群发器通知中心")
	b.mailer.Notify(t, EventRaffleActivity, fmt.Sprintf("【博士轩群发器】新抽奖活动《%s》— %d 个账号已参与", kwLabel, len(names)), sb.String())
}
