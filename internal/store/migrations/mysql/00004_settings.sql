-- +goose NO TRANSACTION
-- +goose Up
-- Server settings an administrator changes in the app (add-server-settings, design S2): one row per setting.
CREATE TABLE IF NOT EXISTS settings (
    name  VARCHAR(64) NOT NULL PRIMARY KEY,
    value TEXT        NOT NULL
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;
