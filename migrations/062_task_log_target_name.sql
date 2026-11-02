-- +goose Up
-- 062: 任务日志补充群名列(2026-10-21)
-- 引擎一直解析了 TargetName 但写库时丢弃(表无此列), 前端日志只能显示 chatID——补齐链路
ALTER TABLE task_logs ADD COLUMN IF NOT EXISTS target_name TEXT NOT NULL DEFAULT '';

-- +goose Down
ALTER TABLE task_logs DROP COLUMN IF EXISTS target_name;
