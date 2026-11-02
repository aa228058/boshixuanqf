-- +goose Up
CREATE TABLE IF NOT EXISTS probe_configs (
    id             SMALLINT PRIMARY KEY DEFAULT 1 CHECK (id = 1),
    test_username  TEXT NOT NULL DEFAULT '',
    private_invite TEXT NOT NULL DEFAULT '',
    public_link    TEXT NOT NULL DEFAULT '',
    updated_at     TIMESTAMPTZ NOT NULL DEFAULT now()
);
INSERT INTO probe_configs (id) VALUES (1) ON CONFLICT (id) DO NOTHING;
-- +goose Down
DROP TABLE IF EXISTS probe_configs;
