-- +goose Up
-- 私聊发送需要 InputPeerUser.AccessHash: 同步时从 TG Contact 写入, 发送私聊消息时直发不报错.
-- 已存在行默认 0(存量联系人在下次同步群组时自动补齐)
ALTER TABLE contacts ADD COLUMN IF NOT EXISTS access_hash BIGINT NOT NULL DEFAULT 0;

-- +goose Down
ALTER TABLE contacts DROP COLUMN IF EXISTS access_hash;
