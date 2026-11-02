-- +goose Up
ALTER TABLE tenants ADD COLUMN IF NOT EXISTS notify_email TEXT NOT NULL DEFAULT '';
ALTER TABLE tenants ADD COLUMN IF NOT EXISTS notify_settings JSONB NOT NULL DEFAULT '{}'::jsonb;
-- +goose Down
SELECT 1;
