-- +goose Up
-- 创建采集链接持久化表

CREATE TABLE IF NOT EXISTS scraped_links (
    link_id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id UUID NOT NULL REFERENCES tenants(tenant_id) ON DELETE CASCADE,
    account_id UUID NOT NULL REFERENCES accounts(account_id) ON DELETE CASCADE,
    link_type VARCHAR(20) NOT NULL CHECK (link_type IN ('private', 'public')),
    link_hash VARCHAR(255) NOT NULL,
    full_url TEXT NOT NULL,
    source_chat_id BIGINT,
    source_chat_title TEXT,
    resolved_chat_id BIGINT,
    resolved_title TEXT,
    member_count INT DEFAULT 0,
    status VARCHAR(20) NOT NULL DEFAULT 'new' CHECK (status IN ('new', 'resolved', 'joined', 'ignored')),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    resolved_at TIMESTAMPTZ,
    UNIQUE(tenant_id, link_hash)
);

CREATE INDEX IF NOT EXISTS idx_scraped_links_tenant ON scraped_links(tenant_id);
CREATE INDEX IF NOT EXISTS idx_scraped_links_status ON scraped_links(status);
CREATE INDEX IF NOT EXISTS idx_scraped_links_created ON scraped_links(created_at DESC);

-- +goose Down
DROP TABLE IF EXISTS scraped_links;
