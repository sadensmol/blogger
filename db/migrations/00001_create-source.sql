-- +goose Up
CREATE TABLE source (
    id INTEGER PRIMARY KEY,
    site_url TEXT NOT NULL,
    feed_url TEXT NOT NULL,
    title TEXT NOT NULL,
    status TEXT NOT NULL,
    origin TEXT NOT NULL,
    reason TEXT NOT NULL DEFAULT '',
    status_changed_at TEXT NOT NULL,
    deleted_at TEXT,
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL,
    CONSTRAINT source_feed_url_uq UNIQUE (feed_url),
    CONSTRAINT source_status_ck CHECK (status IN ('active', 'trial', 'paused')),
    CONSTRAINT source_origin_ck CHECK (origin IN ('user', 'discovered'))
);

-- +goose Down
DROP TABLE source;
