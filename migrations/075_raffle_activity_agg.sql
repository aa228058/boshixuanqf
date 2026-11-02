-- +goose Up
-- R121: 抽奖活动聚合 —— 同一活动(账号+群+关键词)只保留一行, 状态就地流转。
-- activity_key 唯一约束保证 joined/winner/mention 不会重复插行。
ALTER TABLE raffle_results ADD COLUMN IF NOT EXISTS activity_key TEXT;
-- 历史数据回填: 账号ID:群ID:关键词 (回填后才有唯一约束, 分两步)
UPDATE raffle_results SET activity_key = account_id::text || ':' || tg_chat_id || ':' || COALESCE(keyword, '')
WHERE activity_key IS NULL;
-- 同 activity_key 保留 time 最新的一行, 其余删除(先清重复再建唯一索引)
DELETE FROM raffle_results a USING raffle_results b
WHERE a.ctid < b.ctid
  AND a.tenant_id = b.tenant_id
  AND a.activity_key = b.activity_key;
CREATE UNIQUE INDEX IF NOT EXISTS idx_raffle_results_activity ON raffle_results(tenant_id, activity_key);
-- +goose Down
DROP INDEX IF EXISTS idx_raffle_results_activity;
ALTER TABLE raffle_results DROP COLUMN IF EXISTS activity_key;
