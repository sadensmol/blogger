package tests

import (
	"database/sql"
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/slack-go/slack"
	"github.com/stretchr/testify/require"

	"github.com/sadensmol/blogger/internal/feed"
	"github.com/sadensmol/blogger/internal/llm"
	"github.com/sadensmol/blogger/internal/repository"
	"github.com/sadensmol/blogger/internal/service"
	"github.com/sadensmol/blogger/internal/slackpost"
	"github.com/sadensmol/blogger/internal/usecase"
)

const (
	appTimeout    = 5 * time.Second
	agentTimeout  = 10 * time.Second
	HistoryWindow = 365 * 24 * time.Hour
)

type AppData struct {
	DB          *sql.DB
	Now         func() time.Time
	Agents      *FakeAgents
	Backends    []llm.Backend
	SlackURL    string
	ChannelID   string
	ProfilePath string
	Location    *time.Location
	DigestSize  int
}

// App is the serve wiring on fakes: real services and usecases, fake feeds, agents and Slack.
type App struct {
	Sources   *service.Source
	Runs      *service.Runs
	Votes     *service.Votes
	Articles  *service.Articles
	Poster    *slackpost.Poster
	Digest    *usecase.Digest
	Discovery *usecase.Discovery
	Suggest   *usecase.Suggest
}

func NewApp(t *testing.T, d AppData) *App {
	t.Helper()

	feeds := feed.NewClient(appTimeout, feed.WithLoopbackAllowed())
	sources := service.NewSource(repository.NewSource(d.DB), feeds, d.Now)
	runs := service.NewRuns(repository.NewRun(d.DB), d.Now)
	posts := service.NewPosts(repository.NewPost(d.DB), d.Now)
	votes := service.NewVotes(service.VotesData{
		Reactions: repository.NewReaction(d.DB),
		Posts:     posts,
		Sources:   sources,
		Now:       d.Now,
	})
	articles := service.NewArticles(service.ArticlesData{
		Sources: sources,
		Repo:    repository.NewArticle(d.DB),
		Feeds:   feeds,
		Runs:    runs,
		History: HistoryWindow,
		Now:     d.Now,
	})
	backends, err := llm.NewBackends(llm.Config{
		Order:     d.Backends,
		ClaudeBin: d.Agents.ClaudeBin,
		CodexBin:  d.Agents.CodexBin,
		RunAs:     llm.RunAs{Home: d.Agents.Home},
		AgentDir:  t.TempDir(),
		Timeout:   agentTimeout,
	})
	require.NoError(t, err)
	runner := llm.NewRunner(backends, t.TempDir(), llm.RunAs{})
	poster := slackpost.NewPoster(
		slack.New("xoxb-test", slack.OptionAPIURL(fmt.Sprintf("%s/api/", d.SlackURL))),
		&http.Client{Timeout: appTimeout},
		d.ChannelID,
	)

	return &App{
		Sources:  sources,
		Runs:     runs,
		Votes:    votes,
		Articles: articles,
		Poster:   poster,
		Digest: usecase.NewDigest(usecase.DigestData{
			Articles: articles,
			Sources:  sources,
			Votes:    votes,
			Profile:  service.NewProfile(d.ProfilePath, repository.NewProfileVersion(d.DB), d.Now),
			Posts:    posts,
			Runs:     runs,
			LLM:      runner,
			Poster:   poster,
			Size:     d.DigestSize,
			Location: d.Location,
			Now:      d.Now,
		}),
		Discovery: usecase.NewDiscovery(usecase.DiscoveryData{
			Sources: sources,
			Feeds:   feeds,
			Runs:    runs,
			LLM:     runner,
		}),
		Suggest: usecase.NewSuggest(usecase.SuggestData{
			Sources:   sources,
			Proposals: service.NewProposals(repository.NewProposal(d.DB), d.Now),
			Feeds:     feeds,
			Runs:      runs,
			LLM:       runner,
			Poster:    poster,
		}),
	}
}

func (a *App) JobsData(size int) usecase.JobsData {
	return usecase.JobsData{Digest: a.Digest, Discovery: a.Discovery, Suggest: a.Suggest, Size: size}
}
