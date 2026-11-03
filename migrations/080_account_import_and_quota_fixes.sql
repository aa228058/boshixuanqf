-- +goose Up
-- 空手机号代表尚未从 Session/Telegram 资料中取得号码，不应参与手机号唯一性约束。
-- 保留非空手机号在同一租户内唯一。
ALTER TABLE accounts DROP CONSTRAINT IF EXISTS accounts_tenant_id_phone_key;
CREATE UNIQUE INDEX IF NOT EXISTS idx_accounts_tenant_phone_nonempty
    ON accounts (tenant_id, phone)
    WHERE phone IS NOT NULL AND btrim(phone) <> '';

-- +goose Down
DROP INDEX IF EXISTS idx_accounts_tenant_phone_nonempty;
-- 不恢复旧 UNIQUE(tenant_id, phone)：已有多个空手机号时回滚会失败。
