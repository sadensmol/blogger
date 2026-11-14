-- +goose NO TRANSACTION
-- SQLite cannot drop NOT NULL in place. The table is rebuilt with foreign keys off,
-- because dropping article would fail on the posts that point at it.
-- An undated feed item used to get the fetch time as its date, which equals created_at to the second:
-- such a date becomes unknown.

-- +goose Up
PRAGMA foreign_keys = OFF;
BEGIN;
CREATE TABLE article_new (
    id INTEGER PRIMARY KEY,
    source_id INTEGER NOT NULL,
    url TEXT NOT NULL,
    title TEXT NOT NULL,
    excerpt TEXT NOT NULL DEFAULT '',
    published_at TEXT,
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL,
    CONSTRAINT article_url_uq UNIQUE (url),
    CONSTRAINT article_source_id_source_id_fk FOREIGN KEY (source_id) REFERENCES source (id)
);
INSERT INTO article_new (id, source_id, url, title, excerpt, published_at, created_at, updated_at)
SELECT id, source_id, url, title, excerpt, NULLIF(published_at, created_at), created_at, updated_at FROM article;
DROP TABLE article;
ALTER TABLE article_new RENAME TO article;
CREATE INDEX article_source_id_published_at_idx ON article (source_id, published_at);
COMMIT;
PRAGMA foreign_keys = ON;

-- +goose Down
PRAGMA foreign_keys = OFF;
BEGIN;
CREATE TABLE article_old (
    id INTEGER PRIMARY KEY,
    source_id INTEGER NOT NULL,
    url TEXT NOT NULL,
    title TEXT NOT NULL,
    excerpt TEXT NOT NULL DEFAULT '',
    published_at TEXT NOT NULL,
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL,
    CONSTRAINT article_url_uq UNIQUE (url),
    CONSTRAINT article_source_id_source_id_fk FOREIGN KEY (source_id) REFERENCES source (id)
);
INSERT INTO article_old (id, source_id, url, title, excerpt, published_at, created_at, updated_at)
SELECT id, source_id, url, title, excerpt, COALESCE(published_at, created_at), created_at, updated_at FROM article;
DROP TABLE article;
ALTER TABLE article_old RENAME TO article;
CREATE INDEX article_source_id_published_at_idx ON article (source_id, published_at);
COMMIT;
PRAGMA foreign_keys = ON;
