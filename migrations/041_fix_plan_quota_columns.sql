-- +goose Up
-- +goose StatementBegin
-- 生产库迁移历史与 schema 脱节（017 未真正应用，/api/v1/plans 报 column does not exist）。
-- 幂等补齐：IF NOT EXISTS 保证重复执行安全。数值种子与 017 保持一致。
ALTER TABLE plans ADD COLUMN IF NOT EXISTS max_daily_broadcasts INT DEFAULT 50;
ALTER TABLE plans ADD COLUMN IF NOT EXISTS max_daily_scrapes INT DEFAULT 100;
ALTER TABLE plans ADD COLUMN IF NOT EXISTS max_monitor_rules INT DEFAULT 5;
-- 存量套餐回填（017 的 UPDATE 同样未执行过；按 plan_id 特征匹配，幂等）
UPDATE plans SET max_daily_broadcasts = 20,   max_daily_scrapes = 0,    max_monitor_rules = 0   WHERE plan_id LIKE '%free%' OR plan_id LIKE '%trial%';
UPDATE plans SET max_daily_broadcasts = 100,  max_daily_scrapes = 500,  max_monitor_rules = 10  WHERE plan_id LIKE '%basic%'  OR plan_id LIKE '%std%';
UPDATE plans SET max_daily_broadcasts = 500,  max_daily_scrapes = 2000, max_monitor_rules = 50  WHERE plan_id LIKE '%pro%'    AND max_daily_broadcasts = 50;
UPDATE plans SET max_daily_broadcasts = 9999, max_daily_scrapes = 9999, max_monitor_rules = 999 WHERE plan_id LIKE '%enterprise%' OR plan_id LIKE '%ent%' OR plan_id LIKE '%ult%';
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
-- 列可能被子sequent 代码依赖，Down 仅回退回填值不回退列
UPDATE plans SET max_daily_broadcasts = DEFAULT, max_daily_scrapes = DEFAULT, max_monitor_rules = DEFAULT;
-- +goose StatementEnd
