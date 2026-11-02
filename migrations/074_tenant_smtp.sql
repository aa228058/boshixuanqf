-- +goose Up
ALTER TABLE tenants ADD COLUMN IF NOT EXISTS notify_smtp JSONB NOT NULL DEFAULT '{}'::jsonb;
-- +goose Down
SELECT 1;
