package testsrepository

import (
	"context"
	"database/sql"
	"time"
)

type Run struct {
	db *sql.DB
}

func NewRun(db *sql.DB) *Run {
	return &Run{db: db}
}

func (r *Run) Insert(ctx context.Context, kind, status string, now time.Time) (int64, error) {
	var id int64
	err := r.db.QueryRowContext(ctx, `
INSERT INTO run (kind, status, started_at, created_at, updated_at)
VALUES (?, ?, ?, ?, ?)
RETURNING id`,
		kind, status, now.Format(time.RFC3339), now.Format(time.RFC3339), now.Format(time.RFC3339)).Scan(&id)
	return id, err
}

func (r *Run) Status(ctx context.Context, id int64) (string, error) {
	var status string
	err := r.db.QueryRowContext(ctx, `SELECT status FROM run WHERE id = ?`, id).Scan(&status)
	return status, err
}
