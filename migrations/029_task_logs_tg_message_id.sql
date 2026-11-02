-- +goose Up
-- 发送结果可核对: task_logs 记录 Telegram 返回的 message_id.
-- 验收: 任取 success 日志 tg_message_id 非空, 且与 TG 客户端实际消息一致.
ALTER TABLE task_logs ADD COLUMN IF NOT EXISTS tg_message_id BIGINT NOT NULL DEFAULT 0;

-- +goose Down
ALTER TABLE task_logs DROP COLUMN IF EXISTS tg_message_id;
