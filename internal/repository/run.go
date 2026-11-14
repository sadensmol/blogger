package repository

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/sadensmol/blogger/internal/domain"
)

const interruptedRunError = "interrupted"

type Run struct {
	db *sql.DB
}

func NewRun(db *sql.DB) *Run {
	return &Run{db: db}
}

type FinishRunData struct {
	ID      domain.RunID
	Status  domain.RunStatus
	Backend string
	Error   string
	Now     time.Time
}

func (r *Run) Create(ctx context.Context, kind domain.RunKind, now time.Time) (domain.RunID, error) {
	var id int64
	err := r.db.QueryRowContext(ctx, `
INSERT INTO run (kind, status, started_at, created_at, updated_at)
VALUES (?, ?, ?, ?, ?)
RETURNING id`,
		kind, domain.RunStatusRunning, formatTime(now), formatTime(now), formatTime(now),
	).Scan(&id)
	if err != nil {
		return 0, fmt.Errorf("cannot create %s run: %w", kind, err)
	}
	return domain.RunID(id), nil
}

func (r *Run) Finish(ctx context.Context, d FinishRunData) error {
	result, err := r.db.ExecContext(ctx, `
UPDATE run SET status = ?, backend = ?, error = ?, finished_at = ?, updated_at = ?
WHERE id = ?`,
		d.Status, d.Backend, d.Error, formatTime(d.Now), formatTime(d.Now), d.ID)
	if err != nil {
		return fmt.Errorf("cannot finish run %d: %w", d.ID, err)
	}
	return requireAffected(result, fmt.Sprintf("run %d", d.ID))
}

func (r *Run) LastStartedAt(ctx context.Context, kind domain.RunKind) (time.Time, error) {
	var startedAt sql.NullString
	if err := r.db.QueryRowContext(ctx, `SELECT MAX(started_at) FROM run WHERE kind = ?`, kind).Scan(&startedAt); err != nil {
		return time.Time{}, fmt.Errorf("cannot find last %s run: %w", kind, err)
	}
	return parseOptionalTime(startedAt)
}

func (r *Run) LastMorningOrShowStartedAt(ctx context.Context) (time.Time, error) {
	var startedAt sql.NullString
	if err := r.db.QueryRowContext(ctx, `SELECT MAX(started_at) FROM run WHERE kind IN (?, ?)`,
		domain.RunKindDigestMorning, domain.RunKindDigestShow).Scan(&startedAt); err != nil {
		return time.Time{}, fmt.Errorf("cannot find last morning or show digest run: %w", err)
	}
	return parseOptionalTime(startedAt)
}

func (r *Run) LastFinishedStartedAt(ctx context.Context, kind domain.RunKind) (time.Time, error) {
	var startedAt sql.NullString
	if err := r.db.QueryRowContext(ctx, `SELECT MAX(started_at) FROM run WHERE kind = ? AND finished_at IS NOT NULL`, kind).Scan(&startedAt); err != nil {
		return time.Time{}, fmt.Errorf("cannot find last finished %s run: %w", kind, err)
	}
	return parseOptionalTime(startedAt)
}

func (r *Run) FailRunning(ctx context.Context, now time.Time) (int64, error) {
	result, err := r.db.ExecContext(ctx, `
UPDATE run SET status = ?, error = ?, finished_at = ?, updated_at = ?
WHERE status = ?`,
		domain.RunStatusFailed, interruptedRunError, formatTime(now), formatTime(now), domain.RunStatusRunning)
	if err != nil {
		return 0, fmt.Errorf("cannot fail running runs: %w", err)
	}
	n, err := result.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("cannot count failed runs: %w", err)
	}
	return n, nil
}
