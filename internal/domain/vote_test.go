package domain_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/sadensmol/blogger/internal/domain"
)

func TestParseVote(t *testing.T) {
	tests := []struct {
		reaction string
		want     domain.Vote
		wantOK   bool
	}{
		{"+1", domain.VoteUp, true},
		{"thumbsup", domain.VoteUp, true},
		{"+1::skin-tone-3", domain.VoteUp, true},
		{"-1", domain.VoteDown, true},
		{"thumbsdown::skin-tone-6", domain.VoteDown, true},
		{"heart", 0, false},
	}
	for _, tt := range tests {
		t.Run(tt.reaction, func(t *testing.T) {
			got, ok := domain.ParseVote(tt.reaction)
			require.Equal(t, tt.wantOK, ok)
			require.Equal(t, tt.want, got)
		})
	}
}

func TestVoteReaction(t *testing.T) {
	require.Equal(t, "+1", domain.VoteUp.Reaction())
	require.Equal(t, "-1", domain.VoteDown.Reaction())
}
