-- +goose Up
-- Add reply_content column to monitor_rules for auto-reply feature
ALTER TABLE monitor_rules ADD COLUMN IF NOT EXISTS reply_content TEXT NOT NULL DEFAULT '';

-- +goose Down
ALTER TABLE monitor_rules DROP COLUMN IF EXISTS reply_content;
