-- +goose Up
ALTER TABLE task_logs ADD COLUMN IF NOT EXISTS target_name VARCHAR(512);

-- +goose Down
ALTER TABLE task_logs DROP COLUMN IF EXISTS target_name;
