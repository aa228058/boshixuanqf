-- +goose Up
-- 协议号系统 P2: 协同战役三张表
CREATE TABLE campaigns (
    id            UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id     UUID NOT NULL,
    name          VARCHAR(200) NOT NULL,
    status        VARCHAR(20) NOT NULL DEFAULT 'queued',
                  -- queued/running/paused/completed/cancelled/failed
    objective     JSONB NOT NULL,
                  -- {kind:'join'|'broadcast'|'pipeline',
                  --  join_targets:[...], message:{text,media_path},
                  --  send_delay:[5,12]}
    total_count   INT NOT NULL DEFAULT 0,
    done_count    INT NOT NULL DEFAULT 0,
    failed_count  INT NOT NULL DEFAULT 0,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    started_at    TIMESTAMPTZ, finished_at TIMESTAMPTZ
);
CREATE INDEX idx_campaigns_tenant ON campaigns(tenant_id, status, created_at DESC);

CREATE TABLE campaign_accounts (
    campaign_id UUID NOT NULL REFERENCES campaigns(id) ON DELETE CASCADE,
    account_id  UUID NOT NULL,
    role        VARCHAR(20) NOT NULL,        -- adder/broadcaster
    status      VARCHAR(20) NOT NULL DEFAULT 'active',
                -- active/flood/done/replaced/dead
    assigned    INT NOT NULL DEFAULT 0,
    done        INT NOT NULL DEFAULT 0,
    failed      INT NOT NULL DEFAULT 0,
    replaced_by UUID,
    PRIMARY KEY (campaign_id, account_id)
);

CREATE TABLE campaign_targets (
    id          BIGSERIAL PRIMARY KEY,
    campaign_id UUID NOT NULL REFERENCES campaigns(id) ON DELETE CASCADE,
    target      TEXT NOT NULL,               -- 群链接/@名/群ID/用户名
    kind        VARCHAR(10) NOT NULL,        -- join / send
    status      VARCHAR(10) NOT NULL DEFAULT 'pending',
                -- pending/claimed/done/failed
    account_id  UUID,
    attempts    INT NOT NULL DEFAULT 0,
    UNIQUE (campaign_id, target, kind)
);
CREATE INDEX idx_ct_pick ON campaign_targets(campaign_id, kind, status) WHERE status = 'pending';
