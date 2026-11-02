-- +goose Up
-- Encrypt existing session data and change column type to text (base64)

-- First, add a new text column
ALTER TABLE sessions ADD COLUMN IF NOT EXISTS session_data_new TEXT;

-- Note: Existing data will be encrypted on next save
-- The application will handle encryption/decryption transparently

-- Drop old column and rename new one (run after deployment)
-- ALTER TABLE sessions DROP COLUMN IF EXISTS session_data;
-- ALTER TABLE sessions RENAME COLUMN session_data_new TO session_data;

-- +goose Down
-- ALTER TABLE sessions ADD COLUMN session_data BYTEA;
