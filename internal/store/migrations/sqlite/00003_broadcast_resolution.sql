-- +goose Up
-- The output resolution a broadcast's owner chose (add-output-quality); '' is the server's default.
ALTER TABLE broadcasts ADD COLUMN resolution VARCHAR(8) NOT NULL DEFAULT '';
