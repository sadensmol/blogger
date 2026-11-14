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
	SiteURL          URL // the page that lists the blog's posts: the blog's identity
	FeedURL          URL // empty: the blog has no feed
	Title, Reason    string
	Status           SourceStatus
	Origin           SourceOrigin
	StatusChangedAt  time.Time
	Votes            Votes // all time, for lists
	VotesSinceStatus Votes // since StatusChangedAt, for the status rule
	FetchedAt        time.Time // the last fetch of the blog that worked
	FetchFailedAt    time.Time // the last fetch of the blog that failed
	BackfilledAt     time.Time // when older articles were collected
	BackfillTries    int       // backfills that collected nothing
	BackfillTriedAt  time.Time
}

const (
	feedBackoff        = time.Hour
	maxBackfillTries   = 3
	backfillRetryAfter = 24 * time.Hour
)

func (s Source) HasFeed() bool {
	return s.FeedURL != ""
}

func (s Source) NeverFetched() bool {
	return s.FetchedAt.IsZero()
}

// FeedBackedOff reports a blog whose fetch failed within the last hour: it is not read again yet.
func (s Source) FeedBackedOff(now time.Time) bool {
	return !s.FetchFailedAt.IsZero() && now.Sub(s.FetchFailedAt) < feedBackoff
}

// BackfillDue reports a blog whose older articles are not collected yet; a backfill that found nothing
// is tried again a day later, three times at most.
func (s Source) BackfillDue(now time.Time) bool {
	return s.BackfilledAt.IsZero() && s.BackfillTries < maxBackfillTries && now.Sub(s.BackfillTriedAt) >= backfillRetryAfter
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

// SourceSpec is one import entry. URL is the blog page; FeedURL is empty when the entry names no feed.
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
