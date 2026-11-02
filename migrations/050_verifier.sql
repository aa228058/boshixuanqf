-- +goose Up
-- 入群验证: AI配置(租户级, key加密存储) + 验证记录 + 租户开关
CREATE TABLE IF NOT EXISTS ai_configs (
    tenant_id UUID PRIMARY KEY REFERENCES tenants(tenant_id) ON DELETE CASCADE,
    provider VARCHAR(32) NOT NULL DEFAULT 'glm',   -- glm | qwen | custom
    api_key TEXT NOT NULL DEFAULT '',              -- AES-GCM 加密后存储
    model VARCHAR(64) NOT NULL DEFAULT '',
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS verification_logs (
    vl_id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id UUID NOT NULL,
    account_id UUID NOT NULL,
    group_chat_id BIGINT NOT NULL DEFAULT 0,
    group_title VARCHAR(512) NOT NULL DEFAULT '',
    status VARCHAR(20) NOT NULL DEFAULT 'pending', -- pending | pass | fail | stopped
    reason VARCHAR(512) NOT NULL DEFAULT '',
    steps JSONB NOT NULL DEFAULT '[]'::jsonb,      -- [{action, detail, at}]
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX IF NOT EXISTS idx_verification_logs_tenant ON verification_logs(tenant_id, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_verification_logs_account ON verification_logs(account_id, created_at DESC);

ALTER TABLE tenants ADD COLUMN IF NOT EXISTS verifier_enabled BOOLEAN NOT NULL DEFAULT FALSE;

-- +goose Down
DROP TABLE IF EXISTS verification_logs;
DROP TABLE IF EXISTS ai_configs;
ALTER TABLE tenants DROP COLUMN IF EXISTS verifier_enabled;
