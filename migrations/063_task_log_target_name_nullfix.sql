-- +goose Up
-- 063: 历史 task_logs.target_name NULL 清理(2026-10-22, 审计 Z5)
-- 020 加列时无 DEFAULT, 存量行为 NULL; 062 的 DEFAULT '' 因 IF NOT EXISTS 被跳过。
-- 而 ListByTask 的 Scan 期待非 NULL(TEXT)——含历史日志的部署升级后日志接口 500。
UPDATE task_logs SET target_name = '' WHERE target_name IS NULL;

-- +goose Down
-- 不可逆, 无需 Down 操作(留空语句保持 goose 解析)
SELECT 1;
