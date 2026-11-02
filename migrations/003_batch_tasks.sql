-- +goose Up
-- +goose StatementBegin

-- batch_tasks table for auto-split large broadcasts
CREATE TABLE IF NOT EXISTS batch_tasks (
    batch_id    UUID PRIMARY KEY,
    tenant_id   UUID NOT NULL REFERENCES tenants(tenant_id) ON DELETE CASCADE,
    message     TEXT NOT NULL,
    total_msgs  INT NOT NULL DEFAULT 0,
    sub_tasks   UUID[] NOT NULL DEFAULT '{}',
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_batch_tasks_tenant ON batch_tasks(tenant_id);

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE IF EXISTS batch_tasks;
-- +goose StatementEnd
