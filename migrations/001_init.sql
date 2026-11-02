-- +goose Up
-- +goose StatementBegin

-- Tenants
CREATE TABLE IF NOT EXISTS tenants (
    tenant_id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name VARCHAR(255) NOT NULL,
    api_key_hash VARCHAR(255) NOT NULL,
    api_key_prefix VARCHAR(16),
    max_accounts INT NOT NULL DEFAULT 20,
    max_concurrent_tasks INT NOT NULL DEFAULT 5,
    status VARCHAR(20) NOT NULL DEFAULT 'active' CHECK (status IN ('active', 'suspended', 'deleted')),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- Accounts
CREATE TABLE IF NOT EXISTS accounts (
    account_id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id UUID NOT NULL REFERENCES tenants(tenant_id) ON DELETE CASCADE,
    phone VARCHAR(32) NOT NULL,
    session_name VARCHAR(255) NOT NULL,
    status VARCHAR(20) NOT NULL DEFAULT 'offline' CHECK (status IN ('online', 'offline', 'pending_auth', 'authenticated', 'restricted', 'banned')),
    level VARCHAR(10) NOT NULL DEFAULT 'normal' CHECK (level IN ('new', 'normal', 'old')),
    health_score INT NOT NULL DEFAULT 100 CHECK (health_score BETWEEN 0 AND 100),
    device_profile JSONB NOT NULL DEFAULT '{}',
    last_online TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE(tenant_id, phone)
);

-- Tasks
CREATE TABLE IF NOT EXISTS tasks (
    task_id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id UUID NOT NULL REFERENCES tenants(tenant_id) ON DELETE CASCADE,
    type VARCHAR(32) NOT NULL CHECK (type IN ('broadcast', 'scrape_members', 'scrape_links', 'join_groups', 'monitor_forward', 'monitor_reply')),
    status VARCHAR(20) NOT NULL DEFAULT 'pending' CHECK (status IN ('pending', 'queued', 'running', 'paused', 'completed', 'failed', 'cancelled')),
    priority INT NOT NULL DEFAULT 5 CHECK (priority BETWEEN 1 AND 10),
    account_ids JSONB NOT NULL DEFAULT '[]',
    target_ids JSONB NOT NULL DEFAULT '[]',
    payload JSONB NOT NULL DEFAULT '{}',
    progress JSONB NOT NULL DEFAULT '{"total":0,"done":0,"failed":0}'::jsonb,
    locked_at TIMESTAMPTZ,
    worker_id VARCHAR(64),
    retry_count INT NOT NULL DEFAULT 0,
    scheduled_at TIMESTAMPTZ,
    started_at TIMESTAMPTZ,
    completed_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- Task Logs
CREATE TABLE IF NOT EXISTS task_logs (
    log_id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    task_id UUID NOT NULL REFERENCES tasks(task_id) ON DELETE CASCADE,
    tenant_id UUID NOT NULL REFERENCES tenants(tenant_id) ON DELETE CASCADE,
    account_id UUID REFERENCES accounts(account_id) ON DELETE SET NULL,
    target_id VARCHAR(255),
    action VARCHAR(64) NOT NULL,
    status VARCHAR(20) NOT NULL CHECK (status IN ('success', 'failed', 'skipped')),
    tg_response TEXT,
    error_code VARCHAR(64),
    executed_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- Groups
CREATE TABLE IF NOT EXISTS groups (
    group_id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id UUID NOT NULL REFERENCES tenants(tenant_id) ON DELETE CASCADE,
    tg_chat_id BIGINT NOT NULL,
    title VARCHAR(512),
    username VARCHAR(255),
    type VARCHAR(20) NOT NULL DEFAULT 'group' CHECK (type IN ('group', 'channel', 'supergroup')),
    member_count INT NOT NULL DEFAULT 0,
    is_joined BOOLEAN NOT NULL DEFAULT FALSE,
    joined_at TIMESTAMPTZ,
    last_scraped_at TIMESTAMPTZ,
    metadata JSONB NOT NULL DEFAULT '{}',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE(tenant_id, tg_chat_id)
);

-- Members
CREATE TABLE IF NOT EXISTS members (
    member_id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id UUID NOT NULL,
    group_id UUID NOT NULL REFERENCES groups(group_id) ON DELETE CASCADE,
    tg_user_id BIGINT NOT NULL,
    username VARCHAR(255),
    first_name VARCHAR(255),
    last_name VARCHAR(255),
    phone VARCHAR(32),
    bio TEXT,
    last_seen TIMESTAMPTZ,
    scraped_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE(tenant_id, group_id, tg_user_id)
);

-- Monitor Rules
CREATE TABLE IF NOT EXISTS monitor_rules (
    rule_id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id UUID NOT NULL REFERENCES tenants(tenant_id) ON DELETE CASCADE,
    name VARCHAR(255) NOT NULL,
    type VARCHAR(20) NOT NULL CHECK (type IN ('forward', 'reply')),
    source_chat_ids JSONB NOT NULL DEFAULT '[]',
    target_chat_ids JSONB NOT NULL DEFAULT '[]',
    match_type VARCHAR(20) NOT NULL CHECK (match_type IN ('contains', 'regex', 'exact', 'from_user')),
    keywords JSONB NOT NULL DEFAULT '[]',
    is_active BOOLEAN NOT NULL DEFAULT TRUE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- Account Quotas
CREATE TABLE IF NOT EXISTS account_quotas (
    account_id UUID NOT NULL REFERENCES accounts(account_id) ON DELETE CASCADE,
    quota_type VARCHAR(32) NOT NULL CHECK (quota_type IN ('broadcast', 'join', 'scrape')),
    daily_limit INT NOT NULL DEFAULT 0,
    used_today INT NOT NULL DEFAULT 0,
    over_limit_count INT NOT NULL DEFAULT 0,
    reset_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (account_id, quota_type)
);

-- Risk Events
CREATE TABLE IF NOT EXISTS risk_events (
    event_id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id UUID NOT NULL REFERENCES tenants(tenant_id) ON DELETE CASCADE,
    account_id UUID NOT NULL REFERENCES accounts(account_id) ON DELETE CASCADE,
    event_type VARCHAR(32) NOT NULL CHECK (event_type IN ('flood_wait', 'phone_banned', 'restriction', 'rate_limit', 'peer_flood', 'account_banned', 'session_revoked', 'broadcast_flood_wait', 'broadcast_fail', 'account_restricted', 'session_restore_failed')),
    severity VARCHAR(10) NOT NULL CHECK (severity IN ('low', 'medium', 'high', 'critical')),
    details JSONB NOT NULL DEFAULT '{}',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- Note: batch_tasks and pending_approvals tables are created in
-- separate migration files (003_batch_tasks.sql and 004_pending_approvals.sql)
-- to avoid schema conflicts.

-- Audit Logs
CREATE TABLE IF NOT EXISTS audit_logs (
    log_id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id UUID NOT NULL REFERENCES tenants(tenant_id) ON DELETE CASCADE,
    user_id VARCHAR(255) NOT NULL,
    action VARCHAR(64) NOT NULL,
    target VARCHAR(512),
    ip INET,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- Indexes
CREATE INDEX IF NOT EXISTS idx_accounts_tenant_status ON accounts(tenant_id, status);
CREATE INDEX IF NOT EXISTS idx_tasks_status_priority ON tasks(tenant_id, status, priority DESC, created_at ASC);
CREATE INDEX IF NOT EXISTS idx_tasks_locked_at ON tasks(locked_at);
CREATE INDEX IF NOT EXISTS idx_task_logs_task_time ON task_logs(task_id, executed_at DESC);
CREATE INDEX IF NOT EXISTS idx_members_tenant_group ON members(tenant_id, group_id);
CREATE INDEX IF NOT EXISTS idx_members_username ON members(username);
CREATE INDEX IF NOT EXISTS idx_risk_events_account ON risk_events(tenant_id, account_id, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_groups_tenant ON groups(tenant_id);
CREATE INDEX IF NOT EXISTS idx_monitor_rules_tenant ON monitor_rules(tenant_id);
CREATE INDEX IF NOT EXISTS idx_audit_logs_tenant ON audit_logs(tenant_id, created_at DESC);

-- Missing indexes for P0 performance fixes
CREATE INDEX IF NOT EXISTS idx_account_quotas_reset_at ON account_quotas(reset_at);
CREATE INDEX IF NOT EXISTS idx_risk_events_tenant_type ON risk_events(tenant_id, event_type, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_task_logs_tenant_time ON task_logs(tenant_id, executed_at DESC);
CREATE INDEX IF NOT EXISTS idx_tasks_tenant_status ON tasks(tenant_id, status);
CREATE INDEX IF NOT EXISTS idx_tasks_tenant_created ON tasks(tenant_id, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_tenants_api_key_prefix ON tenants(api_key_prefix) WHERE status = 'active';

-- Function to notify on new task
CREATE OR REPLACE FUNCTION notify_new_task()
RETURNS TRIGGER AS $$
BEGIN
    PERFORM pg_notify('new_task', json_build_object('task_id', NEW.task_id, 'tenant_id', NEW.tenant_id)::text);
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

-- Trigger for task notifications
DROP TRIGGER IF EXISTS trg_notify_new_task ON tasks;
CREATE TRIGGER trg_notify_new_task
    AFTER INSERT ON tasks
    FOR EACH ROW
    WHEN (NEW.status = 'queued')
    EXECUTE FUNCTION notify_new_task();

-- Trigger for task status change to queued
DROP TRIGGER IF EXISTS trg_notify_task_queued ON tasks;
CREATE TRIGGER trg_notify_task_queued
    AFTER UPDATE OF status ON tasks
    FOR EACH ROW
    WHEN (OLD.status != 'queued' AND NEW.status = 'queued')
    EXECUTE FUNCTION notify_new_task();

-- [SECURITY] 默认租户已移除（2026-09-18 专项深查 P0-1）：
-- 原 INSERT 携带 bcrypt 官方文档公开的 "password" 哈希，ON CONFLICT DO NOTHING
-- 使每次全新部署都出现一个已知凭证的活跃租户（20 账号/5 并发）。
-- 已部署数据库需手动清理：DELETE FROM tenants WHERE tenant_id='00000000-0000-0000-0000-000000000001';
-- 新部署请通过注册接口创建租户。

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TRIGGER IF EXISTS trg_notify_task_queued ON tasks;
DROP TRIGGER IF EXISTS trg_notify_new_task ON tasks;
DROP FUNCTION IF EXISTS notify_new_task();
DROP TABLE IF EXISTS audit_logs;
DROP TABLE IF EXISTS pending_approvals;
DROP TABLE IF EXISTS batch_tasks;
DROP TABLE IF EXISTS risk_events;
DROP TABLE IF EXISTS account_quotas;
DROP TABLE IF EXISTS monitor_rules;
DROP TABLE IF EXISTS members;
DROP TABLE IF EXISTS groups;
DROP TABLE IF EXISTS task_logs;
DROP TABLE IF EXISTS tasks;
DROP TABLE IF EXISTS accounts;
DROP TABLE IF EXISTS tenants;
-- +goose StatementEnd
