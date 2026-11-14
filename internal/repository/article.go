package repository

import (
	"context"
	"database/sql"
	"errors"
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
	shown         int
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
		Shown:       r.shown,
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

func (r *Article) FindLatestURLBySource(ctx context.Context, id domain.SourceID) (domain.URL, error) {
	var u string
	err := r.db.QueryRowContext(ctx, `SELECT url FROM article WHERE source_id = ? ORDER BY id DESC LIMIT 1`, id).Scan(&u)
	if errors.Is(err, sql.ErrNoRows) {
		return "", fmt.Errorf("latest article of source %d: %w", id, domain.ErrNotFound)
	}
	if err != nil {
		return "", fmt.Errorf("cannot find latest article of source %d: %w", id, err)
	}
	return domain.URL(u), nil
}

func (r *Article) UpdateUndatedPublishedAt(ctx context.Context, u domain.URL, publishedAt, now time.Time) error {
	_, err := r.db.ExecContext(ctx, `UPDATE article SET published_at = ?, updated_at = ? WHERE url = ? AND published_at IS NULL`,
		formatTime(publishedAt), formatTime(now), u)
	if err != nil {
		return fmt.Errorf("cannot date article %s: %w", u, err)
	}
	return nil
}

const notVotedOfActiveOrTrialQueryFormat = `
SELECT a.id, a.source_id, s.title, a.url, a.title, a.excerpt, a.published_at, COUNT(p.id)
FROM article a
JOIN source s ON s.id = a.source_id
LEFT JOIN post p ON p.article_id = a.id
WHERE s.deleted_at IS NULL
  AND s.status IN ('active', 'trial')
  AND %s
  AND NOT EXISTS (
    SELECT 1 FROM reaction r JOIN post voted ON voted.id = r.post_id
    WHERE voted.article_id = a.id AND r.removed_at IS NULL
  )
GROUP BY a.id
HAVING COUNT(p.id) = 0 OR (COUNT(p.id) < ? AND MAX(p.created_at) <= ?)
ORDER BY a.published_at IS NULL, a.published_at DESC, a.id DESC`

var (
	notVotedOfActiveOrTrialPublishedAfterQuery = fmt.Sprintf(notVotedOfActiveOrTrialQueryFormat,
		"a.published_at > ?")
	notVotedOfActiveOrTrialPublishedAfterOrUndatedQuery = fmt.Sprintf(notVotedOfActiveOrTrialQueryFormat,
		"(a.published_at > ? OR a.published_at IS NULL)")
)

// NotVotedFilter always passes a never posted article; a posted one passes when it was posted fewer than MaxPosts times,
// the last time before LastPostedBefore.
type NotVotedFilter struct {
	PublishedAfter   time.Time
	LastPostedBefore time.Time
	MaxPosts         int
}

func (r *Article) ListNotVotedOfActiveOrTrialPublishedAfter(ctx context.Context, f NotVotedFilter) ([]domain.Article, error) {
	return r.listNotVoted(ctx, notVotedOfActiveOrTrialPublishedAfterQuery, f)
}

func (r *Article) ListNotVotedOfActiveOrTrialPublishedAfterOrUndated(ctx context.Context, f NotVotedFilter) ([]domain.Article, error) {
	return r.listNotVoted(ctx, notVotedOfActiveOrTrialPublishedAfterOrUndatedQuery, f)
}

func (r *Article) listNotVoted(ctx context.Context, query string, f NotVotedFilter) ([]domain.Article, error) {
	rows, err := r.db.QueryContext(ctx, query, formatTime(f.PublishedAfter), f.MaxPosts, formatTime(f.LastPostedBefore))
	if err != nil {
		return nil, fmt.Errorf("cannot query not voted articles: %w", err)
	}
	defer rows.Close()
	articles := []domain.Article{}
	for rows.Next() {
		var row articleRow
		if err := rows.Scan(&row.id, &row.sourceID, &row.sourceTitle, &row.url, &row.title, &row.excerpt, &row.publishedAt, &row.shown); err != nil {
			return nil, fmt.Errorf("cannot scan article: %w", err)
		}
		article, err := repositoryArticle(row).toDomain()
		if err != nil {
			return nil, err
		}
		articles = append(articles, article)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("cannot read not voted articles: %w", err)
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
