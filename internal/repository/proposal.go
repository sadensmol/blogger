package repository

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/sadensmol/blogger/internal/domain"
)

type Proposal struct {
	db *sql.DB
}

func NewProposal(db *sql.DB) *Proposal {
	return &Proposal{db: db}
}

func (r *Proposal) Create(ctx context.Context, p domain.Proposal, now time.Time) (domain.ProposalID, error) {
	var id int64
	err := r.db.QueryRowContext(ctx, `
INSERT INTO blog_proposal (run_id, site_url, feed_url, title, about, status, created_at, updated_at)
VALUES (?, ?, ?, ?, ?, ?, ?, ?)
RETURNING id`,
		p.RunID, p.Blog.SiteURL, optionalURL(p.Blog.FeedURL), p.Blog.Title, p.Blog.About, p.Status, formatTime(now), formatTime(now),
	).Scan(&id)
	if err != nil {
		return 0, fmt.Errorf("cannot create proposal of blog %s: %w", p.Blog.SiteURL, err)
	}
	return domain.ProposalID(id), nil
}

func (r *Proposal) UpdateMessageByRunID(ctx context.Context, runID domain.RunID, channelID, ts string, now time.Time) error {
	result, err := r.db.ExecContext(ctx, `UPDATE blog_proposal SET channel_id = ?, message_ts = ?, updated_at = ? WHERE run_id = ?`,
		channelID, ts, formatTime(now), runID)
	if err != nil {
		return fmt.Errorf("cannot set message %s %s of the proposals of run %d: %w", channelID, ts, runID, err)
	}
	return requireAffected(result, fmt.Sprintf("proposals of run %d", runID))
}

func (r *Proposal) UpdateStatus(ctx context.Context, id domain.ProposalID, status domain.ProposalStatus, now time.Time) error {
	result, err := r.db.ExecContext(ctx, `UPDATE blog_proposal SET status = ?, updated_at = ? WHERE id = ?`,
		status, formatTime(now), id)
	if err != nil {
		return fmt.Errorf("cannot set status of proposal %d: %w", id, err)
	}
	return requireAffected(result, fmt.Sprintf("proposal %d", id))
}

func (r *Proposal) DeleteByRunID(ctx context.Context, runID domain.RunID) error {
	result, err := r.db.ExecContext(ctx, `DELETE FROM blog_proposal WHERE run_id = ?`, runID)
	if err != nil {
		return fmt.Errorf("cannot delete the proposals of run %d: %w", runID, err)
	}
	return requireAffected(result, fmt.Sprintf("proposals of run %d", runID))
}

type proposalRow struct {
	id, runID                      int64
	channelID, messageTS, feedURL  sql.NullString
	siteURL, title, about          string
	status                         string
}

type repositoryProposal proposalRow

func (r repositoryProposal) toDomain() (domain.Proposal, error) {
	status, err := domain.ParseProposalStatus(r.status)
	if err != nil {
		return domain.Proposal{}, fmt.Errorf("cannot map proposal %d: %w", r.id, err)
	}
	return domain.Proposal{
		ID:        domain.ProposalID(r.id),
		RunID:     domain.RunID(r.runID),
		ChannelID: r.channelID.String,
		MessageTS: r.messageTS.String,
		Blog: domain.BlogProposal{
			SiteURL: domain.URL(r.siteURL),
			FeedURL: domain.URL(r.feedURL.String),
			Title:   r.title,
			About:   r.about,
		},
		Status: status,
	}, nil
}

func (r *Proposal) FindByID(ctx context.Context, id domain.ProposalID) (domain.Proposal, error) {
	var row proposalRow
	err := r.db.QueryRowContext(ctx, `
SELECT id, run_id, channel_id, message_ts, site_url, feed_url, title, about, status
FROM blog_proposal
WHERE id = ?`, id,
	).Scan(&row.id, &row.runID, &row.channelID, &row.messageTS, &row.siteURL, &row.feedURL, &row.title, &row.about, &row.status)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.Proposal{}, fmt.Errorf("proposal %d: %w", id, domain.ErrNotFound)
	}
	if err != nil {
		return domain.Proposal{}, fmt.Errorf("cannot find proposal %d: %w", id, err)
	}
	return repositoryProposal(row).toDomain()
}

func (r *Proposal) ListByMessage(ctx context.Context, channelID, ts string) ([]domain.Proposal, error) {
	rows, err := r.db.QueryContext(ctx, `
SELECT id, run_id, channel_id, message_ts, site_url, feed_url, title, about, status
FROM blog_proposal
WHERE channel_id = ? AND message_ts = ?
ORDER BY id`, channelID, ts)
	if err != nil {
		return nil, fmt.Errorf("cannot query the proposals of message %s %s: %w", channelID, ts, err)
	}
	defer rows.Close()
	proposals := []domain.Proposal{}
	for rows.Next() {
		var row proposalRow
		if err := rows.Scan(&row.id, &row.runID, &row.channelID, &row.messageTS, &row.siteURL, &row.feedURL, &row.title, &row.about, &row.status); err != nil {
			return nil, fmt.Errorf("cannot scan a proposal of message %s %s: %w", channelID, ts, err)
		}
		p, err := repositoryProposal(row).toDomain()
		if err != nil {
			return nil, err
		}
		proposals = append(proposals, p)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("cannot read the proposals of message %s %s: %w", channelID, ts, err)
	}
	return proposals, nil
}
