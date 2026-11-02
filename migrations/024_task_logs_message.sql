-- +goose Up
-- 统一日志系统: task_logs 增加可直接展示的中文可读文案列。
ALTER TABLE task_logs ADD COLUMN IF NOT EXISTS message TEXT NOT NULL DEFAULT '';

-- +goose Down
ALTER TABLE task_logs DROP COLUMN IF EXISTS message;
