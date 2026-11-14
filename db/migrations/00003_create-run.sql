-- +goose Up
CREATE TABLE run (
    id INTEGER PRIMARY KEY,
    kind TEXT NOT NULL,
    status TEXT NOT NULL,
    backend TEXT NOT NULL DEFAULT '',
    error TEXT NOT NULL DEFAULT '',
    started_at TEXT NOT NULL,
    finished_at TEXT,
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL,
    CONSTRAINT run_status_ck CHECK (status IN ('running', 'ok', 'failed'))
);

CREATE INDEX run_kind_started_at_idx ON run (kind, started_at);

-- +goose Down
DROP TABLE run;
