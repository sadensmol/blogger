-- +goose Up
CREATE TABLE post (
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

CREATE INDEX post_run_id_idx ON post (run_id);

-- +goose Down
DROP TABLE post;
