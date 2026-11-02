-- +goose Up
-- 会话表增加 server_address 列：存储导入协议号时 Telethon 记录的真实 DC 地址。
-- 恢复会话时优先拨这个地址，避免内置地址表 IP 被墙/抖动导致 i/o timeout。
ALTER TABLE sessions ADD COLUMN IF NOT EXISTS server_address TEXT DEFAULT '';

-- +goose Down
ALTER TABLE sessions DROP COLUMN IF EXISTS server_address;
