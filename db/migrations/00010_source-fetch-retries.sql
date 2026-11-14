-- +goose Up
ALTER TABLE source ADD COLUMN backfill_tries INTEGER NOT NULL DEFAULT 0;
ALTER TABLE source ADD COLUMN backfill_tried_at TEXT;
ALTER TABLE source ADD COLUMN fetch_failed_at TEXT;

-- +goose Down
ALTER TABLE source DROP COLUMN fetch_failed_at;
ALTER TABLE source DROP COLUMN backfill_tried_at;
ALTER TABLE source DROP COLUMN backfill_tries;
