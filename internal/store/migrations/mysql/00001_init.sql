-- MariaDB DDL is not transactional: every statement is re-runnable (IF NOT EXISTS) so a migration
-- interrupted half-way can simply be run again.
-- +goose NO TRANSACTION
-- +goose Up
-- Every table of the data model (design.md). Ids are ULID strings, times are UTC, e-mails are stored
-- lower-cased (the CHECK refuses anything else; the binary collation keeps the comparison exact),
-- JSON documents are text.
CREATE TABLE IF NOT EXISTS users (
    id            CHAR(26)     NOT NULL PRIMARY KEY,
    email         VARCHAR(320) CHARACTER SET utf8mb4 COLLATE utf8mb4_bin NOT NULL,
    name          VARCHAR(200) NOT NULL,
    password_hash VARCHAR(255) NOT NULL,
    theme         VARCHAR(64)  NOT NULL DEFAULT '',
    language      VARCHAR(16)  NOT NULL DEFAULT '',
    disabled_at   DATETIME(6)  NULL,
    UNIQUE KEY users_email_uq (email),
    CONSTRAINT users_email_lower CHECK (email = lower(email))
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE IF NOT EXISTS sessions (
    id_hash      CHAR(64)    NOT NULL PRIMARY KEY,
    user_id      CHAR(26)    NOT NULL,
    last_used_at DATETIME(6) NOT NULL,
    KEY sessions_user_idx (user_id),
    CONSTRAINT sessions_user_fk FOREIGN KEY (user_id) REFERENCES users (id) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE IF NOT EXISTS broadcasts (
    id              CHAR(26)      NOT NULL PRIMARY KEY,
    owner_id        CHAR(26)      NOT NULL,
    title           VARCHAR(200)  NOT NULL,
    invite_sealed   BLOB          NOT NULL,
    invite_hash     CHAR(64)      NOT NULL,
    stage           TEXT          NOT NULL,
    stage_version   BIGINT        NOT NULL DEFAULT 0,
    logo            VARCHAR(64)   NOT NULL DEFAULT '',
    output_state    VARCHAR(16)   NOT NULL DEFAULT 'offline',
    output_reason   VARCHAR(64)   NOT NULL DEFAULT '',
    output_run      BIGINT        NOT NULL DEFAULT 0,
    output_deadline DATETIME(6)   NULL,
    ingest_id       VARCHAR(255)  NOT NULL DEFAULT '',
    ingest_url      VARCHAR(1024) NOT NULL DEFAULT '',
    started_at      DATETIME(6)   NULL,
    UNIQUE KEY broadcasts_invite_hash_uq (invite_hash),
    KEY broadcasts_owner_idx (owner_id),
    KEY broadcasts_output_state_idx (output_state),
    CONSTRAINT broadcasts_owner_fk FOREIGN KEY (owner_id) REFERENCES users (id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE IF NOT EXISTS banners (
    id           CHAR(26)     NOT NULL PRIMARY KEY,
    broadcast_id CHAR(26)     NOT NULL,
    text         VARCHAR(500) NOT NULL,
    KEY banners_broadcast_idx (broadcast_id),
    CONSTRAINT banners_broadcast_fk FOREIGN KEY (broadcast_id) REFERENCES broadcasts (id) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE IF NOT EXISTS guests (
    id           CHAR(26)     NOT NULL PRIMARY KEY,
    broadcast_id CHAR(26)     NOT NULL,
    name         VARCHAR(100) NOT NULL,
    cookie_hash  CHAR(64)     NOT NULL,
    joined_at    DATETIME(6)  NOT NULL,
    last_join_at DATETIME(6)  NOT NULL,
    removed_at   DATETIME(6)  NULL,
    UNIQUE KEY guests_cookie_hash_uq (cookie_hash),
    KEY guests_broadcast_idx (broadcast_id),
    CONSTRAINT guests_broadcast_fk FOREIGN KEY (broadcast_id) REFERENCES broadcasts (id) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE IF NOT EXISTS destinations (
    id             CHAR(26)     NOT NULL PRIMARY KEY,
    owner_id       CHAR(26)     NOT NULL,
    name           VARCHAR(200) NOT NULL,
    scheme         VARCHAR(8)   NOT NULL,
    host           VARCHAR(255) NOT NULL,
    address_sealed BLOB         NOT NULL,
    has_key        BOOLEAN      NOT NULL DEFAULT FALSE,
    KEY destinations_owner_idx (owner_id),
    CONSTRAINT destinations_owner_fk FOREIGN KEY (owner_id) REFERENCES users (id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE IF NOT EXISTS broadcast_destinations (
    broadcast_id   CHAR(26) NOT NULL,
    destination_id CHAR(26) NOT NULL,
    PRIMARY KEY (broadcast_id, destination_id),
    KEY broadcast_destinations_destination_idx (destination_id),
    CONSTRAINT broadcast_destinations_broadcast_fk FOREIGN KEY (broadcast_id) REFERENCES broadcasts (id) ON DELETE CASCADE,
    CONSTRAINT broadcast_destinations_destination_fk FOREIGN KEY (destination_id) REFERENCES destinations (id) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;
