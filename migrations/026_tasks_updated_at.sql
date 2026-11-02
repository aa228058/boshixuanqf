-- +goose Up
-- tasks 表补充 updated_at 列(代码中的 UPDATE 语句依赖它, 001 建表时遗漏)
ALTER TABLE tasks ADD COLUMN IF NOT EXISTS updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW();
UPDATE tasks SET updated_at = created_at WHERE updated_at IS NULL;

-- +goose Down
ALTER TABLE tasks DROP COLUMN IF EXISTS updated_at;
