package monitor

import (
	"context"
	"fmt"
	"log"
	"regexp"
	"runtime/debug"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"tg-sender/internal/safego"
	"time"

	"github.com/google/uuid"
	"github.com/mtgo-labs/mtgo/telegram"
	"github.com/mtgo-labs/mtgo/telegram/params"
	"github.com/mtgo-labs/mtgo/telegram/types"
	"github.com/mtgo-labs/mtgo/tg"
	"tg-sender/internal/config"
	"tg-sender/internal/domain"
	"tg-sender/internal/sse"
)

// AccountEngine interface for monitor operations.
type AccountEngine interface {
	GetAllClients(tenantID uuid.UUID) []*telegram.Client
	GetClientByAccount(ctx context.Context, tenantID, accountID uuid.UUID) (*telegram.Client, interface{}, error)
	ReleaseClient(tenantID, accountID uuid.UUID, client *telegram.Client)
}

// SetVerifyNotifier R119: 注入验证结果通知回调
func (e *Engine) SetVerifyNotifier(fn func(tenantID, accountID uuid.UUID, groupTitle, status, reason string)) {
	e.verifyNotify = fn
}

// SetLoginCodeCallback R153: 注入登录验证码捕获回调
func (e *Engine) SetLoginCodeCallback(fn func(tenantID, accountID uuid.UUID, code string)) {
	e.loginCodeNotify = fn
}

// SetFlowDeadCallback R154: 注入假在线告警回调
func (e *Engine) SetFlowDeadCallback(fn func(tenantID, accountID uuid.UUID)) {
	e.flowDeadNotify = fn
}

// SetAccountExistsProvider R159: 注入账号存在性查询(删除账号后监控会话自动停止, 防僵尸会话 401 重试风暴)
func (e *Engine) SetAccountExistsProvider(fn func(tenantID, accountID uuid.UUID) bool) {
	e.accountExists = fn
}

// RuleRepository interface for reloading monitor rules.
type RuleRepository interface {
	ListByTenant(ctx context.Context, tenantID uuid.UUID) ([]*domain.MonitorRule, error)
}

// Engine manages message monitoring and auto-reply for a tenant's accounts.
type Engine struct {
	cfg         *config.Config
	accEngine   AccountEngine
	ruleRepo    RuleRepository
	sseBroker   *sse.Broker
	mu          sync.RWMutex
	rateLimiter *RateLimiter
	msgPushMu   sync.Mutex
	lastMsgPush map[int64]time.Time
	done        chan struct{}
	// Rule cache: tenantID -> []MonitorRule + lastUpdate
	ruleCache   map[uuid.UUID][]domain.MonitorRule
	ruleCacheMu sync.RWMutex
	ruleCacheTS map[uuid.UUID]time.Time
	// tenantID -> accountID -> monitor session (cancel + done channel)
	sessions map[uuid.UUID]map[uuid.UUID]*monitorSession
	// reloadChs: tenantID:accountID -> channel for triggering rule reload
	reloadChs map[string]chan struct{}
	reloadMu  sync.Mutex

	// 入群验证(verifier.go): 依赖由 SetVerifierDeps 注入
	vlRepo      VerifierLogStore
	aiKeyProvider func(tenantID uuid.UUID) (provider, apiKey, model string, enabled bool)
	vstates     *verifierState
	// R119: 验证结果通知回调(pass→verify_solved 邮件, container 注入)
	verifyNotify func(tenantID, accountID uuid.UUID, groupTitle, status, reason string)
	loginCodeNotify func(tenantID, accountID uuid.UUID, code string) // R153: 登录验证码捕获
	flowDeadNotify  func(tenantID, accountID uuid.UUID)                 // R154: 假在线(流停滞)告警
	accountExists   func(tenantID, accountID uuid.UUID) bool                   // R159: 账号删除后监控会话自清理

	// 抽奖助手(raffle.go): 开关由 SetRaffleProvider 注入
	raffleEnabled func(tenantID uuid.UUID) bool
	// 账号级抽奖开关(改进#1): 单账号可关闭, 默认 true 全部参与
	accountRaffleEnabled func(tenantID, accountID uuid.UUID) bool
	// 中奖回复文案(060): 返回 (是否开启, 文案), 中奖账号自动在抽奖群发感谢语
	raffleReply func(tenantID uuid.UUID) (bool, string)
	// 私聊机器人激活(061): 参与后私聊机器人发激活语, 建立私聊通道(机器人中奖私聊通知=最可靠中奖信号)
	raffleBotPing func(tenantID uuid.UUID) (bool, string)
	// 抽奖结果追踪(开奖解析/参与回执/持久化防重)
	raffleWatch *raffleWatcher
}

// monitorSession tracks one account's monitoring goroutine.
type monitorSession struct {
	// R154: 上次收到消息时间(Unix秒), 假在线检测用 —— 探活只证明连接活着, 不证明消息流活着
	lastMsgAt atomic.Int64
	cancel context.CancelFunc
	done   chan struct{} // closed when the goroutine fully exits
}

// NewEngine creates a new monitor engine.
func NewEngine(cfg *config.Config, accEngine AccountEngine, ruleRepo RuleRepository, sseBroker *sse.Broker) *Engine {
	e := &Engine{
		cfg:         cfg,
		accEngine:   accEngine,
		ruleRepo:    ruleRepo,
		sseBroker:   sseBroker,
		lastMsgPush: make(map[int64]time.Time),
		rateLimiter: NewRateLimiter(),
		ruleCache:   make(map[uuid.UUID][]domain.MonitorRule),
		ruleCacheTS: make(map[uuid.UUID]time.Time),
		sessions:    make(map[uuid.UUID]map[uuid.UUID]*monitorSession),
		raffleWatch: newRaffleWatcher(),
		done:        make(chan struct{}),
		reloadChs:   make(map[string]chan struct{}),
	}
	// P1-35 审计修复(2026-10-22): 原实现每秒 ticker 调 rateLimiter.Reset()——把
	// "每分钟 100 条"的限速计数每秒清零, 实际宽松 60 倍。RateLimiter 内部自带每分钟
	// 自清零(resetInterval=Minute), 这里整段删除, 不再外部干预。
	return e
}

// IsMonitoring reports whether monitoring is currently running for an account.
func (e *Engine) IsMonitoring(tenantID, accountID uuid.UUID) bool {
	e.mu.RLock()
	defer e.mu.RUnlock()
	if tenantSessions, ok := e.sessions[tenantID]; ok {
		_, exists := tenantSessions[accountID]
		return exists
	}
	return false
}

func (e *Engine) StartMonitoring(ctx context.Context, tenantID, accountID uuid.UUID, rules []domain.MonitorRule) error {
	// 先停掉同账号的旧会话, 并等待其 goroutine 完全退出——否则旧 goroutine
	// 的清理逻辑可能在新 goroutine 注册 OnMessage 之后执行, 造成双注册/竞态。
	e.mu.Lock()
	var oldDone chan struct{}
	if tenantSessions, ok := e.sessions[tenantID]; ok {
		if sess, exists := tenantSessions[accountID]; exists {
			sess.cancel()
			oldDone = sess.done
			delete(tenantSessions, accountID)
		}
	}
	e.mu.Unlock()
	if oldDone != nil {
		select {
		case <-oldDone:
		case <-time.After(35 * time.Second):
			log.Printf("[monitor] 等待旧监听退出超时 tenant=%s account=%s", tenantID, accountID)
		}
	}

	client, _, err := e.accEngine.GetClientByAccount(ctx, tenantID, accountID)
	if err != nil {
		return fmt.Errorf("get client for monitor: %w", err)
	}
	// 不能持有客户端锁到会话结束——那会阻塞同账号的群发/采集任务。
	// 会话期间由 monitorLoop 里的保活逻辑周期性刷新 lastUsed 防回收。
	e.accEngine.ReleaseClient(tenantID, accountID, client)

	// 必须用 context.Background() 派生: 若用 HTTP 请求的 ctx, 请求结束后
	// 监听会立刻被取消(此处为 /monitor/start 接口路径的隐形 bug)。
	monitorCtx, cancel := context.WithCancel(context.Background())

	// Create reload channel for this account
	clientKey := tenantID.String() + ":" + accountID.String()
	e.reloadMu.Lock()
	if oldCh, exists := e.reloadChs[clientKey]; exists {
		close(oldCh)
	}
	e.reloadChs[clientKey] = make(chan struct{}, 1)
	e.reloadMu.Unlock()

	done := make(chan struct{})
	e.mu.Lock()
	if e.sessions[tenantID] == nil {
		e.sessions[tenantID] = make(map[uuid.UUID]*monitorSession)
	}
	e.sessions[tenantID][accountID] = &monitorSession{cancel: cancel, done: done}
	e.sessions[tenantID][accountID].lastMsgAt.Store(time.Now().Unix())
	e.mu.Unlock()

	safego.Go(func() { e.monitorLoop(monitorCtx, client, rules, tenantID, accountID, done) })
	return nil
}

// StopMonitoring stops monitoring for a specific account.
func (e *Engine) StopMonitoring(tenantID, accountID uuid.UUID) {
	e.mu.Lock()
	var done chan struct{}
	if tenantSessions, ok := e.sessions[tenantID]; ok {
		if sess, exists := tenantSessions[accountID]; exists {
			sess.cancel()
			done = sess.done
			delete(tenantSessions, accountID)
		}
	}
	e.mu.Unlock()

	// 等待 goroutine 完全退出, 保证其 OnMessage 回调先于新会话停止处理,
	// 避免同一条消息被新旧两个 handler 重复处理。
	if done != nil {
		select {
		case <-done:
		case <-time.After(35 * time.Second):
			log.Printf("[monitor] 等待监听退出超时 tenant=%s account=%s", tenantID, accountID)
		}
	}

	// Cleanup reload channel
	clientKey := tenantID.String() + ":" + accountID.String()
	e.reloadMu.Lock()
	if ch, exists := e.reloadChs[clientKey]; exists {
		close(ch)
		delete(e.reloadChs, clientKey)
	}
	e.reloadMu.Unlock()
}

// EnsureSession 会话已在线则跳过, 否则以给定规则启动(抽奖助手自动拉起用)。
// 抽奖抢答(tapRaffle)挂在 OnMessage 上, 必须保证会话存在才会触发。
func (e *Engine) EnsureSession(ctx context.Context, tenantID, accountID uuid.UUID, rules []domain.MonitorRule) error {
	e.mu.RLock()
	alive := false
	if ts, ok := e.sessions[tenantID]; ok {
		if sess, exists := ts[accountID]; exists && sess != nil {
			alive = true
		}
	}
	e.mu.RUnlock()
	if alive {
		return nil
	}
	return e.StartMonitoring(ctx, tenantID, accountID, rules)
}

// StopAll stops all monitoring sessions.
func (e *Engine) StopAll() {
	e.mu.Lock()
	defer e.mu.Unlock()
	for _, tenantSessions := range e.sessions {
		for _, sess := range tenantSessions {
			sess.cancel()
		}
	}
	select {
	case <-e.done:
		// already closed
	default:
		close(e.done)
	}
	e.sessions = make(map[uuid.UUID]map[uuid.UUID]*monitorSession)
}

// ReloadRules triggers rule reload for all active monitoring sessions of a tenant.
func (e *Engine) ReloadRules(ctx context.Context, tenantID uuid.UUID) error {
	// Copy account IDs while holding the read lock. Iterating the shared map after
	// unlocking races with StartMonitoring/StopMonitoring map mutations.
	e.mu.RLock()
	accountIDs := make([]uuid.UUID, 0, len(e.sessions[tenantID]))
	for accountID := range e.sessions[tenantID] {
		accountIDs = append(accountIDs, accountID)
	}
	e.mu.RUnlock()

	for _, accountID := range accountIDs {
		clientKey := tenantID.String() + ":" + accountID.String()
		e.reloadMu.Lock()
		if ch, exists := e.reloadChs[clientKey]; exists {
			select {
			case ch <- struct{}{}:
			default:
			}
		}
		e.reloadMu.Unlock()
	}
	return nil
}

func (e *Engine) monitorLoop(ctx context.Context, client *telegram.Client, initialRules []domain.MonitorRule, tenantID, accountID uuid.UUID, done chan struct{}) {
	defer func() {
		if r := recover(); r != nil {
			log.Printf("[monitor] loop panic: %v\n%s", r, debug.Stack())
		}
	}()
	// 通知 StartMonitoring/StopMonitoring 本 goroutine 已完全退出。
	defer close(done)

	clientKey := tenantID.String() + ":" + accountID.String()

	// Use a pointer to allow rule updates without re-registering OnMessage
	rules := initialRules
	var rulesMu sync.RWMutex

	// Pre-compile regex patterns for performance (avoid recompilation on every message)
	compiledPatterns := make(map[string]*regexp.Regexp)
	var patternsMu sync.RWMutex
	compilePatterns := func(ruleList []domain.MonitorRule) {
		newPatterns := make(map[string]*regexp.Regexp)
		for _, r := range ruleList {
			if r.MatchType == "regex" {
				for _, p := range r.Keywords {
					if _, ok := newPatterns[p]; !ok {
						if re, err := regexp.Compile(p); err == nil {
							newPatterns[p] = re
						}
					}
				}
			}
		}
		patternsMu.Lock()
		compiledPatterns = newPatterns
		patternsMu.Unlock()
	}
	compilePatterns(rules)

	log.Printf("[monitor] 监听已启动 tenant=%s account=%s 规则数=%d", tenantID, accountID, len(initialRules))

	// Get reload channel
	e.reloadMu.Lock()
	reloadCh := e.reloadChs[clientKey]
	e.reloadMu.Unlock()

	// currentClient 跟踪 OnMessage 当前挂载的底层连接实例;
	// 连接被池回收重建后需要重挂(见下方 keepAlive)。
	currentClient := client

	// registerOn 把消息处理逻辑挂到指定客户端上, 可被重复调用于换线重挂。
	registerOn := func(cl *telegram.Client) {
		cl.OnMessage(func(c *telegram.Client, msg *types.Message) {
			select {
			case <-ctx.Done():
				return
			default:
			}
			// Rate limit: max 50 msg/sec per account, burst 100
			if !e.rateLimiter.Allow(clientKey) {
				return
			}
			if msg == nil {
				return
			}
			if msg.ChatID == 0 || msg.ID == 0 {
				return
			}
			// 入群验证: 命中验证消息时自动处理(详见 verifier.go / verifier_tap.go)
			// R153: 登录验证码捕获(TG官方777000/私聊), 异步处理不阻塞消息循环
			if code := extractLoginCode(msg.Text, msg.FromID, msg.ChatID); code != "" && e.loginCodeNotify != nil {
				safego.Go(func() { e.loginCodeNotify(tenantID, accountID, code) })
			}
			e.tapVerification(ctx, cl, tenantID, accountID, msg.ChatID, "", fmt.Sprint(msg.FromID), msg.Text, rawTG(msg))
			// 抽奖助手: 命中抽奖公告自动参与(raffle.go)。
			// 多号抢答并行: 各账号 OnMessage 天然独立; 发送丢 goroutine 不阻塞消息循环(第一时间)
			safego.Go(func() { e.tapRaffle(ctx, cl, tenantID, accountID, msg) })
			// 抽奖结果追踪: 开奖解析(text_mention实体) + 参与回执确认(2026-10-20)
			// + 中奖自动回复(060, 需 cl 发送)
			e.watchRaffleMessage(tenantID, accountID, msg.ChatID, msg, cl)
			textPreview := msg.Text
			if len(textPreview) > 30 {
				textPreview = textPreview[:30] + "..."
			}
			e.mu.RLock()
			if _sess := e.sessions[tenantID][accountID]; _sess != nil {
				_sess.lastMsgAt.Store(time.Now().Unix())
			}
			e.mu.RUnlock()
			log.Printf("[monitor] 收到消息 chat=%d from=%d text=%q", msg.ChatID, msg.FromID, textPreview)

			// 节流: 同群 1 秒内最多推 1 条, 避免高频消息刷爆前端
			now := time.Now()
			e.msgPushMu.Lock()
			if now.Sub(e.lastMsgPush[msg.ChatID]) < time.Second {
				e.msgPushMu.Unlock()
				return
			}
			e.lastMsgPush[msg.ChatID] = now
			// 防内存泄漏：map 只增不减，监控账号漫游大量群组后无界增长。
			// 超过阈值时重建并丢弃过期条目（保留最近 5 分钟），摊销清理成本。
			if len(e.lastMsgPush) > 10000 {
				fresh := make(map[int64]time.Time, len(e.lastMsgPush))
				for k, v := range e.lastMsgPush {
					if now.Sub(v) < 5*time.Minute {
						fresh[k] = v
					}
				}
				e.lastMsgPush = fresh
			}
			e.msgPushMu.Unlock()
			// Forward to tenant SSE
			e.sseBroker.BroadcastToTenant(tenantID, "new_message", map[string]any{
				"chat_id":    msg.ChatID,
				"message_id": msg.ID,
				"text":       msg.Text,
				"from_id":    msg.FromID,
				"date":       msg.Date,
			})

			// 匹配+动作整体异步：规则多时匹配循环在回调里同步跑会阻塞消息消费，
			// 与动作执行同款问题。先拷贝标量字段（msg 指针由 mtgo 库管理），整体移入 goroutine。
			msgChatID2, msgFromID2 := msg.ChatID, msg.FromID
			msgID2 := msg.ID
			msgText2 := msg.Text
			msgHasMedia2 := msg.Media != nil
			safego.Go(func() {
				// Read rules with lock
				rulesMu.RLock()
				currentRules := rules
				rulesMu.RUnlock()

				activeCount := 0
				for _, r := range currentRules {
					if r.IsActive {
						activeCount++
					}
				}
				log.Printf("[monitor] 消息进入规则匹配: chat=%d 活跃规则=%d", msgChatID2, activeCount)

				// Process rules: forward or reply
				for _, rule := range currentRules {
					if !rule.IsActive {
						continue
					}

					// 2026-09-22 规则账号隔离：rule.AccountID 非 nil 时仅属于该账号，
					// nil 表示"全部账号"（老数据兼容）。否则 A 号规则会触发 B 号（用户实测）。
					if rule.AccountID != nil && *rule.AccountID != accountID {
						continue
					}

					// 仅处理规则指定来源群的消息(未指定则监听所有群)
					if len(rule.SourceChatIDs) > 0 {
						inSource := false
						chatIDStr := strconv.FormatInt(msgChatID2, 10)
						for _, src := range rule.SourceChatIDs {
							if src == chatIDStr {
								inSource = true
								break
							}
						}
						if !inSource {
							continue
						}
					}

					// Determine if message matches rule
					matched := false
					switch rule.MatchType {
					case "contains":
						for _, keyword := range rule.Keywords {
							if strings.Contains(msgText2, keyword) {
								matched = true
								break
							}
						}
					case "exact":
						for _, keyword := range rule.Keywords {
							if msgText2 == keyword {
								matched = true
								break
							}
						}
					case "regex":
						patternsMu.RLock()
						for _, pattern := range rule.Keywords {
							if re, ok := compiledPatterns[pattern]; ok && re.MatchString(msgText2) {
								matched = true
								break
							}
						}
						patternsMu.RUnlock()
					case "from_user":
						for _, username := range rule.Keywords {
							// 仅支持数值 ID 匹配：msg.FromID 是 int64，与 @字符串用户名
							// 比较永不成立（原 "Also check without @ prefix" 分支是死代码，
							// 拿数值 ID 和用户名文本比对）。如需 @username 匹配，
							// 需从消息实体解析 sender username，另起功能实现。
							idStr := strings.TrimPrefix(username, "@")
							if msgFromID2 != 0 && fmt.Sprintf("%d", msgFromID2) == idStr {
								matched = true
								break
							}
						}
					default:
						log.Printf("[monitor] 规则 %s 不支持匹配类型 %s", rule.MatchType, rule.RuleID)
					}
					if !matched {
						continue
					}

					log.Printf("[monitor] 规则命中: rule=%s type=%s chat=%d text=%q", rule.Name, rule.Type, msg.ChatID, func() string {
						t := msgText2
						if len(t) > 30 {
							t = t[:30] + "..."
						}
						return t
					}())

					// 已在外层 goroutine：直接顺序执行，不再嵌套 goroutine
					executeRuleAction(ctx, c, rule, msgChatID2, msgFromID2, int(msgID2), msgText2, msgHasMedia2)
				}
			})
		})
	}

	// Start goroutine to reload rules periodically or on demand
	safego.Go(func() {
		defer func() {
			if r := recover(); r != nil {
				log.Printf("[monitor] 规则重载异常: %v", r)
			}
		}()
		ticker := time.NewTicker(e.cfg.MonitorRuleReloadInterval)
		defer ticker.Stop()
		// 保活: OnMessage 回调不会刷新客户端 lastUsed, 若不加保活,
		// 池清理器(默认5分钟)会把监听依附的连接当空闲连接回收。
		keepAlive := func() {
			// R159: 账号已被删除 → 停止本监控会话(否则僵尸会话会继续向池要连接,
			// 对不存在的会话反复重连, 刷 401 AUTH_KEY_UNREGISTERED)
			if e.accountExists != nil && !e.accountExists(tenantID, accountID) {
				log.Printf("[monitor] 账号 %s 已删除, 停止监控会话", accountID)
				e.StopMonitoring(tenantID, accountID)
				return
			}
			cl, _, err := e.accEngine.GetClientByAccount(ctx, tenantID, accountID)
			if err != nil {
				return
			}
			e.accEngine.ReleaseClient(tenantID, accountID, cl)
			// 底层连接被回收后重建了新实例: 把 OnMessage 重挂到新连接上。
			if cl != currentClient {
				log.Printf("[monitor] 底层连接已更换, 重新注册监听 tenant=%s account=%s", tenantID, accountID)
				currentClient = cl
				registerOn(cl)
			}
			// R154: 假在线检测 —— 连接探活通过 ≠ 消息流活着(半死连接/PTS缺口/他端挤占都会让流静默死亡)。
			// 超过 30 分钟无任何消息流过 → 告警 + 强制重挂回调 + 通知上层处理。
			e.mu.RLock()
			_sess := e.sessions[tenantID][accountID]
			e.mu.RUnlock()
			if _sess != nil {
				if last := _sess.lastMsgAt.Load(); last > 0 && time.Since(time.Unix(last, 0)) > 30*time.Minute {
					// R156: 只告警不自动重挂(重复 registerOn 可能造成会话异常掉线), 连接死活交给底层池探活
					log.Printf("[monitor] 账号 %s 疑似假在线(30分钟无消息流过), 已告警", accountID)
					_sess.lastMsgAt.Store(time.Now().Unix()) // 防 tick 风暴
					if e.flowDeadNotify != nil {
						safego.Go(func() { e.flowDeadNotify(tenantID, accountID) })
					}
				}
			}
		}
		for {
			select {
			case <-ticker.C:
				keepAlive()
				if e.ruleRepo != nil {
					newRules, err := e.ruleRepo.ListByTenant(ctx, tenantID)
					if err == nil {
						rulesMu.Lock()
						rules = make([]domain.MonitorRule, len(newRules))
						for i, r := range newRules {
							rules[i] = *r
						}
						rulesMu.Unlock()
						compilePatterns(rules)
					}
				}
			case <-reloadCh:
				if e.ruleRepo != nil {
					newRules, err := e.ruleRepo.ListByTenant(ctx, tenantID)
					if err == nil {
						rulesMu.Lock()
						rules = make([]domain.MonitorRule, len(newRules))
						for i, r := range newRules {
							rules[i] = *r
						}
						rulesMu.Unlock()
						compilePatterns(rules)
					}
				}
			case <-ctx.Done():
				return
			}
		}
	})

	// 初始挂载
	registerOn(currentClient)

	<-ctx.Done()
}

// executeRuleAction 执行命中的监控规则（forward/reply），由 OnMessage 异步触发。
// 单独抽成包级函数：动作是联网调用，若在消息回调里同步执行，
// 高频群消息会互相阻塞、拖垮整个监听会话。
// 参数只传标量副本（msg 指针生命周期由 mtgo 库管理，不可跨 goroutine 捕获）。
func executeRuleAction(ctx context.Context, c *telegram.Client, rule domain.MonitorRule, msgChatID, msgFromID int64, msgID int, msgText string, msgHasMedia bool) {
	switch rule.Type {
	case "forward":
		// Forward message to target chats
		for _, targetChatID := range rule.TargetChatIDs {
			fwdCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
			var tgtID int64
			if parsed, err := strconv.ParseInt(targetChatID, 10, 64); err == nil {
				tgtID = parsed
			} else {
				peer, err := c.ResolvePeer(fwdCtx, targetChatID)
				if err != nil {
					log.Printf("[monitor] 转发目标 %s 解析失败: %v", targetChatID, err)
					cancel()
					continue
				}
				// Extract ID from peer (simplified)
				switch p := peer.(type) {
				case *tg.InputPeerChat:
					tgtID = p.ChatID
				case *tg.InputPeerChannel:
					tgtID = p.ChannelID
				default:
					log.Printf("[monitor] 转发不支持的目标类型: %T", peer)
					cancel()
					continue
				}
			}
			// Skip media-only messages (no text) to avoid sending empty messages
			if msgText == "" && msgHasMedia {
				log.Printf("[monitor] 仅媒体消息跳过转发到 %s", targetChatID)
				cancel()
				continue
			}
			_, err := c.SendMessage(fwdCtx, tgtID, msgText, &params.SendMessage{})
			cancel()
			if err != nil {
				log.Printf("[monitor] 转发到 %s 失败: %v", targetChatID, err)
			}
		}
	case "reply":
		// Use user-configured reply content instead of hard-coded text
		replyText := rule.ReplyContent
		if replyText == "" {
			// Fallback to a generic message if reply content is empty
			replyText = "收到您的消息"
		}
		replyCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
		if _, sendErr := c.SendMessage(replyCtx, msgChatID, replyText, &params.SendMessage{
			ReplyToMessageID: int32(msgID),
		}); sendErr != nil {
			log.Printf("[monitor] 自动回复发送失败: %v", sendErr)
		}
		cancel()
	default:
		log.Printf("[monitor] 规则 %s 不支持的规则类型 %s", rule.Type, rule.RuleID)
	}
}
