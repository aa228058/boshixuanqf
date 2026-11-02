package repository

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"tg-sender/internal/domain"
)

// SystemGroupRepository 平台级群组库(找群功能)。跨租户共享, 所有查询无租户过滤。
type SystemGroupRepository struct {
	db *pgxpool.Pool
}

func NewSystemGroupRepository(db *pgxpool.Pool) *SystemGroupRepository {
	return &SystemGroupRepository{db: db}
}

// SystemGroupFilter 列表筛选
type SystemGroupFilter struct {
	Keyword  string // 标题/username/link 模糊
	GType    string // public | private | channel | unknown | ""
	Status   string // active | joined | invalid | ""
	Source   string // scraped | joined | bio | ""
	Page     int
	PageSize int
}

// Upsert 归并写入: dedup_key 冲突时热度+1、取更优标题/更大成员数、刷新最后收录时间。
func (r *SystemGroupRepository) Upsert(ctx context.Context, sg *domain.SystemGroup) error {
	seenAt := sg.LastSeenAt
	if seenAt.IsZero() {
		seenAt = time.Now()
	}
	_, err := r.db.Exec(ctx, `
		INSERT INTO system_groups (dedup_key, source, title, username, tme_link, gtype, member_count, status, ref_count, last_seen_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,1,$9)
		ON CONFLICT (dedup_key) DO UPDATE SET
			-- 每条独立来源记录贡献一次引用热度；游标负责避免运行期间重复扫描。
			ref_count   = system_groups.ref_count + 1,
			member_count= GREATEST(system_groups.member_count, EXCLUDED.member_count),
			last_seen_at= GREATEST(system_groups.last_seen_at, EXCLUDED.last_seen_at),
			status      = CASE WHEN system_groups.status = 'joined' THEN 'joined' ELSE EXCLUDED.status END,
			title       = CASE WHEN system_groups.title IS NULL OR system_groups.title = '' OR system_groups.title = system_groups.tme_link
			                   THEN EXCLUDED.title ELSE system_groups.title END,
			username    = CASE WHEN system_groups.username = '' OR system_groups.username IS NULL
			                   THEN EXCLUDED.username ELSE system_groups.username END,
			gtype       = CASE WHEN system_groups.gtype = 'unknown' THEN EXCLUDED.gtype ELSE system_groups.gtype END
	`, sg.DedupKey(), sg.Source, sg.Title, sg.Username, sg.TmeLink, sg.GType, sg.MemberCount, sg.Status, seenAt)
	return err
}

// MarkInvalidByDedupKey R87-M10: 加入失败等场景把群标记为失效("已失效"筛选的数据源)。
// 已加入的群不降级。
func (r *SystemGroupRepository) MarkInvalidByDedupKey(ctx context.Context, key string) error {
	_, err := r.db.Exec(ctx,
		`UPDATE system_groups SET status='invalid' WHERE dedup_key=$1 AND status != 'joined'`, key)
	return err
}

// GetByIDs 按主键批量取(批量加入用), 保持传入顺序无要求。
func (r *SystemGroupRepository) GetByIDs(ctx context.Context, ids []uuid.UUID) ([]domain.SystemGroup, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	rows, err := r.db.Query(ctx, `
		SELECT sg_id, source, COALESCE(title,''), COALESCE(username,''), COALESCE(tme_link,''),
		       gtype, member_count, status, ref_count, last_seen_at
		FROM system_groups WHERE sg_id = ANY($1)`, ids)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.SystemGroup
	for rows.Next() {
		var g domain.SystemGroup
		if err := rows.Scan(&g.SGID, &g.Source, &g.Title, &g.Username, &g.TmeLink, &g.GType, &g.MemberCount, &g.Status, &g.RefCount, &g.LastSeenAt); err != nil {
			return nil, err
		}
		out = append(out, g)
	}
	return out, rows.Err()
}

// List 分页查询
func (r *SystemGroupRepository) List(ctx context.Context, f SystemGroupFilter) ([]domain.SystemGroup, int64, error) {
	var where []string
	var args []interface{}
	n := 0
	// 关键字三字段模糊(同一参数复用)
	if f.Keyword != "" {
		n++
		kw := "%" + f.Keyword + "%"
		where = append(where, fmt.Sprintf("(title ILIKE $%d OR username ILIKE $%d OR tme_link ILIKE $%d)", n, n, n))
		args = append(args, kw)
	}
	if f.GType != "" {
		n++
		where = append(where, fmt.Sprintf("gtype = $%d", n))
		args = append(args, f.GType)
	}
	if f.Status != "" {
		n++
		where = append(where, fmt.Sprintf("status = $%d", n))
		args = append(args, f.Status)
	}
	if f.Source != "" {
		n++
		where = append(where, fmt.Sprintf("source = $%d", n))
		args = append(args, f.Source)
	}
	w := ""
	if len(where) > 0 {
		w = "WHERE " + strings.Join(where, " AND ")
	}
	var total int64
	if err := r.db.QueryRow(ctx, "SELECT COUNT(*) FROM system_groups "+w, args...).Scan(&total); err != nil {
		return nil, 0, err
	}
	page, size := f.Page, f.PageSize
	if page < 1 {
		page = 1
	}
	if size <= 0 || size > 100 {
		size = 20
	}
	n++
	args = append(args, size)
	n++
	args = append(args, (page-1)*size)
	rows, err := r.db.Query(ctx, `
		SELECT sg_id, source, COALESCE(title,''), COALESCE(username,''), COALESCE(tme_link,''),
		       gtype, member_count, status, ref_count, last_seen_at
		FROM system_groups `+w+`
		ORDER BY ref_count DESC, last_seen_at DESC, sg_id DESC  -- 唯一tiebreaker: 防同值行间顺序不定导致翻页重复/遗漏
		LIMIT $`+fmt.Sprint(n-1)+` OFFSET $`+fmt.Sprint(n), args...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	var out []domain.SystemGroup
	for rows.Next() {
		var g domain.SystemGroup
		if err := rows.Scan(&g.SGID, &g.Source, &g.Title, &g.Username, &g.TmeLink, &g.GType, &g.MemberCount, &g.Status, &g.RefCount, &g.LastSeenAt); err != nil {
			return nil, 0, err
		}
		out = append(out, g)
	}
	return out, total, rows.Err()
}
