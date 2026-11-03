-- +goose Up
-- Keep task and risk history after an account's credentials are deleted.
-- These IDs become historical references, not live foreign-key relationships.
ALTER TABLE task_logs DROP CONSTRAINT IF EXISTS task_logs_account_id_fkey;
ALTER TABLE risk_events DROP CONSTRAINT IF EXISTS risk_events_account_id_fkey;

ALTER TABLE task_logs
    ADD COLUMN IF NOT EXISTS account_phone_snapshot TEXT NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS account_remark_snapshot TEXT NOT NULL DEFAULT '';

ALTER TABLE risk_events
    ADD COLUMN IF NOT EXISTS account_phone_snapshot TEXT NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS account_remark_snapshot TEXT NOT NULL DEFAULT '';

UPDATE task_logs tl SET account_phone_snapshot = COALESCE(a.phone, ''), account_remark_snapshot = COALESCE(a.remark, '')
FROM accounts a WHERE tl.account_id = a.account_id;

UPDATE risk_events re SET account_phone_snapshot = COALESCE(a.phone, ''), account_remark_snapshot = COALESCE(a.remark, '')
FROM accounts a WHERE re.account_id = a.account_id;

-- +goose Down
-- History rows cannot safely be discarded or re-attached automatically.
-- Keep the columns and detached historical references on rollback.
SELECT 1;
