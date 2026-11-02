-- +goose Up
ALTER TABLE plans DROP COLUMN IF EXISTS max_monitor_rules;
ALTER TABLE plans DROP COLUMN IF EXISTS max_daily_broadcasts;
ALTER TABLE plans DROP COLUMN IF EXISTS max_daily_scrapes;
