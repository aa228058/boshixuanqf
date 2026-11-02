-- +goose Up
-- +goose StatementBegin

-- Drop old junction table if exists (from 009 migration)
DROP TABLE IF EXISTS account_groups CASCADE;

-- Create new account_groups table for account group management
CREATE TABLE IF NOT EXISTS account_groups (
    group_id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id UUID NOT NULL REFERENCES tenants(tenant_id) ON DELETE CASCADE,
    name VARCHAR(255) NOT NULL,
    color VARCHAR(50) NOT NULL DEFAULT '#0071e3',
    account_ids UUID[] NOT NULL DEFAULT '{}',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_account_groups_tenant ON account_groups(tenant_id);

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP INDEX IF EXISTS idx_account_groups_tenant;
DROP TABLE IF EXISTS account_groups;
-- +goose StatementEnd
