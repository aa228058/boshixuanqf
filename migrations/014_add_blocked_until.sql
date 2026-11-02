-- +goose Up
-- Add blocked_until for FloodWait auto-recovery
ALTER TABLE accounts ADD COLUMN IF NOT EXISTS blocked_until TIMESTAMPTZ;

-- +goose Down
-- ALTER TABLE accounts DROP COLUMN IF EXISTS blocked_until;
