-- +goose Up
ALTER TABLE accounts ADD COLUMN IF NOT EXISTS probe_checked_at TIMESTAMPTZ;
ALTER TABLE accounts ADD COLUMN IF NOT EXISTS probe_result JSONB NOT NULL DEFAULT '{}'::jsonb;
-- +goose Down
SELECT 1;
