package testsrepository

import (
	"context"
	"database/sql"
	"fmt"
	"time"
)

type Post struct {
	db *sql.DB
}

func NewPost(db *sql.DB) *Post {
	return &Post{db: db}
}

func (r *Post) Insert(ctx context.Context, articleID, runID int64, now time.Time) (int64, error) {
	var id int64
	err := r.db.QueryRowContext(ctx, `
INSERT INTO post (article_id, run_id, channel_id, message_ts, description, created_at, updated_at)
VALUES (?, ?, 'C1', ?, 'description', ?, ?)
RETURNING id`,
		articleID, runID, fmt.Sprintf("ts-%d", articleID), now.Format(time.RFC3339), now.Format(time.RFC3339)).Scan(&id)
	return id, err
}
