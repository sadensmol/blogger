package tests_test

import (
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/suite"

	"github.com/sadensmol/blogger/internal/domain"
	"github.com/sadensmol/blogger/internal/feed"
	"github.com/sadensmol/blogger/internal/llm"
	"github.com/sadensmol/blogger/internal/repository"
	"github.com/sadensmol/blogger/internal/service"
	"github.com/sadensmol/blogger/internal/usecase"
	"github.com/sadensmol/blogger/tests"
	testsrepository "github.com/sadensmol/blogger/tests/repository"
)

const (
	knownFeedPath = "/known.xml"
	goodFeedFmt   = "/good-%d.xml"
	deadFeedPath  = "/dead.xml"
	emptyFeedPath = "/empty.xml"
	goodReason    = "Writes about Go."
	maxAdded      = 3
)

type DiscoveryTestSuite struct {
	tests.Suite

	feeds   *tests.FakeFeeds
	agents  *tests.FakeAgents
	sources *testsrepository.Source
	runs    *testsrepository.Run
}

func TestDiscoveryTestSuite(t *testing.T) {
	suite.Run(t, &DiscoveryTestSuite{})
}

func (s *DiscoveryTestSuite) SetupTest() {
	s.Suite.SetupTest()
	s.feeds = tests.NewFakeFeeds()
	s.agents = tests.NewFakeAgents(s.T())
	s.sources = testsrepository.NewSource(s.DB)
	s.runs = testsrepository.NewRun(s.DB)
}

func (s *DiscoveryTestSuite) TearDownTest() {
	s.feeds.Close()
	s.Suite.TearDownTest()
}

func (s *DiscoveryTestSuite) discovery(order ...llm.Backend) *usecase.Discovery {
	backends, err := llm.NewBackends(llm.Config{
		Order:      order,
		ClaudeBin:  s.agents.ClaudeBin,
		CodexBin:   s.agents.CodexBin,
		LocalModel: "qwen3:4b",
		Home:       s.agents.Home,
		AgentDir:   s.T().TempDir(),
		Timeout:    10 * time.Second,
	})
	s.Require().NoError(err)
	return usecase.NewDiscovery(usecase.DiscoveryData{
		Sources: service.NewSource(repository.NewSource(s.DB), nil, s.Clock.Now),
		Feeds:   feed.NewClient(5*time.Second, feed.WithLoopbackAllowed()),
		Runs:    service.NewRuns(repository.NewRun(s.DB), s.Clock.Now),
		LLM:     llm.NewRunner(backends, s.T().TempDir()),
	})
}

func (s *DiscoveryTestSuite) feedURL(path string) string {
	return fmt.Sprintf("%s%s", s.feeds.URL(), path)
}

func (s *DiscoveryTestSuite) setFeed(path string) {
	s.feeds.Set(path, "application/rss+xml", fmt.Sprintf(itemsFmt, fmt.Sprintf(itemFmt, "Post", s.feedURL("/post"))))
}

func (s *DiscoveryTestSuite) insertBlog(path string) int64 {
	id, err := s.sources.Insert(s.Ctx, testsrepository.SourceRow{FeedURL: s.feedURL(path), Status: string(domain.SourceActive)}, s.Clock.Now())
	s.Require().NoError(err)
	return id
}

func (s *DiscoveryTestSuite) answer(paths ...string) {
	blogs := make([]map[string]any, 0, len(paths))
	for _, path := range paths {
		blogs = append(blogs, map[string]any{
			"site_url": s.feedURL(path), "feed_url": s.feedURL(path), "title": path, "reason": goodReason,
		})
	}
	s.agents.Answer(llm.Claude, llm.SkillDiscoverBlogs, map[string]any{"blogs": blogs})
}

func (s *DiscoveryTestSuite) TestDiscoveryAddsParsingFeedsAsTrial() {
	s.insertBlog(knownFeedPath)
	s.setFeed(knownFeedPath)
	good := fmt.Sprintf(goodFeedFmt, 1)
	s.setFeed(good)
	s.answer(good, deadFeedPath, knownFeedPath)

	added, err := s.discovery(llm.Claude).Run(s.Ctx)

	s.Require().NoError(err)
	s.Equal(1, added)
	s.Equal(2, s.CountRows("source"))
	detail, err := s.sources.Detail(s.Ctx, s.feedURL(good))
	s.Require().NoError(err)
	s.Equal(testsrepository.SourceDetail{Status: string(domain.SourceTrial), Origin: string(domain.OriginDiscovered), Reason: goodReason}, detail)
	result, err := s.runs.LatestByKind(s.Ctx, string(domain.RunKindDiscovery))
	s.Require().NoError(err)
	s.Equal(testsrepository.RunResult{Status: string(domain.RunStatusOK), Backend: string(llm.Claude)}, result)
}

func (s *DiscoveryTestSuite) TestDiscoverySkipsFeedWithoutItems() {
	s.insertBlog(knownFeedPath)
	s.feeds.Set(emptyFeedPath, "application/rss+xml", fmt.Sprintf(itemsFmt, ""))
	s.answer(emptyFeedPath)

	added, err := s.discovery(llm.Claude).Run(s.Ctx)

	s.Require().NoError(err)
	s.Zero(added)
	s.Equal(1, s.CountRows("source"))
}

func (s *DiscoveryTestSuite) TestDiscoverySkipsRemovedBlog() {
	s.setFeed(knownFeedPath)
	id := s.insertBlog(knownFeedPath)
	s.Require().NoError(s.sources.MarkDeleted(s.Ctx, id, s.Clock.Now()))
	other := s.insertBlog("/other.xml")
	s.Require().NotZero(other)
	s.answer(knownFeedPath)

	added, err := s.discovery(llm.Claude).Run(s.Ctx)

	s.Require().NoError(err)
	s.Zero(added)
	s.Equal(2, s.CountRows("source"))
}

func (s *DiscoveryTestSuite) TestDiscoveryAddsAtMostThree() {
	s.insertBlog(knownFeedPath)
	paths := make([]string, 0, 5)
	for i := range 5 {
		path := fmt.Sprintf(goodFeedFmt, i)
		s.setFeed(path)
		paths = append(paths, path)
	}
	s.answer(paths...)

	added, err := s.discovery(llm.Claude).Run(s.Ctx)

	s.Require().NoError(err)
	s.Equal(maxAdded, added)
	s.Equal(1+maxAdded, s.CountRows("source"))
}

func (s *DiscoveryTestSuite) TestDiscoveryNeverUsesCodexOSS() {
	s.insertBlog(knownFeedPath)
	s.agents.Answer(llm.CodexOSS, llm.SkillDiscoverBlogs, map[string]any{"blogs": []map[string]any{}})

	added, err := s.discovery(llm.CodexOSS).Run(s.Ctx)

	s.Require().ErrorIs(err, llm.ErrAllBackendsFailed)
	s.Zero(added)
	s.Empty(s.agents.Args(llm.CodexOSS))
}

func (s *DiscoveryTestSuite) TestDiscoveryWithoutBlogsDoesNothing() {
	added, err := s.discovery(llm.Claude).Run(s.Ctx)

	s.Require().NoError(err)
	s.Zero(added)
	s.Empty(s.agents.Args(llm.Claude))
	result, err := s.runs.LatestByKind(s.Ctx, string(domain.RunKindDiscovery))
	s.Require().NoError(err)
	s.Equal(string(domain.RunStatusOK), result.Status)
}
