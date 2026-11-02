-- +goose Up
-- R181.1: accounts.status 允许 'session_revoked'(会话被 TG 吊销/需重新登录)。
-- R157 引入该状态值时未放宽 CHECK 约束, "标记需重新登录"一直落库失败,
-- 死密钥账号在前端永远停留旧状态(通常是 online), 状态显示与实际严重不一致。
ALTER TABLE accounts DROP CONSTRAINT IF EXISTS accounts_status_check;
ALTER TABLE accounts ADD CONSTRAINT accounts_status_check
    CHECK (status IN ('online', 'offline', 'pending_auth', 'authenticated', 'restricted', 'banned', 'session_revoked'));

-- +goose Down
ALTER TABLE accounts DROP CONSTRAINT IF EXISTS accounts_status_check;
ALTER TABLE accounts ADD CONSTRAINT accounts_status_check
    CHECK (status IN ('online', 'offline', 'pending_auth', 'authenticated', 'restricted', 'banned'));
