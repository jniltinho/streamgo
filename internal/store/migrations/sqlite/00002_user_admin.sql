-- +goose Up
-- Administrators (add-user-management, design U1, U2): on an existing install the oldest enabled account (the
-- smallest ULID) becomes one, or the oldest account when none is enabled.
ALTER TABLE users ADD COLUMN is_admin BOOLEAN NOT NULL DEFAULT FALSE;
UPDATE users SET is_admin = TRUE
WHERE id = COALESCE((SELECT MIN(id) FROM users WHERE disabled_at IS NULL), (SELECT MIN(id) FROM users));
