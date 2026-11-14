package tests_test

import (
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/suite"

	"github.com/sadensmol/blogger/internal/domain"
	"github.com/sadensmol/blogger/internal/feed"
	"github.com/sadensmol/blogger/internal/repository"
	"github.com/sadensmol/blogger/internal/service"
	"github.com/sadensmol/blogger/tests"
	testsrepository "github.com/sadensmol/blogger/tests/repository"
)

const (
	feedAPath      = "/a.xml"
	feedBPath      = "/b.xml"
	pausedFeedPath = "/paused.xml"
	itemsFmt       = `<?xml version="1.0"?><rss version="2.0"><channel><title>T</title><link>http://x</link>%s</channel></rss>`
	itemFmt        = `<item><title>%s</title><link>%s</link></item>`
	firstRunID     = 1
)

type FetchTestSuite struct {
	tests.Suite

	feeds    *tests.FakeFeeds
	sources  *testsrepository.Source
	runs     *testsrepository.Run
	articles *service.Articles
	runsSvc  *service.Runs
}

func TestFetchTestSuite(t *testing.T) {
	suite.Run(t, &FetchTestSuite{})
}

func (s *FetchTestSuite) SetupTest() {
	s.Suite.SetupTest()
	s.feeds = tests.NewFakeFeeds()
	s.sources = testsrepository.NewSource(s.DB)
	s.runs = testsrepository.NewRun(s.DB)
	sourceSvc := service.NewSource(repository.NewSource(s.DB), nil, s.Clock.Now)
	s.runsSvc = service.NewRuns(repository.NewRun(s.DB), s.Clock.Now)
	s.articles = service.NewArticles(service.ArticlesData{
		Sources: sourceSvc,
		Repo:    repository.NewArticle(s.DB),
		Feeds:   feed.NewClient(5*time.Second, feed.WithLoopbackAllowed()),
		Runs:    s.runsSvc,
		Now:     s.Clock.Now,
	})
}

func (s *FetchTestSuite) TearDownTest() {
	s.feeds.Close()
	s.Suite.TearDownTest()
}

func (s *FetchTestSuite) setFeed(path string, links ...string) {
	var items string
	for i, link := range links {
		items += fmt.Sprintf(itemFmt, fmt.Sprintf("Item %d", i), link)
	}
	s.feeds.Set(path, "application/rss+xml", fmt.Sprintf(itemsFmt, items))
}

func (s *FetchTestSuite) insertSource(path string, status domain.SourceStatus) {
	_, err := s.sources.Insert(s.Ctx, testsrepository.SourceRow{FeedURL: s.feeds.URL() + path, Status: string(status)}, s.Clock.Now())
	s.Require().NoError(err)
}

func (s *FetchTestSuite) TestFetchAllStoresNewArticlesOnce() {
	s.setFeed(feedAPath, s.feeds.URL()+"/one", s.feeds.URL()+"/two")
	s.insertSource(feedAPath, domain.SourceActive)

	first, err := s.articles.FetchAll(s.Ctx)
	s.Require().NoError(err)
	second, err := s.articles.FetchAll(s.Ctx)
	s.Require().NoError(err)

	s.Equal(2, first)
	s.Equal(0, second)
	s.Equal(2, s.CountRows("article"))
}

func (s *FetchTestSuite) TestFetchAllWithoutBlogs() {
	n, err := s.articles.FetchAll(s.Ctx)

	s.Require().NoError(err)
	s.Equal(0, n)
}

func (s *FetchTestSuite) TestFetchAllSkipsPausedSources() {
	s.setFeed(pausedFeedPath, s.feeds.URL()+"/one")
	s.insertSource(pausedFeedPath, domain.SourcePaused)

	_, err := s.articles.FetchAll(s.Ctx)

	s.Require().NoError(err)
	s.Equal(0, s.feeds.Requests(pausedFeedPath))
}

func (s *FetchTestSuite) TestFetchAllContinuesWhenOneFeedFails() {
	s.insertSource(feedAPath, domain.SourceActive)
	s.setFeed(feedBPath, s.feeds.URL()+"/one")
	s.insertSource(feedBPath, domain.SourceActive)

	n, err := s.articles.FetchAll(s.Ctx)

	s.Require().NoError(err)
	s.Equal(1, n)
	status, err := s.runs.Status(s.Ctx, firstRunID)
	s.Require().NoError(err)
	s.Equal(string(domain.RunStatusOK), status)
}

func (s *FetchTestSuite) TestFetchIfOlderThanSkipsFreshFetch() {
	s.setFeed(feedAPath, s.feeds.URL()+"/one")
	s.insertSource(feedAPath, domain.SourceActive)
	_, err := s.runs.Insert(s.Ctx, string(domain.RunKindFetch), string(domain.RunStatusOK), s.Clock.Now().Add(-30*time.Minute))
	s.Require().NoError(err)

	s.Require().NoError(s.articles.FetchIfOlderThan(s.Ctx, time.Hour))

	s.Equal(0, s.feeds.Requests(feedAPath))
}

func (s *FetchTestSuite) TestTrackingParamsStoredOnce() {
	s.setFeed(feedAPath, s.feeds.URL()+"/a?utm_source=x", s.feeds.URL()+"/a")
	s.insertSource(feedAPath, domain.SourceActive)

	_, err := s.articles.FetchAll(s.Ctx)

	s.Require().NoError(err)
	s.Equal(1, s.CountRows("article"))
}

func (s *FetchTestSuite) TestFailInterruptedMarksRunningRuns() {
	_, err := s.runs.Insert(s.Ctx, string(domain.RunKindFetch), string(domain.RunStatusRunning), s.Clock.Now())
	s.Require().NoError(err)

	n, err := s.runsSvc.FailInterrupted(s.Ctx)

	s.Require().NoError(err)
	s.Equal(int64(1), n)
}
