-- +goose Up
-- +goose StatementBegin

-- Add username/password fields to tenants
ALTER TABLE tenants
    ADD COLUMN IF NOT EXISTS username VARCHAR(50) UNIQUE,
    ADD COLUMN IF NOT EXISTS password_hash VARCHAR(255);

-- Create index for username lookups
CREATE INDEX IF NOT EXISTS idx_tenants_username ON tenants(username);

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
ALTER TABLE tenants DROP COLUMN IF EXISTS username;
ALTER TABLE tenants DROP COLUMN IF EXISTS password_hash;
-- +goose StatementEnd
