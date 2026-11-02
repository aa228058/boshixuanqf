-- +goose Up
-- 文案变量(数据字典): 群发内容里写 {词典名}, 发送时随机替换为词条, 让每条消息不一样。
CREATE TABLE IF NOT EXISTS text_dicts (
    dict_id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id UUID NOT NULL REFERENCES tenants(tenant_id) ON DELETE CASCADE,
    name VARCHAR(64) NOT NULL,             -- 变量名, 群发里写 {name}
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (tenant_id, name)
);
CREATE TABLE IF NOT EXISTS text_dict_entries (
    entry_id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    dict_id UUID NOT NULL REFERENCES text_dicts(dict_id) ON DELETE CASCADE,
    word VARCHAR(512) NOT NULL,
    weight INT NOT NULL DEFAULT 1
);
CREATE INDEX IF NOT EXISTS idx_text_dict_entries_dict ON text_dict_entries(dict_id);

-- +goose Down
DROP TABLE IF EXISTS text_dict_entries;
DROP TABLE IF EXISTS text_dicts;
