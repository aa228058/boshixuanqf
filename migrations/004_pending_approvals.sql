-- +goose Up
CREATE TABLE IF NOT EXISTS pending_approvals (
    id SERIAL PRIMARY KEY,
    task_id UUID NOT NULL REFERENCES tasks(task_id) ON DELETE CASCADE,
    tenant_id UUID NOT NULL,
    account_id UUID NOT NULL,
    chat_id BIGINT DEFAULT 0,
    access_hash BIGINT DEFAULT 0,
    target_link TEXT NOT NULL,
    status VARCHAR(20) DEFAULT 'pending',
    check_count INT DEFAULT 0,
    max_checks INT DEFAULT 12,
    created_at TIMESTAMPTZ DEFAULT NOW(),
    checked_at TIMESTAMPTZ,
    UNIQUE(task_id)
);

CREATE INDEX IF NOT EXISTS idx_pending_approvals_status ON pending_approvals(status);
CREATE INDEX IF NOT EXISTS idx_pending_approvals_tenant ON pending_approvals(tenant_id);

-- +goose Down
DROP TABLE IF EXISTS pending_approvals;
