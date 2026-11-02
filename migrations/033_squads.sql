-- +goose Up
-- 协议号系统: 编队(账号小队)表
-- 设计修正: account_groups 是 TG 群聊的分组(账号↔群), 不是账号编队,
-- 所以编队用独立两张表, 一个编队 = 一组同角色协议号。
CREATE TABLE squads (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id   UUID NOT NULL,
    name        VARCHAR(100) NOT NULL,
    role        VARCHAR(20) NOT NULL DEFAULT '',
                -- '' 未分配 | adder 拉人 | broadcaster 群发 | support 客服 | warmup 养号
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX idx_squads_tenant ON squads(tenant_id, role);

CREATE TABLE squad_members (
    squad_id    UUID NOT NULL REFERENCES squads(id) ON DELETE CASCADE,
    account_id  UUID NOT NULL,
    added_at    TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (squad_id, account_id)
);
CREATE INDEX idx_squad_members_account ON squad_members(account_id);
