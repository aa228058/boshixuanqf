-- +goose Up
-- members 表补充 has_photo (last_seen/phone/bio 已存在, 采集侧回填)
ALTER TABLE members ADD COLUMN IF NOT EXISTS has_photo BOOLEAN NOT NULL DEFAULT FALSE;

-- +goose Down
ALTER TABLE members DROP COLUMN IF EXISTS has_photo;
