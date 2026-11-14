package tests_test

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/suite"

	"github.com/sadensmol/blogger/internal/domain"
	"github.com/sadensmol/blogger/internal/feed"
	"github.com/sadensmol/blogger/internal/llm"
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

	historyFeedPath   = "/history.xml"
	robotsPath        = "/robots.txt"
	sitemapPath       = "/sitemap.xml"
	postsSitemapPath  = "/sitemap-posts.xml"
	oldSitemapPath    = "/sitemap-old.xml"
	customSitemapPath = "/custom-sitemap.xml"
	xmlType           = "application/xml"
	textType          = "text/plain"
	datedItemFmt      = `<item><title>%s</title><link>%s</link><pubDate>%s</pubDate></item>`
	sitemapIndexFmt   = `<?xml version="1.0"?><sitemapindex xmlns="http://www.sitemaps.org/schemas/sitemap/0.9">%s</sitemapindex>`
	sitemapChildFmt   = `<sitemap><loc>%s</loc><lastmod>%s</lastmod></sitemap>`
	urlsetFmt         = `<?xml version="1.0"?><urlset xmlns="http://www.sitemaps.org/schemas/sitemap/0.9">%s</urlset>`
	urlFmt            = `<url><loc>%s</loc><lastmod>%s</lastmod></url>`
	pageFmt           = `<!doctype html><html><head>%s</head><body><h1>ignored</h1></body></html>`
	backfillMax       = 200

	blogPagePath   = "/blog"
	newsPagePath   = "/news"
	blogPageFmt    = `<!doctype html><html><head><title>Blog</title>%s</head><body><nav><a href="/about">About</a></nav>%s</body></html>`
	linkFmt        = `<a href="%s">%s</a>`
	articleBodyFmt = `<!doctype html><html><head>%s</head><body>%s</body></html>`
)

type FetchTestSuite struct {
	tests.Suite

	feeds       *tests.FakeFeeds
	agents      *tests.FakeAgents
	renderer    *tests.FakeRenderer
	sources     *testsrepository.Source
	articleRows *testsrepository.Article
	runs        *testsrepository.Run
	articles    *service.Articles
	runsSvc     *service.Runs
}

func TestFetchTestSuite(t *testing.T) {
	suite.Run(t, &FetchTestSuite{})
}

func (s *FetchTestSuite) SetupTest() {
	s.Suite.SetupTest()
	s.feeds = tests.NewFakeFeeds()
	s.sources = testsrepository.NewSource(s.DB)
	s.articleRows = testsrepository.NewArticle(s.DB)
	s.runs = testsrepository.NewRun(s.DB)
	s.agents = tests.NewFakeAgents(s.T())
	s.renderer = tests.NewFakeRenderer()
	sourceSvc := service.NewSource(repository.NewSource(s.DB), nil, s.Clock.Now)
	s.runsSvc = service.NewRuns(repository.NewRun(s.DB), s.Clock.Now)
	backends, err := llm.NewBackends(llm.Config{
		Order:     []llm.Backend{llm.Claude},
		ClaudeBin: s.agents.ClaudeBin,
		RunAs:     llm.RunAs{Home: s.agents.Home},
		AgentDir:  s.T().TempDir(),
		Timeout:   10 * time.Second,
	})
	s.Require().NoError(err)
	s.articles = service.NewArticles(service.ArticlesData{
		Sources:  sourceSvc,
		Repo:     repository.NewArticle(s.DB),
		Verdicts: repository.NewLinkVerdict(s.DB),
		Feeds:    feed.NewClient(5*time.Second, feed.WithLoopbackAllowed(), feed.WithRenderer(s.renderer)),
		LLM:      llm.NewRunner(backends, s.T().TempDir(), llm.RunAs{}),
		Runs:     s.runsSvc,
		History:  tests.HistoryWindow,
		Now:      s.Clock.Now,
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

func (s *FetchTestSuite) insertSource(path string, status domain.SourceStatus) int64 {
	id, err := s.sources.Insert(s.Ctx, testsrepository.SourceRow{FeedURL: s.feeds.URL() + path, Status: string(status)}, s.Clock.Now())
	s.Require().NoError(err)
	return id
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

func (s *FetchTestSuite) TestFetchFeedsIfOlderThanSkipsFreshFetch() {
	s.setFeed(feedAPath, s.feeds.URL()+"/one")
	id := s.insertSource(feedAPath, domain.SourceActive)
	s.Require().NoError(s.sources.SetFetchedAt(s.Ctx, id, s.Clock.Now().Add(-30*time.Minute)))
	_, err := s.runs.Insert(s.Ctx, string(domain.RunKindFetch), string(domain.RunStatusOK), s.Clock.Now().Add(-30*time.Minute))
	s.Require().NoError(err)

	s.Require().NoError(s.articles.FetchFeedsIfOlderThan(s.Ctx, time.Hour))

	s.Equal(0, s.feeds.Requests(feedAPath))
}

func (s *FetchTestSuite) TestFetchFeedsIfOlderThanReadsNeverFetchedBlogDespiteFreshFetch() {
	s.setFeed(feedAPath, s.feeds.URL()+"/one")
	fetchedID := s.insertSource(feedAPath, domain.SourceActive)
	s.Require().NoError(s.sources.SetFetchedAt(s.Ctx, fetchedID, s.Clock.Now().Add(-30*time.Minute)))
	s.setFeed(feedBPath, s.feeds.URL()+"/two")
	s.insertSource(feedBPath, domain.SourceActive)
	_, err := s.runs.Insert(s.Ctx, string(domain.RunKindFetch), string(domain.RunStatusOK), s.Clock.Now().Add(-30*time.Minute))
	s.Require().NoError(err)

	s.Require().NoError(s.articles.FetchFeedsIfOlderThan(s.Ctx, time.Hour))

	s.Equal(0, s.feeds.Requests(feedAPath))
	s.Equal(1, s.feeds.Requests(feedBPath))
	s.Equal(1, s.CountRows("article"))
	s.Require().NoError(s.articles.FetchFeedsIfOlderThan(s.Ctx, time.Hour))
	s.Equal(1, s.feeds.Requests(feedBPath))
}

func (s *FetchTestSuite) TestFetchFeedsIfOlderThanIgnoresUnfinishedFetch() {
	s.setFeed(feedAPath, s.feeds.URL()+"/one")
	id := s.insertSource(feedAPath, domain.SourceActive)
	s.Require().NoError(s.sources.SetFetchedAt(s.Ctx, id, s.Clock.Now().Add(-time.Minute)))
	_, err := s.runs.Insert(s.Ctx, string(domain.RunKindFetch), string(domain.RunStatusRunning), s.Clock.Now().Add(-time.Minute))
	s.Require().NoError(err)

	s.Require().NoError(s.articles.FetchFeedsIfOlderThan(s.Ctx, time.Hour))

	s.Equal(1, s.feeds.Requests(feedAPath))
}

func (s *FetchTestSuite) TestFetchFeedsIfOlderThanCountsItsOwnRunAsFresh() {
	s.setFeed(feedAPath, s.feeds.URL()+"/one")
	s.insertSource(feedAPath, domain.SourceActive)
	s.Require().NoError(s.articles.FetchFeedsIfOlderThan(s.Ctx, time.Hour))
	s.Clock.Advance(30 * time.Minute)

	s.Require().NoError(s.articles.FetchFeedsIfOlderThan(s.Ctx, time.Hour))

	s.Equal(1, s.feeds.Requests(feedAPath))
	result, err := s.runs.LatestByKind(s.Ctx, string(domain.RunKindFetchFeeds))
	s.Require().NoError(err)
	s.Equal(string(domain.RunStatusOK), result.Status)
	_, err = s.runs.LatestByKind(s.Ctx, string(domain.RunKindFetch))
	s.Require().ErrorIs(err, sql.ErrNoRows)
}

func (s *FetchTestSuite) TestFailedFeedWaitsAnHour() {
	s.insertSource(feedAPath, domain.SourceActive)

	_, err := s.articles.FetchAll(s.Ctx)
	s.Require().NoError(err)
	s.Clock.Advance(59 * time.Minute)
	_, err = s.articles.FetchAll(s.Ctx)
	s.Require().NoError(err)
	s.Equal(1, s.feeds.Requests(feedAPath))
	s.Clock.Advance(2 * time.Minute)
	_, err = s.articles.FetchAll(s.Ctx)
	s.Require().NoError(err)

	s.Equal(2, s.feeds.Requests(feedAPath))
}

func (s *FetchTestSuite) TestFetchFeedsIfOlderThanNeverBackfills() {
	s.setFeed(feedAPath, s.feeds.URL()+"/one")
	id := s.insertSource(feedAPath, domain.SourceActive)

	s.Require().NoError(s.articles.FetchFeedsIfOlderThan(s.Ctx, time.Hour))

	s.Equal(1, s.feeds.Requests(feedAPath))
	s.Equal(0, s.feeds.Requests(robotsPath))
	s.Equal(0, s.feeds.Requests(sitemapPath))
	backfilledAt, err := s.sources.BackfilledAt(s.Ctx, id)
	s.Require().NoError(err)
	s.False(backfilledAt.Valid)
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

func (s *FetchTestSuite) insertBlog(feedPath string) int64 {
	id, err := s.sources.Insert(s.Ctx, testsrepository.SourceRow{
		SiteURL: s.feeds.URL(),
		FeedURL: s.feeds.URL() + feedPath,
		Status:  string(domain.SourceActive),
	}, s.Clock.Now())
	s.Require().NoError(err)
	return id
}

func (s *FetchTestSuite) setSitemap(path string, entries ...string) {
	var urls strings.Builder
	for i := 0; i < len(entries); i += 2 {
		fmt.Fprintf(&urls, urlFmt, s.feeds.URL()+entries[i], entries[i+1])
	}
	s.feeds.Set(path, xmlType, fmt.Sprintf(urlsetFmt, urls.String()))
}

func (s *FetchTestSuite) setPage(path, head string) {
	s.feeds.Set(path, htmlType, fmt.Sprintf(pageFmt, head))
}

func (s *FetchTestSuite) stored(sourceID int64) []testsrepository.StoredArticle {
	articles, err := s.articleRows.ListBySource(s.Ctx, sourceID)
	s.Require().NoError(err)
	return articles
}

func (s *FetchTestSuite) storedURLs(sourceID int64) []string {
	var urls []string
	for _, a := range s.stored(sourceID) {
		urls = append(urls, strings.TrimPrefix(a.URL, s.feeds.URL()))
	}
	return urls
}

func (s *FetchTestSuite) rawStoredURLs(sourceID int64) []string {
	var urls []string
	for _, a := range s.stored(sourceID) {
		urls = append(urls, a.URL)
	}
	return urls
}

func (s *FetchTestSuite) fetchAndBackfill() {
	_, err := s.articles.FetchAll(s.Ctx)
	s.Require().NoError(err)
	_, err = s.articles.Backfill(s.Ctx)
	s.Require().NoError(err)
}

func (s *FetchTestSuite) requireBackfilled(sourceID int64) {
	backfilledAt, err := s.sources.BackfilledAt(s.Ctx, sourceID)
	s.Require().NoError(err)
	s.True(backfilledAt.Valid)
}

func (s *FetchTestSuite) TestBackfillsFromNestedSitemapsInsideTheWindow() {
	s.setFeed(historyFeedPath, s.feeds.URL()+"/posts/recent")
	id := s.insertBlog(historyFeedPath)
	s.feeds.Set(sitemapPath, xmlType, fmt.Sprintf(sitemapIndexFmt,
		fmt.Sprintf(sitemapChildFmt, s.feeds.URL()+postsSitemapPath, "2026-10-01")+
			fmt.Sprintf(sitemapChildFmt, s.feeds.URL()+oldSitemapPath, "2024-01-01")))
	s.setSitemap(postsSitemapPath,
		"/posts/new", "2026-09-01T08:00:00Z",
		"/posts/plain", "2026-03-01T10:00:00+00:00",
		"/posts/ancient", "2024-05-01",
		"/tag/go", "2026-09-01",
		"/category/go/page/2", "2026-09-01",
		"/page/2", "2026-09-01",
		"/notes/tag", "2026-02-01",
		"/", "2026-11-01")
	s.setSitemap(oldSitemapPath, "/posts/in-old-sitemap", "2026-09-01")
	s.setPage("/posts/new", `<meta property="og:title" content="New &amp; shiny"><meta property="og:description" content="&lt;p&gt;About &lt;b&gt;Go&lt;/b&gt;.&lt;/p&gt;"><title>Ignored title</title>`)
	s.setPage("/posts/plain", `<title> Plain
 post </title><meta name="description" content="Plain description.">`)
	s.setPage("/posts/ancient", `<title>Ancient</title>`)
	s.setPage("/tag/go", `<title>Tag</title>`)
	s.setPage("/page/2", `<title>Page 2</title>`)
	s.setPage("/notes/tag", `<title>A note about tags</title>`)

	s.fetchAndBackfill()

	s.Equal([]testsrepository.StoredArticle{
		{URL: s.feeds.URL() + "/posts/new", Title: "New & shiny", Excerpt: "About Go.", PublishedAt: sql.NullString{String: "2026-09-01T08:00:00Z", Valid: true}},
		{URL: s.feeds.URL() + "/posts/plain", Title: "Plain post", Excerpt: "Plain description.", PublishedAt: sql.NullString{String: "2026-03-01T10:00:00Z", Valid: true}},
		{URL: s.feeds.URL() + "/notes/tag", Title: "A note about tags", PublishedAt: sql.NullString{String: "2026-02-01T00:00:00Z", Valid: true}},
		{URL: s.feeds.URL() + "/posts/recent", Title: "Item 0"},
	}, s.stored(id))
	s.Equal(0, s.feeds.Requests(oldSitemapPath))
	s.Equal(0, s.feeds.Requests("/posts/ancient"))
	s.Equal(0, s.feeds.Requests("/tag/go"))
	s.Equal(0, s.feeds.Requests("/page/2"))
	s.requireBackfilled(id)
}

func (s *FetchTestSuite) TestBackfillsFromSitemapNamedInRobots() {
	s.setFeed(historyFeedPath)
	id := s.insertBlog(historyFeedPath)
	s.feeds.Set(robotsPath, textType, fmt.Sprintf("User-agent: *\nDisallow:\nSitemap: %s%s\n", s.feeds.URL(), customSitemapPath))
	s.setSitemap(customSitemapPath, "/posts/one", "2026-08-01")
	s.setPage("/posts/one", `<title>One</title><meta property="article:published_time" content="2026-07-30T09:00:00Z">`)

	s.fetchAndBackfill()

	s.Equal([]testsrepository.StoredArticle{
		{URL: s.feeds.URL() + "/posts/one", Title: "One", PublishedAt: sql.NullString{String: "2026-07-30T09:00:00Z", Valid: true}},
	}, s.stored(id))
	s.Equal(0, s.feeds.Requests(sitemapPath))
}

func (s *FetchTestSuite) TestBackfillsAtMostTheCapPerBlog() {
	s.setFeed(historyFeedPath)
	id := s.insertBlog(historyFeedPath)
	var entries []string
	for i := range backfillMax + 5 {
		path := fmt.Sprintf("/posts/%d", i)
		entries = append(entries, path, time.Date(2026, 1, 1, i, 0, 0, 0, time.UTC).Format(time.RFC3339))
		s.setPage(path, fmt.Sprintf("<title>Post %d</title>", i))
	}
	s.setSitemap(sitemapPath, entries...)

	s.fetchAndBackfill()

	s.Len(s.stored(id), backfillMax)
	s.Equal(0, s.feeds.Requests("/posts/0"))
	s.requireBackfilled(id)
}

func (s *FetchTestSuite) TestBackfillsOlderWordPressFeedPagesUntilTheWindowEnds() {
	id := s.insertBlog(historyFeedPath)
	s.setFeed(historyFeedPath, s.feeds.URL()+"/p1")
	dated := func(name, day string) string {
		published, err := time.Parse(time.DateOnly, day)
		s.Require().NoError(err)
		return fmt.Sprintf(datedItemFmt, name, s.feeds.URL()+"/"+name, published.Format(time.RFC1123Z))
	}
	s.feeds.Set(historyFeedPath+"?paged=2", rssType, fmt.Sprintf(itemsFmt, dated("p2a", "2026-06-01")+dated("p2b", "2026-05-01")))
	s.feeds.Set(historyFeedPath+"?paged=3", rssType, fmt.Sprintf(itemsFmt, dated("p3a", "2026-01-01")+dated("p3b", "2025-01-01")))
	s.feeds.Set(historyFeedPath+"?paged=4", rssType, fmt.Sprintf(itemsFmt, dated("p4a", "2025-12-01")))

	s.fetchAndBackfill()

	s.Equal([]string{"/p2a", "/p2b", "/p3a", "/p1"}, s.storedURLs(id))
	s.requireBackfilled(id)
}

func (s *FetchTestSuite) TestBackfillSkipsAnUnreadablePage() {
	s.setFeed(historyFeedPath)
	id := s.insertBlog(historyFeedPath)
	s.setSitemap(sitemapPath, "/posts/missing", "2026-09-01", "/posts/older", "2026-08-01")
	s.setPage("/posts/older", "<title>Older</title>")

	s.fetchAndBackfill()

	s.Equal([]string{"/posts/older"}, s.storedURLs(id))
	s.requireBackfilled(id)
}

func (s *FetchTestSuite) TestBackfillStopsAfterFiveUnreadablePagesInARow() {
	s.setFeed(historyFeedPath)
	id := s.insertBlog(historyFeedPath)
	var entries []string
	for day := 9; day >= 5; day-- {
		entries = append(entries, fmt.Sprintf("/posts/missing-%d", day), fmt.Sprintf("2026-09-%02d", day))
	}
	entries = append(entries, "/posts/older", "2026-08-01")
	s.setSitemap(sitemapPath, entries...)
	s.setPage("/posts/older", "<title>Older</title>")

	s.fetchAndBackfill()

	s.Empty(s.stored(id))
	s.Equal(0, s.feeds.Requests("/posts/older"))
}

func (s *FetchTestSuite) TestBackfillThatFindsNothingIsTriedOnceADayThreeTimes() {
	s.setFeed(historyFeedPath)
	s.insertBlog(historyFeedPath)
	s.setSitemap(sitemapPath)

	s.fetchAndBackfill()
	s.fetchAndBackfill()
	s.Equal(1, s.feeds.Requests(robotsPath))
	for _, want := range []int{2, 3, 3} {
		s.Clock.Advance(25 * time.Hour)
		s.fetchAndBackfill()
		s.Equal(want, s.feeds.Requests(robotsPath))
	}
}

func (s *FetchTestSuite) TestBackfillDoneIsNotRepeated() {
	s.setFeed(historyFeedPath)
	id := s.insertBlog(historyFeedPath)
	s.setSitemap(sitemapPath, "/posts/one", "2026-08-01")
	s.setPage("/posts/one", "<title>One</title>")

	s.fetchAndBackfill()
	s.Clock.Advance(25 * time.Hour)
	s.fetchAndBackfill()

	s.requireBackfilled(id)
	s.Equal(1, s.feeds.Requests(robotsPath))
}

func (s *FetchTestSuite) TestBackfillStoresNoSecondRowForAnotherHostSpellingOfAFeedArticle() {
	wwwURL := strings.Replace(s.feeds.URL(), "http://", "http://www.", 1)
	s.setFeed(historyFeedPath, wwwURL+"/posts/a")
	id := s.insertBlog(historyFeedPath)
	s.setSitemap(sitemapPath, "/posts/a", "2026-08-01")
	s.setPage("/posts/a", "<title>A</title>")

	s.fetchAndBackfill()

	s.Equal([]string{wwwURL + "/posts/a"}, s.rawStoredURLs(id))
	s.Equal(0, s.feeds.Requests("/posts/a"))
}

func (s *FetchTestSuite) TestUndatedFeedItemStaysUndatedUntilTheSitemapDatesIt() {
	s.setFeed(historyFeedPath, s.feeds.URL()+"/posts/undated", s.feeds.URL()+"/posts/never-dated")
	id := s.insertBlog(historyFeedPath)
	s.setSitemap(sitemapPath, "/posts/undated", "2026-04-01T00:00:00Z")

	s.fetchAndBackfill()

	s.Equal([]testsrepository.StoredArticle{
		{URL: s.feeds.URL() + "/posts/undated", Title: "Item 0", PublishedAt: sql.NullString{String: "2026-04-01T00:00:00Z", Valid: true}},
		{URL: s.feeds.URL() + "/posts/never-dated", Title: "Item 1"},
	}, s.stored(id))
}

func (s *FetchTestSuite) insertPageBlog(pagePath string) int64 {
	id, err := s.sources.Insert(s.Ctx, testsrepository.SourceRow{
		SiteURL: s.feeds.URL() + pagePath,
		Status:  string(domain.SourceActive),
	}, s.Clock.Now())
	s.Require().NoError(err)
	return id
}

func (s *FetchTestSuite) setBlogPage(path, head string, hrefs ...string) {
	s.feeds.Set(path, htmlType, s.blogPage(head, hrefs...))
}

func (s *FetchTestSuite) blogPage(head string, hrefs ...string) string {
	var links strings.Builder
	for _, href := range hrefs {
		fmt.Fprintf(&links, linkFmt, href, strings.Trim(href, "/"))
	}
	return fmt.Sprintf(blogPageFmt, head, links.String())
}

func (s *FetchTestSuite) setTitledPages(paths ...string) {
	for _, path := range paths {
		s.setPage(path, fmt.Sprintf("<title>%s</title>", strings.Trim(path, "/")))
	}
}

func (s *FetchTestSuite) TestFetchReadsArticlesFromTheBlogPageAndItsNextPage() {
	id := s.insertPageBlog(blogPagePath)
	s.setBlogPage(blogPagePath, `<link rel="next" href="/blog?page=2">`,
		"/blog/one", "/blog/two", "/blog/tag/go", "/blog/category/news", "/blog/author/ann", "/blog/logo.png", "/blog", "/blog/one#comments")
	s.setBlogPage(blogPagePath+"?page=2", "", "/blog/three", "/blog?page=1")
	s.setTitledPages("/blog/one", "/blog/two", "/blog/three")

	created, err := s.articles.FetchAll(s.Ctx)

	s.Require().NoError(err)
	s.Equal(3, created)
	s.ElementsMatch([]string{"/blog/one", "/blog/two", "/blog/three"}, s.storedURLs(id))
	for _, listing := range []string{"/blog/tag/go", "/blog/category/news", "/blog/author/ann", "/blog/logo.png", "/about"} {
		s.Zero(s.feeds.Requests(listing), listing)
	}
	s.Empty(s.renderer.Rendered())
	s.Empty(s.agents.Args(llm.Claude))
}

func (s *FetchTestSuite) TestFetchStoresAnArticleOfBothTheFeedAndThePageOnce() {
	s.setFeed(feedAPath, s.feeds.URL()+"/blog/one", s.feeds.URL()+"/blog/two")
	id, err := s.sources.Insert(s.Ctx, testsrepository.SourceRow{
		SiteURL: s.feeds.URL() + blogPagePath,
		FeedURL: s.feeds.URL() + feedAPath,
		Status:  string(domain.SourceActive),
	}, s.Clock.Now())
	s.Require().NoError(err)
	s.setBlogPage(blogPagePath, "", "/blog/two", "/blog/three")
	s.setTitledPages("/blog/three")

	created, err := s.articles.FetchAll(s.Ctx)

	s.Require().NoError(err)
	s.Equal(3, created)
	s.ElementsMatch([]string{"/blog/one", "/blog/two", "/blog/three"}, s.storedURLs(id))
	s.Zero(s.feeds.Requests("/blog/two"))
}

func (s *FetchTestSuite) TestFetchReadsPageArticleDetailsFromJSONLDMetaAndTime() {
	id := s.insertPageBlog(blogPagePath)
	s.setBlogPage(blogPagePath, "", "/blog/ld", "/blog/meta", "/blog/time")
	s.feeds.Set("/blog/ld", htmlType, fmt.Sprintf(articleBodyFmt,
		`<title>Site | LD</title><meta name="description" content="LD description.">`,
		`<script type="application/ld+json">{"@context":"https://schema.org","@graph":[{"@type":"WebSite","name":"Site"},`+
			`{"@type":"BlogPosting","headline":"LD headline","datePublished":"2026-10-02T09:30:00.000Z"}]}</script><p>LD body.</p>`))
	s.feeds.Set("/blog/meta", htmlType, fmt.Sprintf(articleBodyFmt,
		`<meta property="og:title" content="Meta title"><meta property="og:description" content="Meta description.">`+
			`<meta property="article:published_time" content="2026-09-03T10:00:00+00:00">`,
		`<time datetime="2020-01-01">old</time><p>Meta body.</p>`))
	s.feeds.Set("/blog/time", htmlType, fmt.Sprintf(articleBodyFmt,
		`<title>Time title</title>`,
		`<header><time datetime="2026-08-04">4 Aug</time></header><p>Sign in</p><p>First <b>real</b> paragraph, long enough to be text.</p><p>Second.</p>`))

	_, err := s.articles.FetchAll(s.Ctx)

	s.Require().NoError(err)
	s.Equal([]testsrepository.StoredArticle{
		{URL: s.feeds.URL() + "/blog/ld", Title: "LD headline", Excerpt: "LD description.", PublishedAt: sql.NullString{String: "2026-10-02T09:30:00Z", Valid: true}},
		{URL: s.feeds.URL() + "/blog/meta", Title: "Meta title", Excerpt: "Meta description.", PublishedAt: sql.NullString{String: "2026-09-03T10:00:00Z", Valid: true}},
		{URL: s.feeds.URL() + "/blog/time", Title: "Time title", Excerpt: "First real paragraph, long enough to be text.", PublishedAt: sql.NullString{String: "2026-08-04T00:00:00Z", Valid: true}},
	}, s.stored(id))
}

func (s *FetchTestSuite) TestFetchRendersABlogPageWhoseHTMLListsNoArticle() {
	id := s.insertPageBlog(blogPagePath)
	s.setBlogPage(blogPagePath, "")
	s.renderer.Set(domain.URL(s.feeds.URL()+blogPagePath), s.blogPage("", "/blog/one", "/blog/two"))
	s.setTitledPages("/blog/one", "/blog/two")

	created, err := s.articles.FetchAll(s.Ctx)

	s.Require().NoError(err)
	s.Equal(2, created)
	s.ElementsMatch([]string{"/blog/one", "/blog/two"}, s.storedURLs(id))
	s.Equal([]domain.URL{domain.URL(s.feeds.URL() + blogPagePath)}, s.renderer.Rendered())
}

func (s *FetchTestSuite) TestFetchNeverRendersThePageOfABlogWhoseFeedGivesItems() {
	s.setFeed(feedAPath, s.feeds.URL()+"/posts/one")
	_, err := s.sources.Insert(s.Ctx, testsrepository.SourceRow{
		SiteURL: s.feeds.URL() + blogPagePath,
		FeedURL: s.feeds.URL() + feedAPath,
		Status:  string(domain.SourceActive),
	}, s.Clock.Now())
	s.Require().NoError(err)
	s.setBlogPage(blogPagePath, "", "/posts/one", "/elsewhere")

	_, err = s.articles.FetchAll(s.Ctx)

	s.Require().NoError(err)
	s.Empty(s.renderer.Rendered())
	s.Empty(s.agents.Args(llm.Claude))
}

func (s *FetchTestSuite) TestFetchAsksTheAgentOnlyAboutNewUnclearLinks() {
	id := s.insertPageBlog(newsPagePath)
	s.setBlogPage(newsPagePath, "", "/posts/x", "/posts/y")
	s.renderer.Set(domain.URL(s.feeds.URL()+newsPagePath), s.blogPage("", "/posts/x", "/posts/y"))
	s.setTitledPages("/posts/x", "/posts/y", "/posts/z")
	s.agents.Answer(llm.Claude, llm.SkillClassifyLinks, map[string]any{"articles": []string{s.feeds.URL() + "/posts/x", s.feeds.URL() + "/posts/y"}})

	_, err := s.articles.FetchAll(s.Ctx)
	s.Require().NoError(err)
	s.Require().ElementsMatch([]string{"/posts/x", "/posts/y"}, s.storedURLs(id))
	s.Require().Len(s.agents.Args(llm.Claude), 1)
	s.Equal(3, s.CountRows("link_verdict"))

	s.setBlogPage(newsPagePath, "", "/posts/z", "/posts/x", "/posts/y")
	s.renderer.Set(domain.URL(s.feeds.URL()+newsPagePath), s.blogPage("", "/posts/z", "/posts/x", "/posts/y"))
	s.agents.Answer(llm.Claude, llm.SkillClassifyLinks, map[string]any{"articles": []string{s.feeds.URL() + "/posts/z"}})
	_, err = s.articles.FetchAll(s.Ctx)
	s.Require().NoError(err)

	s.ElementsMatch([]string{"/posts/x", "/posts/y", "/posts/z"}, s.storedURLs(id))
	s.Require().Len(s.agents.Args(llm.Claude), 2)
	var input struct {
		BlogURL string `json:"blog_url"`
		Links   []struct {
			URL string `json:"url"`
		} `json:"links"`
	}
	s.Require().NoError(json.Unmarshal(s.agents.Input(llm.Claude, llm.SkillClassifyLinks), &input))
	s.Equal(s.feeds.URL()+newsPagePath, input.BlogURL)
	s.Require().Len(input.Links, 1)
	s.Equal(s.feeds.URL()+"/posts/z", input.Links[0].URL)

	_, err = s.articles.FetchAll(s.Ctx)
	s.Require().NoError(err)
	s.Len(s.agents.Args(llm.Claude), 2)
}
