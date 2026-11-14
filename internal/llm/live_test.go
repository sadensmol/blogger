//go:build live

package llm_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/sadensmol/blogger/internal/domain"
	"github.com/sadensmol/blogger/internal/llm"
)

const (
	LiveNotes   = "Go and databases"
	LiveProfile = domain.Profile("## My notes\nGo and databases\n\n## Learned\n")
)

var liveBackends = []llm.Backend{llm.Claude, llm.Codex}

func liveRunner(t *testing.T, backend llm.Backend) *llm.Runner {
	t.Helper()

	agentDir, err := filepath.Abs("../../agent")
	require.NoError(t, err)
	home, err := os.UserHomeDir()
	require.NoError(t, err)

	backends, err := llm.NewBackends(llm.Config{
		Order:     []llm.Backend{backend},
		ClaudeBin: "claude",
		CodexBin:  "codex",
		AgentDir:  agentDir,
		Home:      home,
		Timeout:   10 * time.Minute,
	})
	require.NoError(t, err)
	return llm.NewRunner(backends, t.TempDir())
}

func liveCandidates() []domain.Article {
	published := time.Date(2026, 11, 13, 9, 0, 0, 0, time.UTC)
	return []domain.Article{
		{ID: 1, SourceID: 1, SourceTitle: "Go Notes", URL: "https://go.example.com/sqlite-wal", Title: "Tuning SQLite WAL mode in a Go service", Excerpt: "How we set busy_timeout and WAL checkpoints for a pure-Go SQLite driver.", PublishedAt: published},
		{ID: 2, SourceID: 2, SourceTitle: "DB Weekly", URL: "https://db.example.com/postgres-indexes", Title: "When a Postgres partial index beats a full one", Excerpt: "Three real queries and their plans before and after a partial index.", PublishedAt: published},
		{ID: 3, SourceID: 3, SourceTitle: "Garden Life", URL: "https://garden.example.com/tomatoes", Title: "Ten tips for growing tomatoes on a balcony", Excerpt: "Pots, soil and watering for city tomatoes.", PublishedAt: published},
		{ID: 4, SourceID: 4, SourceTitle: "Style Daily", URL: "https://style.example.com/autumn-coats", Title: "The autumn coats everyone wears this year", Excerpt: "Colours and cuts of the season.", PublishedAt: published},
	}
}

func TestLivePickArticles(t *testing.T) {
	for _, backend := range liveBackends {
		t.Run(string(backend), func(t *testing.T) {
			picks, used, err := liveRunner(t, backend).PickArticles(context.Background(), llm.PickInput{
				Candidates: liveCandidates(),
				Profile:    LiveProfile,
				Max:        2,
			})

			require.NoError(t, err)
			require.Equal(t, backend, used)
			require.LessOrEqual(t, len(picks), 2)
			t.Logf("picks: %+v", picks)
		})
	}
}

func TestLiveUpdateProfileKeepsNotes(t *testing.T) {
	candidates := liveCandidates()
	votes := []domain.VotedArticle{
		{Article: candidates[0], Topics: []string{"go", "sqlite"}, Vote: domain.VoteUp},
		{Article: candidates[2], Topics: []string{"gardening"}, Vote: domain.VoteDown},
	}
	for _, backend := range liveBackends {
		t.Run(string(backend), func(t *testing.T) {
			updated, used, err := liveRunner(t, backend).UpdateProfile(context.Background(), LiveProfile, votes)

			require.NoError(t, err)
			require.Equal(t, backend, used)
			require.Equal(t, LiveNotes, updated.Notes())
			t.Logf("profile:\n%s", updated)
		})
	}
}

func TestLiveInitProfile(t *testing.T) {
	sources := []llm.SourceSummary{
		{
			Source:       domain.Source{ID: 1, SiteURL: "https://go.example.com/", Title: "Go Notes"},
			RecentTitles: []string{"Tuning SQLite WAL mode in a Go service", "Context cancellation in long Go jobs"},
		},
		{
			Source:       domain.Source{ID: 2, SiteURL: "https://db.example.com/", Title: "DB Weekly"},
			RecentTitles: []string{"When a Postgres partial index beats a full one", "Reading EXPLAIN ANALYZE"},
		},
	}
	current := domain.Profile("").WithNotes(LiveNotes)
	for _, backend := range liveBackends {
		t.Run(string(backend), func(t *testing.T) {
			created, used, err := liveRunner(t, backend).InitProfile(context.Background(), current, sources)

			require.NoError(t, err)
			require.Equal(t, backend, used)
			require.Equal(t, LiveNotes, created.Notes())
			require.False(t, created.LearnedEmpty())
			t.Logf("profile:\n%s", created)
		})
	}
}

func TestLiveDiscoverBlogs(t *testing.T) {
	sources := []domain.Source{
		{ID: 1, SiteURL: "https://dave.cheney.net/", FeedURL: "https://dave.cheney.net/feed/atom", Title: "Dave Cheney", Status: domain.SourceActive, Votes: domain.Votes{Up: 6}},
		{ID: 2, SiteURL: "https://use-the-index-luke.com/", FeedURL: "https://use-the-index-luke.com/blog/feed", Title: "Use The Index, Luke", Status: domain.SourceActive, Votes: domain.Votes{Up: 4}},
	}
	for _, backend := range liveBackends {
		t.Run(string(backend), func(t *testing.T) {
			proposals, used, err := liveRunner(t, backend).DiscoverBlogs(context.Background(), sources, 2)

			require.NoError(t, err)
			require.Equal(t, backend, used)
			require.NotEmpty(t, proposals)
			t.Logf("proposals: %+v", proposals)
		})
	}
}
