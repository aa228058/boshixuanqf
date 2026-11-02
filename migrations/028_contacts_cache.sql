-- +goose Up
-- 私聊联系人缓存: 同步时保存, 群发反查昵称 + 选择器秒开
CREATE TABLE IF NOT EXISTS contacts (
    contact_id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id UUID NOT NULL REFERENCES tenants(tenant_id) ON DELETE CASCADE,
    account_id UUID NOT NULL REFERENCES accounts(account_id) ON DELETE CASCADE,
    tg_user_id BIGINT NOT NULL,
    username VARCHAR(255) NOT NULL DEFAULT '',
    first_name VARCHAR(255) NOT NULL DEFAULT '',
    last_name VARCHAR(255) NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE(account_id, tg_user_id)
);
CREATE INDEX IF NOT EXISTS idx_contacts_account ON contacts(account_id);

-- +goose Down
DROP TABLE IF EXISTS contacts;
