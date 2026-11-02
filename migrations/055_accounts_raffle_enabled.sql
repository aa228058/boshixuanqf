-- +goose Up
-- 抽奖助手支持按账号开关(改进#1): 默认全部账号参与, 可在账号详情/设置里逐账号关闭
ALTER TABLE accounts ADD COLUMN IF NOT EXISTS raffle_enabled BOOLEAN NOT NULL DEFAULT true;

-- +goose Down
ALTER TABLE accounts DROP COLUMN IF EXISTS raffle_enabled;
