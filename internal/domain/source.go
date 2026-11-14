package domain

import (
	"fmt"
	"time"
)

type SourceID int64

type SourceStatus string

const (
	SourceActive SourceStatus = "active"
	SourceTrial  SourceStatus = "trial"
	SourcePaused SourceStatus = "paused"
)

func ParseSourceStatus(s string) (SourceStatus, error) {
	switch status := SourceStatus(s); status {
	case SourceActive, SourceTrial, SourcePaused:
		return status, nil
	default:
		return "", fmt.Errorf("unknown source status %q", s)
	}
}

type SourceOrigin string

const (
	OriginUser       SourceOrigin = "user"
	OriginDiscovered SourceOrigin = "discovered"
)

type Votes struct{ Up, Down int }

func (v Votes) Net() int {
	return v.Up - v.Down
}

type Source struct {
	ID               SourceID
	SiteURL, FeedURL URL
	Title, Reason    string
	Status           SourceStatus
	Origin           SourceOrigin
	StatusChangedAt  time.Time
	Votes            Votes // all time, for lists
	VotesSinceStatus Votes // since StatusChangedAt, for the status rule
	FetchedAt        time.Time // the last read of the feed that worked
	BackfilledAt     time.Time // when the older articles were collected once
}

func (s Source) NeverFetched() bool {
	return s.FetchedAt.IsZero()
}

func (s Source) Backfilled() bool {
	return !s.BackfilledAt.IsZero()
}

func (s Source) StatusAfterVotes() SourceStatus {
	net := s.VotesSinceStatus.Net()
	switch {
	case s.Status == SourceActive && net <= -3:
		return SourcePaused
	case s.Status == SourceTrial && net >= 2:
		return SourceActive
	case s.Status == SourceTrial && net <= -2:
		return SourcePaused
	default:
		return s.Status
	}
}

// SourceSpec is one import entry.
type SourceSpec struct {
	URL, FeedURL URL
	Title        string
	Status       SourceStatus
}

type SourceChange struct{ Before, After Source }

type SourcesDiff struct {
	Add, Remove []Source
	Change      []SourceChange
}
