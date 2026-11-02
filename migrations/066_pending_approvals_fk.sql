-- +goose Up
-- 066: pending_approvals.tenant_id 补外键(2026-10-22, 审计 V25)
-- 004 只给 task_id 加了 CASCADE, tenant_id 是裸列——租户删除后审批行成为孤儿,
-- 永远不被查询也不被清理。先清存量孤儿再加 FK。
DELETE FROM pending_approvals
WHERE tenant_id NOT IN (SELECT tenant_id FROM tenants);

ALTER TABLE pending_approvals
    ADD CONSTRAINT fk_pending_approvals_tenant
    FOREIGN KEY (tenant_id) REFERENCES tenants(tenant_id) ON DELETE CASCADE;

-- +goose Down
ALTER TABLE pending_approvals DROP CONSTRAINT IF EXISTS fk_pending_approvals_tenant;
