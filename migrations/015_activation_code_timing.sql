-- +goose Up
-- Add timing_type to activation_codes for immediate vs on-use expiration calculation
ALTER TABLE activation_codes ADD COLUMN IF NOT EXISTS timing_type VARCHAR(20) NOT NULL DEFAULT 'immediate' CHECK (timing_type IN ('immediate', 'on_use'));

-- Update existing codes to immediate
UPDATE activation_codes SET timing_type = 'immediate' WHERE timing_type IS NULL;

-- +goose Down
-- Remove timing_type column
ALTER TABLE activation_codes DROP COLUMN IF EXISTS timing_type;
