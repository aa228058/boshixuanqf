-- +goose Up
-- 群链接库: 私密群兜底进群用(随使用自动积累: 粘贴的链接/监听收到的链接成功后归档)
ALTER TABLE groups ADD COLUMN IF NOT EXISTS invite_link TEXT NOT NULL DEFAULT '';
