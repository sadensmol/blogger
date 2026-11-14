-- +goose Up
CREATE TABLE blog_proposal (
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

CREATE INDEX blog_proposal_run_id_idx ON blog_proposal (run_id);
CREATE INDEX blog_proposal_channel_id_message_ts_idx ON blog_proposal (channel_id, message_ts);

-- +goose Down
DROP TABLE blog_proposal;
