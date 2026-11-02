-- +goose Up
-- +goose StatementBegin

-- Only run if the old junction table structure still exists (no name column)
DO $$
BEGIN
    -- Check if account_groups table exists but doesn't have 'name' column
    IF EXISTS (
        SELECT 1 FROM information_schema.tables 
        WHERE table_name = 'account_groups'
    ) AND NOT EXISTS (
        SELECT 1 FROM information_schema.columns 
        WHERE table_name = 'account_groups' AND column_name = 'name'
    ) THEN
        -- Drop old junction table
        DROP TABLE account_groups CASCADE;
        
        -- Create new account_groups management table
        CREATE TABLE account_groups (
            group_id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
            tenant_id UUID NOT NULL REFERENCES tenants(tenant_id) ON DELETE CASCADE,
            name VARCHAR(255) NOT NULL,
            color VARCHAR(50) NOT NULL DEFAULT '#0071e3',
            account_ids UUID[] NOT NULL DEFAULT '{}',
            created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
        );
        
        CREATE INDEX idx_account_groups_tenant ON account_groups(tenant_id);
    END IF;
END $$;

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
-- No safe down migration for this fix
-- +goose StatementEnd
