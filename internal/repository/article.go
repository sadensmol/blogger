package repository

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/sadensmol/blogger/internal/domain"
)

type Article struct {
	db *sql.DB
}

func NewArticle(db *sql.DB) *Article {
	return &Article{db: db}
}

type articleRow struct {
	id, sourceID  int64
	sourceTitle   string
	url, title    string
	excerpt       string
	publishedAt   string
}

type repositoryArticle articleRow

func (r repositoryArticle) toDomain() (domain.Article, error) {
	publishedAt, err := parseTime(r.publishedAt)
	if err != nil {
		return domain.Article{}, fmt.Errorf("cannot map article %d: %w", r.id, err)
	}
	return domain.Article{
		ID:          domain.ArticleID(r.id),
		SourceID:    domain.SourceID(r.sourceID),
		SourceTitle: r.sourceTitle,
		URL:         domain.URL(r.url),
		Title:       r.title,
		Excerpt:     r.excerpt,
		PublishedAt: publishedAt,
	}, nil
}

func (r *Article) CreateIgnoringDuplicate(ctx context.Context, a domain.Article, now time.Time) (bool, error) {
	result, err := r.db.ExecContext(ctx, `
INSERT INTO article (source_id, url, title, excerpt, published_at, created_at, updated_at)
VALUES (?, ?, ?, ?, ?, ?, ?)
ON CONFLICT (url) DO NOTHING`,
		a.SourceID, a.URL, a.Title, a.Excerpt, formatTime(a.PublishedAt), formatTime(now), formatTime(now))
	if err != nil {
		return false, fmt.Errorf("cannot create article %s: %w", a.URL, err)
	}
	n, err := result.RowsAffected()
	if err != nil {
		return false, fmt.Errorf("cannot count rows of article %s: %w", a.URL, err)
	}
	return n == 1, nil
}

func (r *Article) ListNotPostedOfActiveOrTrialPublishedAfter(ctx context.Context, after time.Time) ([]domain.Article, error) {
	rows, err := r.db.QueryContext(ctx, `
SELECT a.id, a.source_id, s.title, a.url, a.title, a.excerpt, a.published_at
FROM article a
JOIN source s ON s.id = a.source_id
WHERE s.deleted_at IS NULL
  AND s.status IN ('active', 'trial')
  AND a.published_at > ?
  AND NOT EXISTS (SELECT 1 FROM post p WHERE p.article_id = a.id)
ORDER BY a.published_at DESC, a.id DESC`, formatTime(after))
	if err != nil {
		return nil, fmt.Errorf("cannot query not posted articles: %w", err)
	}
	defer rows.Close()
	articles := []domain.Article{}
	for rows.Next() {
		var row articleRow
		if err := rows.Scan(&row.id, &row.sourceID, &row.sourceTitle, &row.url, &row.title, &row.excerpt, &row.publishedAt); err != nil {
			return nil, fmt.Errorf("cannot scan article: %w", err)
		}
		article, err := repositoryArticle(row).toDomain()
		if err != nil {
			return nil, err
		}
		articles = append(articles, article)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("cannot read not posted articles: %w", err)
	}
	return articles, nil
}

func (r *Article) ListTitlesBySource(ctx context.Context, id domain.SourceID, limit int) ([]string, error) {
	rows, err := r.db.QueryContext(ctx,
		`SELECT title FROM article WHERE source_id = ? ORDER BY published_at DESC, id DESC LIMIT ?`, id, limit)
	if err != nil {
		return nil, fmt.Errorf("cannot query titles of source %d: %w", id, err)
	}
	defer rows.Close()
	titles := []string{}
	for rows.Next() {
		var title string
		if err := rows.Scan(&title); err != nil {
			return nil, fmt.Errorf("cannot scan title: %w", err)
		}
		titles = append(titles, title)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("cannot read titles of source %d: %w", id, err)
	}
	return titles, nil
}
