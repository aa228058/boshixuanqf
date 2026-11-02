-- +goose Up
-- 战役目标扩展: 群peer缓存(加群成功后存, 发送阶段直接发进群) + 采集结果
ALTER TABLE campaign_targets ADD COLUMN IF NOT EXISTS peer JSONB;
ALTER TABLE campaign_targets ADD COLUMN IF NOT EXISTS result JSONB;
