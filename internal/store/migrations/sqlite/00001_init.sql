-- +goose Up
-- Every table of the data model (design.md). Ids are ULID strings, times are UTC, e-mails are stored
-- lower-cased (the CHECK refuses anything else), JSON documents are text.
CREATE TABLE users (
    id            CHAR(26)     NOT NULL PRIMARY KEY,
    email         VARCHAR(320) NOT NULL CHECK (email = lower(email)),
    name          VARCHAR(200) NOT NULL,
    password_hash VARCHAR(255) NOT NULL,
    theme         VARCHAR(64)  NOT NULL DEFAULT '',
    language      VARCHAR(16)  NOT NULL DEFAULT '',
    disabled_at   DATETIME
);
CREATE UNIQUE INDEX users_email_uq ON users (email);

CREATE TABLE sessions (
    id_hash      CHAR(64) NOT NULL PRIMARY KEY,
    user_id      CHAR(26) NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    last_used_at DATETIME NOT NULL
);
CREATE INDEX sessions_user_idx ON sessions (user_id);

CREATE TABLE broadcasts (
    id              CHAR(26)      NOT NULL PRIMARY KEY,
    owner_id        CHAR(26)      NOT NULL REFERENCES users (id),
    title           VARCHAR(200)  NOT NULL,
    invite_sealed   BLOB          NOT NULL,
    invite_hash     CHAR(64)      NOT NULL,
    stage           TEXT          NOT NULL,
    stage_version   BIGINT        NOT NULL DEFAULT 0,
    logo            VARCHAR(64)   NOT NULL DEFAULT '',
    output_state    VARCHAR(16)   NOT NULL DEFAULT 'offline',
    output_reason   VARCHAR(64)   NOT NULL DEFAULT '',
    output_run      BIGINT        NOT NULL DEFAULT 0,
    output_deadline DATETIME,
    ingest_id       VARCHAR(255)  NOT NULL DEFAULT '',
    ingest_url      VARCHAR(1024) NOT NULL DEFAULT '',
    started_at      DATETIME
);
CREATE UNIQUE INDEX broadcasts_invite_hash_uq ON broadcasts (invite_hash);
CREATE INDEX broadcasts_owner_idx ON broadcasts (owner_id);
CREATE INDEX broadcasts_output_state_idx ON broadcasts (output_state);

CREATE TABLE banners (
    id           CHAR(26)     NOT NULL PRIMARY KEY,
    broadcast_id CHAR(26)     NOT NULL REFERENCES broadcasts (id) ON DELETE CASCADE,
    text         VARCHAR(500) NOT NULL
);
CREATE INDEX banners_broadcast_idx ON banners (broadcast_id);

CREATE TABLE guests (
    id           CHAR(26)     NOT NULL PRIMARY KEY,
    broadcast_id CHAR(26)     NOT NULL REFERENCES broadcasts (id) ON DELETE CASCADE,
    name         VARCHAR(100) NOT NULL,
    cookie_hash  CHAR(64)     NOT NULL,
    joined_at    DATETIME     NOT NULL,
    last_join_at DATETIME     NOT NULL,
    removed_at   DATETIME
);
CREATE UNIQUE INDEX guests_cookie_hash_uq ON guests (cookie_hash);
CREATE INDEX guests_broadcast_idx ON guests (broadcast_id);

CREATE TABLE destinations (
    id             CHAR(26)     NOT NULL PRIMARY KEY,
    owner_id       CHAR(26)     NOT NULL REFERENCES users (id),
    name           VARCHAR(200) NOT NULL,
    scheme         VARCHAR(8)   NOT NULL,
    host           VARCHAR(255) NOT NULL,
    address_sealed BLOB         NOT NULL,
    has_key        BOOLEAN      NOT NULL DEFAULT FALSE
);
CREATE INDEX destinations_owner_idx ON destinations (owner_id);

CREATE TABLE broadcast_destinations (
    broadcast_id   CHAR(26) NOT NULL REFERENCES broadcasts (id) ON DELETE CASCADE,
    destination_id CHAR(26) NOT NULL REFERENCES destinations (id) ON DELETE CASCADE,
    PRIMARY KEY (broadcast_id, destination_id)
);
CREATE INDEX broadcast_destinations_destination_idx ON broadcast_destinations (destination_id);
