-- +goose Up
-- +goose StatementBegin
-- R9-Q01: squad_members 此前仅有 account_id 索引，而 GetMembers/SetMembers 按 squad_id 查询，
-- 编队成员列表在大编队下走全表扫描。补 squad_id 索引。
CREATE INDEX IF NOT EXISTS idx_squad_members_squad ON squad_members(squad_id);
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP INDEX IF EXISTS idx_squad_members_squad;
-- +goose StatementEnd
