package repository

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"tg-sender/internal/domain"
)

// QuotaRepository handles quota data access.
type QuotaRepository struct {
	db *pgxpool.Pool
}

// NewQuotaRepository creates a new quota repository.
func NewQuotaRepository(db *pgxpool.Pool) *QuotaRepository {
	return &QuotaRepository{db: db}
}

// Get retrieves quota for an account.
func (r *QuotaRepository) Get(ctx context.Context, accountID uuid.UUID, quotaType string) (*domain.AccountQuota, error) {
	var q domain.AccountQuota
	err := r.db.QueryRow(ctx, `
		SELECT account_id, quota_type, daily_limit, used_today, over_limit_count, reset_at
		FROM account_quotas WHERE account_id = $1 AND quota_type = $2`, accountID, quotaType,
	).Scan(&q.AccountID, &q.QuotaType, &q.DailyLimit, &q.UsedToday, &q.OverLimitCount, &q.ResetAt)
	if err != nil {
		return nil, fmt.Errorf("get quota: %w", err)
	}
	return &q, nil
}

// GetAll retrieves all quotas for an account.
func (r *QuotaRepository) GetAll(ctx context.Context, accountID uuid.UUID) ([]*domain.AccountQuota, error) {
	rows, err := r.db.Query(ctx, `
		SELECT account_id, quota_type, daily_limit, used_today, over_limit_count, reset_at
		FROM account_quotas WHERE account_id = $1`, accountID)
	if err != nil {
		return nil, fmt.Errorf("get all quotas: %w", err)
	}
	defer rows.Close()

	var quotas []*domain.AccountQuota
	for rows.Next() {
		var q domain.AccountQuota
		if err := rows.Scan(&q.AccountID, &q.QuotaType, &q.DailyLimit, &q.UsedToday, &q.OverLimitCount, &q.ResetAt); err != nil {
			return nil, err
		}
		quotas = append(quotas, &q)
	}
	return quotas, rows.Err()
}

// Upsert creates or updates a quota.
func (r *QuotaRepository) Upsert(ctx context.Context, q *domain.AccountQuota) error {
	_, err := r.db.Exec(ctx, `
		INSERT INTO account_quotas (account_id, quota_type, daily_limit, used_today, over_limit_count, reset_at)
		VALUES ($1, $2, $3, $4, $5, $6)
		ON CONFLICT (account_id, quota_type) DO UPDATE SET
			daily_limit = EXCLUDED.daily_limit,
			used_today = EXCLUDED.used_today,
			over_limit_count = EXCLUDED.over_limit_count,
			reset_at = EXCLUDED.reset_at`,
		q.AccountID, q.QuotaType, q.DailyLimit, q.UsedToday, q.OverLimitCount, q.ResetAt)
	return err
}

// IncrementUsage increments used_today for a quota and returns current usage info.
// This is a "soft quota" — it never blocks, only records usage for monitoring and notifications.
func (r *QuotaRepository) IncrementUsage(ctx context.Context, accountID uuid.UUID, quotaType string) (*domain.UsageResult, error) {
	var result domain.UsageResult
	err := pgx.BeginFunc(ctx, r.db, func(tx pgx.Tx) error {
		err := tx.QueryRow(ctx, `
			SELECT daily_limit, used_today, over_limit_count FROM account_quotas
			WHERE account_id = $1 AND quota_type = $2 FOR UPDATE`, accountID, quotaType).Scan(&result.DailyLimit, &result.UsedToday, &result.OverLimitCount)
		if err != nil {
			return err
		}

		newUsed := result.UsedToday + 1
		overLimit := 0
		if newUsed > result.DailyLimit {
			overLimit = 1
			result.OverLimit = true
		}
		result.UsedToday = newUsed

		_, err = tx.Exec(ctx, `
			UPDATE account_quotas
			SET used_today = $3, over_limit_count = over_limit_count + $4, reset_at = $5
			WHERE account_id = $1 AND quota_type = $2`,
			accountID, quotaType, newUsed, overLimit, time.Now().UTC().Add(24*time.Hour).Truncate(24*time.Hour))
		return err
	})
	if err != nil {
		return nil, err
	}
	return &result, nil
}

// AddUsage increments usage by n in a single transaction (batched variant of IncrementUsage).
// Same soft-quota semantics: never blocks, only records usage for monitoring and notifications.
func (r *QuotaRepository) AddUsage(ctx context.Context, accountID uuid.UUID, quotaType string, n int) (*domain.UsageResult, error) {
	if n <= 0 {
		return &domain.UsageResult{}, nil
	}
	var result domain.UsageResult
	err := pgx.BeginFunc(ctx, r.db, func(tx pgx.Tx) error {
		err := tx.QueryRow(ctx, `
			SELECT daily_limit, used_today, over_limit_count FROM account_quotas
			WHERE account_id = $1 AND quota_type = $2 FOR UPDATE`, accountID, quotaType).Scan(&result.DailyLimit, &result.UsedToday, &result.OverLimitCount)
		if err != nil {
			return err
		}

		newUsed := result.UsedToday + n
		overLimit := 0
		if newUsed > result.DailyLimit {
			overLimit = 1
			result.OverLimit = true
		}
		result.UsedToday = newUsed

		_, err = tx.Exec(ctx, `
			UPDATE account_quotas
			SET used_today = $3, over_limit_count = over_limit_count + $4, reset_at = $5
			WHERE account_id = $1 AND quota_type = $2`,
			accountID, quotaType, newUsed, overLimit, time.Now().UTC().Add(24*time.Hour).Truncate(24*time.Hour))
		return err
	})
	if err != nil {
		return nil, err
	}
	return &result, nil
}

// ResetDaily resets all daily quotas at midnight UTC.
func (r *QuotaRepository) ResetDaily(ctx context.Context) error {
	// P1-17/V21 审计修复: 原来 reset_at 用 "UTC日期+1天"(timestamptz 按会话时区转换)
	// 而比较用 "UTC timestamp"(不带时区)——类型/时区口径不一致, 非 UTC 会话重置永不触发。
	// 统一用 now()(timestamptz, 内部无时区歧义)。
	_, err := r.db.Exec(ctx, `
		UPDATE account_quotas
		SET used_today = 0, over_limit_count = 0, reset_at = now() + INTERVAL '1 day'
		WHERE reset_at <= now()
		  AND used_today > 0`)
	return err
}

// InitAccountQuotas creates default quotas for a new account.
func (r *QuotaRepository) InitAccountQuotas(ctx context.Context, accountID uuid.UUID, level domain.AccountLevel) error {
	cfg, _ := domain.GetQuotaConfig(level)
	resetAt := time.Now().Add(24 * time.Hour).Truncate(24 * time.Hour)

	quotas := []domain.AccountQuota{
		{AccountID: accountID, QuotaType: "broadcast", DailyLimit: cfg["broadcast_daily"], ResetAt: resetAt},
		{AccountID: accountID, QuotaType: "join", DailyLimit: cfg["join_daily"], ResetAt: resetAt},
		{AccountID: accountID, QuotaType: "scrape", DailyLimit: cfg["scrape_daily"], ResetAt: resetAt},
	}

	return pgx.BeginFunc(ctx, r.db, func(tx pgx.Tx) error {
		for _, q := range quotas {
			_, err := tx.Exec(ctx, `
				INSERT INTO account_quotas (account_id, quota_type, daily_limit, used_today, over_limit_count, reset_at)
				VALUES ($1, $2, $3, 0, 0, $4)
				ON CONFLICT (account_id, quota_type) DO NOTHING`,
				q.AccountID, q.QuotaType, q.DailyLimit, q.ResetAt)
			if err != nil {
				return err
			}
		}
		return nil
	})
}

// DeleteByAccount deletes all quotas for an account.
func (r *QuotaRepository) DeleteByAccount(ctx context.Context, accountID uuid.UUID) error {
	_, err := r.db.Exec(ctx, `DELETE FROM account_quotas WHERE account_id = $1`, accountID)
	return err
}
