package testsrepository

import (
	"context"
	"database/sql"
)

type Proposal struct {
	db *sql.DB
}

func NewProposal(db *sql.DB) *Proposal {
	return &Proposal{db: db}
}

type ProposalRow struct {
	ID        int64
	FeedURL   string
	Title     string
	About     string
	Status    string
	MessageTS string
}

func (r *Proposal) List(ctx context.Context) ([]ProposalRow, error) {
	rows, err := r.db.QueryContext(ctx, `SELECT id, feed_url, title, about, status, COALESCE(message_ts, '') FROM blog_proposal ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var proposals []ProposalRow
	for rows.Next() {
		var p ProposalRow
		if err := rows.Scan(&p.ID, &p.FeedURL, &p.Title, &p.About, &p.Status, &p.MessageTS); err != nil {
			return nil, err
		}
		proposals = append(proposals, p)
	}
	return proposals, rows.Err()
}
