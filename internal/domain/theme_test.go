package domain_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/sadensmol/blogger/internal/domain"
)

var scoredThemes = domain.Themes{
	{Tag: "go", Votes: domain.Votes{Up: 3, Down: 1}},
	{Tag: "sqlite", Votes: domain.Votes{Up: 1, Down: 2}},
	{Tag: "rust"},
}

func articleURLs(articles []domain.Article) []domain.URL {
	urls := make([]domain.URL, 0, len(articles))
	for _, a := range articles {
		urls = append(urls, a.URL)
	}
	return urls
}

func TestThemesScoreSumsNetVotesOfTheTags(t *testing.T) {
	score := scoredThemes.Score([]domain.Tag{"go", "sqlite"})

	require.Equal(t, 1, score)
}

func TestThemesScoreCountsAnUnknownTagAsZero(t *testing.T) {
	score := scoredThemes.Score([]domain.Tag{"go", "zig"})

	require.Equal(t, 2, score)
}

func TestThemesRankPutsTheHighestScoreFirst(t *testing.T) {
	articles := []domain.Article{
		{URL: "https://example.com/a", Tags: []domain.Tag{"sqlite"}},
		{URL: "https://example.com/b", Tags: []domain.Tag{"go"}},
		{URL: "https://example.com/c"},
	}

	ranked := scoredThemes.Rank(articles)

	require.Equal(t, []domain.URL{"https://example.com/b", "https://example.com/c", "https://example.com/a"}, articleURLs(ranked))
}

func TestThemesRankKeepsTheOrderOfEqualScores(t *testing.T) {
	articles := []domain.Article{
		{URL: "https://example.com/a", Tags: []domain.Tag{"rust"}},
		{URL: "https://example.com/b"},
		{URL: "https://example.com/c", Tags: []domain.Tag{"go"}},
		{URL: "https://example.com/d", Tags: []domain.Tag{"zig"}},
	}

	ranked := scoredThemes.Rank(articles)

	require.Equal(t, []domain.URL{
		"https://example.com/c", "https://example.com/a", "https://example.com/b", "https://example.com/d",
	}, articleURLs(ranked))
}
