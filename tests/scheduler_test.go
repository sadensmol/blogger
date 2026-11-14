package tests_test

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/slack-go/slack"
	"github.com/stretchr/testify/suite"

	"github.com/sadensmol/blogger/internal/domain"
	"github.com/sadensmol/blogger/internal/feed"
	"github.com/sadensmol/blogger/internal/llm"
	"github.com/sadensmol/blogger/internal/repository"
	"github.com/sadensmol/blogger/internal/scheduler"
	"github.com/sadensmol/blogger/internal/service"
	"github.com/sadensmol/blogger/internal/slackpost"
	"github.com/sadensmol/blogger/internal/usecase"
	"github.com/sadensmol/blogger/tests"
	testsrepository "github.com/sadensmol/blogger/tests/repository"
)

const (
	schedulerFeedPath   = "/scheduler.xml"
	schedulerArticle    = "a"
	schedulerZone       = "Asia/Tbilisi"
	schedulerFetchEvery = 3 * time.Hour
	schedulerWait       = 10 * time.Second
	schedulerPoll       = 20 * time.Millisecond
	schedulerHeader     = "Your reading for"
)

type SchedulerTestSuite struct {
	tests.Suite

	feeds     *tests.FakeFeeds
	slack     *tests.FakeSlack
	agents    *tests.FakeAgents
	runs      *testsrepository.Run
	location  *time.Location
	scheduler *scheduler.Scheduler
}

func TestSchedulerTestSuite(t *testing.T) {
	suite.Run(t, &SchedulerTestSuite{})
}

func (s *SchedulerTestSuite) SetupTest() {
	s.Suite.SetupTest()
	s.feeds = tests.NewFakeFeeds()
	s.slack = tests.NewFakeSlack()
	s.agents = tests.NewFakeAgents(s.T())
	s.runs = testsrepository.NewRun(s.DB)
	var err error
	s.location, err = time.LoadLocation(schedulerZone)
	s.Require().NoError(err)

	s.feeds.Set(schedulerFeedPath, "application/rss+xml", fmt.Sprintf(itemsFmt, fmt.Sprintf(itemFmt, schedulerArticle, s.articleURL())))
	_, err = testsrepository.NewSource(s.DB).Insert(s.Ctx, testsrepository.SourceRow{
		FeedURL: fmt.Sprintf("%s%s", s.feeds.URL(), schedulerFeedPath),
		Status:  string(domain.SourceActive),
	}, s.Clock.Now())
	s.Require().NoError(err)
	s.Require().NoError(testsrepository.NewProfileVersion(s.DB).Insert(s.Ctx, string(domain.ProfileInit), s.Clock.Now().Add(-2*time.Hour)))
	s.agents.Answer(llm.Claude, llm.SkillPickArticles, map[string]any{"picks": []map[string]any{{
		"url": s.articleURL(), "description": "A post.", "topics": []string{"go"}, "reason": "Matches Go.",
	}}})
	s.agents.Answer(llm.Claude, llm.SkillDiscoverBlogs, map[string]any{"blogs": []map[string]any{}})

	s.scheduler = s.newScheduler()
}

func (s *SchedulerTestSuite) TearDownTest() {
	s.feeds.Close()
	s.slack.Close()
	s.Suite.TearDownTest()
}

func (s *SchedulerTestSuite) articleURL() string {
	return fmt.Sprintf("%s/%s", s.feeds.URL(), schedulerArticle)
}

func (s *SchedulerTestSuite) newScheduler() *scheduler.Scheduler {
	profilePath := filepath.Join(s.T().TempDir(), "interests.md")
	s.Require().NoError(os.WriteFile(profilePath, fmt.Appendf(nil, digestProfileFmt, digestNotes, digestLearned), 0o644))

	sourceSvc := service.NewSource(repository.NewSource(s.DB), nil, s.Clock.Now)
	runsSvc := service.NewRuns(repository.NewRun(s.DB), s.Clock.Now)
	postsSvc := service.NewPosts(repository.NewPost(s.DB), s.Clock.Now)
	feeds := feed.NewClient(5*time.Second, feed.WithLoopbackAllowed())
	articles := service.NewArticles(service.ArticlesData{
		Sources: sourceSvc,
		Repo:    repository.NewArticle(s.DB),
		Feeds:   feeds,
		Runs:    runsSvc,
		Now:     s.Clock.Now,
	})
	backends, err := llm.NewBackends(llm.Config{
		Order:     []llm.Backend{llm.Claude},
		ClaudeBin: s.agents.ClaudeBin,
		CodexBin:  s.agents.CodexBin,
		Home:      s.agents.Home,
		AgentDir:  s.T().TempDir(),
		Timeout:   10 * time.Second,
	})
	s.Require().NoError(err)
	runner := llm.NewRunner(backends, s.T().TempDir())

	jobs := usecase.NewJobs(usecase.JobsData{
		Digest: usecase.NewDigest(usecase.DigestData{
			Articles: articles,
			Sources:  sourceSvc,
			Votes: service.NewVotes(service.VotesData{
				Reactions: repository.NewReaction(s.DB),
				Posts:     postsSvc,
				Sources:   sourceSvc,
				Now:       s.Clock.Now,
			}),
			Profile:  service.NewProfile(profilePath, repository.NewProfileVersion(s.DB), s.Clock.Now),
			Posts:    postsSvc,
			Runs:     runsSvc,
			LLM:      runner,
			Poster:   slackpost.NewPoster(slack.New("xoxb-test", slack.OptionAPIURL(s.slack.URL+"/api/")), digestChannelID),
			Size:     digestSize,
			Location: s.location,
			Now:      s.Clock.Now,
		}),
		Discovery: usecase.NewDiscovery(usecase.DiscoveryData{
			Sources: sourceSvc,
			Feeds:   feeds,
			Runs:    runsSvc,
			LLM:     runner,
		}),
		Size: 3,
	})
	ctx, cancel := context.WithCancel(s.Ctx)
	done := make(chan struct{})
	go func() {
		defer close(done)
		jobs.Run(ctx)
	}()
	s.T().Cleanup(func() {
		cancel()
		<-done
	})

	return scheduler.New(scheduler.Data{
		Jobs:       jobs,
		Articles:   articles,
		Runs:       runsSvc,
		At:         scheduler.DailyTime{Hour: 10},
		Location:   s.location,
		FetchEvery: schedulerFetchEvery,
		Now:        s.Clock.Now,
	})
}

func (s *SchedulerTestSuite) localTime(hour, minute int) time.Time {
	return time.Date(2026, 11, 14, hour, minute, 0, 0, s.location)
}

func (s *SchedulerTestSuite) runStatus(kind domain.RunKind) string {
	result, err := s.runs.LatestByKind(s.Ctx, string(kind))
	if err != nil {
		return ""
	}
	return result.Status
}

func (s *SchedulerTestSuite) waitForRun(kind domain.RunKind) {
	s.Eventually(func() bool { return s.runStatus(kind) == string(domain.RunStatusOK) }, schedulerWait, schedulerPoll)
}

func (s *SchedulerTestSuite) morningRuns() int {
	var n int
	s.Require().NoError(s.DB.QueryRowContext(s.Ctx, "SELECT COUNT(*) FROM run WHERE kind = ?",
		string(domain.RunKindDigestMorning)).Scan(&n))
	return n
}

func (s *SchedulerTestSuite) headers() int {
	var n int
	for _, text := range s.slack.Posted() {
		if strings.HasPrefix(text, schedulerHeader) {
			n++
		}
	}
	return n
}

func (s *SchedulerTestSuite) TestTickRunsMorningDigestOnce() {
	s.Clock.Set(s.localTime(10, 1))

	s.scheduler.Tick(s.Ctx)
	s.scheduler.Tick(s.Ctx)
	s.waitForRun(domain.RunKindDigestMorning)
	s.scheduler.Tick(s.Ctx)

	posted := s.slack.Posted()
	s.Require().Len(posted, 2)
	s.True(strings.HasPrefix(posted[0], schedulerHeader))
	s.Contains(posted[1], s.articleURL())
	s.Equal(1, s.morningRuns())
}

func (s *SchedulerTestSuite) TestTickAfterLateStartCatchesUp() {
	s.Clock.Set(s.localTime(11, 30))

	s.scheduler.Tick(s.Ctx)
	s.waitForRun(domain.RunKindDigestMorning)

	s.Equal(1, s.headers())
}

func (s *SchedulerTestSuite) TestTickWithWrongClockDoesNothing() {
	s.Clock.Set(time.Date(1970, 1, 1, 10, 0, 0, 0, s.location))

	s.scheduler.Tick(s.Ctx)

	s.Equal(0, s.CountRows("run"))
	s.Empty(s.slack.Posted())
}

func (s *SchedulerTestSuite) TestTickRunsDiscoveryAfterMorningWhenWeekPassed() {
	s.Clock.Set(s.localTime(10, 1))
	_, err := s.runs.Insert(s.Ctx, string(domain.RunKindDiscovery), string(domain.RunStatusOK), s.Clock.Now().Add(-8*24*time.Hour))
	s.Require().NoError(err)

	s.scheduler.Tick(s.Ctx)
	s.waitForRun(domain.RunKindDigestMorning)
	s.Eventually(func() bool {
		morningID, morningErr := s.runs.LatestID(s.Ctx, string(domain.RunKindDigestMorning))
		discoveryID, discoveryErr := s.runs.LatestID(s.Ctx, string(domain.RunKindDiscovery))
		return morningErr == nil && discoveryErr == nil && discoveryID > morningID &&
			s.runStatus(domain.RunKindDiscovery) == string(domain.RunStatusOK)
	}, schedulerWait, schedulerPoll)
}

func (s *SchedulerTestSuite) TestTickFetchesEveryThreeHours() {
	s.Clock.Set(s.localTime(20, 0))
	_, err := s.runs.Insert(s.Ctx, string(domain.RunKindFetch), string(domain.RunStatusOK), s.Clock.Now().Add(-schedulerFetchEvery))
	s.Require().NoError(err)
	_, err = s.runs.Insert(s.Ctx, string(domain.RunKindDiscovery), string(domain.RunStatusOK), s.Clock.Now())
	s.Require().NoError(err)

	s.scheduler.Tick(s.Ctx)

	s.Equal(1, s.feeds.Requests(schedulerFeedPath))
}
