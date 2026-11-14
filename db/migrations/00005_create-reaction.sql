-- +goose Up
CREATE TABLE reaction (
    post_id INTEGER NOT NULL,
    user_id TEXT NOT NULL,
    vote INTEGER NOT NULL,
    removed_at TEXT,
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL,
    PRIMARY KEY (post_id, user_id, vote),
    CONSTRAINT reaction_vote_ck CHECK (vote IN (1, -1)),
    CONSTRAINT reaction_post_id_post_id_fk FOREIGN KEY (post_id) REFERENCES post (id)
) WITHOUT ROWID;

CREATE INDEX reaction_updated_at_idx ON reaction (updated_at);

-- +goose Down
DROP TABLE reaction;
