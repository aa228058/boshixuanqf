-- +goose Up
-- +goose StatementBegin
-- 043: 扩展 task_logs.status 的 CHECK 约束
-- 代码侧长期使用但白名单缺失的值：info / pending / new / not_supported / running / paused / wait / cancelled
-- 缺失导致这些日志(含"任务已创建")插入即 23514，任务日志批量写入整体失败（2026-09-22 日志实证）。
ALTER TABLE task_logs DROP CONSTRAINT IF EXISTS task_logs_status_check;
ALTER TABLE task_logs ADD CONSTRAINT task_logs_status_check
  CHECK (status IN ('success','failed','skipped','info','pending','new',
    'not_supported','running','paused','wait','cancelled'));
-- +goose StatementEnd
