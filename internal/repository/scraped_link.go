package repository

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"tg-sender/internal/domain"
)

// ScrapedLinkRepository handles scraped link data access.
type ScrapedLinkRepository struct {
	db *pgxpool.Pool
}

// NewScrapedLinkRepository creates a new scraped link repository.
func NewScrapedLinkRepository(db *pgxpool.Pool) *ScrapedLinkRepository {
	return &ScrapedLinkRepository{db: db}
}

// Create inserts a single scraped link.
func (r *ScrapedLinkRepository) Create(ctx context.Context, link *domain.ScrapedLink) error {
	_, err := r.db.Exec(ctx, `
		INSERT INTO scraped_links (link_id, tenant_id, account_id, link_type, link_hash, full_url,
			source_chat_id, source_chat_title, resolved_chat_id, resolved_title, member_count, status, created_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13)
		ON CONFLICT (tenant_id, link_hash) DO NOTHING`,
		link.LinkID, link.TenantID, link.AccountID, link.LinkType, link.LinkHash, link.FullURL,
		link.SourceChatID, link.SourceChatTitle, link.ResolvedChatID, link.ResolvedTitle,
		link.MemberCount, link.Status, link.CreatedAt)
	return err
}

// CreateBatch inserts multiple scraped links with deduplication.
func (r *ScrapedLinkRepository) CreateBatch(ctx context.Context, links []*domain.ScrapedLink) error {
	if len(links) == 0 {
		return nil
	}
	batch := &pgx.Batch{}
	for _, link := range links {
		batch.Queue(`
			INSERT INTO scraped_links (link_id, tenant_id, account_id, link_type, link_hash, full_url,
				source_chat_id, source_chat_title, status, created_at)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
			ON CONFLICT (tenant_id, link_hash) DO NOTHING`,
			link.LinkID, link.TenantID, link.AccountID, link.LinkType, link.LinkHash, link.FullURL,
			link.SourceChatID, link.SourceChatTitle, link.Status, link.CreatedAt)
	}
	br := r.db.SendBatch(ctx, batch)
	defer br.Close()
	// P1-16 审计修复: 原来只调一次 br.Exec()——pgx batch 的 Exec 每次只消费一个
	// 结果, 第 2~N 条的错误被静默丢弃(插入失败无人知)。按入队数逐个 Exec 消费
	// 全部结果, 任何一条失败都返回; Close 做最终汇总。
	// 注意: 不可用 br.Next()——本项目 pgx v5.9 的 BatchResults 接口没有 Next
	// 方法(2026-10-23 构建实证), 按计数消费是正确姿势。
	for i := 0; i < len(links); i++ {
		if _, err := br.Exec(); err != nil {
			return fmt.Errorf("scraped_link batch insert: %w", err)
		}
	}
	return br.Close()
}

// GetByHash retrieves a link by tenant and hash.
func (r *ScrapedLinkRepository) GetByHash(ctx context.Context, tenantID uuid.UUID, linkHash string) (*domain.ScrapedLink, error) {
	var link domain.ScrapedLink
	var resolvedAt *time.Time
	err := r.db.QueryRow(ctx, `
		SELECT link_id, tenant_id, account_id, link_type, link_hash, full_url,
			source_chat_id, source_chat_title, resolved_chat_id, resolved_title, member_count, status, created_at, resolved_at
		FROM scraped_links WHERE tenant_id = $1 AND link_hash = $2`,
		tenantID, linkHash).Scan(
		&link.LinkID, &link.TenantID, &link.AccountID, &link.LinkType, &link.LinkHash, &link.FullURL,
		&link.SourceChatID, &link.SourceChatTitle, &link.ResolvedChatID, &link.ResolvedTitle,
		&link.MemberCount, &link.Status, &link.CreatedAt, &resolvedAt)
	if err != nil {
		return nil, err
	}
	link.ResolvedAt = resolvedAt
	return &link, nil
}

// ListByAccount 查询某账号采集到的链接(结果页用)。
// since 为零值时不做时间过滤; 传任务创建时间即可圈出单次任务窗口。
func (r *ScrapedLinkRepository) ListByAccount(ctx context.Context, tenantID, accountID uuid.UUID, since time.Time, limit, offset int) ([]*domain.ScrapedLink, int, error) {
	// 注意: COUNT 查询必须只引用自己用到的参数位, 否则 Postgres 无法推断未使用参数的类型(42P18)。
	var total int
	var err error
	if since.IsZero() {
		err = r.db.QueryRow(ctx,
			`SELECT COUNT(*) FROM scraped_links WHERE tenant_id = $1 AND account_id = $2`,
			tenantID, accountID).Scan(&total)
	} else {
		err = r.db.QueryRow(ctx,
			`SELECT COUNT(*) FROM scraped_links WHERE tenant_id = $1 AND account_id = $2 AND created_at >= $3`,
			tenantID, accountID, since).Scan(&total)
	}
	if err != nil {
		return nil, 0, fmt.Errorf("count scraped links by account: %w", err)
	}

	var rows pgx.Rows
	if since.IsZero() {
		rows, err = r.db.Query(ctx, `
		SELECT link_id, tenant_id, account_id, link_type, link_hash, full_url,
			source_chat_id, source_chat_title, resolved_chat_id, resolved_title, member_count, status, created_at, resolved_at
		FROM scraped_links
		WHERE tenant_id = $1 AND account_id = $2
		ORDER BY created_at DESC LIMIT $3 OFFSET $4`, tenantID, accountID, limit, offset)
	} else {
		rows, err = r.db.Query(ctx, `
		SELECT link_id, tenant_id, account_id, link_type, link_hash, full_url,
			source_chat_id, source_chat_title, resolved_chat_id, resolved_title, member_count, status, created_at, resolved_at
		FROM scraped_links
		WHERE tenant_id = $1 AND account_id = $2 AND created_at >= $5
		ORDER BY created_at DESC LIMIT $3 OFFSET $4`, tenantID, accountID, limit, offset, since)
	}
	if err != nil {
		return nil, 0, fmt.Errorf("list scraped links by task: %w", err)
	}
	defer rows.Close()

	var links []*domain.ScrapedLink
	for rows.Next() {
		var link domain.ScrapedLink
		// resolved_* 和 member_count 对未解析的私密链接为 NULL, 必须用可空类型接收
		var resolvedAt *time.Time
		var resolvedChatID sql.NullInt64
		var resolvedTitle sql.NullString
		var memberCount sql.NullInt64
		if err := rows.Scan(
			&link.LinkID, &link.TenantID, &link.AccountID, &link.LinkType, &link.LinkHash, &link.FullURL,
			&link.SourceChatID, &link.SourceChatTitle, &resolvedChatID, &resolvedTitle,
			&memberCount, &link.Status, &link.CreatedAt, &resolvedAt,
		); err != nil {
			return nil, 0, err
		}
		link.ResolvedChatID = resolvedChatID.Int64
		link.ResolvedTitle = resolvedTitle.String
		link.MemberCount = int(memberCount.Int64)
		link.ResolvedAt = resolvedAt
		links = append(links, &link)
	}
	return links, total, rows.Err()
}

// List retrieves scraped links for a tenant with optional status filter.
func (r *ScrapedLinkRepository) List(ctx context.Context, tenantID uuid.UUID, status string, limit, offset int) ([]*domain.ScrapedLink, int, error) {
	var rows pgx.Rows
	var err error
	var total int

	if status != "" {
		err = r.db.QueryRow(ctx, `SELECT COUNT(*) FROM scraped_links WHERE tenant_id = $1 AND status = $2`, tenantID, status).Scan(&total)
	} else {
		err = r.db.QueryRow(ctx, `SELECT COUNT(*) FROM scraped_links WHERE tenant_id = $1`, tenantID).Scan(&total)
	}
	if err != nil {
		return nil, 0, fmt.Errorf("count scraped links: %w", err)
	}

	if status != "" {
		rows, err = r.db.Query(ctx, `
			SELECT link_id, tenant_id, account_id, link_type, link_hash, full_url,
				source_chat_id, source_chat_title, resolved_chat_id, resolved_title, member_count, status, created_at, resolved_at
			FROM scraped_links WHERE tenant_id = $1 AND status = $2
			ORDER BY created_at DESC LIMIT $3 OFFSET $4`, tenantID, status, limit, offset)
	} else {
		rows, err = r.db.Query(ctx, `
			SELECT link_id, tenant_id, account_id, link_type, link_hash, full_url,
				source_chat_id, source_chat_title, resolved_chat_id, resolved_title, member_count, status, created_at, resolved_at
			FROM scraped_links WHERE tenant_id = $1
			ORDER BY created_at DESC LIMIT $2 OFFSET $3`, tenantID, limit, offset)
	}
	if err != nil {
		return nil, 0, fmt.Errorf("list scraped links: %w", err)
	}
	defer rows.Close()

	var links []*domain.ScrapedLink
	for rows.Next() {
		var link domain.ScrapedLink
		var resolvedAt *time.Time
		var resolvedChatID sql.NullInt64
		var resolvedTitle sql.NullString
		var memberCount sql.NullInt64
		err := rows.Scan(
			&link.LinkID, &link.TenantID, &link.AccountID, &link.LinkType, &link.LinkHash, &link.FullURL,
			&link.SourceChatID, &link.SourceChatTitle, &resolvedChatID, &resolvedTitle,
			&memberCount, &link.Status, &link.CreatedAt, &resolvedAt)
		if err != nil {
			return nil, 0, err
		}
		link.ResolvedChatID = resolvedChatID.Int64
		link.ResolvedTitle = resolvedTitle.String
		link.MemberCount = int(memberCount.Int64)
		link.ResolvedAt = resolvedAt
		links = append(links, &link)
	}
	return links, total, rows.Err()
}

// UpdateResolved updates resolved info for a public link.
func (r *ScrapedLinkRepository) UpdateResolved(ctx context.Context, linkID uuid.UUID, resolvedChatID int64, resolvedTitle string, memberCount int) error {
	_, err := r.db.Exec(ctx, `
		UPDATE scraped_links SET resolved_chat_id = $2, resolved_title = $3, member_count = $4, status = 'resolved', resolved_at = NOW()
		WHERE link_id = $1`,
		linkID, resolvedChatID, resolvedTitle, memberCount)
	return err
}

// UpdateStatus updates link status.
func (r *ScrapedLinkRepository) UpdateStatus(ctx context.Context, linkID uuid.UUID, status string) error {
	_, err := r.db.Exec(ctx, `UPDATE scraped_links SET status = $2 WHERE link_id = $1`, linkID, status)
	return err
}

// CountByTenant counts total links for a tenant.
func (r *ScrapedLinkRepository) CountByTenant(ctx context.Context, tenantID uuid.UUID) (int, error) {
	var count int
	err := r.db.QueryRow(ctx, `SELECT COUNT(*) FROM scraped_links WHERE tenant_id = $1`, tenantID).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("count scraped links: %w", err)
	}
	return count, nil
}

// CountByTenantSince 统计某时间点后该租户入库的链接数(按任务窗口计数)。
func (r *ScrapedLinkRepository) CountByTenantSince(ctx context.Context, tenantID uuid.UUID, since time.Time) (int, error) {
	var count int
	err := r.db.QueryRow(ctx, `SELECT COUNT(*) FROM scraped_links WHERE tenant_id = $1 AND created_at >= $2`, tenantID, since).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("count scraped links since: %w", err)
	}
	return count, nil
}

// CountByTenantAndStatus counts links by status for a tenant.
func (r *ScrapedLinkRepository) CountByTenantAndStatus(ctx context.Context, tenantID uuid.UUID, status string) (int, error) {
	var count int
	err := r.db.QueryRow(ctx, `SELECT COUNT(*) FROM scraped_links WHERE tenant_id = $1 AND status = $2`, tenantID, status).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("count scraped links by status: %w", err)
	}
	return count, nil
}
