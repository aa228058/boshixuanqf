-- +goose Up
-- 账号能力体检结果(找群同款弹层的数据): 安全项实测 + 发送类项由风控记录推断。
ALTER TABLE accounts ADD COLUMN IF NOT EXISTS capabilities JSONB;

-- +goose Down
ALTER TABLE accounts DROP COLUMN IF EXISTS capabilities;
