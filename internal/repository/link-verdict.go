package repository

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/rs/zerolog/log"

	"github.com/sadensmol/blogger/internal/domain"
)

type LinkVerdict struct {
	db *sql.DB
}

func NewLinkVerdict(db *sql.DB) *LinkVerdict {
	return &LinkVerdict{db: db}
}

// ListBySource maps each judged link of a blog to whether it is an article.
func (r *LinkVerdict) ListBySource(ctx context.Context, id domain.SourceID) (map[domain.URL]bool, error) {
	rows, err := r.db.QueryContext(ctx, `SELECT url, article FROM link_verdict WHERE source_id = ?`, id)
	if err != nil {
		return nil, fmt.Errorf("cannot query link verdicts of source %d: %w", id, err)
	}
	defer rows.Close()
	verdicts := map[domain.URL]bool{}
	for rows.Next() {
		var u string
		var article bool
		if err := rows.Scan(&u, &article); err != nil {
			return nil, fmt.Errorf("cannot scan link verdict: %w", err)
		}
		verdicts[domain.URL(u)] = article
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("cannot read link verdicts of source %d: %w", id, err)
	}
	return verdicts, nil
}

func (r *LinkVerdict) CreateIgnoringDuplicates(ctx context.Context, id domain.SourceID, verdicts map[domain.URL]bool, now time.Time) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("cannot begin saving link verdicts of source %d: %w", id, err)
	}
	defer func() {
		if err := tx.Rollback(); err != nil && !errors.Is(err, sql.ErrTxDone) {
			log.Ctx(ctx).Error().Err(err).Msgf("cannot roll back the link verdicts of source %d", id)
		}
	}()
	for u, article := range verdicts {
		if _, err := tx.ExecContext(ctx, `
INSERT INTO link_verdict (source_id, url, article, created_at, updated_at)
VALUES (?, ?, ?, ?, ?)
ON CONFLICT (source_id, url) DO NOTHING`,
			id, u, article, formatTime(now), formatTime(now)); err != nil {
			return fmt.Errorf("cannot create link verdict of %s: %w", u, err)
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("cannot save link verdicts of source %d: %w", id, err)
	}
	return nil
}
