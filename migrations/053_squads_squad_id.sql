-- +goose Up
-- 修复编队功能(BUG#1): repository/squad.go 全部 SQL 使用 squad_id 列,
-- 但 033 建表时主键叫 id。补列并回填, 代码无需改动即可恢复。
ALTER TABLE squads ADD COLUMN IF NOT EXISTS squad_id UUID;

-- 存量数据回填: squad_id = 原主键 id
UPDATE squads SET squad_id = id WHERE squad_id IS NULL;

ALTER TABLE squads ALTER COLUMN squad_id SET NOT NULL;

-- squad_id 与 id 一一对应, 建唯一索引(新插入由应用层生成 uuid)
CREATE UNIQUE INDEX IF NOT EXISTS uq_squads_squad_id ON squads(squad_id);

-- +goose Down
DROP INDEX IF EXISTS uq_squads_squad_id;
ALTER TABLE squads DROP COLUMN IF EXISTS squad_id;
