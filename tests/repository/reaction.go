package testsrepository

import (
	"context"
	"database/sql"
	"time"
)

type Reaction struct {
	db *sql.DB
}

func NewReaction(db *sql.DB) *Reaction {
	return &Reaction{db: db}
}

type ReactionRow struct {
	PostID    int64
	UserID    string
	Vote      int
	Removed   bool
	CreatedAt time.Time
}

func (r *Reaction) Insert(ctx context.Context, row ReactionRow) error {
	var removedAt sql.NullString
	if row.Removed {
		removedAt = sql.NullString{String: row.CreatedAt.Format(time.RFC3339), Valid: true}
	}
	_, err := r.db.ExecContext(ctx, `
INSERT INTO reaction (post_id, user_id, vote, removed_at, created_at, updated_at)
VALUES (?, ?, ?, ?, ?, ?)`,
		row.PostID, row.UserID, row.Vote, removedAt, row.CreatedAt.Format(time.RFC3339), row.CreatedAt.Format(time.RFC3339))
	return err
}
