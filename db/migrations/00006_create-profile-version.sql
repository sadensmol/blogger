-- +goose Up
CREATE TABLE profile_version (
    id INTEGER PRIMARY KEY,
    content TEXT NOT NULL,
    change TEXT NOT NULL,
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL,
    CONSTRAINT profile_version_change_ck CHECK (change IN ('init', 'update', 'notes'))
);

-- +goose Down
DROP TABLE profile_version;
