package repository

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// AIConfigRepository 租户级 AI 配置(api_key 密文存储)。
type AIConfigRepository struct {
	db *pgxpool.Pool
}

func NewAIConfigRepository(db *pgxpool.Pool) *AIConfigRepository {
	return &AIConfigRepository{db: db}
}

// Save 覆盖式保存(不存在则插入)。apiKeyEnc 为密文。
func (r *AIConfigRepository) Save(ctx context.Context, tenantID uuid.UUID, provider, apiKeyEnc, model string) error {
	_, err := r.db.Exec(ctx, `
		INSERT INTO ai_configs (tenant_id, provider, api_key, model, updated_at)
		VALUES ($1, $2, $3, $4, NOW())
		ON CONFLICT (tenant_id) DO UPDATE SET provider = EXCLUDED.provider,
			api_key = EXCLUDED.api_key, model = EXCLUDED.model, updated_at = NOW()`,
		tenantID, provider, apiKeyEnc, model)
	return err
}

// Load 读取; 返回 provider, apiKey密文, model, hasKey
func (r *AIConfigRepository) Load(ctx context.Context, tenantID uuid.UUID) (provider, apiKeyEnc, model string, hasKey bool, err error) {
	err = r.db.QueryRow(ctx,
		`SELECT provider, api_key, model, api_key <> '' FROM ai_configs WHERE tenant_id = $1`, tenantID).
		Scan(&provider, &apiKeyEnc, &model, &hasKey)
	if err != nil {
		// P2-84 审计修复: 原来任何 DB 错误都当"无配置"——DB 抖动时 AI 验证静默降级,
		// 该开的验证没开无人察觉。区分: ErrNoRows=真无配置(正常), 其他=故障(上报)。
		if errors.Is(err, pgx.ErrNoRows) {
			return "", "", "", false, nil // 无配置不视为错误
		}
		return "", "", "", false, fmt.Errorf("ai_config load db error: %w", err)
	}
	return provider, apiKeyEnc, model, hasKey, nil
}

// SetVerifierEnabled 租户级入群验证总开关
func (r *AIConfigRepository) SetVerifierEnabled(ctx context.Context, tenantID uuid.UUID, enabled bool) error {
	_, err := r.db.Exec(ctx, `UPDATE tenants SET verifier_enabled = $2 WHERE tenant_id = $1`, tenantID, enabled)
	return err
}

// VerifierEnabled 查询开关
func (r *AIConfigRepository) VerifierEnabled(ctx context.Context, tenantID uuid.UUID) (bool, error) {
	var en bool
	err := r.db.QueryRow(ctx, `SELECT verifier_enabled FROM tenants WHERE tenant_id = $1`, tenantID).Scan(&en)
	return en, err
}

var _ = time.Now
