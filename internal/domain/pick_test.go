package domain_test

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/sadensmol/blogger/internal/domain"
)

func TestValidatePicks(t *testing.T) {
	count := domain.PickCount{Max: 5}
	candidates := []domain.Article{
		{URL: "https://a.example.com/1"}, {URL: "https://a.example.com/2"}, {URL: "https://b.example.com/1"},
	}
	pick := func(url domain.URL) domain.Pick { return domain.Pick{URL: url, Description: "worth reading"} }

	t.Run("valid", func(t *testing.T) {
		require.NoError(t, domain.ValidatePicks(
			[]domain.Pick{pick("https://a.example.com/1"), pick("https://b.example.com/1")}, candidates, count))
	})
	t.Run("zero picks", func(t *testing.T) {
		require.NoError(t, domain.ValidatePicks(nil, candidates, count))
	})
	t.Run("zero picks when at least one is required", func(t *testing.T) {
		err := domain.ValidatePicks(nil, candidates, domain.PickCount{Min: 1, Max: 5})
		require.ErrorIs(t, err, domain.ErrBadOutput)
	})
	t.Run("one pick when at least one is required", func(t *testing.T) {
		err := domain.ValidatePicks([]domain.Pick{pick("https://a.example.com/1")}, candidates, domain.PickCount{Min: 1, Max: 5})
		require.NoError(t, err)
	})
	t.Run("unknown url", func(t *testing.T) {
		err := domain.ValidatePicks([]domain.Pick{pick("https://x.example.com/1")}, candidates, count)
		require.ErrorIs(t, err, domain.ErrBadOutput)
	})
	t.Run("duplicate url", func(t *testing.T) {
		err := domain.ValidatePicks(
			[]domain.Pick{pick("https://a.example.com/1"), pick("https://a.example.com/1")}, candidates, count)
		require.ErrorIs(t, err, domain.ErrBadOutput)
	})
	t.Run("too many picks", func(t *testing.T) {
		picks := make([]domain.Pick, 6)
		for i := range picks {
			picks[i] = pick("https://a.example.com/1")
		}
		require.ErrorIs(t, domain.ValidatePicks(picks, candidates, count), domain.ErrBadOutput)
	})
	t.Run("empty description", func(t *testing.T) {
		err := domain.ValidatePicks([]domain.Pick{{URL: "https://a.example.com/1"}}, candidates, count)
		require.ErrorIs(t, err, domain.ErrBadOutput)
	})
	t.Run("description over 300 characters", func(t *testing.T) {
		long := domain.Pick{URL: "https://a.example.com/1", Description: strings.Repeat("я", 301)}
		require.ErrorIs(t, domain.ValidatePicks([]domain.Pick{long}, candidates, count), domain.ErrBadOutput)
	})
	t.Run("description of 300 characters", func(t *testing.T) {
		ok := domain.Pick{URL: "https://a.example.com/1", Description: strings.Repeat("я", 300)}
		require.NoError(t, domain.ValidatePicks([]domain.Pick{ok}, candidates, count))
	})
}
