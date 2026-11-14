package domain_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/sadensmol/blogger/internal/domain"
)

func TestStatusAfterVotes(t *testing.T) {
	tests := []struct {
		name     string
		status   domain.SourceStatus
		up, down int
		want     domain.SourceStatus
	}{
		{"active at -2 stays", domain.SourceActive, 0, 2, domain.SourceActive},
		{"active at -3 pauses", domain.SourceActive, 0, 3, domain.SourcePaused},
		{"trial at +2 activates", domain.SourceTrial, 2, 0, domain.SourceActive},
		{"trial at -2 pauses", domain.SourceTrial, 0, 2, domain.SourcePaused},
		{"trial at +1 stays", domain.SourceTrial, 1, 0, domain.SourceTrial},
		{"paused stays", domain.SourcePaused, 5, 0, domain.SourcePaused},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			source := domain.Source{Status: tt.status, VotesSinceStatus: domain.Votes{Up: tt.up, Down: tt.down}}
			require.Equal(t, tt.want, source.StatusAfterVotes())
		})
	}
}

func TestParseSourceStatus(t *testing.T) {
	got, err := domain.ParseSourceStatus("trial")
	require.NoError(t, err)
	require.Equal(t, domain.SourceTrial, got)

	_, err = domain.ParseSourceStatus("deleted")
	require.Error(t, err)
}

func TestFeedBackedOff(t *testing.T) {
	now := time.Date(2026, 11, 14, 12, 0, 0, 0, time.UTC)
	tests := []struct {
		name     string
		failedAt time.Time
		want     bool
	}{
		{name: "never failed", want: false},
		{name: "failed 59 minutes ago", failedAt: now.Add(-59 * time.Minute), want: true},
		{name: "failed an hour ago", failedAt: now.Add(-time.Hour), want: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.want, domain.Source{FetchFailedAt: tt.failedAt}.FeedBackedOff(now))
		})
	}
}

func TestBackfillDue(t *testing.T) {
	now := time.Date(2026, 11, 14, 12, 0, 0, 0, time.UTC)
	tests := []struct {
		name   string
		source domain.Source
		want   bool
	}{
		{name: "never tried", want: true},
		{name: "done", source: domain.Source{BackfilledAt: now.Add(-48 * time.Hour)}, want: false},
		{name: "found nothing 23 hours ago", source: domain.Source{BackfillTries: 1, BackfillTriedAt: now.Add(-23 * time.Hour)}, want: false},
		{name: "found nothing a day ago", source: domain.Source{BackfillTries: 2, BackfillTriedAt: now.Add(-24 * time.Hour)}, want: true},
		{name: "found nothing three times", source: domain.Source{BackfillTries: 3, BackfillTriedAt: now.Add(-48 * time.Hour)}, want: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.want, tt.source.BackfillDue(now))
		})
	}
}
