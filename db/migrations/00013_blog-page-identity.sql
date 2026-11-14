-- +goose NO TRANSACTION
-- The blog page (site_url) becomes the identity of a blog, and a blog may have no feed.
-- SQLite cannot change constraints in place. The tables are rebuilt with foreign keys off,
-- because dropping source would fail on the articles that point at it.
-- A site_url that more blogs share stays with one of them: a not deleted blog first, then the oldest.
-- The others take their feed url as their page, which is unique already.
-- Down gives a blog without a feed its page as its feed.

-- +goose Up
PRAGMA foreign_keys = OFF;
BEGIN;
CREATE TABLE source_new (
    id INTEGER PRIMARY KEY,
    site_url TEXT NOT NULL,
    feed_url TEXT,
    title TEXT NOT NULL,
    status TEXT NOT NULL,
    origin TEXT NOT NULL,
    reason TEXT NOT NULL DEFAULT '',
    status_changed_at TEXT NOT NULL,
    deleted_at TEXT,
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL,
    fetched_at TEXT,
    backfilled_at TEXT,
    backfill_tries INTEGER NOT NULL DEFAULT 0,
    backfill_tried_at TEXT,
    fetch_failed_at TEXT,
    CONSTRAINT source_site_url_uq UNIQUE (site_url),
    CONSTRAINT source_feed_url_uq UNIQUE (feed_url),
    CONSTRAINT source_status_ck CHECK (status IN ('active', 'trial', 'paused')),
    CONSTRAINT source_origin_ck CHECK (origin IN ('user', 'discovered'))
);
INSERT INTO source_new (id, site_url, feed_url, title, status, origin, reason, status_changed_at, deleted_at,
    created_at, updated_at, fetched_at, backfilled_at, backfill_tries, backfill_tried_at, fetch_failed_at)
SELECT s.id,
    CASE WHEN EXISTS (
        SELECT 1 FROM source first
        WHERE first.site_url = s.site_url AND first.id <> s.id
          AND ((first.deleted_at IS NULL AND s.deleted_at IS NOT NULL)
            OR ((first.deleted_at IS NULL) = (s.deleted_at IS NULL) AND first.id < s.id))
    ) THEN s.feed_url ELSE s.site_url END,
    s.feed_url, s.title, s.status, s.origin, s.reason, s.status_changed_at, s.deleted_at,
    s.created_at, s.updated_at, s.fetched_at, s.backfilled_at, s.backfill_tries, s.backfill_tried_at, s.fetch_failed_at
FROM source s;
DROP TABLE source;
ALTER TABLE source_new RENAME TO source;

CREATE TABLE blog_proposal_new (
    id INTEGER PRIMARY KEY,
    run_id INTEGER NOT NULL,
    channel_id TEXT,
    message_ts TEXT,
    site_url TEXT NOT NULL,
    feed_url TEXT,
    title TEXT NOT NULL,
    about TEXT NOT NULL,
    status TEXT NOT NULL,
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL,
    CONSTRAINT blog_proposal_status_ck CHECK (status IN ('proposed', 'added')),
    CONSTRAINT blog_proposal_channel_id_message_ts_ck CHECK ((channel_id IS NULL) = (message_ts IS NULL)),
    CONSTRAINT blog_proposal_run_id_run_id_fk FOREIGN KEY (run_id) REFERENCES run (id)
);
INSERT INTO blog_proposal_new (id, run_id, channel_id, message_ts, site_url, feed_url, title, about, status, created_at, updated_at)
SELECT id, run_id, channel_id, message_ts, site_url, feed_url, title, about, status, created_at, updated_at FROM blog_proposal;
DROP TABLE blog_proposal;
ALTER TABLE blog_proposal_new RENAME TO blog_proposal;
CREATE INDEX blog_proposal_run_id_idx ON blog_proposal (run_id);
CREATE INDEX blog_proposal_channel_id_message_ts_idx ON blog_proposal (channel_id, message_ts);

CREATE TABLE link_verdict (
    source_id INTEGER NOT NULL,
    url TEXT NOT NULL,
    article INTEGER NOT NULL,
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL,
    CONSTRAINT link_verdict_pk PRIMARY KEY (source_id, url),
    CONSTRAINT link_verdict_article_ck CHECK (article IN (0, 1)),
    CONSTRAINT link_verdict_source_id_source_id_fk FOREIGN KEY (source_id) REFERENCES source (id)
);
COMMIT;
PRAGMA foreign_keys = ON;

-- +goose Down
PRAGMA foreign_keys = OFF;
BEGIN;
DROP TABLE link_verdict;

CREATE TABLE blog_proposal_old (
    id INTEGER PRIMARY KEY,
    run_id INTEGER NOT NULL,
    channel_id TEXT,
    message_ts TEXT,
    site_url TEXT NOT NULL,
    feed_url TEXT NOT NULL,
    title TEXT NOT NULL,
    about TEXT NOT NULL,
    status TEXT NOT NULL,
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL,
    CONSTRAINT blog_proposal_status_ck CHECK (status IN ('proposed', 'added')),
    CONSTRAINT blog_proposal_channel_id_message_ts_ck CHECK ((channel_id IS NULL) = (message_ts IS NULL)),
    CONSTRAINT blog_proposal_run_id_run_id_fk FOREIGN KEY (run_id) REFERENCES run (id)
);
INSERT INTO blog_proposal_old (id, run_id, channel_id, message_ts, site_url, feed_url, title, about, status, created_at, updated_at)
SELECT id, run_id, channel_id, message_ts, site_url, COALESCE(feed_url, site_url), title, about, status, created_at, updated_at
FROM blog_proposal;
DROP TABLE blog_proposal;
ALTER TABLE blog_proposal_old RENAME TO blog_proposal;
CREATE INDEX blog_proposal_run_id_idx ON blog_proposal (run_id);
CREATE INDEX blog_proposal_channel_id_message_ts_idx ON blog_proposal (channel_id, message_ts);

CREATE TABLE source_old (
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
    fetched_at TEXT,
    backfilled_at TEXT,
    backfill_tries INTEGER NOT NULL DEFAULT 0,
    backfill_tried_at TEXT,
    fetch_failed_at TEXT,
    CONSTRAINT source_feed_url_uq UNIQUE (feed_url),
    CONSTRAINT source_status_ck CHECK (status IN ('active', 'trial', 'paused')),
    CONSTRAINT source_origin_ck CHECK (origin IN ('user', 'discovered'))
);
INSERT INTO source_old (id, site_url, feed_url, title, status, origin, reason, status_changed_at, deleted_at,
    created_at, updated_at, fetched_at, backfilled_at, backfill_tries, backfill_tried_at, fetch_failed_at)
SELECT id, site_url, COALESCE(feed_url, site_url), title, status, origin, reason, status_changed_at, deleted_at,
    created_at, updated_at, fetched_at, backfilled_at, backfill_tries, backfill_tried_at, fetch_failed_at
FROM source;
DROP TABLE source;
ALTER TABLE source_old RENAME TO source;
COMMIT;
PRAGMA foreign_keys = ON;
