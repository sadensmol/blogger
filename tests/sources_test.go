package tests_test

import (
	"bytes"
	"fmt"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/suite"
	"go.yaml.in/yaml/v3"

	controllercli "github.com/sadensmol/blogger/internal/controller/cli"
	"github.com/sadensmol/blogger/internal/domain"
	"github.com/sadensmol/blogger/internal/feed"
	"github.com/sadensmol/blogger/internal/repository"
	"github.com/sadensmol/blogger/internal/service"
	"github.com/sadensmol/blogger/tests"
	testsrepository "github.com/sadensmol/blogger/tests/repository"
)

const (
	blogPath   = "/blog"
	feedPath   = "/feed.xml"
	blogTitle  = "Example"
	pageHTML   = `<html><head><link rel="alternate" type="application/rss+xml" href="/feed.xml"></head></html>`
	feedXMLFmt = `<?xml version="1.0"?><rss version="2.0"><channel><title>%s</title><link>%s</link><item><title>One</title><link>%s/one</link></item></channel></rss>`
	htmlType   = "text/html"
	rssType    = "application/rss+xml"
)

type SourcesTestSuite struct {
	tests.Suite

	feeds     *tests.FakeFeeds
	sources   *testsrepository.Source
	articles  *testsrepository.Article
	runs      *testsrepository.Run
	posts     *testsrepository.Post
	reactions *testsrepository.Reaction
	svc       *service.Source
}

func TestSourcesTestSuite(t *testing.T) {
	suite.Run(t, &SourcesTestSuite{})
}

func (s *SourcesTestSuite) SetupTest() {
	s.Suite.SetupTest()
	s.feeds = tests.NewFakeFeeds()
	s.feeds.Set(blogPath, htmlType, pageHTML)
	s.feeds.Set(feedPath, rssType, fmt.Sprintf(feedXMLFmt, blogTitle, s.feeds.URL()+blogPath, s.feeds.URL()))
	s.sources = testsrepository.NewSource(s.DB)
	s.articles = testsrepository.NewArticle(s.DB)
	s.runs = testsrepository.NewRun(s.DB)
	s.posts = testsrepository.NewPost(s.DB)
	s.reactions = testsrepository.NewReaction(s.DB)
	s.svc = service.NewSource(repository.NewSource(s.DB), feed.NewClient(5*time.Second, feed.WithLoopbackAllowed()), s.Clock.Now)
}

func (s *SourcesTestSuite) TearDownTest() {
	s.feeds.Close()
	s.Suite.TearDownTest()
}

func (s *SourcesTestSuite) insertSource(feedURL, title string, status domain.SourceStatus) int64 {
	id, err := s.sources.Insert(s.Ctx, testsrepository.SourceRow{FeedURL: feedURL, Title: title, Status: string(status)}, s.Clock.Now())
	s.Require().NoError(err)
	return id
}

func (s *SourcesTestSuite) insertUpVotes(sourceID int64, users ...string) {
	articleID, err := s.articles.Insert(s.Ctx, testsrepository.ArticleRow{
		SourceID: sourceID, URL: fmt.Sprintf("%s/article-%d", s.feeds.URL(), sourceID), PublishedAt: s.Clock.Now(), Now: s.Clock.Now(),
	})
	s.Require().NoError(err)
	runID, err := s.runs.Insert(s.Ctx, string(domain.RunKindFetch), string(domain.RunStatusOK), s.Clock.Now())
	s.Require().NoError(err)
	postID, err := s.posts.Insert(s.Ctx, articleID, runID, s.Clock.Now())
	s.Require().NoError(err)
	for _, user := range users {
		s.Require().NoError(s.reactions.Insert(s.Ctx, testsrepository.ReactionRow{
			PostID: postID, UserID: user, Vote: int(domain.VoteUp), CreatedAt: s.Clock.Now(),
		}))
	}
}

func (s *SourcesTestSuite) runCLI(stdin string, args ...string) (string, error) {
	var out bytes.Buffer
	err := controllercli.Sources(s.Ctx, controllercli.SourcesData{
		Svc: s.svc, Args: args, In: strings.NewReader(stdin), Out: &out,
	})
	return out.String(), err
}

func (s *SourcesTestSuite) TestAddResolvesFeedAndStoresActiveUserSource() {
	src, err := s.svc.Add(s.Ctx, s.feeds.URL()+blogPath)

	s.Require().NoError(err)
	s.Equal(domain.SourceActive, src.Status)
	s.Equal(domain.OriginUser, src.Origin)
	s.Equal(domain.URL(s.feeds.URL()+feedPath), src.FeedURL)
	s.Equal(blogTitle, src.Title)
}

func (s *SourcesTestSuite) TestAddKnownFeedFails() {
	s.insertSource(s.feeds.URL()+feedPath, blogTitle, domain.SourceActive)

	_, err := s.svc.Add(s.Ctx, s.feeds.URL()+blogPath)

	s.ErrorIs(err, domain.ErrAlreadyExists)
}

func (s *SourcesTestSuite) TestAddPageWithoutFeedFails() {
	const barePath = "/bare"
	bare := tests.NewFakeFeeds()
	defer bare.Close()
	bare.Set(barePath, htmlType, "<html></html>")

	_, err := s.svc.Add(s.Ctx, bare.URL()+barePath)

	s.ErrorIs(err, domain.ErrNotFound)
	s.Equal(0, s.CountRows("source"))
}

func (s *SourcesTestSuite) TestRemoveThenAddAgainKeepsID() {
	id := s.insertSource(s.feeds.URL()+feedPath, blogTitle, domain.SourceActive)

	_, err := s.svc.Remove(s.Ctx, strconv.FormatInt(id, 10))
	s.Require().NoError(err)
	again, err := s.svc.Add(s.Ctx, s.feeds.URL()+blogPath)

	s.Require().NoError(err)
	s.Equal(domain.SourceID(id), again.ID)
}

func (s *SourcesTestSuite) TestPauseAndResumeByURL() {
	feedURL := s.feeds.URL() + feedPath
	s.insertSource(feedURL, blogTitle, domain.SourceActive)

	s.Clock.Advance(time.Hour)
	paused, err := s.svc.Pause(s.Ctx, feedURL)
	s.Require().NoError(err)
	s.Equal(domain.SourcePaused, paused.Status)
	s.Equal(s.Clock.Now(), paused.StatusChangedAt)

	s.Clock.Advance(time.Hour)
	resumed, err := s.svc.Resume(s.Ctx, feedURL)
	s.Require().NoError(err)
	s.Equal(domain.SourceActive, resumed.Status)
	s.Equal(s.Clock.Now(), resumed.StatusChangedAt)
}

func (s *SourcesTestSuite) TestImportDryRunChangesNothing() {
	feedA, feedB, feedC := s.feeds.URL()+"/a.xml", s.feeds.URL()+"/b.xml", s.feeds.URL()+"/c.xml"
	idA := s.insertSource(feedA, "A", domain.SourceActive)
	idB := s.insertSource(feedB, "B", domain.SourceActive)
	file := fmt.Sprintf("sources:\n  - feed: %s\n    status: paused\n  - feed: %s\n", feedA, feedC)

	out, err := s.runCLI(file, "import")

	s.Require().NoError(err)
	s.Contains(out, "~ change")
	s.Contains(out, "+ add")
	s.Contains(out, "- remove")
	s.Contains(out, "3 changes.")
	s.Equal(2, s.CountRows("source"))
	for _, id := range []int64{idA, idB} {
		deletedAt, err := s.sources.DeletedAt(s.Ctx, id)
		s.Require().NoError(err)
		s.False(deletedAt.Valid)
	}
	status, err := s.sources.Status(s.Ctx, idA)
	s.Require().NoError(err)
	s.Equal(string(domain.SourceActive), status)
}

func (s *SourcesTestSuite) TestImportApplyKeepsVotesOfKeptSources() {
	feedA, feedB, feedC := s.feeds.URL()+"/a.xml", s.feeds.URL()+"/b.xml", s.feeds.URL()+"/c.xml"
	idA := s.insertSource(feedA, "A", domain.SourceActive)
	idB := s.insertSource(feedB, "B", domain.SourceActive)
	s.insertUpVotes(idA, userOne, userTwo)
	file := fmt.Sprintf("sources:\n  - feed: %s\n    status: paused\n  - feed: %s\n", feedA, feedC)

	out, err := s.runCLI(file, "import", "--apply")

	s.Require().NoError(err)
	s.Contains(out, "Applied 3 changes.")
	deletedAt, err := s.sources.DeletedAt(s.Ctx, idB)
	s.Require().NoError(err)
	s.True(deletedAt.Valid)
	status, err := s.sources.Status(s.Ctx, idA)
	s.Require().NoError(err)
	s.Equal(string(domain.SourcePaused), status)
	upVotes, err := s.sources.UpVotes(s.Ctx, idA)
	s.Require().NoError(err)
	s.Equal(2, upVotes)
	idC, err := s.sources.IDByFeedURL(s.Ctx, feedC)
	s.Require().NoError(err)
	cStatus, err := s.sources.Status(s.Ctx, idC)
	s.Require().NoError(err)
	s.Equal(string(domain.SourceActive), cStatus)
}

func (s *SourcesTestSuite) TestImportDashReadsInput() {
	feedA := s.feeds.URL() + "/a.xml"

	out, err := s.runCLI(fmt.Sprintf("sources:\n  - feed: %s\n", feedA), "import", "--apply", "-")

	s.Require().NoError(err)
	s.Contains(out, "Applied 1 changes.")
	_, err = s.sources.IDByFeedURL(s.Ctx, feedA)
	s.Require().NoError(err)
}

func (s *SourcesTestSuite) TestExportPrintsAllNotDeletedSources() {
	idA := s.insertSource(s.feeds.URL()+"/a.xml", "A", domain.SourceActive)
	s.insertSource(s.feeds.URL()+"/b.xml", "B", domain.SourcePaused)
	idRemoved := s.insertSource(s.feeds.URL()+"/c.xml", "C", domain.SourceActive)
	s.Require().NoError(s.sources.MarkDeleted(s.Ctx, idRemoved, s.Clock.Now()))
	s.insertUpVotes(idA, userOne, userTwo)

	out, err := s.runCLI("", "export")

	s.Require().NoError(err)
	var exported struct {
		Sources []struct {
			Title  string `yaml:"title"`
			Status string `yaml:"status"`
			Up     int    `yaml:"up"`
			Down   int    `yaml:"down"`
		} `yaml:"sources"`
	}
	s.Require().NoError(yaml.Unmarshal([]byte(out), &exported))
	s.Require().Len(exported.Sources, 2)
	s.Equal("A", exported.Sources[0].Title)
	s.Equal(2, exported.Sources[0].Up)
	s.Equal("paused", exported.Sources[1].Status)
}

func (s *SourcesTestSuite) TestExportWithoutBlogsPrintsEmptyList() {
	out, err := s.runCLI("", "export")

	s.Require().NoError(err)
	s.Equal("sources: []\n", out)
}

func (s *SourcesTestSuite) TestImportRejectsUnknownStatus() {
	id := s.insertSource(s.feeds.URL()+"/a.xml", "A", domain.SourceActive)
	file := fmt.Sprintf("sources:\n  - feed: %s/a.xml\n    status: sleeping\n", s.feeds.URL())

	_, err := s.runCLI(file, "import", "--apply")

	s.Require().Error(err)
	s.Contains(err.Error(), "status")
	status, err := s.sources.Status(s.Ctx, id)
	s.Require().NoError(err)
	s.Equal(string(domain.SourceActive), status)
}

func (s *SourcesTestSuite) TestImportRejectsDuplicateFeed() {
	feedC := s.feeds.URL() + "/c.xml"
	file := fmt.Sprintf("sources:\n  - feed: %s\n  - feed: %s\n", feedC, feedC)

	_, err := s.runCLI(file, "import", "--apply")

	s.Require().Error(err)
	s.Contains(err.Error(), "more than once")
	s.Equal(0, s.CountRows("source"))
}

func (s *SourcesTestSuite) TestImportKeepsStatusWhenEntryOmitsIt() {
	feedA := s.feeds.URL() + "/a.xml"
	id := s.insertSource(feedA, "A", domain.SourcePaused)

	out, err := s.runCLI(fmt.Sprintf("sources:\n  - feed: %s\n", feedA), "import", "--apply")

	s.Require().NoError(err)
	s.Equal("No changes.\n", out)
	status, err := s.sources.Status(s.Ctx, id)
	s.Require().NoError(err)
	s.Equal(string(domain.SourcePaused), status)
}

func (s *SourcesTestSuite) TestImportRejectsUnknownKey() {
	file := fmt.Sprintf("sources:\n  - feed: %s/a.xml\n    statsu: paused\n", s.feeds.URL())

	_, err := s.runCLI(file, "import", "--apply")

	s.Require().Error(err)
	s.Equal(0, s.CountRows("source"))
}

func (s *SourcesTestSuite) TestCLIAnswersForUserLevelFailures() {
	const missingRef = "99"
	out, err := s.runCLI("", "pause", missingRef)
	s.Require().NoError(err)
	s.Equal("No blog matches 99.\n", out)

	s.insertSource(s.feeds.URL()+feedPath, blogTitle, domain.SourceActive)
	out, err = s.runCLI("", "add", s.feeds.URL()+blogPath)
	s.Require().NoError(err)
	s.Equal("Already in the list.\n", out)

	bare := tests.NewFakeFeeds()
	defer bare.Close()
	bare.Set("/bare", htmlType, "<html></html>")
	out, err = s.runCLI("", "add", bare.URL()+"/bare")
	s.Require().NoError(err)
	s.Equal(fmt.Sprintf("No feed found at %s/bare.\n", bare.URL()), out)
}

func (s *SourcesTestSuite) TestCLIUnparseableRefMatchesNoBlog() {
	out, err := s.runCLI("", "remove", "foo")

	s.Require().NoError(err)
	s.Equal("No blog matches foo.\n", out)
}

func (s *SourcesTestSuite) TestCLIList() {
	out, err := s.runCLI("", "list")
	s.Require().NoError(err)
	s.Equal("No blogs yet.\n", out)

	id := s.insertSource(s.feeds.URL()+feedPath, blogTitle, domain.SourceActive)
	s.insertUpVotes(id, userOne)
	out, err = s.runCLI("", "list")
	s.Require().NoError(err)
	s.Equal(fmt.Sprintf("#%d active +1/-0 Example — %s%s\n", id, s.feeds.URL(), feedPath), out)
}

func (s *SourcesTestSuite) TestCLIPauseByID() {
	s.insertSource(s.feeds.URL()+"/a.xml", "A", domain.SourceActive)
	id := s.insertSource(s.feeds.URL()+"/b.xml", blogTitle, domain.SourceActive)

	out, err := s.runCLI("", "pause", strconv.FormatInt(id, 10))

	s.Require().NoError(err)
	s.Equal(fmt.Sprintf("Paused #%d Example.\n", id), out)
}
