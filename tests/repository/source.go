package testsrepository

import (
	"cmp"
	"context"
	"database/sql"
	"fmt"
	"time"
)

type Source struct {
	db *sql.DB
}

func NewSource(db *sql.DB) *Source {
	return &Source{db: db}
}

type SourceRow struct {
	SiteURL string // empty: the feed url with /site
	FeedURL string // empty: no feed
	Title   string
	Status  string
}

func (r *Source) Insert(ctx context.Context, row SourceRow, now time.Time) (int64, error) {
	title := row.Title
	if title == "" {
		title = cmp.Or(row.FeedURL, row.SiteURL)
	}
	siteURL := row.SiteURL
	if siteURL == "" {
		siteURL = fmt.Sprintf("%s/site", row.FeedURL)
	}
	feedURL := sql.NullString{String: row.FeedURL, Valid: row.FeedURL != ""}
	var id int64
	err := r.db.QueryRowContext(ctx, `
INSERT INTO source (site_url, feed_url, title, status, origin, status_changed_at, created_at, updated_at)
VALUES (?, ?, ?, ?, 'user', ?, ?, ?)
RETURNING id`,
		siteURL, feedURL, title, row.Status,
		now.Format(time.RFC3339), now.Format(time.RFC3339), now.Format(time.RFC3339)).Scan(&id)
	return id, err
}

func (r *Source) DeletedAt(ctx context.Context, id int64) (sql.NullString, error) {
	var deletedAt sql.NullString
	err := r.db.QueryRowContext(ctx, `SELECT deleted_at FROM source WHERE id = ?`, id).Scan(&deletedAt)
	return deletedAt, err
}

func (r *Source) SetStatusChangedAt(ctx context.Context, id int64, at time.Time) error {
	_, err := r.db.ExecContext(ctx, `UPDATE source SET status_changed_at = ? WHERE id = ?`, at.Format(time.RFC3339), id)
	return err
}

func (r *Source) MarkDeleted(ctx context.Context, id int64, now time.Time) error {
	_, err := r.db.ExecContext(ctx, `UPDATE source SET deleted_at = ? WHERE id = ?`, now.Format(time.RFC3339), id)
	return err
}

func (r *Source) Status(ctx context.Context, id int64) (string, error) {
	var status string
	err := r.db.QueryRowContext(ctx, `SELECT status FROM source WHERE id = ?`, id).Scan(&status)
	return status, err
}

func (r *Source) IDByFeedURL(ctx context.Context, feedURL string) (int64, error) {
	var id int64
	err := r.db.QueryRowContext(ctx, `SELECT id FROM source WHERE feed_url = ?`, feedURL).Scan(&id)
	return id, err
}

func (r *Source) UpVotes(ctx context.Context, id int64) (int, error) {
	var n int
	err := r.db.QueryRowContext(ctx, `
SELECT COUNT(*)
FROM reaction r
JOIN post p ON p.id = r.post_id
JOIN article a ON a.id = p.article_id
WHERE a.source_id = ? AND r.vote = 1 AND r.removed_at IS NULL`, id).Scan(&n)
	return n, err
}

func (r *Source) DownVotes(ctx context.Context, id int64) (int, error) {
	var n int
	err := r.db.QueryRowContext(ctx, `
SELECT COUNT(*)
FROM reaction r
JOIN post p ON p.id = r.post_id
JOIN article a ON a.id = p.article_id
WHERE a.source_id = ? AND r.vote = -1 AND r.removed_at IS NULL`, id).Scan(&n)
	return n, err
}

type SourceDetail struct {
	Status string
	Origin string
	Reason string
}

func (r *Source) Detail(ctx context.Context, feedURL string) (SourceDetail, error) {
	var d SourceDetail
	err := r.db.QueryRowContext(ctx, `SELECT status, origin, reason FROM source WHERE feed_url = ?`, feedURL).Scan(&d.Status, &d.Origin, &d.Reason)
	return d, err
}

func (r *Source) SetFetchedAt(ctx context.Context, id int64, at time.Time) error {
	_, err := r.db.ExecContext(ctx, `UPDATE source SET fetched_at = ? WHERE id = ?`, at.Format(time.RFC3339), id)
	return err
}

func (r *Source) BackfilledAt(ctx context.Context, id int64) (sql.NullString, error) {
	var backfilledAt sql.NullString
	err := r.db.QueryRowContext(ctx, `SELECT backfilled_at FROM source WHERE id = ?`, id).Scan(&backfilledAt)
	return backfilledAt, err
}

type SourceURLs struct {
	SiteURL string
	FeedURL sql.NullString
}

func (r *Source) URLs(ctx context.Context, id int64) (SourceURLs, error) {
	var u SourceURLs
	err := r.db.QueryRowContext(ctx, `SELECT site_url, feed_url FROM source WHERE id = ?`, id).Scan(&u.SiteURL, &u.FeedURL)
	return u, err
}

func (r *Source) IDBySiteURL(ctx context.Context, siteURL string) (int64, error) {
	var id int64
	err := r.db.QueryRowContext(ctx, `SELECT id FROM source WHERE site_url = ?`, siteURL).Scan(&id)
	return id, err
}
