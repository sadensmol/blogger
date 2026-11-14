package testsrepository

import (
	"context"
	"database/sql"
	"time"
)

type Article struct {
	db *sql.DB
}

func NewArticle(db *sql.DB) *Article {
	return &Article{db: db}
}

type ArticleRow struct {
	SourceID    int64
	URL         string
	PublishedAt time.Time
	Now         time.Time
}

func (r *Article) Insert(ctx context.Context, row ArticleRow) (int64, error) {
	var id int64
	err := r.db.QueryRowContext(ctx, `
INSERT INTO article (source_id, url, title, published_at, created_at, updated_at)
VALUES (?, ?, ?, ?, ?, ?)
RETURNING id`,
		row.SourceID, row.URL, row.URL, row.PublishedAt.Format(time.RFC3339),
		row.Now.Format(time.RFC3339), row.Now.Format(time.RFC3339)).Scan(&id)
	return id, err
}
