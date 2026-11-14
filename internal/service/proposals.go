package service

import (
	"context"
	"fmt"
	"time"

	"github.com/sadensmol/blogger/internal/domain"
	"github.com/sadensmol/blogger/internal/repository"
)

type Proposals struct {
	repo *repository.Proposal
	now  func() time.Time
}

func NewProposals(repo *repository.Proposal, now func() time.Time) *Proposals {
	return &Proposals{repo: repo, now: now}
}

func (p *Proposals) Create(ctx context.Context, proposal domain.Proposal) (domain.ProposalID, error) {
	id, err := p.repo.Create(ctx, proposal, p.now())
	if err != nil {
		return 0, fmt.Errorf("cannot save proposal: %w", err)
	}
	return id, nil
}

func (p *Proposals) SetMessage(ctx context.Context, runID domain.RunID, channelID, ts string) error {
	if err := p.repo.UpdateMessageByRunID(ctx, runID, channelID, ts, p.now()); err != nil {
		return fmt.Errorf("cannot save the message of the proposals of run %d: %w", runID, err)
	}
	return nil
}

func (p *Proposals) MarkAdded(ctx context.Context, id domain.ProposalID) error {
	if err := p.repo.UpdateStatus(ctx, id, domain.ProposalAdded, p.now()); err != nil {
		return fmt.Errorf("cannot mark proposal %d added: %w", id, err)
	}
	return nil
}

func (p *Proposals) Find(ctx context.Context, id domain.ProposalID) (domain.Proposal, error) {
	proposal, err := p.repo.FindByID(ctx, id)
	if err != nil {
		return domain.Proposal{}, fmt.Errorf("cannot find proposal: %w", err)
	}
	return proposal, nil
}

func (p *Proposals) ListByMessage(ctx context.Context, channelID, ts string) ([]domain.Proposal, error) {
	proposals, err := p.repo.ListByMessage(ctx, channelID, ts)
	if err != nil {
		return nil, fmt.Errorf("cannot list proposals: %w", err)
	}
	return proposals, nil
}
