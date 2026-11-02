-- +goose Up
-- 060: 抽奖助手中奖回复设置(2026-10-21)
-- 中奖账号自动在抽奖群发送感谢语(普通群消息), 文案租户自定义, 支持 {奖品} 变量
ALTER TABLE tenants
    ADD COLUMN IF NOT EXISTS raffle_reply_enabled BOOLEAN NOT NULL DEFAULT FALSE,
    ADD COLUMN IF NOT EXISTS raffle_reply_text TEXT NOT NULL DEFAULT '';

-- +goose Down
ALTER TABLE tenants
    DROP COLUMN IF EXISTS raffle_reply_enabled,
    DROP COLUMN IF EXISTS raffle_reply_text;
