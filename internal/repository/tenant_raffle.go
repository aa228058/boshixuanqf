package repository

import (
	"context"

	"github.com/google/uuid"
)

// 抽奖助手开关(052 迁移加的 tenants.raffle_enabled 列)

// IsRaffleEnabled 查询租户抽奖助手开关
func (r *TenantRepository) IsRaffleEnabled(ctx context.Context, tenantID uuid.UUID) (bool, error) {
	var en bool
	err := r.db.QueryRow(ctx, `SELECT raffle_enabled FROM tenants WHERE tenant_id = $1`, tenantID).Scan(&en)
	if err != nil {
		return false, err
	}
	return en, nil
}

// SetRaffleEnabled 设置租户抽奖助手开关
func (r *TenantRepository) SetRaffleEnabled(ctx context.Context, tenantID uuid.UUID, enabled bool) error {
	_, err := r.db.Exec(ctx, `UPDATE tenants SET raffle_enabled = $2 WHERE tenant_id = $1`, tenantID, enabled)
	return err
}

// GetRaffleReply 查询中奖回复设置(060 迁移加的列): 返回 (是否开启, 文案)
func (r *TenantRepository) GetRaffleReply(ctx context.Context, tenantID uuid.UUID) (bool, string, error) {
	var en bool
	var text string
	err := r.db.QueryRow(ctx, `SELECT raffle_reply_enabled, raffle_reply_text FROM tenants WHERE tenant_id = $1`, tenantID).Scan(&en, &text)
	if err != nil {
		return false, "", err
	}
	return en, text, nil
}

// SetRaffleReply 保存中奖回复设置
func (r *TenantRepository) SetRaffleReply(ctx context.Context, tenantID uuid.UUID, enabled bool, text string) error {
	_, err := r.db.Exec(ctx, `UPDATE tenants SET raffle_reply_enabled = $2, raffle_reply_text = $3 WHERE tenant_id = $1`, tenantID, enabled, text)
	return err
}

// GetRaffleBotPing 查询私聊机器人激活设置(061): 返回 (是否开启, 激活语文案)
func (r *TenantRepository) GetRaffleBotPing(ctx context.Context, tenantID uuid.UUID) (bool, string, error) {
	var en bool
	var text string
	err := r.db.QueryRow(ctx, `SELECT raffle_bot_ping_enabled, raffle_bot_ping_text FROM tenants WHERE tenant_id = $1`, tenantID).Scan(&en, &text)
	if err != nil {
		return false, "", err
	}
	return en, text, nil
}

// SetRaffleBotPing 保存私聊机器人激活设置
func (r *TenantRepository) SetRaffleBotPing(ctx context.Context, tenantID uuid.UUID, enabled bool, text string) error {
	_, err := r.db.Exec(ctx, `UPDATE tenants SET raffle_bot_ping_enabled = $2, raffle_bot_ping_text = $3 WHERE tenant_id = $1`, tenantID, enabled, text)
	return err
}
