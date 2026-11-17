-- MariaDB DDL is not transactional: every statement is re-runnable so an interrupted migration can
-- simply be run again.
-- +goose NO TRANSACTION
-- +goose Up
-- Administrators (add-user-management, design U1, U2): on an existing install the oldest enabled account (the
-- smallest ULID) becomes one, or the oldest account when none is enabled. MariaDB refuses a subquery on the
-- updated table, hence the derived tables.
ALTER TABLE users ADD COLUMN IF NOT EXISTS is_admin BOOLEAN NOT NULL DEFAULT FALSE;
UPDATE users SET is_admin = TRUE
WHERE id = COALESCE((SELECT e.id FROM (SELECT MIN(id) AS id FROM users WHERE disabled_at IS NULL) AS e),
                    (SELECT a.id FROM (SELECT MIN(id) AS id FROM users) AS a));
