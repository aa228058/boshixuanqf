-- +goose Up
-- 创建 token 黑名单表

CREATE TABLE IF NOT EXISTS token_blacklist (
    jti VARCHAR(255) PRIMARY KEY,
    tenant_id UUID NOT NULL,
    expires_at TIMESTAMPTZ NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_token_blacklist_tenant ON token_blacklist(tenant_id);
CREATE INDEX IF NOT EXISTS idx_token_blacklist_expires ON token_blacklist(expires_at);

-- +goose Down
DROP TABLE IF EXISTS token_blacklist;
