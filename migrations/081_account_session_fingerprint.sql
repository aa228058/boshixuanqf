-- +goose Up
-- +goose StatementBegin

-- Store only a digest of imported Telegram auth keys, never the key itself.
ALTER TABLE accounts ADD COLUMN IF NOT EXISTS session_fingerprint VARCHAR(64);
CREATE UNIQUE INDEX IF NOT EXISTS idx_accounts_tenant_session_fingerprint
    ON accounts (tenant_id, session_fingerprint)
    WHERE session_fingerprint IS NOT NULL AND session_fingerprint <> '';

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin

DROP INDEX IF EXISTS idx_accounts_tenant_session_fingerprint;
ALTER TABLE accounts DROP COLUMN IF EXISTS session_fingerprint;

-- +goose StatementEnd
