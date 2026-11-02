-- +goose Up
-- 抽奖结果追踪: 参与回执 + 开奖解析(text_mention 实体提取中奖名单)
CREATE TABLE IF NOT EXISTS raffle_results (
    id           BIGSERIAL PRIMARY KEY,
    tenant_id    UUID NOT NULL REFERENCES tenants(tenant_id) ON DELETE CASCADE,
    account_id   UUID NOT NULL REFERENCES accounts(account_id) ON DELETE CASCADE,
    tg_chat_id   BIGINT NOT NULL,
    kind         VARCHAR(20) NOT NULL,          -- joined(参与成功) / winner(中奖) / mentioned(开奖提及未中)
    keyword      TEXT,                          -- 活动关键词
    prize        TEXT,                          -- 奖品文本(中奖时)
    detail       TEXT,                          -- 原始消息摘要
    created_at   TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_raffle_results_tenant ON raffle_results(tenant_id, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_raffle_results_account ON raffle_results(account_id, created_at DESC);

-- +goose Down
DROP TABLE IF EXISTS raffle_results;
