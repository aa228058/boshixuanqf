-- +goose Up
-- 战役目标: 失败原因(战役报告按此统计TOP)
ALTER TABLE campaign_targets ADD COLUMN IF NOT EXISTS last_error TEXT DEFAULT '';
