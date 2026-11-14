package domain_test

import (
	"testing"

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
