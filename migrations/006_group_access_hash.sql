-- +goose Up
-- +goose StatementBegin
ALTER TABLE groups ADD COLUMN IF NOT EXISTS access_hash BIGINT NOT NULL DEFAULT 0;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
ALTER TABLE groups DROP COLUMN IF EXISTS access_hash;
-- +goose StatementEnd
