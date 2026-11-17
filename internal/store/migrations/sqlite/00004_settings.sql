-- +goose Up
-- Server settings an administrator changes in the app (add-server-settings, design S2): one row per setting.
CREATE TABLE settings (
    name  VARCHAR(64) NOT NULL PRIMARY KEY,
    value TEXT        NOT NULL
);
