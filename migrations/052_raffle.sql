-- +goose Up
-- 抽奖助手: 账号在群内看到抽奖机器人的"参与关键词"消息时自动发关键词参与。
ALTER TABLE tenants ADD COLUMN IF NOT EXISTS raffle_enabled BOOLEAN NOT NULL DEFAULT FALSE;

-- +goose Down
ALTER TABLE tenants DROP COLUMN IF EXISTS raffle_enabled;
