-- +goose Up
-- +goose StatementBegin

-- Step 1: Create account_groups junction table
CREATE TABLE IF NOT EXISTS account_groups (
    account_id UUID NOT NULL REFERENCES accounts(account_id) ON DELETE CASCADE,
    group_id UUID NOT NULL REFERENCES groups(group_id) ON DELETE CASCADE,
    is_joined BOOLEAN NOT NULL DEFAULT TRUE,
    joined_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (account_id, group_id)
);

-- Step 2: Add index for fast lookups
CREATE INDEX IF NOT EXISTS idx_account_groups_account ON account_groups(account_id);
CREATE INDEX IF NOT EXISTS idx_account_groups_group ON account_groups(group_id);

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP INDEX IF EXISTS idx_account_groups_group;
DROP INDEX IF EXISTS idx_account_groups_account;
DROP TABLE IF EXISTS account_groups;
-- +goose StatementEnd
