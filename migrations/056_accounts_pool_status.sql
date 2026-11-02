-- +goose Up
-- 账号备用池(用户定稿版): standby=备用(不占套餐账号额度, 停业务) / active=启用(占额度, 正常干活)
-- 死号两条出路: ①重新连接(存session直接拉回, 不用验证码, 仍占额度)
--              ②备用号转正(启用后占额度, 接手工作)
ALTER TABLE accounts ADD COLUMN IF NOT EXISTS pool_status VARCHAR(20) NOT NULL DEFAULT 'active';

CREATE INDEX IF NOT EXISTS idx_accounts_pool ON accounts(tenant_id, pool_status);

-- +goose Down
DROP INDEX IF EXISTS idx_accounts_pool;
ALTER TABLE accounts DROP COLUMN IF EXISTS pool_status;
