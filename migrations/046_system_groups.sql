-- +goose Up
-- 平台级群组库(找群功能): 汇总所有租户采集到的私密/公开群链接、已加入的公开群、成员简介里挖到的群。
-- 平台共享(任何登录租户可见), 不含来源租户身份。
CREATE TABLE IF NOT EXISTS system_groups (
    sg_id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    dedup_key VARCHAR(255) NOT NULL UNIQUE,        -- 归并键: username 或 链接hash
    source VARCHAR(20) NOT NULL DEFAULT 'scraped', -- scraped=链接采集 | joined=已加入公开群 | bio=成员简介挖掘
    title VARCHAR(512),
    username VARCHAR(255),
    tme_link VARCHAR(255),
    gtype VARCHAR(20) NOT NULL DEFAULT 'unknown',  -- public | private | channel | unknown
    member_count INT NOT NULL DEFAULT 0,
    status VARCHAR(20) NOT NULL DEFAULT 'active',  -- active | joined | invalid
    ref_count INT NOT NULL DEFAULT 1,              -- 被多少个来源发现(热度)
    last_seen_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX IF NOT EXISTS idx_system_groups_type ON system_groups(gtype);
CREATE INDEX IF NOT EXISTS idx_system_groups_status ON system_groups(status);
CREATE INDEX IF NOT EXISTS idx_system_groups_title ON system_groups(title);

-- +goose Down
DROP TABLE IF EXISTS system_groups;
