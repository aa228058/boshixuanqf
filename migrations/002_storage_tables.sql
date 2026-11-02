-- +goose Up
-- +goose StatementBegin

CREATE TABLE IF NOT EXISTS sessions (
    session_id VARCHAR(255) PRIMARY KEY,
    dc INT NOT NULL DEFAULT 0,
    api_id INT NOT NULL DEFAULT 0,
    api_hash VARCHAR(255) NOT NULL DEFAULT '',
    test_mode BOOLEAN NOT NULL DEFAULT FALSE,
    auth_key BYTEA,
    state BYTEA,
    user_id BIGINT NOT NULL DEFAULT 0,
    is_bot BOOLEAN NOT NULL DEFAULT FALSE,
    first_name VARCHAR(255),
    last_name VARCHAR(255),
    username VARCHAR(255),
    date INT NOT NULL DEFAULT 0
);

CREATE TABLE IF NOT EXISTS peers (
    id BIGINT PRIMARY KEY,
    type INT NOT NULL DEFAULT 0,
    access_hash BIGINT NOT NULL DEFAULT 0,
    username VARCHAR(255),
    usernames VARCHAR(255),
    first_name VARCHAR(255),
    last_name VARCHAR(255),
    phone_number VARCHAR(32),
    is_bot BOOLEAN NOT NULL DEFAULT FALSE,
    photo_id BIGINT NOT NULL DEFAULT 0,
    language VARCHAR(10),
    last_updated BIGINT NOT NULL DEFAULT 0
);

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin

DROP TABLE IF EXISTS peers;
DROP TABLE IF EXISTS sessions;

-- +goose StatementEnd
