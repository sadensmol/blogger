package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	"github.com/sadensmol/blogger/internal/domain"
)

type Reaction struct {
	db *sql.DB
}

func NewReaction(db *sql.DB) *Reaction {
	return &Reaction{db: db}
}

type votedArticleRow struct {
	article articleRow
	tags    string
	vote    int
}

type repositoryVotedArticle votedArticleRow

func (r repositoryVotedArticle) toDomain() (domain.VotedArticle, error) {
	article, err := repositoryArticle(r.article).toDomain()
	if err != nil {
		return domain.VotedArticle{}, err
	}
	tags := []domain.Tag{}
	if err := json.Unmarshal([]byte(r.tags), &tags); err != nil {
		return domain.VotedArticle{}, fmt.Errorf("cannot decode tags of article %d: %w", r.article.id, err)
	}
	return domain.VotedArticle{Article: article, Tags: tags, Vote: domain.Vote(r.vote)}, nil
}

func (r *Reaction) Upsert(ctx context.Context, e domain.ReactionEvent, postID domain.PostID, now time.Time) error {
	_, err := r.db.ExecContext(ctx, `
INSERT INTO reaction (post_id, user_id, vote, created_at, updated_at)
VALUES (?, ?, ?, ?, ?)
ON CONFLICT (post_id, user_id, vote) DO UPDATE SET removed_at = NULL, updated_at = excluded.updated_at
WHERE reaction.removed_at IS NOT NULL`,
		postID, e.UserID, e.Vote, formatTime(now), formatTime(now))
	if err != nil {
		return fmt.Errorf("cannot save reaction of user %s on post %d: %w", e.UserID, postID, err)
	}
	return nil
}

func (r *Reaction) MarkRemoved(ctx context.Context, e domain.ReactionEvent, postID domain.PostID, now time.Time) error {
	_, err := r.db.ExecContext(ctx, `
UPDATE reaction SET removed_at = ?, updated_at = ?
WHERE post_id = ? AND user_id = ? AND vote = ? AND removed_at IS NULL`,
		formatTime(now), formatTime(now), postID, e.UserID, e.Vote)
	if err != nil {
		return fmt.Errorf("cannot remove reaction of user %s on post %d: %w", e.UserID, postID, err)
	}
	return nil
}

func (r *Reaction) ExistsChangedAfter(ctx context.Context, after time.Time) (bool, error) {
	var exists bool
	err := r.db.QueryRowContext(ctx, `SELECT EXISTS (SELECT 1 FROM reaction WHERE updated_at > ?)`, formatTime(after)).Scan(&exists)
	if err != nil {
		return false, fmt.Errorf("cannot check reactions changed after %s: %w", formatTime(after), err)
	}
	return exists, nil
}

func (r *Reaction) ListLatestNotRemoved(ctx context.Context, limit int) ([]domain.VotedArticle, error) {
	rows, err := r.db.QueryContext(ctx, `
SELECT a.id, a.source_id, s.title, a.url, a.title, a.excerpt, a.published_at,
	(SELECT json_group_array(t.tag) FROM (SELECT tag FROM article_tag WHERE article_id = a.id ORDER BY tag) t),
	r.vote
FROM reaction r
JOIN post p ON p.id = r.post_id
JOIN article a ON a.id = p.article_id
JOIN source s ON s.id = a.source_id
WHERE r.removed_at IS NULL
ORDER BY r.updated_at DESC, a.id DESC
LIMIT ?`, limit)
	if err != nil {
		return nil, fmt.Errorf("cannot query latest reactions: %w", err)
	}
	defer rows.Close()
	voted := []domain.VotedArticle{}
	for rows.Next() {
		var row votedArticleRow
		if err := rows.Scan(&row.article.id, &row.article.sourceID, &row.article.sourceTitle, &row.article.url,
			&row.article.title, &row.article.excerpt, &row.article.publishedAt, &row.tags, &row.vote); err != nil {
			return nil, fmt.Errorf("cannot scan reaction: %w", err)
		}
		votedArticle, err := repositoryVotedArticle(row).toDomain()
		if err != nil {
			return nil, err
		}
		voted = append(voted, votedArticle)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("cannot read latest reactions: %w", err)
	}
	return voted, nil
}

type themeRow struct {
	tag      string
	up, down int
}

type repositoryTheme themeRow

func (r repositoryTheme) toDomain() domain.Theme {
	return domain.Theme{Tag: domain.Tag(r.tag), Votes: domain.Votes{Up: r.up, Down: r.down}}
}

func (r *Reaction) SumVotesByTagNotRemoved(ctx context.Context) (domain.Themes, error) {
	rows, err := r.db.QueryContext(ctx, `
SELECT t.tag, SUM(r.vote = ?), SUM(r.vote = ?)
FROM reaction r
JOIN post p ON p.id = r.post_id
JOIN article_tag t ON t.article_id = p.article_id
WHERE r.removed_at IS NULL
GROUP BY t.tag
ORDER BY t.tag`, domain.VoteUp, domain.VoteDown)
	if err != nil {
		return nil, fmt.Errorf("cannot query votes by tag: %w", err)
	}
	defer rows.Close()
	themes := domain.Themes{}
	for rows.Next() {
		var row themeRow
		if err := rows.Scan(&row.tag, &row.up, &row.down); err != nil {
			return nil, fmt.Errorf("cannot scan votes of a tag: %w", err)
		}
		themes = append(themes, repositoryTheme(row).toDomain())
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("cannot read votes by tag: %w", err)
	}
	return themes, nil
}
