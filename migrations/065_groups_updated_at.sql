-- +goose Up
-- 065: groups 表补 updated_at(2026-10-22, 审计 V26)
-- 群库聚合水位只看 created_at——群的 title/member_count 更新或重新入群时
-- created_at 不变, 永不重新聚合, 群库永远停留最初值。加列 + 回填 + trigger 自动维护。
ALTER TABLE groups ADD COLUMN IF NOT EXISTS updated_at TIMESTAMPTZ;

-- 存量行回填为 created_at(避免把迁移时刻误当更新时间)
UPDATE groups SET updated_at = created_at WHERE updated_at IS NULL;

ALTER TABLE groups ALTER COLUMN updated_at SET NOT NULL;
ALTER TABLE groups ALTER COLUMN updated_at SET DEFAULT NOW();

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION touch_groups_updated_at() RETURNS trigger AS $$
BEGIN
    NEW.updated_at = NOW();
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;
-- +goose StatementEnd

DROP TRIGGER IF EXISTS groups_touch_updated_at ON groups;
CREATE TRIGGER groups_touch_updated_at
    BEFORE UPDATE ON groups
    FOR EACH ROW EXECUTE FUNCTION touch_groups_updated_at();

-- +goose Down
DROP TRIGGER IF EXISTS groups_touch_updated_at ON groups;
DROP FUNCTION IF EXISTS touch_groups_updated_at();
ALTER TABLE groups DROP COLUMN IF EXISTS updated_at;
