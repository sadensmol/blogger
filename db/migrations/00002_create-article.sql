-- +goose Up
CREATE TABLE article (
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

CREATE INDEX article_source_id_published_at_idx ON article (source_id, published_at);

-- +goose Down
DROP TABLE article;
