-- +goose Up
-- 审计整改: 索引/孤儿清理/外键( NOT VALID 避免历史数据阻塞 )

-- P1-08: task account_ids JSONB 查询索引
CREATE INDEX IF NOT EXISTS idx_tasks_account_ids_gin ON tasks USING GIN (account_ids);
CREATE INDEX IF NOT EXISTS idx_tasks_account_created ON tasks (tenant_id, account_ids, created_at DESC);

-- P1-09: squad_members 孤儿清理 + 外键
DELETE FROM squad_members WHERE account_id NOT IN (SELECT account_id FROM accounts);
ALTER TABLE squad_members ADD CONSTRAINT fk_squad_members_account
    FOREIGN KEY (account_id) REFERENCES accounts(account_id) NOT VALID;

-- P1-10: campaign 表 tenant 外键(应用层已校验, DB 兜底)
ALTER TABLE campaigns ADD CONSTRAINT fk_campaigns_tenant
    FOREIGN KEY (tenant_id) REFERENCES tenants(tenant_id) NOT VALID;
ALTER TABLE campaign_accounts ADD CONSTRAINT fk_campaign_accounts_account
    FOREIGN KEY (account_id) REFERENCES accounts(account_id) NOT VALID;
DELETE FROM campaign_accounts WHERE account_id NOT IN (SELECT account_id FROM accounts);
