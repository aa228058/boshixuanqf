package task

import (
	"context"
	"log"
	"runtime/debug"
	"strings"
	"sync"
	"sync/atomic"
	"tg-sender/internal/safego"
	"time"

	"github.com/google/uuid"
	"github.com/mtgo-labs/mtgo/telegram"
	"github.com/mtgo-labs/mtgo/telegram/types"
	"github.com/mtgo-labs/mtgo/tg"
	"tg-sender/internal/config"
	"tg-sender/internal/domain"
	"tg-sender/internal/repository"
)

// rawLink represents a link extracted from a message before processing.
type rawLink struct {
	TenantID        uuid.UUID
	AccountID       uuid.UUID
	LinkType        string // private | public
	LinkHash        string
	FullURL         string
	SourceChatID    int64
	SourceChatTitle string
}

// accountHandler tracks OnMessage handler for an account.
type accountHandler struct {
	active     int32
	tenantID   uuid.UUID
	accountID  uuid.UUID
	client     *telegram.Client
	cancelFunc func()
}

// LinkScraper is a persistent background service that continuously scrapes links
// from all online accounts' messages.
// AccountLister 供常驻监听自动发现上线账号(多租户共用)
type AccountLister interface {
	ListAll(ctx context.Context) ([]*domain.Account, error)
}

type LinkScraper struct {
	cfg         *config.Config
	accEngine   AccountEngine
	linkRepo    domain.ScrapedLinkRepository
	groupRepo   *repository.GroupRepository
	accountRepo AccountLister

	// Runtime state
	handlers   sync.Map // accountID(string) -> *accountHandler
	linkQueue  chan *rawLink
	dedupMap   map[string]time.Time
	dedupMu    sync.RWMutex
	batchLinks []*domain.ScrapedLink
	batchMu    sync.Mutex
	stopCh     chan struct{}
	stopOnce   sync.Once
	wg         sync.WaitGroup
}

// NewLinkScraper creates a new persistent link scraper.
func NewLinkScraper(cfg *config.Config, accEngine AccountEngine, linkRepo domain.ScrapedLinkRepository, groupRepo *repository.GroupRepository, accountRepo AccountLister) *LinkScraper {
	return &LinkScraper{
		cfg:         cfg,
		accEngine:   accEngine,
		linkRepo:    linkRepo,
		groupRepo:   groupRepo,
		accountRepo: accountRepo,
		linkQueue:   make(chan *rawLink, 10000), // Buffer 10k links
		dedupMap:    make(map[string]time.Time),
		stopCh:      make(chan struct{}),
	}
}

// Start begins the link scraper background workers.
func (ls *LinkScraper) Start(ctx context.Context) {
	// Start the link consumer (async processing with rate limiting)
	ls.wg.Add(1)
	safego.Go(func() { ls.consumerLoop(ctx) })

	// Start the batch writer (flush to DB periodically)
	ls.wg.Add(1)
	safego.Go(func() { ls.batchWriterLoop(ctx) })

	// Start the dedup cleaner (remove old entries)
	ls.wg.Add(1)
	safego.Go(func() { ls.dedupCleanerLoop(ctx) })

	// Start the account watcher (register handlers for online accounts)
	ls.wg.Add(1)
	safego.Go(func() { ls.accountWatcherLoop(ctx) })

	log.Printf("[link-scraper] 常驻链接采集服务已启动")
}

// Stop gracefully shuts down the link scraper.
func (ls *LinkScraper) Stop() {
	ls.stopOnce.Do(func() { close(ls.stopCh) })
	// Deactivate all handlers
	ls.handlers.Range(func(key, value interface{}) bool {
		if h := value.(*accountHandler); h != nil {
			atomic.StoreInt32(&h.active, 0)
			if h.cancelFunc != nil {
				h.cancelFunc()
			}
		}
		ls.handlers.Delete(key)
		return true
	})
	ls.wg.Wait()
	log.Printf("[link-scraper] 常驻链接采集服务已停止")
}

// GetStats returns current scraper statistics.
func (ls *LinkScraper) GetStats() map[string]any {
	ls.dedupMu.RLock()
	dedupSize := len(ls.dedupMap)
	ls.dedupMu.RUnlock()

	queueLen := len(ls.linkQueue)

	var handlerCount int
	ls.handlers.Range(func(_, _ interface{}) bool {
		handlerCount++
		return true
	})

	return map[string]any{
		"handlers_active": handlerCount,
		"queue_length":    queueLen,
		"dedup_cache":     dedupSize,
	}
}

// RegisterAccount registers an OnMessage handler for an online account.
// touchAccount 刷新账号客户端的 lastUsed 时间戳, 防止池清理器把
// 常驻监听依附的连接当作空闲连接回收(OnMessage 回调本身不更新 lastUsed)。
func (ls *LinkScraper) touchAccount(ctx context.Context, tenantID, accountID uuid.UUID) {
	if c, err := ls.accEngine.GetClient(ctx, tenantID, accountID); err == nil {
		ls.accEngine.ReleaseClient(tenantID, accountID, c)
	}
}

func (ls *LinkScraper) RegisterAccount(ctx context.Context, tenantID, accountID uuid.UUID) {
	key := accountID.String()

	// 已注册且底层连接仍然存活: 只做保活(刷新 lastUsed 防止被空闲回收), 避免重复注册。
	if v, loaded := ls.handlers.Load(key); loaded {
		if h, ok := v.(*accountHandler); ok && h != nil && h.client != nil && h.client.IsConnected() {
			ls.touchAccount(ctx, tenantID, accountID)
			return
		}
		// 残留条目(底层连接已被替换/回收): 先注销再重新注册。
		ls.UnregisterAccount(accountID)
	}

	client, err := ls.accEngine.GetClient(ctx, tenantID, accountID)
	if err != nil {
		// R181.1: 死密钥账号已被熔断, 每 30s 刷一条"获取客户端失败"纯属噪音, 静默跳过
		if !strings.Contains(err.Error(), "需重新登录") {
			log.Printf("[link-scraper] 获取客户端失败 %s: %v", accountID.String()[:8], err)
		}
		ls.handlers.Delete(key)
		return
	}
	// 注册完回调立刻释放客户端锁。持有它会永久阻塞同一账号的
	// 监控/采集/群发功能(锁不响应 ctx 超时)。
	ls.accEngine.ReleaseClient(tenantID, accountID, client)

	handler := &accountHandler{
		active:    1,
		tenantID:  tenantID,
		accountID: accountID,
		client:    client,
	}
	ls.handlers.Store(key, handler)

	// Register OnMessage callback
	client.OnMessage(func(c *telegram.Client, msg *types.Message) {
		if atomic.LoadInt32(&handler.active) == 0 {
			return
		}
		if msg == nil || msg.Text == "" {
			return
		}
		// Only process group messages (ChatID < 0 for groups/channels)
		if msg.ChatID >= 0 {
			return
		}

		// Extract links from message text
		matches := tmeLinkRe.FindAllStringSubmatch(msg.Text, -1)
		for _, match := range matches {
			if len(match) < 5 {
				continue
			}
			path := match[4]
			if match[3] != "" {
				path = match[3] + path
			}
			fullLink := "https://t.me/" + path

			// Determine link type
			privateMatch := privateLinkRe.FindStringSubmatch(fullLink)
			var linkType, linkHash string
			if len(privateMatch) > 1 {
				linkType = "private"
				linkHash = privateMatch[len(privateMatch)-1]
			} else {
				linkType = "public"
				linkHash = strings.TrimPrefix(path, "@")
				if linkHash == "" {
					continue
				}
			}

			// Push to queue for async processing
			select {
			case ls.linkQueue <- &rawLink{
				TenantID:     tenantID,
				AccountID:    accountID,
				LinkType:     linkType,
				LinkHash:     linkHash,
				FullURL:      fullLink,
				SourceChatID: msg.ChatID,
				SourceChatTitle: func() string {
					if msg.Chat != nil {
						return msg.Chat.Title
					}
					return ""
				}(),
			}:
			case <-ls.stopCh:
				return
			default:
				// Queue full, drop link (log occasionally)
			}
		}
	})

	log.Printf("[link-scraper] 已注册账号 %s 的消息监听", accountID.String()[:8])
}

// IsRegistered 返回账号是否已注册常驻链接监听(前端开关状态用)。
func (ls *LinkScraper) IsRegistered(accountID uuid.UUID) bool {
	_, ok := ls.handlers.Load(accountID.String())
	return ok
}

// UnregisterAccount removes the OnMessage handler for an account.
func (ls *LinkScraper) UnregisterAccount(accountID uuid.UUID) {
	key := accountID.String()
	if h, ok := ls.handlers.Load(key); ok {
		if handler := h.(*accountHandler); handler != nil {
			atomic.StoreInt32(&handler.active, 0)
			if handler.cancelFunc != nil {
				handler.cancelFunc()
			}
		}
		ls.handlers.Delete(key)
		log.Printf("[link-scraper] 已注销账号 %s 的消息监听", accountID.String()[:8])
	}
}

// consumerLoop processes links from the queue with rate limiting.
func (ls *LinkScraper) consumerLoop(ctx context.Context) {
	defer ls.wg.Done()
	// Rate limiter: max 5 ResolvePeer per second
	ticker := time.NewTicker(200 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-ls.stopCh:
			return
		case <-ctx.Done():
			return
		case <-ticker.C:
			// 消费节奏由 ticker 门控：200ms 一条 = 每秒最多 5 次 ResolvePeer，
			// 队列空时直接跳过本轮，避免 ticker 空转处理。
			select {
			case raw := <-ls.linkQueue:
				ls.processLink(ctx, raw)
			default:
			}
		}
	}
}

// processLink handles a single raw link: dedup, resolve (public), store.
func (ls *LinkScraper) processLink(ctx context.Context, raw *rawLink) {
	defer func() {
		if r := recover(); r != nil {
			log.Printf("[link-scraper] processLink panic: %v\n%s\n", r, debug.Stack())
		}
	}()

	// Memory dedup check (24h window)
	ls.dedupMu.RLock()
	if lastSeen, ok := ls.dedupMap[raw.LinkHash]; ok && time.Since(lastSeen) < 24*time.Hour {
		ls.dedupMu.RUnlock()
		return
	}
	ls.dedupMu.RUnlock()

	// Mark as seen in memory
	ls.dedupMu.Lock()
	ls.dedupMap[raw.LinkHash] = time.Now()
	ls.dedupMu.Unlock()

	// Build domain link
	link := &domain.ScrapedLink{
		LinkID:          uuid.New(),
		TenantID:        raw.TenantID,
		AccountID:       raw.AccountID,
		LinkType:        raw.LinkType,
		LinkHash:        raw.LinkHash,
		FullURL:         raw.FullURL,
		SourceChatID:    raw.SourceChatID,
		SourceChatTitle: raw.SourceChatTitle,
		Status:          "new",
		CreatedAt:       time.Now(),
	}

	// For public links, try to resolve group info (with rate limiting)
	if raw.LinkType == "public" {
		client, err := ls.accEngine.GetClient(ctx, raw.TenantID, raw.AccountID)
		if err == nil {
			defer ls.accEngine.ReleaseClient(raw.TenantID, raw.AccountID, client)
			peer, resolveErr := client.ResolvePeer(ctx, raw.LinkHash)
			if resolveErr == nil {
				switch p := peer.(type) {
				case *tg.InputPeerChat:
					link.ResolvedChatID = p.ChatID
				case *tg.InputPeerChannel:
					link.ResolvedChatID = p.ChannelID
				}
				if link.ResolvedChatID != 0 {
					link.Status = "resolved"
					// Try to get dialog info for title/member count
					// 原来 limit=1 只取最近1个会话, 目标群几乎永远不在其中,
					// 导致标题/成员数永远拿不到。放宽到 50 个基本覆盖已加入的群。
					dialogs, dlgErr := client.GetDialogs(ctx, 50, 0)
					if dlgErr == nil {
						for _, chat := range dialogs {
							if chat != nil && chat.ID == link.ResolvedChatID {
								link.ResolvedTitle = chat.Title
								link.MemberCount = chat.MembersCount
								break
							}
						}
					}
				}
			}
		}
	}

	// Add to batch for DB write
	ls.batchMu.Lock()
	ls.batchLinks = append(ls.batchLinks, link)
	ls.batchMu.Unlock()
}

// batchWriterLoop periodically flushes batched links to database.
func (ls *LinkScraper) batchWriterLoop(ctx context.Context) {
	defer ls.wg.Done()
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ls.stopCh:
			ls.flushBatch(ctx)
			return
		case <-ctx.Done():
			ls.flushBatch(ctx)
			return
		case <-ticker.C:
			ls.flushBatch(ctx)
		}
	}
}

// flushBatch writes accumulated links to DB and clears the batch.
func (ls *LinkScraper) flushBatch(ctx context.Context) {
	ls.batchMu.Lock()
	if len(ls.batchLinks) == 0 {
		ls.batchMu.Unlock()
		return
	}
	batch := make([]*domain.ScrapedLink, len(ls.batchLinks))
	copy(batch, ls.batchLinks)
	ls.batchLinks = ls.batchLinks[:0]
	ls.batchMu.Unlock()

	if err := ls.linkRepo.CreateBatch(ctx, batch); err != nil {
		log.Printf("[link-scraper] 批量写入链接失败: %v", err)
	} else {
		log.Printf("[link-scraper] 已批量写入 %d 条链接", len(batch))
	}
}

// dedupCleanerLoop removes old entries from the dedup map to prevent memory growth.
func (ls *LinkScraper) dedupCleanerLoop(ctx context.Context) {
	defer ls.wg.Done()
	ticker := time.NewTicker(10 * time.Minute)
	defer ticker.Stop()

	for {
		select {
		case <-ls.stopCh:
			return
		case <-ctx.Done():
			return
		case <-ticker.C:
			ls.dedupMu.Lock()
			// 爆量保险: 超10万条直接清空重建(约20MB封顶; 短暂重复由DB唯一约束兜底)
			if len(ls.dedupMap) > 100000 {
				ls.dedupMap = make(map[string]time.Time)
			}
			cutoff := time.Now().Add(-25 * time.Hour)
			for hash, ts := range ls.dedupMap {
				if ts.Before(cutoff) {
					delete(ls.dedupMap, hash)
				}
			}
			log.Printf("[link-scraper] 去重缓存清理完成, 当前 %d 条", len(ls.dedupMap))
			ls.dedupMu.Unlock()
		}
	}
}

// accountWatcherLoop 每30秒同步: 上线账号注册监听, 下线注销(多租户全自动)。
func (ls *LinkScraper) accountWatcherLoop(ctx context.Context) {
	defer ls.wg.Done()
	time.Sleep(10 * time.Second) // 等账号引擎完成首批连接
	ls.syncOnlineAccounts(ctx)
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ls.stopCh:
			return
		case <-ticker.C:
			ls.syncOnlineAccounts(ctx)
		}
	}
}

func (ls *LinkScraper) syncOnlineAccounts(ctx context.Context) {
	if ls.accountRepo == nil {
		return
	}
	accounts, err := ls.accountRepo.ListAll(ctx)
	if err != nil {
		return
	}
	online := make(map[string]bool)
	for _, a := range accounts {
		// 门槛放宽: 已授权(authenticated)但尚未入池的账号也尝试注册,
		// 连通性由 GetClient 报错兜底; 只排除明确不可用的状态。
		// 待机(离线)号也注册：GetClient 会现场建连。只排除真正不可用的。
		if a.Status == domain.AccountStatusBanned || a.Status == domain.AccountStatusRestricted || a.Status == domain.AccountStatusPendingAuth {
			continue
		}
		online[a.AccountID.String()] = true
		ls.RegisterAccount(ctx, a.TenantID, a.AccountID)
	}
	ls.handlers.Range(func(k, v any) bool {
		id, err := uuid.Parse(k.(string))
		if err == nil && !online[k.(string)] {
			ls.UnregisterAccount(id)
		}
		return true
	})
}
