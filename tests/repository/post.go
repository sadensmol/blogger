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

func (r *Post) CountWithoutMessage(ctx context.Context) (int, error) {
	var n int
	err := r.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM post WHERE message_ts IS NULL`).Scan(&n)
	return n, err
}

type PostRow struct {
	ArticleID, RunID int64
	MessageTS        string
	Now              time.Time
}

func (r *Post) InsertMessage(ctx context.Context, row PostRow) (int64, error) {
	var id int64
	err := r.db.QueryRowContext(ctx, `
INSERT INTO post (article_id, run_id, channel_id, message_ts, description, created_at, updated_at)
VALUES (?, ?, 'C1', ?, 'description', ?, ?)
RETURNING id`,
		row.ArticleID, row.RunID, row.MessageTS, row.Now.Format(time.RFC3339), row.Now.Format(time.RFC3339)).Scan(&id)
	return id, err
}

func (r *Post) MessagesByURL(ctx context.Context, url string) ([]string, error) {
	rows, err := r.db.QueryContext(ctx, `
SELECT p.message_ts
FROM post p
JOIN article a ON a.id = p.article_id
WHERE a.url = ?
ORDER BY p.id`, url)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var messages []string
	for rows.Next() {
		var ts string
		if err := rows.Scan(&ts); err != nil {
			return nil, err
		}
		messages = append(messages, ts)
	}
	return messages, rows.Err()
}
