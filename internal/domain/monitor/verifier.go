package monitor

import (
	"context"
	"fmt"
	"log"
	"math/rand/v2"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/mtgo-labs/mtgo/telegram"
	"github.com/mtgo-labs/mtgo/tg"
	"tg-sender/internal/ai"
	"tg-sender/internal/domain"
	"tg-sender/internal/safego"
)

// 入群验证执行器(对齐 tgegg 帮助中心"进群AI验证"全部六种模式):
//   ① 单URL关注按钮 ② 加入+我已加入双按钮(30s) ③ 单回调点我验证(1min)
//   ④ bot深链(/start→Poll→回群) ⑤ 图片算式+答案按钮 ⑥ GIF字符码+答案按钮(90s)
// 流程: 检测 → 多步执行(规则优先, AI仅图片兜底) → 回群发测试消息复测 → 记录。
// 停止条件: Stars付费 / 外部网页 / 两轮失败 / 整体超时120s。
// API 依据: mtgo 官方文档(pkg.go.dev github.com/mtgo-labs/mtgo/telegram/types):
//   types.Message 携带 Raw tg.MessageClass(完整原始消息) → extractCandidate 直接可用;
//   Poll 选项原始字节为 PollAnswer.Option, 投票用 MessagesSendVote{Options: [][]byte}。

var (
	verifyKeywordRe = regexp.MustCompile(`验证|解禁|解除限制|点击|订阅|关注|verify|captcha|unmute|subscribe|回答|选择`)
	joinConfirmRe   = regexp.MustCompile(`已加入|已关注|我已|确认|完成|加入|关注|verify|claim|continue|check|done|同意|通过|继续|进入|submit|agree|allow|proceed`)
	mathExprRe      = regexp.MustCompile(`(\d+)\s*([+\-×x*÷/])\s*(\d+)(?:\s*([+\-×x*÷/])\s*(\d+))?`)
	starsRe         = regexp.MustCompile(`(?i)stars|xtr|ton|usdt|支付|付款|购买`)
)

// vstate 一次验证的状态机
type vstate struct {
	mu       sync.Mutex
	logID    uuid.UUID
	tenantID uuid.UUID
	accID    uuid.UUID
	chatID   int64
	title    string
	deadline time.Time
	attempts int
	steps    []any
	finished bool
}

func (s *vstate) addStep(action, detail string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.steps = append(s.steps, map[string]any{"action": action, "detail": detail, "at": time.Now().Format("15:04:05")})
}

// verifierState 每(账号,群)进行中的验证
type verifierState struct {
	mu     sync.Mutex
	byChat map[string]*vstate // key: accountID:chatID
}

func newVerifierState() *verifierState { return &verifierState{byChat: map[string]*vstate{}} }

func (vs *verifierState) key(accountID uuid.UUID, chatID int64) string {
	return accountID.String() + ":" + fmt.Sprint(chatID)
}

func (vs *verifierState) get(accountID uuid.UUID, chatID int64) *vstate {
	vs.mu.Lock()
	defer vs.mu.Unlock()
	return vs.byChat[vs.key(accountID, chatID)]
}

func (vs *verifierState) put(accountID uuid.UUID, chatID int64, s *vstate) {
	vs.mu.Lock()
	defer vs.mu.Unlock()
	vs.byChat[vs.key(accountID, chatID)] = s
}

func (vs *verifierState) remove(accountID uuid.UUID, chatID int64) {
	vs.mu.Lock()
	defer vs.mu.Unlock()
	delete(vs.byChat, vs.key(accountID, chatID))
}

// SetVerifierDeps 注入验证依赖(container 接线)
func (e *Engine) SetVerifierDeps(vlRepo VerifierLogStore, keyProvider func(tenantID uuid.UUID) (provider, apiKey, model string, enabled bool)) {
	e.vlRepo = vlRepo
	e.aiKeyProvider = keyProvider
	e.vstates = newVerifierState()
}

// VerifierLogStore 抽象(避免反向依赖 repository 包的具体类型)
type VerifierLogStore interface {
	Create(ctx context.Context, l *domain.VerificationLog) error
	UpdateStatus(ctx context.Context, vlID uuid.UUID, status, reason string, steps []any) error
}

// vcandidate 从消息提取的验证要素
type vcandidate struct {
	msgID     int32 // tg.Message.ID 为 int32
	chatID    int64
	rawPeer   tg.PeerClass      // 原始 Peer(v0.19 的 Peer* 不带 AccessHash, 需查库)
	peer      tg.InputPeerClass // 由 resolvePeer 解析填充
	text      string
	buttons   []vbutton // 全部内联按钮
	poll      *tg.Poll  // 原生投票(可能为nil)
	hasImage  bool
	imageData []byte   // 已下载的图片/GIF首帧(AI 识别用)
	imageMime string
	rawPhoto  *tg.Photo   // 原始照片(下载用)
	rawDoc    *tg.Document // 原始GIF文档(下载首帧用)
}

type vbutton struct {
	text string
	data []byte // 回调数据(空=URL按钮)
	url  string
}

// HandleVerification 消息入口: 判定是否为验证消息, 是则启动/推进验证状态机。
func (e *Engine) HandleVerification(ctx context.Context, client *telegram.Client, tenantID, accountID uuid.UUID, msg *tg.Message, chatID int64, chatTitle string) {
	if e.vstates == nil || e.aiKeyProvider == nil {
		return
	}
	_, _, _, enabled := e.aiKeyProvider(tenantID)
	if !enabled {
		return
	}
	cand := extractCandidate(msg)
	cand.chatID = chatID // 会话定位(状态机 key)
	e.resolvePeer(ctx, client, cand)
	if cand.peer == nil {
		return // 私聊类验证无 access_hash 来源, 暂不支持
	}
	if !looksLikeVerification(cand) {
		return
	}
	if st := e.vstates.get(accountID, chatID); st != nil && time.Now().Before(st.deadline) {
		return // 同群已有进行中验证
	}
	st := &vstate{
		logID: uuid.New(), tenantID: tenantID, accID: accountID,
		chatID: chatID, title: chatTitle,
		deadline: time.Now().Add(120 * time.Second),
	}
	st.addStep("detect", truncate(cand.text, 80))
	if e.vlRepo != nil {
		_ = e.vlRepo.Create(ctx, &domain.VerificationLog{
			VLID: st.logID, TenantID: tenantID, AccountID: accountID,
			GroupChatID: chatID, GroupTitle: chatTitle, Status: "pending", Steps: st.steps,
		})
	}
	e.vstates.put(accountID, chatID, st)
	safego.Go(func() { e.runVerification(ctx, client, st, cand) })
}

func looksLikeVerification(c *vcandidate) bool {
	if len(c.buttons) > 0 && verifyKeywordRe.MatchString(c.text) {
		return true
	}
	if c.poll != nil {
		return true
	}
	if c.hasImage && verifyKeywordRe.MatchString(c.text) {
		return true
	}
	// P1-22 审计修复: "我是人类 ✅" 这类纯按钮+中性文案的验证原来漏检(无关键词)。
	// 启发式: 单按钮、文案极短(<12字)、文本弱验证倾向 → 纳入验证候选。
	if len(c.buttons) == 1 && len([]rune(c.text)) < 12 {
		bt := c.buttons[0].text
		if len([]rune(bt)) <= 15 {
			return true
		}
	}
	return false
}

// runVerification 验证主流程: 执行一轮动作 → 复测发言 → 不行再一轮 → 结算
func (e *Engine) runVerification(ctx context.Context, client *telegram.Client, st *vstate, cand *vcandidate) {
	defer e.vstates.remove(st.accID, st.chatID)

	// 预下载验证图片(算式图/字符码, 供 AI 兜底)
	e.loadCandidateMedia(ctx, client, cand)

	for round := 1; round <= 2; round++ {
		if time.Now().After(st.deadline) {
			break
		}
		st.mu.Lock()
		st.attempts = round
		st.mu.Unlock()

		actionErr := e.executeRound(ctx, client, st, cand)
		if actionErr != nil {
			st.addStep("stop", actionErr.Error())
			e.finish(st, "stopped", actionErr.Error())
			return
		}
		if e.retestSpeak(ctx, client, st, cand) {
			e.finish(st, "pass", fmt.Sprintf("第 %d 轮通过", round))
			return
		}
		st.addStep("retry", "发言复测未通过, 进行下一轮")
	}
	e.finish(st, "fail", "两轮尝试仍未解除限制")
}

// executeRound 执行一轮验证动作, 返回非nil=应停止(付费/外链/放弃)
func (e *Engine) executeRound(ctx context.Context, client *telegram.Client, st *vstate, cand *vcandidate) error {
	if starsRe.MatchString(cand.text) {
		return fmt.Errorf("付费验证(Stars), 不处理")
	}
	var urlTargets, callbacks []vbutton
	for _, b := range cand.buttons {
		if len(b.data) > 0 {
			callbacks = append(callbacks, b)
		} else if b.url != "" {
			urlTargets = append(urlTargets, b)
		}
	}

	// 1. 原生 Poll: 本地算答案 → 按原始选项字节投票
	if cand.poll != nil {
		if ans := solvePoll(cand.poll); ans >= 0 && ans < len(cand.poll.Answers) {
			pa, ok := cand.poll.Answers[ans].(*tg.PollAnswer)
			if !ok {
				st.addStep("poll_skip", "Poll 选项类型异常")
				return nil
			}
			if err := e.votePoll(ctx, client, cand, pa.Option); err != nil {
				st.addStep("poll_fail", truncate(err.Error(), 100))
			} else {
				st.addStep("poll", "已投正确选项")
				return nil
			}
		} else {
			// V1/V2 审计修复尝试的更正(2026-10-23 构建实证): tg.Poll 在本项目 gotd
			// 版本中【没有】CorrectOption/CorrectOptions 字段——TG 协议对客户端隐藏
			// quiz 正确答案(隐私设计), 官方答案兜底在此库版本不可行。
			// 回退到纯启发式: 算式/关键词答案 + 全部选项兜底点击由后续逻辑处理。
			var correct []int32
			voted := 0
			for _, opt := range correct {
				if opt < 0 || int(opt) >= len(cand.poll.Answers) {
					continue
				}
				pa, ok := cand.poll.Answers[opt].(*tg.PollAnswer)
				if !ok {
					continue
				}
				if err := e.votePoll(ctx, client, cand, pa.Option); err != nil {
					st.addStep("poll_fail", truncate(err.Error(), 100))
				} else {
					voted++
				}
			}
			if voted > 0 {
				st.addStep("poll", fmt.Sprintf("已投 %d 个正确选项(quiz 官方答案)", voted))
				return nil
			}
			st.addStep("poll_skip", "Poll 无算式可解, 放弃(TG 协议不向客户端公开 quiz 答案)")
		}
	}

	// 2. URL按钮: t.me 频道/群 → 加入; bot 深链 → /start
	joinedAny := false
	for _, ub := range urlTargets {
		if starsRe.MatchString(ub.text) || strings.Contains(ub.url, "t.me/Stars") {
			return fmt.Errorf("付费验证按钮, 不处理")
		}
		if !strings.Contains(ub.url, "t.me/") && !strings.Contains(ub.url, "telegram.me/") {
			return fmt.Errorf("外部链接验证(%.40s), 不处理", ub.url)
		}
		if err := e.followUrlAction(ctx, client, st, ub); err != nil {
			st.addStep("url_fail", truncate(err.Error(), 100))
		} else {
			joinedAny = true
		}
	}

	// 3. 算式: 点匹配答案按钮, 无按钮则直接回复答案
	if ans, ok := solveArithmetic(cand.text); ok {
		if e.clickButtonByAnswer(ctx, client, st, cand, fmt.Sprint(ans)) {
			return nil
		}
		if err := e.replyText(ctx, client, cand, fmt.Sprint(ans)); err == nil {
			st.addStep("math_reply", fmt.Sprint(ans))
			return nil
		}
		// V3: 算式答案投递双失败同样留痕(不 return——下面按钮启发式仍可能救场)
		st.addStep("math_deliver_fail", fmt.Sprint(ans))
	}

	// 4. 回调按钮: 刚join过优先「已加入/确认」类; 单按钮直接点
	if len(callbacks) > 0 {
		if pick := chooseCallbackButton(callbacks, joinedAny); pick != nil {
			if err := e.clickCallback(ctx, client, cand, *pick); err != nil {
				st.addStep("click_fail", truncate(err.Error(), 100))
			} else {
				st.addStep("click", truncate(pick.text, 30))
				return nil
			}
		}
	}

	// 5. 图片/GIF: AI 识别(未配置 Key 则停止并说明)
	if cand.hasImage && len(cand.imageData) > 0 {
		provider, key, model, _ := e.aiKeyProvider(st.tenantID)
		if key == "" {
			return fmt.Errorf("图片验证需要 AI Key(设置页免费申请)")
		}
		ans, err := ai.AskImage(ctx, provider, key, model, cand.imageMime, cand.imageData,
			"这是Telegram进群验证图。只输出题目要求的答案(数字结果或图中字符), 不要输出任何其他文字。")
		if err != nil {
			st.addStep("ai_fail", truncate(err.Error(), 100))
			return fmt.Errorf("AI 识别失败: %w", err)
		}
		ans = strings.TrimSpace(ans)
		st.addStep("ai_answer", ans)
		if e.clickButtonByAnswer(ctx, client, st, cand, ans) {
			return nil
		}
		if err := e.replyText(ctx, client, cand, ans); err == nil {
			return nil
		}
		// V3 审计修复: 答案已生成(AI token 已花)但按钮+回复双失败——原来无声吞掉,
		// 下一轮状态机空转再花一次 AI token 重识别。记录步骤让失败可见、可诊断。
		st.addStep("answer_deliver_fail", truncate(ans, 50))
	}
	return nil
}

// retestSpeak 复测: 发测试消息 → 等待 → 查消息是否还在(没被删=已解禁)
func (e *Engine) retestSpeak(ctx context.Context, client *telegram.Client, st *vstate, cand *vcandidate) bool {
	peer := cand.peer
	if peer == nil {
		return false
	}
	time.Sleep(time.Duration(6+rand.IntN(6)) * time.Second)
	testText := fmt.Sprintf("%d", rand.IntN(90)+10)
	sctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	sent, err := client.Raw().MessagesSendMessage(sctx, &tg.MessagesSendMessageRequest{
		Peer: peer, Message: testText, RandomID: rand.Int64(),
	})
	if err != nil {
		st.addStep("retest_send_fail", truncate(err.Error(), 80))
		return false
	}
	msgID := firstSentMsgID(sent)
	time.Sleep(time.Duration(12+rand.IntN(10)) * time.Second)
	hctx, hcancel := context.WithTimeout(ctx, 20*time.Second)
	defer hcancel()
	// 返回类型为接口 tg.MessagesMessagesClass, 需断言取 .Messages
	// V5 审计修复: 原 Limit:10——测试消息发出 12~21 秒后才来查, 活跃群早被顶出前 10 条,
	// 消息明明发出却误判"仍被禁言"。放宽到 30 条。
	histRaw, err := client.Raw().MessagesGetHistory(hctx, &tg.MessagesGetHistoryRequest{Peer: peer, Limit: 30})
	if err != nil {
		return false
	}
	var msgs []tg.MessageClass
	switch hv := histRaw.(type) {
	case *tg.MessagesMessages:
		msgs = hv.Messages
	case *tg.MessagesMessagesSlice:
		msgs = hv.Messages
	case *tg.MessagesChannelMessages:
		msgs = hv.Messages
	}
	for _, m := range msgs {
		// V6 审计修复: msgID 是 int(64 位), TG 消息 ID 已超 int32 范围——
		// int32 截断后匹配/删除的是错消息。nm.ID 是 int32, 向 int 加宽比较避免截断。
		if nm, ok := m.(*tg.Message); ok && int(nm.ID) == msgID {
			st.addStep("retest", "测试消息仍在, 验证通过")
			dctx, dcancel := context.WithTimeout(ctx, 10*time.Second)
			// V6 审计修复: msgID 是 int(64 位)且 TG 消息 ID 已超 int32——截断值对不上就
			// 放弃删除(测试消息留在群里无害, 删错别人消息才要命)
			delID := int32(msgID)
			if int64(delID) == int64(msgID) {
				_, _ = client.Raw().MessagesDeleteMessages(dctx, &tg.MessagesDeleteMessagesRequest{
					ID: []int32{delID}, Revoke: true,
				})
			}
			dcancel()
			return true
		}
	}
	st.addStep("retest_fail", "测试消息消失, 仍被禁言")
	return false
}

func (e *Engine) finish(st *vstate, status, reason string) {
	st.mu.Lock()
	st.finished = true
	steps := append([]any(nil), st.steps...)
	st.mu.Unlock()
	if e.vlRepo != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		_ = e.vlRepo.UpdateStatus(ctx, st.logID, status, reason, steps)
		cancel()
	}
	// R119: 验证结果通知回调(pass→verify_solved 邮件, 由 container 注入)
	if e.verifyNotify != nil {
		e.verifyNotify(st.tenantID, st.accID, st.title, status, reason)
	}
	log.Printf("[verifier] 账号 %s 群 %s: %s (%s)", st.accID, st.title, status, reason)
}

// ── 执行动作 ──

func (e *Engine) clickCallback(ctx context.Context, client *telegram.Client, cand *vcandidate, b vbutton) error {
	if cand.peer == nil {
		return fmt.Errorf("无会话 peer")
	}
	time.Sleep(time.Duration(1+rand.IntN(2)) * time.Second)
	cctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	_, err := client.Raw().MessagesGetBotCallbackAnswer(cctx, &tg.MessagesGetBotCallbackAnswerRequest{
		Peer: cand.peer, MsgID: cand.msgID, Data: b.data,
	})
	return err
}

// followUrlAction URL按钮: 频道/群 → 加入; bot 深链 → /start
func (e *Engine) followUrlAction(ctx context.Context, client *telegram.Client, st *vstate, b vbutton) error {
	username, startParam, isJoinChat := parseTmeLink(normalizeTme(b.url))
	if username == "" {
		return fmt.Errorf("无法解析链接: %.40s", b.url)
	}
	cctx, cancel := context.WithTimeout(ctx, 40*time.Second)
	defer cancel()
	// P0-14 审计修复: 原来先 ResolvePeer——私密群邀请链接(t.me/+hash)解析必失败,
	// 100% 死在 ResolvePeer 到不了下面的 import 分支。isJoinChat 提前直走 ImportChatInvite。
	if isJoinChat {
		if _, err := client.Raw().MessagesImportChatInvite(cctx, &tg.MessagesImportChatInviteRequest{Hash: username}); err != nil {
			return err
		}
		st.addStep("join", username)
		return nil
	}
	peer, err := client.ResolvePeer(cctx, username)
	if err != nil {
		return fmt.Errorf("解析 %s 失败: %w", username, err)
	}
	if startParam != "" { // bot 深链
		if _, err := client.Raw().MessagesSendMessage(cctx, &tg.MessagesSendMessageRequest{
			Peer: peer, Message: "/start " + startParam, RandomID: rand.Int64(),
		}); err != nil {
			return err
		}
		st.addStep("bot_start", username)
		return nil
	}
	// 公开频道/群加入(走 ResolvePeer 成功的 peer)。注: messages.joinChat 已在新版 TL 移除。
	if p, ok := peer.(*tg.InputPeerChannel); ok {
		if _, err := client.Raw().ChannelsJoinChannel(cctx, &tg.ChannelsJoinChannelRequest{
			Channel: &tg.InputChannel{ChannelID: p.ChannelID, AccessHash: p.AccessHash},
		}); err != nil {
			return err
		}
	}
	st.addStep("join", username)
	return nil
}

// votePoll 投票(选项原始字节, 非序号)
func (e *Engine) votePoll(ctx context.Context, client *telegram.Client, cand *vcandidate, option []byte) error {
	if cand.peer == nil {
		return fmt.Errorf("无会话 peer")
	}
	cctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	_, err := client.Raw().MessagesSendVote(cctx, &tg.MessagesSendVoteRequest{
		Peer: cand.peer, MsgID: cand.msgID, Options: [][]byte{option},
	})
	return err
}

// clickButtonByAnswer 点文本等于答案的回调按钮
func (e *Engine) clickButtonByAnswer(ctx context.Context, client *telegram.Client, st *vstate, cand *vcandidate, ans string) bool {
	ans = strings.TrimSpace(ans)
	if ans == "" {
		return false
	}
	// V11 审计修复: 原来严格 == 匹配——按钮文案是 "8 " / "答案: 8" / "8️⃣" 都不匹配。
	// 宽松化: 提取按钮文本中的数字与答案数字比对(数字按钮验证的常规形态)。
	ansNum, ansIsNum := extractNumber(ans)
	for _, b := range cand.buttons {
		if len(b.data) == 0 {
			continue
		}
		bt := strings.TrimSpace(b.text)
		exact := bt == ans
		numMatch := false
		if !exact && ansIsNum {
			if bn, ok := extractNumber(bt); ok && bn == ansNum {
				numMatch = true
			}
		}
		if exact || numMatch {
			if err := e.clickCallback(ctx, client, cand, b); err == nil {
				st.addStep("click_answer", bt)
				return true
			}
		}
	}
	return false
}

// extractNumber 从文本提取第一个整数(忽略空白/冒号/emoji 等非数字前缀)
func extractNumber(s string) (int, bool) {
	var digits []rune
	for _, r := range s {
		if r >= '0' && r <= '9' {
			digits = append(digits, r)
		} else if len(digits) > 0 {
			break // 已开始取数字后遇非数字即停
		}
	}
	if len(digits) == 0 {
		return 0, false
	}
	n := 0
	for _, r := range digits {
		n = n*10 + int(r-'0')
	}
	return n, true
}

func (e *Engine) replyText(ctx context.Context, client *telegram.Client, cand *vcandidate, text string) error {
	if cand.peer == nil {
		return fmt.Errorf("无会话 peer")
	}
	cctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	_, err := client.Raw().MessagesSendMessage(cctx, &tg.MessagesSendMessageRequest{
		Peer: cand.peer, Message: text, RandomID: rand.Int64(),
	})
	return err
}

// loadCandidateMedia 预下载验证图片(Photo 最大尺寸 / GIF Document 缩略图首帧)
func (e *Engine) loadCandidateMedia(ctx context.Context, client *telegram.Client, cand *vcandidate) {
	// P2-14 审计清理: 原空 if 块已删——注释语义并入下方逻辑说明:
	// poll 与按钮类候选不需要图; 图片类通常也有按钮, 保留下载通道。
	var loc tg.InputFileLocationClass
	mime := "image/jpeg"
	if p := cand.rawPhoto; p != nil && len(p.Sizes) > 0 {
		// 取最大 PhotoSize
		var best *tg.PhotoSize
		var hasStripped bool
		for _, s := range p.Sizes {
			if ps, ok := s.(*tg.PhotoSize); ok {
				if best == nil || ps.W > best.W {
					best = ps
				}
			} else if _, ok := s.(*tg.PhotoStrippedSize); ok {
				hasStripped = true
			}
		}
		if best == nil {
			// V4 审计修复: 仅有 PhotoStrippedSize(省流缩略图)时, stripped 没有
			// 标准 file location 可下载——原实现静默 return, 图片验证无声跳过。
			// 至少留可见日志; 候选不标 hasImage, 走既有文本/AI 兜底通道。
			if hasStripped {
				log.Printf("[verifier] 图片仅 stripped 缩略图无法定位下载, 走文本兜底 chat=%d", cand.chatID)
			}
			return
		}
		loc = &tg.InputPhotoFileLocation{
			ID: p.ID, AccessHash: p.AccessHash, FileReference: p.FileReference,
			ThumbSize: best.Type,
		}
	} else if d := cand.rawDoc; d != nil && len(d.Thumbs) > 0 {
		mime = "image/gif"
		var best *tg.PhotoSize
		var hasStripped bool
		for _, s := range d.Thumbs {
			if ps, ok := s.(*tg.PhotoSize); ok {
				if best == nil || ps.W > best.W {
					best = ps
				}
			} else if _, ok := s.(*tg.PhotoStrippedSize); ok {
				hasStripped = true
			}
		}
		if best == nil {
			// V4 审计修复: 同 Photo 分支——stripped 无标准下载位置, 可见日志+文本兜底
			if hasStripped {
				log.Printf("[verifier] 文档缩略图仅 stripped 无法定位下载, 走文本兜底 chat=%d", cand.chatID)
			}
			return
		}
		loc = &tg.InputDocumentFileLocation{
			ID: d.ID, AccessHash: d.AccessHash, FileReference: d.FileReference,
			ThumbSize: best.Type,
		}
	} else {
		return
	}
	cctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	res, err := client.Raw().UploadGetFile(cctx, &tg.UploadGetFileRequest{Location: loc, Limit: 512 * 1024})
	if err != nil {
		return
	}
	f, ok := res.(*tg.UploadFile)
	if !ok || len(f.Bytes) < 50 {
		return
	}
	cand.hasImage = true
	cand.imageData = f.Bytes
	cand.imageMime = mime
}

// ── 工具 ──

// peerChatID PeerClass → 会话 ID(用户为正, 群为负)
func peerChatID(p tg.PeerClass) int64 {
	switch v := p.(type) {
	case *tg.PeerUser:
		return v.UserID
	case *tg.PeerChat:
		return -int64(v.ChatID)
	case *tg.PeerChannel:
		return -1000000000000 - int64(v.ChannelID)
	}
	return 0
}

func truncate(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n]) + "…"
}

func normalizeTme(u string) string {
	u = strings.TrimSpace(u)
	for _, p := range []string{"https://", "http://", "www.", "telegram.me/", "telegram.dog/", "t.me/"} {
		u = strings.TrimPrefix(u, p)
	}
	return u
}

// parseTmeLink 解析 "username" | "username?start=x" | "+joinhash" | "joinchat/hash"
func parseTmeLink(path string) (username, startParam string, isJoinChat bool) {
	if strings.HasPrefix(path, "+") {
		return strings.TrimPrefix(path, "+"), "", true
	}
	if strings.HasPrefix(path, "joinchat/") {
		return strings.TrimPrefix(path, "joinchat/"), "", true
	}
	if i := strings.IndexByte(path, '?'); i >= 0 {
		for _, kv := range strings.Split(path[i+1:], "&") {
			// V9 审计修复: 原来只解析 start= —— bot 的 startgroup= 深链(加群邀请)
			// 解析不到参数, 加群类验证按钮 100% 失败
			if strings.HasPrefix(kv, "start=") {
				startParam = strings.TrimPrefix(kv, "start=")
			}
			if strings.HasPrefix(kv, "startgroup=") {
				startParam = strings.TrimPrefix(kv, "startgroup=")
			}
		}
		path = path[:i]
	}
	path = strings.Trim(path, "/")
	if path == "" {
		return "", "", false
	}
	return path, startParam, false
}

// solveArithmetic 从文本解算式(支持 a+b / a+b+c)
func solveArithmetic(text string) (int, bool) {
	m := mathExprRe.FindStringSubmatch(text)
	if m == nil {
		return 0, false
	}
	nums := []string{m[1], m[3]}
	ops := []string{m[2]}
	if m[5] != "" {
		nums = append(nums, m[5])
		ops = append(ops, m[4])
	}
	vals := make([]int, 0, len(nums))
	for _, n := range nums {
		var v int
		if _, err := fmt.Sscanf(n, "%d", &v); err != nil {
			return 0, false
		}
		vals = append(vals, v)
	}
	// V7 审计修复: 除法除数为 0 时原来返回 0 当答案——按钮里有 "0" 选项就误判
	// 正确。预检: 无解时 return false, 让流程走 quiz 官方答案/AI 兜底。
	for i, op := range ops {
		if (op == "/" || op == "÷") && vals[i+1] == 0 {
			return 0, false
		}
	}
	// V8 审计修复: 原来从左到右算——2+3*4 算成 20。三操作数时先乘除后加减。
	if len(ops) == 2 {
		if isMulDiv(ops[0]) && !isMulDiv(ops[1]) {
			// a*b+c / a/b+c: 先算前两个
			return applyOp(applyOp(vals[0], vals[1], ops[0]), vals[2], ops[1]), true
		}
		if isMulDiv(ops[1]) {
			// a+b*c / a-b/c: 先算后两个
			return applyOp(vals[0], applyOp(vals[1], vals[2], ops[1]), ops[0]), true
		}
	}
	res := vals[0]
	for i, op := range ops {
		res = applyOp(res, vals[i+1], op)
	}
	return res, true
}

func isMulDiv(op string) bool {
	return op == "*" || op == "x" || op == "×" || op == "/" || op == "÷"
}

func applyOp(a, b int, op string) int {
	switch op {
	case "+":
		return a + b
	case "-":
		return a - b
	case "*", "x", "×":
		return a * b
	case "/", "÷":
		if b == 0 {
			return 0
		}
		return a / b
	}
	return a
}

// pollAnswerText 取选项文本(PollAnswer.Text 为 *TextWithEntities)
func pollAnswerText(a tg.PollAnswerClass) string {
	if pa, ok := a.(*tg.PollAnswer); ok && pa.Text != nil {
		return pa.Text.Text
	}
	return ""
}

// solvePoll 从 Poll 解正确选项序号: 题干算式匹配选项 或 选项自身是算式
func solvePoll(p *tg.Poll) int {
	if len(p.Answers) == 0 {
		return -1
	}
	question := ""
	if p.Question != nil {
		question = p.Question.Text // Question 为 *TextWithEntities
	}
	if ans, ok := solveArithmetic(question); ok {
		for i, a := range p.Answers {
			var v int
			if _, err := fmt.Sscanf(strings.TrimSpace(pollAnswerText(a)), "%d", &v); err == nil && v == ans {
				return i
			}
		}
	}
	for _, a := range p.Answers {
		if ans, ok := solveArithmetic(pollAnswerText(a)); ok {
			for j, b := range p.Answers {
				var v int
				if _, err := fmt.Sscanf(strings.TrimSpace(pollAnswerText(b)), "%d", &v); err == nil && v == ans {
					return j
				}
			}
			break
		}
	}
	return -1
}

// chooseCallbackButton 选择要点的回调按钮
func chooseCallbackButton(callbacks []vbutton, joined bool) *vbutton {
	if len(callbacks) == 1 {
		return &callbacks[0]
	}
	best := -1
	for i, b := range callbacks {
		if joinConfirmRe.MatchString(b.text) {
			best = i
			if joined {
				break
			}
		}
	}
	if best >= 0 {
		return &callbacks[best]
	}
	return nil
}

func firstSentMsgID(u tg.UpdatesClass) int {
	// P1-24 审计修复: 原来只断言 *tg.Updates——TG 高频发送时返回 *tg.UpdatesCombined
	// (批量更新容器), 断言失败返回 0, 复测找不到测试消息而误判"仍被禁言"。
	var upds []tg.UpdateClass
	switch up := u.(type) {
	case *tg.Updates:
		upds = up.Updates
	case *tg.UpdatesCombined:
		upds = up.Updates
	default:
		return 0
	}
	for _, upd := range upds {
		if m, ok := upd.(*tg.UpdateMessageID); ok {
			return int(m.ID) // tg 的 ID 为 int32
		}
	}
	return 0
}

// resolvePeer 按会话类型构造 InputPeer。
// v0.19 的 Peer* 结构不再携带 AccessHash(新版 TL 已移除), 频道需通过
// ChannelsGetChannels 取回; 私聊(PeerUser)无 access_hash 来源, 返回 nil 由调用方跳过。
func (e *Engine) resolvePeer(ctx context.Context, client *telegram.Client, cand *vcandidate) {
	switch p := cand.rawPeer.(type) {
	case *tg.PeerChannel:
		cctx, cancel := context.WithTimeout(ctx, 15*time.Second)
		defer cancel()
		res, err := client.Raw().ChannelsGetChannels(cctx, &tg.ChannelsGetChannelsRequest{
			ID: []tg.InputChannelClass{&tg.InputChannel{ChannelID: p.ChannelID, AccessHash: 0}},
		})
		if err == nil {
			// 返回接口 tg.ChatsClass, 断言取 .Chats
			var chats []tg.ChatClass
			switch cv := res.(type) {
			case *tg.MessagesChats:
				chats = cv.Chats
			case *tg.MessagesChatsSlice:
				chats = cv.Chats
			}
			for _, ch := range chats {
				if c, ok := ch.(*tg.Channel); ok && c.ID == p.ChannelID {
					cand.peer = &tg.InputPeerChannel{ChannelID: c.ID, AccessHash: c.AccessHash}
					return
				}
			}
		}
	case *tg.PeerChat:
		cand.peer = &tg.InputPeerChat{ChatID: p.ChatID}
	}
	// PeerUser: 私聊验证无 access_hash 来源, 不支持(验证几乎只发生在群/频道)
}

// extractCandidate 从原始消息提取验证要素。
// v0.19 结构: tg.Message.PeerID 是字段(接口); ReplyMarkup.Rows 元素为
// *KeyboardButtonRow(行内 .Buttons); Poll.Question/选项 Text 均为 *TextWithEntities。
func extractCandidate(msg *tg.Message) *vcandidate {
	c := &vcandidate{msgID: msg.ID, text: msg.Message}
	c.rawPeer = msg.PeerID // 字段, 非方法
	c.chatID = peerChatID(msg.PeerID)
	if rm, ok := msg.ReplyMarkup.(*tg.ReplyInlineMarkup); ok {
		for _, row := range rm.Rows { // []*KeyboardButtonRow
			if row == nil {
				continue
			}
			for _, btn := range row.Buttons {
				switch b := btn.(type) {
				case *tg.KeyboardButtonCallback:
					c.buttons = append(c.buttons, vbutton{text: b.Text, data: b.Data})
				case *tg.KeyboardButtonURL:
					c.buttons = append(c.buttons, vbutton{text: b.Text, url: b.URL})
				}
			}
		}
	}
	if msg.Media != nil {
		if mp, ok := msg.Media.(*tg.MessageMediaPoll); ok && mp.Poll != nil {
			c.poll = mp.Poll
		}
	}
	// v0.19 的 tg.Message 无 Photo 字段, 照片统一走 Media
	if mp, ok := msg.Media.(*tg.MessageMediaPhoto); ok {
		if p, ok2 := mp.Photo.(*tg.Photo); ok2 {
			c.rawPhoto = p
		}
	}
	if doc, ok := msg.Media.(*tg.MessageMediaDocument); ok {
		if d, ok := doc.Document.(*tg.Document); ok && d.MimeType == "image/gif" {
			c.rawDoc = d
		}
	}
	return c
}
