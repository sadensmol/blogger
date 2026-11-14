package domain_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/sadensmol/blogger/internal/domain"
)

const currentProfile = domain.Profile("## My notes\nGo, databases\n\n## Learned\n- rust\n")

func TestWithNotesFrom(t *testing.T) {
	t.Run("changed notes are restored", func(t *testing.T) {
		llm := domain.Profile("## My notes\nsomething else\n\n## Learned\n- rust\n- sqlite\n")

		got, err := llm.WithNotesFrom(currentProfile)

		require.NoError(t, err)
		require.Equal(t, currentProfile.Notes(), got.Notes())
		require.Contains(t, string(got), "- sqlite")
	})
	t.Run("dropped notes come back before learned", func(t *testing.T) {
		llm := domain.Profile("## Learned\n- rust\n")

		got, err := llm.WithNotesFrom(currentProfile)

		require.NoError(t, err)
		require.Equal(t, domain.Profile("## My notes\nGo, databases\n\n## Learned\n- rust\n"), got)
	})
	t.Run("output without learned is bad", func(t *testing.T) {
		_, err := domain.Profile("## My notes\nGo\n").WithNotesFrom(currentProfile)
		require.ErrorIs(t, err, domain.ErrBadOutput)
	})
}

func TestWithNotesOnEmptyProfile(t *testing.T) {
	got := domain.Profile("").WithNotes("Go")

	require.Equal(t, "Go", got.Notes())
	require.True(t, got.LearnedEmpty())
}

func TestLearnedEmpty(t *testing.T) {
	require.True(t, domain.Profile("## My notes\nGo\n\n## Learned\n\n").LearnedEmpty())
	require.False(t, domain.Profile("## My notes\nGo\n\n## Learned\n- databases\n").LearnedEmpty())
}
