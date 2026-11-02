package repository

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"tg-sender/internal/domain"
)

// SupportConfigRepository handles support config data access.
type SupportConfigRepository struct {
	db *pgxpool.Pool
}

// NewSupportConfigRepository creates a new repository.
func NewSupportConfigRepository(db *pgxpool.Pool) *SupportConfigRepository {
	return &SupportConfigRepository{db: db}
}

// Get returns the support config (always returns row 1).
func (r *SupportConfigRepository) Get(ctx context.Context) (*domain.SupportConfig, error) {
	var c domain.SupportConfig
	err := r.db.QueryRow(ctx, `
	    SELECT id, contact_type, contact_value, display_name, qr_code_url, is_active, updated_at
	    FROM support_config WHERE id = 1`,
	).Scan(&c.ID, &c.ContactType, &c.ContactValue, &c.DisplayName, &c.QRCodeURL, &c.IsActive, &c.UpdatedAt)
	if err != nil {
		// P2-85 审计修复: 表空(id=1 无行)时原来返回 500——客服配置未初始化=不显示,
		// 是正常状态不是错误。ErrNoRows 返回 nil 配置。
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, fmt.Errorf("get support config: %w", err)
	}
	return &c, nil
}

// Update updates the support config.
func (r *SupportConfigRepository) Update(ctx context.Context, c *domain.SupportConfig) error {
	_, err := r.db.Exec(ctx, `
	    UPDATE support_config SET contact_type = $1, contact_value = $2, display_name = $3, qr_code_url = $4, is_active = $5, updated_at = NOW()
	    WHERE id = 1`,
		c.ContactType, c.ContactValue, c.DisplayName, c.QRCodeURL, c.IsActive,
	)
	return err
}
