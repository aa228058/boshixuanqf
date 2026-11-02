-- +goose Up
-- 068: tasks.status CHECK 补 'scheduled'(2026-10-22, 审计 P2-26)
-- domain.TaskStatusScheduled 在状态机转换表(service/task.go)与测试里已被引用,
-- 但 001 的 CHECK 约束不含该值——未来任何把任务行写入 scheduled 的代码都会触发
-- CHECK 违例 500。先验证存量无该值, 再重建约束扩入。
-- +goose StatementBegin
DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM tasks WHERE status = 'scheduled') THEN
        RAISE EXCEPTION '存量存在 scheduled 任务, 先迁移数据再执行本迁移';
    END IF;
END $$;
-- +goose StatementEnd

ALTER TABLE tasks DROP CONSTRAINT IF EXISTS tasks_status_check;
ALTER TABLE tasks ADD CONSTRAINT tasks_status_check
    CHECK (status IN ('pending', 'queued', 'running', 'paused', 'completed', 'failed', 'cancelled', 'scheduled'));

-- +goose Down
ALTER TABLE tasks DROP CONSTRAINT IF EXISTS tasks_status_check;
ALTER TABLE tasks ADD CONSTRAINT tasks_status_check
    CHECK (status IN ('pending', 'queued', 'running', 'paused', 'completed', 'failed', 'cancelled'));
