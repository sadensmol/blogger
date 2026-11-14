package domain

import "fmt"

type ProposalID int64

type ProposalStatus string

const (
	ProposalProposed ProposalStatus = "proposed"
	ProposalAdded    ProposalStatus = "added"
)

func ParseProposalStatus(s string) (ProposalStatus, error) {
	switch status := ProposalStatus(s); status {
	case ProposalProposed, ProposalAdded:
		return status, nil
	default:
		return "", fmt.Errorf("unknown proposal status %q", s)
	}
}

// Proposal is a blog shown with an add button in a /discover message.
type Proposal struct {
	ID        ProposalID
	RunID     RunID
	ChannelID string
	MessageTS string
	Blog      BlogProposal
	Status    ProposalStatus
}
