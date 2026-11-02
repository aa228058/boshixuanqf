-- +goose Up
-- 账号头像：登录成功后从 Telegram 拉取自己的头像存本地文件, 数据库存相对路径。
ALTER TABLE accounts ADD COLUMN IF NOT EXISTS avatar_path TEXT DEFAULT '';

-- +goose Down
ALTER TABLE accounts DROP COLUMN IF EXISTS avatar_path;
