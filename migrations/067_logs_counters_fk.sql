-- +goose Up
-- 067: verification_logs / plan_daily_counters 补 tenant FK(2026-10-22, 审计 P2-30)
-- pending_approvals 的同款问题已由 066 修复。这两张表 tenant_id 是裸列,
-- 租户删除后残留孤儿行。先清存量孤儿再加 FK(与 066 同模式)。
DELETE FROM verification_logs
WHERE tenant_id NOT IN (SELECT tenant_id FROM tenants);

DELETE FROM plan_daily_counters
WHERE tenant_id NOT IN (SELECT tenant_id FROM tenants);

ALTER TABLE verification_logs
    ADD CONSTRAINT fk_verification_logs_tenant
    FOREIGN KEY (tenant_id) REFERENCES tenants(tenant_id) ON DELETE CASCADE;

ALTER TABLE plan_daily_counters
    ADD CONSTRAINT fk_plan_daily_counters_tenant
    FOREIGN KEY (tenant_id) REFERENCES tenants(tenant_id) ON DELETE CASCADE;

-- +goose Down
ALTER TABLE verification_logs DROP CONSTRAINT IF EXISTS fk_verification_logs_tenant;
ALTER TABLE plan_daily_counters DROP CONSTRAINT IF EXISTS fk_plan_daily_counters_tenant;
