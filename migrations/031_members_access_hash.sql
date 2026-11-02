-- +goose Up
-- 历史消息采集的活跃用户带 AccessHash, 私聊直发必需
ALTER TABLE members ADD COLUMN IF NOT EXISTS access_hash BIGINT NOT NULL DEFAULT 0;

-- +goose Down
ALTER TABLE members DROP COLUMN IF EXISTS access_hash;
