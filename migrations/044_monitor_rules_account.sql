-- +goose Up
-- +goose StatementBegin
-- 044: 监控规则增加账号归属列，实现规则隔离
-- 背景：原表只有 tenant_id，A 号创建的规则会被 B 号触发（用户实测）。
-- account_id 为 NULL 表示"全部账号"（老数据默认行为，向后兼容）。
ALTER TABLE monitor_rules ADD COLUMN IF NOT EXISTS account_id UUID;
CREATE INDEX IF NOT EXISTS idx_monitor_rules_account ON monitor_rules(tenant_id, account_id);
-- +goose StatementEnd
