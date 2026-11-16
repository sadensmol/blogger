-- The topics of old posts become the tags of their articles, so old votes count for themes at once.

-- +goose Up
CREATE TABLE article_tag (
    article_id INTEGER NOT NULL,
    tag TEXT NOT NULL,
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL,
    CONSTRAINT article_tag_pk PRIMARY KEY (article_id, tag),
    CONSTRAINT article_tag_article_id_article_id_fk FOREIGN KEY (article_id) REFERENCES article (id)
) WITHOUT ROWID;

CREATE INDEX article_tag_tag_idx ON article_tag (tag);

INSERT OR IGNORE INTO article_tag (article_id, tag, created_at, updated_at)
SELECT p.article_id, lower(trim(j.value, char(32, 9, 10, 13))),
    strftime('%Y-%m-%dT%H:%M:%SZ', 'now'), strftime('%Y-%m-%dT%H:%M:%SZ', 'now')
FROM post p, json_each(p.topics) j
WHERE j.type = 'text'
  AND trim(j.value, char(32, 9, 10, 13)) <> ''
  AND length(trim(j.value, char(32, 9, 10, 13))) <= 40;

-- +goose Down
DROP TABLE article_tag;
