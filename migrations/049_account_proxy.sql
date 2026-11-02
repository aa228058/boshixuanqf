-- +goose Up
-- 账号级代理: socks5://user:pass@host:port, 空=直连。连接该账号的所有 TG 客户端走此代理。
ALTER TABLE accounts ADD COLUMN IF NOT EXISTS proxy_url TEXT DEFAULT '';

-- +goose Down
ALTER TABLE accounts DROP COLUMN IF EXISTS proxy_url;
