-- +goose Up
-- R153: 登录验证码监控 —— 协议号登录手机时, 自动捕获 TG 官方验证码消息并展示
ALTER TABLE accounts ADD COLUMN IF NOT EXISTS last_login_code TEXT NOT NULL DEFAULT '';
ALTER TABLE accounts ADD COLUMN IF NOT EXISTS login_code_at TIMESTAMPTZ;

-- +goose Down
ALTER TABLE accounts DROP COLUMN IF EXISTS login_code_at;
ALTER TABLE accounts DROP COLUMN IF EXISTS last_login_code;
