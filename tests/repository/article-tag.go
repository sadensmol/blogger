package testsrepository

import (
	"context"
	"database/sql"
	"encoding/json"
	"time"
)

type ArticleTag struct {
	db *sql.DB
}

func NewArticleTag(db *sql.DB) *ArticleTag {
	return &ArticleTag{db: db}
}

func (r *ArticleTag) Insert(ctx context.Context, articleID int64, now time.Time, tags ...string) error {
	for _, tag := range tags {
		if _, err := r.db.ExecContext(ctx, `
INSERT INTO article_tag (article_id, tag, created_at, updated_at)
VALUES (?, ?, ?, ?)`,
			articleID, tag, now.Format(time.RFC3339), now.Format(time.RFC3339)); err != nil {
			return err
		}
	}
	return nil
}

func (r *ArticleTag) ListByURL(ctx context.Context, url string) ([]string, error) {
	rows, err := r.db.QueryContext(ctx, `
SELECT t.tag
FROM article_tag t
JOIN article a ON a.id = t.article_id
WHERE a.url = ?
ORDER BY t.tag`, url)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	tags := []string{}
	for rows.Next() {
		var tag string
		if err := rows.Scan(&tag); err != nil {
			return nil, err
		}
		tags = append(tags, tag)
	}
	return tags, rows.Err()
}

func (r *ArticleTag) SetPostTopics(ctx context.Context, postID int64, topics []string) error {
	raw, err := json.Marshal(topics)
	if err != nil {
		return err
	}
	_, err = r.db.ExecContext(ctx, `UPDATE post SET topics = ? WHERE id = ?`, string(raw), postID)
	return err
}
