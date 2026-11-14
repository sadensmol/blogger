package repository

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/sadensmol/blogger/internal/domain"
)

type Source struct {
	db *sql.DB
}

func NewSource(db *sql.DB) *Source {
	return &Source{db: db}
}

type sourceRow struct {
	id                int64
	siteURL           string
	feedURL           sql.NullString
	title, reason     string
	status, origin    string
	statusChangedAt   string
	fetchedAt         sql.NullString
	fetchFailedAt     sql.NullString
	backfilledAt      sql.NullString
	backfillTries     int
	backfillTriedAt   sql.NullString
	votesUp, votesDown int
	sinceUp, sinceDown int
}

type repositorySource sourceRow

func (r repositorySource) toDomain() (domain.Source, error) {
	status, err := domain.ParseSourceStatus(r.status)
	if err != nil {
		return domain.Source{}, fmt.Errorf("cannot map source %d: %w", r.id, err)
	}
	statusChangedAt, err := parseTime(r.statusChangedAt)
	if err != nil {
		return domain.Source{}, fmt.Errorf("cannot map source %d: %w", r.id, err)
	}
	fetchedAt, err := parseOptionalTime(r.fetchedAt)
	if err != nil {
		return domain.Source{}, fmt.Errorf("cannot map source %d: %w", r.id, err)
	}
	fetchFailedAt, err := parseOptionalTime(r.fetchFailedAt)
	if err != nil {
		return domain.Source{}, fmt.Errorf("cannot map source %d: %w", r.id, err)
	}
	backfilledAt, err := parseOptionalTime(r.backfilledAt)
	if err != nil {
		return domain.Source{}, fmt.Errorf("cannot map source %d: %w", r.id, err)
	}
	backfillTriedAt, err := parseOptionalTime(r.backfillTriedAt)
	if err != nil {
		return domain.Source{}, fmt.Errorf("cannot map source %d: %w", r.id, err)
	}
	return domain.Source{
		ID:               domain.SourceID(r.id),
		SiteURL:          domain.URL(r.siteURL),
		FeedURL:          domain.URL(r.feedURL.String),
		Title:            r.title,
		Reason:           r.reason,
		Status:           status,
		Origin:           domain.SourceOrigin(r.origin),
		StatusChangedAt:  statusChangedAt,
		Votes:            domain.Votes{Up: r.votesUp, Down: r.votesDown},
		VotesSinceStatus: domain.Votes{Up: r.sinceUp, Down: r.sinceDown},
		FetchedAt:        fetchedAt,
		FetchFailedAt:    fetchFailedAt,
		BackfilledAt:     backfilledAt,
		BackfillTries:    r.backfillTries,
		BackfillTriedAt:  backfillTriedAt,
	}, nil
}

const sourceQueryFormat = `
SELECT s.id, s.site_url, s.feed_url, s.title, s.reason, s.status, s.origin, s.status_changed_at,
    s.fetched_at, s.fetch_failed_at, s.backfilled_at, s.backfill_tries, s.backfill_tried_at,
    COALESCE(SUM(r.vote = 1), 0),
    COALESCE(SUM(r.vote = -1), 0),
    COALESCE(SUM(r.vote = 1 AND r.updated_at >= s.status_changed_at), 0),
    COALESCE(SUM(r.vote = -1 AND r.updated_at >= s.status_changed_at), 0)
FROM source s
LEFT JOIN article a ON a.source_id = s.id
LEFT JOIN post p ON p.article_id = a.id
LEFT JOIN reaction r ON r.post_id = p.id AND r.removed_at IS NULL
WHERE s.deleted_at IS NULL %s
GROUP BY s.id
ORDER BY s.id`

var (
	sourceQuery             = fmt.Sprintf(sourceQueryFormat, "")
	sourceByIDQuery         = fmt.Sprintf(sourceQueryFormat, "AND s.id = ?")
	sourceByURLQuery        = fmt.Sprintf(sourceQueryFormat, "AND (s.feed_url = ? OR s.site_url = ?)")
	sourceActiveOrTrialQuery = fmt.Sprintf(sourceQueryFormat, "AND s.status IN ('active', 'trial')")
)

func (r *Source) list(ctx context.Context, query string, args ...any) ([]domain.Source, error) {
	rows, err := r.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("cannot query sources: %w", err)
	}
	defer rows.Close()
	sources := []domain.Source{}
	for rows.Next() {
		var row sourceRow
		if err := rows.Scan(&row.id, &row.siteURL, &row.feedURL, &row.title, &row.reason, &row.status, &row.origin,
			&row.statusChangedAt, &row.fetchedAt, &row.fetchFailedAt, &row.backfilledAt, &row.backfillTries, &row.backfillTriedAt,
			&row.votesUp, &row.votesDown, &row.sinceUp, &row.sinceDown); err != nil {
			return nil, fmt.Errorf("cannot scan source: %w", err)
		}
		source, err := repositorySource(row).toDomain()
		if err != nil {
			return nil, err
		}
		sources = append(sources, source)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("cannot read sources: %w", err)
	}
	return sources, nil
}

func (r *Source) findOne(ctx context.Context, query string, args ...any) (domain.Source, error) {
	sources, err := r.list(ctx, query, args...)
	if err != nil {
		return domain.Source{}, err
	}
	if len(sources) == 0 {
		return domain.Source{}, domain.ErrNotFound
	}
	return sources[0], nil
}

// Create brings back a deleted blog with the same page or feed; a blog in the list is ErrAlreadyExists.
func (r *Source) Create(ctx context.Context, s domain.Source, now time.Time) (domain.Source, error) {
	var id int64
	err := r.db.QueryRowContext(ctx, `
INSERT INTO source (site_url, feed_url, title, status, origin, reason, status_changed_at, created_at, updated_at)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT (site_url) DO UPDATE SET
    feed_url = excluded.feed_url,
    title = excluded.title,
    status = excluded.status,
    origin = excluded.origin,
    reason = excluded.reason,
    status_changed_at = excluded.status_changed_at,
    deleted_at = NULL,
    updated_at = excluded.updated_at
WHERE source.deleted_at IS NOT NULL
ON CONFLICT (feed_url) DO UPDATE SET
    site_url = excluded.site_url,
    title = excluded.title,
    status = excluded.status,
    origin = excluded.origin,
    reason = excluded.reason,
    status_changed_at = excluded.status_changed_at,
    deleted_at = NULL,
    updated_at = excluded.updated_at
WHERE source.deleted_at IS NOT NULL
RETURNING id`,
		s.SiteURL, optionalURL(s.FeedURL), s.Title, s.Status, s.Origin, s.Reason, formatTime(now), formatTime(now), formatTime(now),
	).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.Source{}, fmt.Errorf("source %s: %w", s.SiteURL, domain.ErrAlreadyExists)
	}
	if err != nil {
		return domain.Source{}, fmt.Errorf("cannot create source %s: %w", s.SiteURL, err)
	}
	return r.FindNotDeletedByID(ctx, domain.SourceID(id))
}

func (r *Source) Update(ctx context.Context, s domain.Source, now time.Time) error {
	result, err := r.db.ExecContext(ctx, `
UPDATE source
SET site_url = ?, feed_url = ?, title = ?, status = ?, origin = ?, reason = ?, status_changed_at = ?, updated_at = ?
WHERE id = ? AND deleted_at IS NULL`,
		s.SiteURL, optionalURL(s.FeedURL), s.Title, s.Status, s.Origin, s.Reason, formatTime(s.StatusChangedAt), formatTime(now), s.ID)
	if err != nil {
		return fmt.Errorf("cannot update source %d: %w", s.ID, err)
	}
	return requireAffected(result, fmt.Sprintf("source %d", s.ID))
}

func (r *Source) UpdateFetchedAt(ctx context.Context, id domain.SourceID, now time.Time) error {
	result, err := r.db.ExecContext(ctx,
		`UPDATE source SET fetched_at = ?, updated_at = ? WHERE id = ? AND deleted_at IS NULL`,
		formatTime(now), formatTime(now), id)
	if err != nil {
		return fmt.Errorf("cannot set fetch time of source %d: %w", id, err)
	}
	return requireAffected(result, fmt.Sprintf("source %d", id))
}

func (r *Source) UpdateFetchFailedAt(ctx context.Context, id domain.SourceID, now time.Time) error {
	result, err := r.db.ExecContext(ctx,
		`UPDATE source SET fetch_failed_at = ?, updated_at = ? WHERE id = ? AND deleted_at IS NULL`,
		formatTime(now), formatTime(now), id)
	if err != nil {
		return fmt.Errorf("cannot set fetch failure time of source %d: %w", id, err)
	}
	return requireAffected(result, fmt.Sprintf("source %d", id))
}

func (r *Source) UpdateBackfilledAt(ctx context.Context, id domain.SourceID, now time.Time) error {
	result, err := r.db.ExecContext(ctx,
		`UPDATE source SET backfilled_at = ?, updated_at = ? WHERE id = ? AND deleted_at IS NULL`,
		formatTime(now), formatTime(now), id)
	if err != nil {
		return fmt.Errorf("cannot set backfill time of source %d: %w", id, err)
	}
	return requireAffected(result, fmt.Sprintf("source %d", id))
}

func (r *Source) IncBackfillTries(ctx context.Context, id domain.SourceID, now time.Time) error {
	result, err := r.db.ExecContext(ctx,
		`UPDATE source SET backfill_tries = backfill_tries + 1, backfill_tried_at = ?, updated_at = ? WHERE id = ? AND deleted_at IS NULL`,
		formatTime(now), formatTime(now), id)
	if err != nil {
		return fmt.Errorf("cannot count a backfill try of source %d: %w", id, err)
	}
	return requireAffected(result, fmt.Sprintf("source %d", id))
}

func (r *Source) Delete(ctx context.Context, id domain.SourceID, now time.Time) error {
	result, err := r.db.ExecContext(ctx,
		`UPDATE source SET deleted_at = ?, updated_at = ? WHERE id = ? AND deleted_at IS NULL`,
		formatTime(now), formatTime(now), id)
	if err != nil {
		return fmt.Errorf("cannot delete source %d: %w", id, err)
	}
	return requireAffected(result, fmt.Sprintf("source %d", id))
}

func (r *Source) FindNotDeletedByID(ctx context.Context, id domain.SourceID) (domain.Source, error) {
	source, err := r.findOne(ctx, sourceByIDQuery, id)
	if err != nil {
		return domain.Source{}, fmt.Errorf("cannot find source %d: %w", id, err)
	}
	return source, nil
}

func (r *Source) FindNotDeletedByURL(ctx context.Context, u domain.URL) (domain.Source, error) {
	source, err := r.findOne(ctx, sourceByURLQuery, u, u)
	if err != nil {
		return domain.Source{}, fmt.Errorf("cannot find source %s: %w", u, err)
	}
	return source, nil
}

func (r *Source) ListNotDeleted(ctx context.Context) ([]domain.Source, error) {
	return r.list(ctx, sourceQuery)
}

func (r *Source) ListActiveOrTrial(ctx context.Context) ([]domain.Source, error) {
	return r.list(ctx, sourceActiveOrTrialQuery)
}

// ExistsBySiteURLOrFeedURL counts deleted blogs too. An empty feedURL matches no feed.
func (r *Source) ExistsBySiteURLOrFeedURL(ctx context.Context, siteURL, feedURL domain.URL) (bool, error) {
	var exists bool
	err := r.db.QueryRowContext(ctx, `SELECT EXISTS (SELECT 1 FROM source WHERE site_url = ? OR feed_url = ?)`, siteURL, feedURL).Scan(&exists)
	if err != nil {
		return false, fmt.Errorf("cannot check source %s: %w", siteURL, err)
	}
	return exists, nil
}

func requireAffected(result sql.Result, what string) error {
	n, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("cannot count rows of %s: %w", what, err)
	}
	if n == 0 {
		return fmt.Errorf("%s: %w", what, domain.ErrNotFound)
	}
	return nil
}
