-- +goose Up
-- +goose StatementBegin

-- Plans table
CREATE TABLE IF NOT EXISTS plans (
    plan_id VARCHAR(20) PRIMARY KEY,
    name VARCHAR(50) NOT NULL,
    price_monthly INT NOT NULL DEFAULT 0,
    max_accounts INT NOT NULL DEFAULT 1,
    max_concurrent INT NOT NULL DEFAULT 1,
    description TEXT,
    features JSONB NOT NULL DEFAULT '[]',
    is_active BOOLEAN NOT NULL DEFAULT TRUE,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- Insert default plans
INSERT INTO plans (plan_id, name, price_monthly, max_accounts, max_concurrent, description, features)
VALUES
    ('free', '免费版', 0, 1, 1, '体验基础功能', '["基础广播", "基础加群", "基础采集", "监控规则"]'),
    ('basic', '基础版', 1500, 3, 2, '适合个人用户', '["基础广播", "基础加群", "基础采集", "监控规则", "数据导出"]'),
    ('pro', '专业版', 2500, 10, 5, '适合小团队', '["高级广播", "批量加群", "高级采集", "监控规则", "数据导出", "批量操作", "消息模板"]'),
    ('enterprise', '企业版', 5000, 30, 10, '适合企业用户', '["全功能", "API接入", "优先支持"]')
ON CONFLICT (plan_id) DO NOTHING;

-- Admins table
CREATE TABLE IF NOT EXISTS admins (
    admin_id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    username VARCHAR(50) UNIQUE NOT NULL,
    password_hash VARCHAR(255) NOT NULL,
    role VARCHAR(20) NOT NULL DEFAULT 'admin' CHECK (role IN ('admin', 'super')),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- [SECURITY] 默认管理员已移除（2026-09-18 专项深查 P0-2）：
-- 原 INSERT 携带公开已知的 bcrypt 哈希，且 container.seedData 仅在设置
-- ADMIN_PASSWORD 时才覆盖——未设 env 的部署等于 admin/已知密码 裸奔。
-- 管理员由 seedData 全权创建（首启打印一次性随机密码）。
-- 已部署数据库需手动清理：DELETE FROM admins WHERE username='admin' AND password_hash LIKE '$2a$10$N9qo8u%';

-- Activation codes table
CREATE TABLE IF NOT EXISTS activation_codes (
    code_id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    code VARCHAR(32) UNIQUE NOT NULL,
    plan_id VARCHAR(20) NOT NULL REFERENCES plans(plan_id),
    status VARCHAR(20) NOT NULL DEFAULT 'unused' CHECK (status IN ('unused', 'used', 'expired', 'revoked')),
    used_by UUID REFERENCES tenants(tenant_id),
    used_at TIMESTAMPTZ,
    expires_at TIMESTAMPTZ,
    created_by UUID REFERENCES admins(admin_id),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    notes TEXT
);

-- Support config table (single row)
CREATE TABLE IF NOT EXISTS support_config (
    id SERIAL PRIMARY KEY,
    contact_type VARCHAR(20) NOT NULL DEFAULT 'wechat' CHECK (contact_type IN ('wechat', 'telegram', 'qq', 'email', 'custom')),
    contact_value TEXT NOT NULL DEFAULT '',
    display_name VARCHAR(50) NOT NULL DEFAULT '联系客服',
    qr_code_url TEXT,
    is_active BOOLEAN NOT NULL DEFAULT TRUE,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- Insert default support config
INSERT INTO support_config (contact_type, contact_value, display_name)
VALUES ('wechat', '请配置客服微信号', '联系客服获取卡密')
ON CONFLICT DO NOTHING;

-- Announcements table
CREATE TABLE IF NOT EXISTS announcements (
    announcement_id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    title VARCHAR(255) NOT NULL,
    content TEXT NOT NULL,
    priority VARCHAR(20) NOT NULL DEFAULT 'normal' CHECK (priority IN ('normal', 'urgent')),
    target_type VARCHAR(20) NOT NULL DEFAULT 'all' CHECK (target_type IN ('all', 'specific_plan')),
    target_plans JSONB NOT NULL DEFAULT '[]',
    start_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    end_at TIMESTAMPTZ,
    is_active BOOLEAN NOT NULL DEFAULT TRUE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- Tenant subscriptions tracking
CREATE TABLE IF NOT EXISTS tenant_subscriptions (
    subscription_id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id UUID NOT NULL REFERENCES tenants(tenant_id) ON DELETE CASCADE,
    plan_id VARCHAR(20) NOT NULL REFERENCES plans(plan_id),
    started_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    expires_at TIMESTAMPTZ,
    status VARCHAR(20) NOT NULL DEFAULT 'active' CHECK (status IN ('active', 'expired', 'cancelled')),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- Extend tenants table
ALTER TABLE tenants
    ADD COLUMN IF NOT EXISTS plan_id VARCHAR(20) DEFAULT 'free',
    ADD COLUMN IF NOT EXISTS expires_at TIMESTAMPTZ,
    ADD COLUMN IF NOT EXISTS deleted_at TIMESTAMPTZ;

-- Add foreign key constraint
DO $$ BEGIN
    ALTER TABLE tenants ADD CONSTRAINT fk_tenants_plan_id FOREIGN KEY (plan_id) REFERENCES plans(plan_id);
EXCEPTION WHEN duplicate_object THEN NULL;
END $$;

-- Extend accounts table
ALTER TABLE accounts
    ADD COLUMN IF NOT EXISTS remark VARCHAR(255),
    ADD COLUMN IF NOT EXISTS avatar_url TEXT;

-- NOTE: 曾在此创建 tenant 账号分组表 account_groups, 与 009 的账号-群聊关联表同名冲突,
-- 且该分组表无任何代码使用, 已移除。账号-群聊关联表由 009_account_groups.sql 创建。

-- Add message_templates table
CREATE TABLE IF NOT EXISTS message_templates (
    template_id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id UUID NOT NULL REFERENCES tenants(tenant_id) ON DELETE CASCADE,
    name VARCHAR(100) NOT NULL,
    content TEXT NOT NULL,
    variables JSONB NOT NULL DEFAULT '[]',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- Indexes
CREATE INDEX IF NOT EXISTS idx_activation_codes_status ON activation_codes(status);
CREATE INDEX IF NOT EXISTS idx_activation_codes_plan ON activation_codes(plan_id);
CREATE INDEX IF NOT EXISTS idx_announcements_active ON announcements(is_active, start_at, end_at);
CREATE INDEX IF NOT EXISTS idx_tenant_subscriptions_tenant ON tenant_subscriptions(tenant_id, status);
-- NOTE: idx_account_groups_tenant 不在此创建——account_groups 表由 009 才建立(时序冲突,
-- 全新部署会在本句报错 42P01)。该索引由 010_fix_account_groups.sql 在正确的时序创建。
CREATE INDEX IF NOT EXISTS idx_message_templates_tenant ON message_templates(tenant_id);

-- Update default tenant to free plan
UPDATE tenants SET plan_id = 'free' WHERE plan_id IS NULL;

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE IF EXISTS message_templates;
DROP TABLE IF EXISTS account_groups;
DROP TABLE IF EXISTS tenant_subscriptions;
DROP TABLE IF EXISTS announcements;
DROP TABLE IF EXISTS support_config;
DROP TABLE IF EXISTS activation_codes;
DROP TABLE IF EXISTS admins;
DROP TABLE IF EXISTS plans;
-- +goose StatementEnd
