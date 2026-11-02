-- +goose Up
-- +goose StatementBegin

-- 添加功能开关字段
ALTER TABLE plans ADD COLUMN IF NOT EXISTS can_broadcast BOOLEAN DEFAULT TRUE;
ALTER TABLE plans ADD COLUMN IF NOT EXISTS can_scrape_members BOOLEAN DEFAULT TRUE;
ALTER TABLE plans ADD COLUMN IF NOT EXISTS can_scrape_links BOOLEAN DEFAULT TRUE;
ALTER TABLE plans ADD COLUMN IF NOT EXISTS can_join_groups BOOLEAN DEFAULT TRUE;
ALTER TABLE plans ADD COLUMN IF NOT EXISTS can_monitor BOOLEAN DEFAULT TRUE;
ALTER TABLE plans ADD COLUMN IF NOT EXISTS can_export BOOLEAN DEFAULT FALSE;
ALTER TABLE plans ADD COLUMN IF NOT EXISTS can_use_templates BOOLEAN DEFAULT FALSE;
ALTER TABLE plans ADD COLUMN IF NOT EXISTS max_monitor_rules INT DEFAULT 5;
ALTER TABLE plans ADD COLUMN IF NOT EXISTS max_daily_broadcasts INT DEFAULT 50;
ALTER TABLE plans ADD COLUMN IF NOT EXISTS max_daily_scrapes INT DEFAULT 100;

-- 更新种子数据
UPDATE plans SET
  can_broadcast=TRUE, can_scrape_members=FALSE, can_scrape_links=FALSE,
  can_join_groups=FALSE, can_monitor=FALSE, can_export=FALSE,
  can_use_templates=FALSE, max_monitor_rules=0, max_daily_broadcasts=20, max_daily_scrapes=0
WHERE plan_id='free';

UPDATE plans SET
  can_broadcast=TRUE, can_scrape_members=TRUE, can_scrape_links=FALSE,
  can_join_groups=FALSE, can_monitor=TRUE, can_export=FALSE,
  can_use_templates=FALSE, max_monitor_rules=10, max_daily_broadcasts=100, max_daily_scrapes=500
WHERE plan_id='basic';

UPDATE plans SET
  can_broadcast=TRUE, can_scrape_members=TRUE, can_scrape_links=TRUE,
  can_join_groups=TRUE, can_monitor=TRUE, can_export=TRUE,
  can_use_templates=TRUE, max_monitor_rules=50, max_daily_broadcasts=500, max_daily_scrapes=2000
WHERE plan_id='pro';

UPDATE plans SET
  can_broadcast=TRUE, can_scrape_members=TRUE, can_scrape_links=TRUE,
  can_join_groups=TRUE, can_monitor=TRUE, can_export=TRUE,
  can_use_templates=TRUE, max_monitor_rules=999, max_daily_broadcasts=9999, max_daily_scrapes=9999
WHERE plan_id='enterprise';

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
ALTER TABLE plans DROP COLUMN IF EXISTS can_broadcast;
ALTER TABLE plans DROP COLUMN IF EXISTS can_scrape_members;
ALTER TABLE plans DROP COLUMN IF EXISTS can_scrape_links;
ALTER TABLE plans DROP COLUMN IF EXISTS can_join_groups;
ALTER TABLE plans DROP COLUMN IF EXISTS can_monitor;
ALTER TABLE plans DROP COLUMN IF EXISTS can_export;
ALTER TABLE plans DROP COLUMN IF EXISTS can_use_templates;
ALTER TABLE plans DROP COLUMN IF EXISTS max_monitor_rules;
ALTER TABLE plans DROP COLUMN IF EXISTS max_daily_broadcasts;
ALTER TABLE plans DROP COLUMN IF EXISTS max_daily_scrapes;
-- +goose StatementEnd
