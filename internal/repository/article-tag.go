package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/rs/zerolog/log"

	"github.com/sadensmol/blogger/internal/domain"
)

// ListTagsByArticleIDs maps each given article that has tags to its tags, in tag order.
func (r *Article) ListTagsByArticleIDs(ctx context.Context, ids []domain.ArticleID) (map[domain.ArticleID][]domain.Tag, error) {
	idsJSON, err := json.Marshal(ids)
	if err != nil {
		return nil, fmt.Errorf("cannot encode article ids: %w", err)
	}
	rows, err := r.db.QueryContext(ctx, `
SELECT article_id, tag
FROM article_tag
WHERE article_id IN (SELECT value FROM json_each(?))
ORDER BY article_id, tag`, string(idsJSON))
	if err != nil {
		return nil, fmt.Errorf("cannot query article tags: %w", err)
	}
	defer rows.Close()
	tags := map[domain.ArticleID][]domain.Tag{}
	for rows.Next() {
		var id int64
		var tag string
		if err := rows.Scan(&id, &tag); err != nil {
			return nil, fmt.Errorf("cannot scan article tag: %w", err)
		}
		tags[domain.ArticleID(id)] = append(tags[domain.ArticleID(id)], domain.Tag(tag))
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("cannot read article tags: %w", err)
	}
	return tags, nil
}

func (r *Article) CreateTagsIgnoringDuplicates(ctx context.Context, tags map[domain.ArticleID][]domain.Tag, now time.Time) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("cannot begin saving article tags: %w", err)
	}
	defer func() {
		if err := tx.Rollback(); err != nil && !errors.Is(err, sql.ErrTxDone) {
			log.Ctx(ctx).Error().Err(err).Msgf("cannot roll back the tags of %d articles", len(tags))
		}
	}()
	for id, articleTags := range tags {
		for _, tag := range articleTags {
			if _, err := tx.ExecContext(ctx, `
INSERT INTO article_tag (article_id, tag, created_at, updated_at)
VALUES (?, ?, ?, ?)
ON CONFLICT (article_id, tag) DO NOTHING`,
				id, tag, formatTime(now), formatTime(now)); err != nil {
				return fmt.Errorf("cannot create tag %q of article %d: %w", tag, id, err)
			}
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("cannot save article tags: %w", err)
	}
	return nil
}

// ListTagsByUse lists the distinct tags, the most used first.
func (r *Article) ListTagsByUse(ctx context.Context, limit int) ([]domain.Tag, error) {
	rows, err := r.db.QueryContext(ctx, `
SELECT tag
FROM article_tag
GROUP BY tag
ORDER BY COUNT(*) DESC, tag
LIMIT ?`, limit)
	if err != nil {
		return nil, fmt.Errorf("cannot query tags by use: %w", err)
	}
	defer rows.Close()
	tags := []domain.Tag{}
	for rows.Next() {
		var tag string
		if err := rows.Scan(&tag); err != nil {
			return nil, fmt.Errorf("cannot scan tag: %w", err)
		}
		tags = append(tags, domain.Tag(tag))
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("cannot read tags by use: %w", err)
	}
	return tags, nil
}
