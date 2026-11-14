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
	publishedAt   sql.NullString
}

type repositoryArticle articleRow

func (r repositoryArticle) toDomain() (domain.Article, error) {
	publishedAt, err := parseOptionalTime(r.publishedAt)
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
		a.SourceID, a.URL, a.Title, a.Excerpt, formatOptionalTime(a.PublishedAt), formatTime(now), formatTime(now))
	if err != nil {
		return false, fmt.Errorf("cannot create article %s: %w", a.URL, err)
	}
	n, err := result.RowsAffected()
	if err != nil {
		return false, fmt.Errorf("cannot count rows of article %s: %w", a.URL, err)
	}
	return n == 1, nil
}

func (r *Article) ExistsByURL(ctx context.Context, u domain.URL) (bool, error) {
	var exists bool
	if err := r.db.QueryRowContext(ctx, `SELECT EXISTS (SELECT 1 FROM article WHERE url = ?)`, u).Scan(&exists); err != nil {
		return false, fmt.Errorf("cannot check article %s: %w", u, err)
	}
	return exists, nil
}

func (r *Article) UpdateUndatedPublishedAt(ctx context.Context, u domain.URL, publishedAt, now time.Time) error {
	_, err := r.db.ExecContext(ctx, `UPDATE article SET published_at = ?, updated_at = ? WHERE url = ? AND published_at IS NULL`,
		formatTime(publishedAt), formatTime(now), u)
	if err != nil {
		return fmt.Errorf("cannot date article %s: %w", u, err)
	}
	return nil
}

const notPostedOfActiveOrTrialQueryFormat = `
SELECT a.id, a.source_id, s.title, a.url, a.title, a.excerpt, a.published_at
FROM article a
JOIN source s ON s.id = a.source_id
WHERE s.deleted_at IS NULL
  AND s.status IN ('active', 'trial')
  AND %s
  AND NOT EXISTS (SELECT 1 FROM post p WHERE p.article_id = a.id)
ORDER BY a.published_at IS NULL, a.published_at DESC, a.id DESC`

var (
	notPostedOfActiveOrTrialPublishedAfterQuery = fmt.Sprintf(notPostedOfActiveOrTrialQueryFormat,
		"a.published_at > ?")
	notPostedOfActiveOrTrialPublishedAfterOrUndatedQuery = fmt.Sprintf(notPostedOfActiveOrTrialQueryFormat,
		"(a.published_at > ? OR a.published_at IS NULL)")
)

func (r *Article) ListNotPostedOfActiveOrTrialPublishedAfter(ctx context.Context, after time.Time) ([]domain.Article, error) {
	return r.listNotPosted(ctx, notPostedOfActiveOrTrialPublishedAfterQuery, after)
}

func (r *Article) ListNotPostedOfActiveOrTrialPublishedAfterOrUndated(ctx context.Context, after time.Time) ([]domain.Article, error) {
	return r.listNotPosted(ctx, notPostedOfActiveOrTrialPublishedAfterOrUndatedQuery, after)
}

func (r *Article) listNotPosted(ctx context.Context, query string, after time.Time) ([]domain.Article, error) {
	rows, err := r.db.QueryContext(ctx, query, formatTime(after))
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
