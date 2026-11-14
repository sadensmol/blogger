-- +goose Up
ALTER TABLE source ADD COLUMN fetched_at TEXT;
ALTER TABLE source ADD COLUMN backfilled_at TEXT;

-- +goose Down
ALTER TABLE source DROP COLUMN backfilled_at;
ALTER TABLE source DROP COLUMN fetched_at;
