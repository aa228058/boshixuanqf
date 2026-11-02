-- +goose Up
-- 添加任务循环发送字段

ALTER TABLE tasks ADD COLUMN IF NOT EXISTS is_loop BOOLEAN DEFAULT FALSE;
ALTER TABLE tasks ADD COLUMN IF NOT EXISTS loop_interval_minutes INT DEFAULT 60;
ALTER TABLE tasks ADD COLUMN IF NOT EXISTS min_interval_seconds INT DEFAULT 5;
ALTER TABLE tasks ADD COLUMN IF NOT EXISTS max_interval_seconds INT DEFAULT 15;
ALTER TABLE tasks ADD COLUMN IF NOT EXISTS next_run_at TIMESTAMPTZ;

-- +goose Down
ALTER TABLE tasks DROP COLUMN IF EXISTS is_loop;
ALTER TABLE tasks DROP COLUMN IF EXISTS loop_interval_minutes;
ALTER TABLE tasks DROP COLUMN IF EXISTS min_interval_seconds;
ALTER TABLE tasks DROP COLUMN IF EXISTS max_interval_seconds;
ALTER TABLE tasks DROP COLUMN IF EXISTS next_run_at;