package testsrepository

import (
	"context"
	"database/sql"
	"time"
)

type ProfileVersion struct {
	db *sql.DB
}

func NewProfileVersion(db *sql.DB) *ProfileVersion {
	return &ProfileVersion{db: db}
}

func (r *ProfileVersion) Insert(ctx context.Context, change string, now time.Time) error {
	_, err := r.db.ExecContext(ctx, `
INSERT INTO profile_version (content, change, created_at, updated_at)
VALUES ('content', ?, ?, ?)`,
		change, now.Format(time.RFC3339), now.Format(time.RFC3339))
	return err
}
