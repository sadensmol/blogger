-- +goose NO TRANSACTION
-- SQLite cannot drop NOT NULL in place. The table is rebuilt with foreign keys off,
-- because dropping post would fail on the reactions that point at it.

-- +goose Up
PRAGMA foreign_keys = OFF;
BEGIN;
CREATE TABLE post_new (
    id INTEGER PRIMARY KEY,
    article_id INTEGER NOT NULL,
    run_id INTEGER NOT NULL,
    channel_id TEXT,
    message_ts TEXT,
    description TEXT NOT NULL,
    topics TEXT NOT NULL DEFAULT '[]',
    reason TEXT NOT NULL DEFAULT '',
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL,
    CONSTRAINT post_article_id_uq UNIQUE (article_id),
    CONSTRAINT post_channel_id_message_ts_uq UNIQUE (channel_id, message_ts),
    CONSTRAINT post_channel_id_message_ts_ck CHECK ((channel_id IS NULL) = (message_ts IS NULL)),
    CONSTRAINT post_article_id_article_id_fk FOREIGN KEY (article_id) REFERENCES article (id),
    CONSTRAINT post_run_id_run_id_fk FOREIGN KEY (run_id) REFERENCES run (id)
);
INSERT INTO post_new (id, article_id, run_id, channel_id, message_ts, description, topics, reason, created_at, updated_at)
SELECT id, article_id, run_id, channel_id, message_ts, description, topics, reason, created_at, updated_at FROM post;
DROP TABLE post;
ALTER TABLE post_new RENAME TO post;
CREATE INDEX post_run_id_idx ON post (run_id);
COMMIT;
PRAGMA foreign_keys = ON;

-- +goose Down
PRAGMA foreign_keys = OFF;
BEGIN;
DELETE FROM post WHERE message_ts IS NULL;
CREATE TABLE post_old (
    id INTEGER PRIMARY KEY,
    article_id INTEGER NOT NULL,
    run_id INTEGER NOT NULL,
    channel_id TEXT NOT NULL,
    message_ts TEXT NOT NULL,
    description TEXT NOT NULL,
    topics TEXT NOT NULL DEFAULT '[]',
    reason TEXT NOT NULL DEFAULT '',
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL,
    CONSTRAINT post_article_id_uq UNIQUE (article_id),
    CONSTRAINT post_channel_id_message_ts_uq UNIQUE (channel_id, message_ts),
    CONSTRAINT post_article_id_article_id_fk FOREIGN KEY (article_id) REFERENCES article (id),
    CONSTRAINT post_run_id_run_id_fk FOREIGN KEY (run_id) REFERENCES run (id)
);
INSERT INTO post_old (id, article_id, run_id, channel_id, message_ts, description, topics, reason, created_at, updated_at)
SELECT id, article_id, run_id, channel_id, message_ts, description, topics, reason, created_at, updated_at FROM post;
DROP TABLE post;
ALTER TABLE post_old RENAME TO post;
CREATE INDEX post_run_id_idx ON post (run_id);
COMMIT;
PRAGMA foreign_keys = ON;
