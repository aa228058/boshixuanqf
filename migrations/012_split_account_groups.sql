-- +goose Up
-- +goose StatementBegin

-- Step 1: Backup existing account_groups data if it's the management table (has 'name' column)
DO $$
DECLARE
    has_name_col BOOLEAN;
BEGIN
    SELECT EXISTS (
        SELECT 1 FROM information_schema.columns 
        WHERE table_name = 'account_groups' AND column_name = 'name'
    ) INTO has_name_col;

    IF has_name_col THEN
        -- It's the management table, backup data to temp table
        CREATE TABLE IF NOT EXISTS _backup_account_groups AS 
        SELECT group_id, tenant_id, name, color, account_ids, created_at 
        FROM account_groups;
        
        -- Drop the management table
        DROP TABLE account_groups CASCADE;
    END IF;
END $$;

-- Step 2: Create junction table (original structure from 009)
CREATE TABLE IF NOT EXISTS account_groups (
    account_id UUID NOT NULL REFERENCES accounts(account_id) ON DELETE CASCADE,
    group_id UUID NOT NULL REFERENCES groups(group_id) ON DELETE CASCADE,
    is_joined BOOLEAN NOT NULL DEFAULT TRUE,
    joined_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (account_id, group_id)
);

CREATE INDEX IF NOT EXISTS idx_account_groups_account ON account_groups(account_id);
CREATE INDEX IF NOT EXISTS idx_account_groups_group ON account_groups(group_id);

-- Step 3: Create new management table with different name
CREATE TABLE IF NOT EXISTS account_group_categories (
    category_id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id UUID NOT NULL REFERENCES tenants(tenant_id) ON DELETE CASCADE,
    name VARCHAR(255) NOT NULL,
    color VARCHAR(50) NOT NULL DEFAULT '#0071e3',
    account_ids UUID[] NOT NULL DEFAULT '{}',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_account_group_categories_tenant ON account_group_categories(tenant_id);

-- Step 4: Restore backed up data to new management table
DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM information_schema.tables WHERE table_name = '_backup_account_groups') THEN
        INSERT INTO account_group_categories (category_id, tenant_id, name, color, account_ids, created_at)
        SELECT group_id, tenant_id, name, color, account_ids, created_at 
        FROM _backup_account_groups;
        DROP TABLE _backup_account_groups;
    END IF;
END $$;

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
-- No safe down migration
-- +goose StatementEnd
