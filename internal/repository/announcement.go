package repository

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"tg-sender/internal/domain"
)

// AnnouncementRepository handles announcement data access.
type AnnouncementRepository struct {
	db *pgxpool.Pool
}

// NewAnnouncementRepository creates a new repository.
func NewAnnouncementRepository(db *pgxpool.Pool) *AnnouncementRepository {
	return &AnnouncementRepository{db: db}
}

// List returns active announcements for a tenant's plan.
func (r *AnnouncementRepository) List(ctx context.Context, planID string) ([]domain.Announcement, error) {
	rows, err := r.db.Query(ctx, `
	    SELECT announcement_id, title, content, priority, target_type, target_plans, start_at, end_at, is_active, created_at
	    -- P2-63 审计修复: 原来无 LIMIT——公告多了全量拉, 响应无界
	    FROM announcements
	    WHERE is_active = TRUE AND start_at <= NOW() AND (end_at ISNULL OR end_at > NOW())
	    ORDER BY priority DESC, created_at DESC LIMIT 100`)
	if err != nil {
		return nil, fmt.Errorf("list announcements: %w", err)
	}
	defer rows.Close()

	var list []domain.Announcement
	for rows.Next() {
		var a domain.Announcement
		if err := rows.Scan(&a.AnnouncementID, &a.Title, &a.Content, &a.Priority, &a.TargetType, &a.TargetPlans, &a.StartAt, &a.EndAt, &a.IsActive, &a.CreatedAt); err != nil {
			continue
		}
		list = append(list, a)
	}

	// Filter by plan at application layer to avoid SQL type mismatch
	var filtered []domain.Announcement
	for _, a := range list {
		if a.TargetType == "all" {
			filtered = append(filtered, a)
			continue
		}
		// V35 审计修复: TargetPlans 为 nil/空(老数据 NULL)时循环不执行, 公告被静默漏发。
		// 空计划列表按"面向所有套餐"处理(与 TargetType="all" 同义, 兼容历史数据)。
		if len(a.TargetPlans) == 0 {
			filtered = append(filtered, a)
			continue
		}
		for _, p := range a.TargetPlans {
			if p == planID {
				filtered = append(filtered, a)
				break
			}
		}
	}
	return filtered, nil
}

// ListAll returns all announcements (admin).
func (r *AnnouncementRepository) ListAll(ctx context.Context) ([]domain.Announcement, error) {
	rows, err := r.db.Query(ctx, `
	    SELECT announcement_id, title, content, priority, target_type, target_plans, start_at, end_at, is_active, created_at
	    FROM announcements ORDER BY created_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var list []domain.Announcement
	for rows.Next() {
		var a domain.Announcement
		if err := rows.Scan(&a.AnnouncementID, &a.Title, &a.Content, &a.Priority, &a.TargetType, &a.TargetPlans, &a.StartAt, &a.EndAt, &a.IsActive, &a.CreatedAt); err != nil {
			continue
		}
		list = append(list, a)
	}
	return list, nil
}

// Create creates a new announcement.
func (r *AnnouncementRepository) Create(ctx context.Context, a *domain.Announcement) error {
	_, err := r.db.Exec(ctx, `
	    INSERT INTO announcements (title, content, priority, target_type, target_plans, start_at, end_at)
	    VALUES ($1, $2, $3, $4, $5, $6, $7)`,
		a.Title, a.Content, a.Priority, a.TargetType, a.TargetPlans, a.StartAt, a.EndAt,
	)
	return err
}

// Update updates an announcement.
func (r *AnnouncementRepository) Update(ctx context.Context, a *domain.Announcement) error {
	_, err := r.db.Exec(ctx, `
	    UPDATE announcements SET title = $2, content = $3, priority = $4, target_type = $5, target_plans = $6, start_at = $7, end_at = $8, is_active = $9
	    WHERE announcement_id = $1`,
		a.AnnouncementID, a.Title, a.Content, a.Priority, a.TargetType, a.TargetPlans, a.StartAt, a.EndAt, a.IsActive,
	)
	return err
}

// Delete deletes an announcement.
func (r *AnnouncementRepository) Delete(ctx context.Context, id uuid.UUID) error {
	_, err := r.db.Exec(ctx, `DELETE FROM announcements WHERE announcement_id = $1`, id)
	return err
}
