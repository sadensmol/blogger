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
	PublishedAt time.Time // zero: undated
	Now         time.Time
}

func (r *Article) Insert(ctx context.Context, row ArticleRow) (int64, error) {
	var publishedAt sql.NullString
	if !row.PublishedAt.IsZero() {
		publishedAt = sql.NullString{String: row.PublishedAt.Format(time.RFC3339), Valid: true}
	}
	var id int64
	err := r.db.QueryRowContext(ctx, `
INSERT INTO article (source_id, url, title, published_at, created_at, updated_at)
VALUES (?, ?, ?, ?, ?, ?)
RETURNING id`,
		row.SourceID, row.URL, row.URL, publishedAt,
		row.Now.Format(time.RFC3339), row.Now.Format(time.RFC3339)).Scan(&id)
	return id, err
}

type StoredArticle struct {
	URL, Title, Excerpt string
	PublishedAt         sql.NullString
}

func (r *Article) ListBySource(ctx context.Context, sourceID int64) ([]StoredArticle, error) {
	rows, err := r.db.QueryContext(ctx,
		`SELECT url, title, excerpt, published_at FROM article WHERE source_id = ? ORDER BY published_at DESC, url`, sourceID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var articles []StoredArticle
	for rows.Next() {
		var a StoredArticle
		if err := rows.Scan(&a.URL, &a.Title, &a.Excerpt, &a.PublishedAt); err != nil {
			return nil, err
		}
		articles = append(articles, a)
	}
	return articles, rows.Err()
}
