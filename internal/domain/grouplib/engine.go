// Package grouplib 平台级群组库聚合器(找群功能的后端)。
// 三路来源, 每轮增量归并进 system_groups:
//  1. scraped_links —— 所有租户链接采集到的私密/公开群链接
//  2. groups        —— 所有租户已加入且带 username 的公开群
//  3. members.bio   —— 群成员简介里挖出的 t.me 链接/@用户名(简介采集)
package grouplib

import (
	"context"
	"fmt"
	"log"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"tg-sender/internal/domain"
	"tg-sender/internal/repository"
	"tg-sender/internal/safego"
)

// 简介挖掘正则
var (
	tmeRe = regexp.MustCompile(`(?i)(?:https?://)?(?:www\.)?(?:t\.me|telegram\.me|telegram\.dog)/(joinchat/|\+)?([A-Za-z0-9_\-]{4,64})`)
	// V28 审计修复: 原来会匹配邮箱(@example.com 的 @example)当 TG 用户名采集。
	// 排除逻辑在使用处做(emailTLDs 黑名单)——RE2 引擎不支持负向断言 (?!...),
	// 上一版直接写进正则导致 init() 时 MustCompile panic, 容器起不来(2026-10-23 运行时实证)。
	atRe = regexp.MustCompile(`(?m)@([A-Za-z][A-Za-z0-9_]{3,31})`)
)

// emailTLDs: @用户名 后跟这些域名段的视为邮箱地址, 不当 TG 用户名采集
var emailTLDs = []string{".com", ".net", ".org", ".cn", ".io", ".me", ".edu", ".gov", ".info", ".biz", ".cc", ".xyz"}

type Engine struct {
	repo     *repository.SystemGroupRepository
	pool     *pgxpool.Pool
	acc      accountEngine // 借账号做关键词搜群(由 container 注入)
	interval time.Duration
	stopCh   chan struct{}
	wg       sync.WaitGroup

	mu  sync.Mutex
	slW   time.Time // scraped_links 水位时间
	slWID uuid.UUID // 同一时间戳下的链接主键游标
	gW    time.Time // groups 水位时间
	gWID  uuid.UUID // 同一时间戳下的群组主键游标
	mW    time.Time // members 水位时间
	mWID  uuid.UUID // 同一时间戳下的成员主键游标
}

func NewEngine(pool *pgxpool.Pool, repo *repository.SystemGroupRepository, acc accountEngine) *Engine {
	return &Engine{repo: repo, pool: pool, acc: acc, interval: 2 * time.Minute, stopCh: make(chan struct{})}
}

func (e *Engine) Start() {
	// P2-35/36 审计修复: wg.Add 原在 goroutine 内——Stop 若在 goroutine 启动前
	// 调用, close(stopCh)+Wait 会立即返回(Add 尚未执行, 计数为 0), 而 goroutine
	// 随后 Add 并继续跑 runOnce, 与下一轮 Start 并发聚合。Add 移到锁外同步执行,
	// 保证 Wait 一定等得到本 goroutine。
	e.wg.Add(1)
	safego.Go(func() {
		defer e.wg.Done()
		t := time.NewTicker(e.interval)
		defer t.Stop()
		// 启动即先跑一轮
		e.runOnce()
		for {
			select {
			case <-e.stopCh:
				return
			case <-t.C:
				e.runOnce()
			}
		}
	})
}

func (e *Engine) Stop() {
	close(e.stopCh)
	e.wg.Wait()
}

func (e *Engine) runOnce() {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	var errs []error
	if err := e.aggregateScrapedLinks(ctx); err != nil {
		errs = append(errs, err)
	}
	if err := e.aggregateJoinedGroups(ctx); err != nil {
		errs = append(errs, err)
	}
	if err := e.aggregateMemberBios(ctx); err != nil {
		errs = append(errs, err)
	}
	for _, err := range errs {
		log.Printf("[grouplib] 聚合失败: %v", err)
	}
}

// aggregateScrapedLinks 增量聚合链接采集结果
func (e *Engine) aggregateScrapedLinks(ctx context.Context) error {
	e.mu.Lock()
	wm, wmID := e.slW, e.slWID
	e.mu.Unlock()
	// 用 (created_at, link_id) 复合游标，既不漏同时间戳记录，也不会每轮
	// 回看最近 1 秒导致相同来源反复入库、热度被重复累加。
	rows, err := e.pool.Query(ctx, `
		SELECT link_id, full_url, link_type, COALESCE(NULLIF(resolved_title,''), NULLIF(source_chat_title,''), ''),
		       member_count, status, created_at
		FROM scraped_links
		WHERE (created_at, link_id) > ($1, $2)
		ORDER BY created_at ASC, link_id ASC LIMIT 5000`, wm, wmID)
	if err != nil {
		return err
	}
	defer rows.Close()
	var maxT time.Time
	var maxID uuid.UUID
	n := 0
	for rows.Next() {
		var linkID uuid.UUID
		var url, linkType, title, status string
		var mc int
		var ct time.Time
		if err := rows.Scan(&linkID, &url, &linkType, &title, &mc, &status, &ct); err != nil {
			return err
		}
		username := extractTmeUsername(url)
		gt := "public"
		if linkType == "private" {
			gt = "private"
		}
		st := "active"
		if status == "joined" {
			st = "joined"
		}
		sg := &domain.SystemGroup{Source: "scraped", Title: firstNonEmpty(title, url), Username: username, TmeLink: url, GType: gt, MemberCount: mc, Status: st, LastSeenAt: ct}
		if err := e.repo.Upsert(ctx, sg); err != nil {
			log.Printf("[grouplib] scraped upsert %s: %v; cursor will retry this row", url, err)
			break
		}
		maxT, maxID = ct, linkID
		n++
	}
	if err := rows.Err(); err != nil {
		return err
	}
	if !maxT.IsZero() {
		e.mu.Lock()
		e.slW, e.slWID = maxT, maxID
		e.mu.Unlock()
	}
	if n > 0 {
		log.Printf("[grouplib] scraped 归并 %d 条", n)
	}
	return nil
}

// aggregateJoinedGroups 增量聚合所有租户已加入的公开群(私有群无 username, 别人也进不去, 不入库)
func (e *Engine) aggregateJoinedGroups(ctx context.Context) error {
	e.mu.Lock()
	wm, wmID := e.gW, e.gWID
	e.mu.Unlock()
	// 用 (GREATEST(created_at, updated_at), group_id) 复合游标，标题/人数更新
	// 会进入增量窗口，同一时间戳的群也不会遗漏或被每轮重复归并。
	rows, err := e.pool.Query(ctx, `
		SELECT group_id, title, username, type, member_count, created_at, updated_at
		FROM groups
		WHERE is_joined = TRUE AND username IS NOT NULL AND username <> ''
		  AND (GREATEST(created_at, updated_at), group_id) > ($1, $2)
		ORDER BY GREATEST(created_at, updated_at) ASC, group_id ASC LIMIT 5000`, wm, wmID)
	if err != nil {
		return err
	}
	defer rows.Close()
	var maxT time.Time
	var maxID uuid.UUID
	n := 0
	for rows.Next() {
		var groupID uuid.UUID
		var title, username, gtype string
		var mc int
		var ct, ut time.Time
		if err := rows.Scan(&groupID, &title, &username, &gtype, &mc, &ct, &ut); err != nil {
			return err
		}
		if ut.After(ct) {
			ct = ut
		}
		gt := "public"
		if gtype == "channel" {
			gt = "channel"
		}
		sg := &domain.SystemGroup{
			Source: "joined", Title: firstNonEmpty(title, "@"+username), Username: username,
			TmeLink: "https://t.me/" + username, GType: gt, MemberCount: mc, Status: "joined", LastSeenAt: ct,
		}
		if err := e.repo.Upsert(ctx, sg); err != nil {
			log.Printf("[grouplib] joined upsert %s: %v; cursor will retry this row", username, err)
			break
		}
		maxT, maxID = ct, groupID
		n++
	}
	if err := rows.Err(); err != nil {
		return err
	}
	if !maxT.IsZero() {
		e.mu.Lock()
		e.gW, e.gWID = maxT, maxID
		e.mu.Unlock()
	}
	if n > 0 {
		log.Printf("[grouplib] joined 归并 %d 条", n)
	}
	return nil
}

// aggregateMemberBios 简介挖掘: 从成员 bio 里解析 t.me 链接和 @用户名
func (e *Engine) aggregateMemberBios(ctx context.Context) error {
	e.mu.Lock()
	wm, wmID := e.mW, e.mWID
	e.mu.Unlock()
	rows, err := e.pool.Query(ctx, `
		SELECT member_id, bio, scraped_at FROM members
		WHERE (scraped_at, member_id) > ($1, $2) AND bio IS NOT NULL AND bio <> ''
		  AND (bio ILIKE '%t.me%' OR bio ILIKE '%telegram%' OR bio ILIKE '%@%')
		ORDER BY scraped_at ASC, member_id ASC LIMIT 5000`, wm, wmID)
	if err != nil {
		return err
	}
	defer rows.Close()
	var maxT time.Time
	var maxID uuid.UUID
	n := 0
	for rows.Next() {
		var memberID uuid.UUID
		var bio string
		var ct time.Time
		if err := rows.Scan(&memberID, &bio, &ct); err != nil {
			return err
		}
		processed, upsertErr := e.upsertBioLinks(ctx, bio, ct)
		if upsertErr != nil {
			log.Printf("[grouplib] bio upsert member=%s failed; cursor will retry this row: %v", memberID, upsertErr)
			break
		}
		n += processed
		maxT, maxID = ct, memberID
	}
	if err := rows.Err(); err != nil {
		return err
	}
	if !maxT.IsZero() {
		e.mu.Lock()
		e.mW, e.mWID = maxT, maxID
		e.mu.Unlock()
	}
	if n > 0 {
		log.Printf("[grouplib] bio 挖掘归并 %d 条", n)
	}
	return nil
}

func (e *Engine) upsertBioLinks(ctx context.Context, bio string, seenAt time.Time) (int, error) {
	n := 0
	seen := map[string]bool{}
	for _, m := range tmeRe.FindAllStringSubmatch(bio, -1) {
		isJoin := m[1] != ""
		key := m[2]
		if seen[key] {
			continue
		}
		seen[key] = true
		sg := &domain.SystemGroup{Source: "bio", LastSeenAt: seenAt}
		if isJoin {
			sg.TmeLink = "https://t.me/joinchat/" + key
			sg.GType = "private"
			sg.Title = sg.TmeLink
		} else {
			sg.Username = key
			sg.TmeLink = "https://t.me/" + key
			sg.GType = "public"
			sg.Title = "@" + key
		}
		if err := e.repo.Upsert(ctx, sg); err != nil {
			return n, fmt.Errorf("upsert bio link %s: %w", key, err)
		}
		n++
	}
	// 裸 @用户名(前面不是链接的一部分)——保守: 只收出现在行首或空白后的
	// V28 排除逻辑(RE2 无负向断言, 代码层过滤): @用户名 后跟邮箱域名段的跳过
	for _, m := range atRe.FindAllStringSubmatchIndex(bio, -1) {
		key := bio[m[2]:m[3]]
		// 向后扫域名段, 命中 TLD 黑名单即视为邮箱
		j := m[1]
		for j < len(bio) && (bio[j] == '.' || bio[j] == '-' ||
			(bio[j] >= 'a' && bio[j] <= 'z') || (bio[j] >= 'A' && bio[j] <= 'Z') ||
			(bio[j] >= '0' && bio[j] <= '9')) {
			j++
		}
		tail := strings.ToLower(bio[m[1]:j])
		isEmail := false
		for _, tld := range emailTLDs {
			if strings.Contains(tail, tld) {
				isEmail = true
				break
			}
		}
		if isEmail {
			continue
		}
		if seen[key] {
			continue
		}
		seen[key] = true
		sg := &domain.SystemGroup{Source: "bio", Username: key, TmeLink: "https://t.me/" + key, GType: "public", Title: "@" + key, LastSeenAt: seenAt}
		if err := e.repo.Upsert(ctx, sg); err != nil {
			return n, fmt.Errorf("upsert bio username %s: %w", key, err)
		}
		n++
	}
	return n, nil
}

// extractTmeUsername 从任意 url/文本里提取 t.me 用户名(私密链接返回空, 走 l: 链接键归并)
func extractTmeUsername(url string) string {
	m := tmeRe.FindStringSubmatch(url)
	if m == nil || m[1] != "" {
		return ""
	}
	return m[2]
}

func firstNonEmpty(s ...string) string {
	for _, v := range s {
		if v != "" {
			return v
		}
	}
	return ""
}

// 防 unused 误报(bio 里可能只有 @ 没有链接时 strings 仍被用到)
var _ = strings.TrimSpace
