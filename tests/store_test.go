package tests_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/suite"

	"github.com/sadensmol/blogger/internal/domain"
	"github.com/sadensmol/blogger/internal/repository"
	"github.com/sadensmol/blogger/tests"
	testsrepository "github.com/sadensmol/blogger/tests/repository"
)

const (
	migrationsCount = 6
	feedURL         = "https://example.com/feed"
	unknownSourceID = 999
	userOne         = "U1"
	userTwo         = "U2"
)

type StoreTestSuite struct {
	tests.Suite

	sources         *testsrepository.Source
	articles        *testsrepository.Article
	runs            *testsrepository.Run
	posts           *testsrepository.Post
	reactions       *testsrepository.Reaction
	profileVersions *testsrepository.ProfileVersion
}

func TestStoreTestSuite(t *testing.T) {
	suite.Run(t, &StoreTestSuite{})
}

func (s *StoreTestSuite) SetupTest() {
	s.Suite.SetupTest()
	s.sources = testsrepository.NewSource(s.DB)
	s.articles = testsrepository.NewArticle(s.DB)
	s.runs = testsrepository.NewRun(s.DB)
	s.posts = testsrepository.NewPost(s.DB)
	s.reactions = testsrepository.NewReaction(s.DB)
	s.profileVersions = testsrepository.NewProfileVersion(s.DB)
}

func (s *StoreTestSuite) insertSource(feed, status string) int64 {
	id, err := s.sources.Insert(s.Ctx, testsrepository.SourceRow{FeedURL: feed, Status: status}, s.Clock.Now())
	s.Require().NoError(err)
	return id
}

func (s *StoreTestSuite) insertArticle(sourceID int64, url string, publishedAt time.Time) int64 {
	id, err := s.articles.Insert(s.Ctx, testsrepository.ArticleRow{
		SourceID: sourceID, URL: url, PublishedAt: publishedAt, Now: s.Clock.Now(),
	})
	s.Require().NoError(err)
	return id
}

func (s *StoreTestSuite) insertRun(status string) int64 {
	id, err := s.runs.Insert(s.Ctx, string(domain.RunKindFetch), status, s.Clock.Now())
	s.Require().NoError(err)
	return id
}

func (s *StoreTestSuite) insertPost(articleID, runID int64) int64 {
	id, err := s.posts.Insert(s.Ctx, articleID, runID, s.Clock.Now())
	s.Require().NoError(err)
	return id
}

func (s *StoreTestSuite) insertReaction(postID int64, user string, vote domain.Vote, removed bool) {
	s.Require().NoError(s.reactions.Insert(s.Ctx, testsrepository.ReactionRow{
		PostID: postID, UserID: user, Vote: int(vote), Removed: removed, CreatedAt: s.Clock.Now(),
	}))
}

func (s *StoreTestSuite) TestOpenTwiceAppliesNothing() {
	s.Require().NoError(s.DB.Close())

	db, err := repository.Open(s.Ctx, s.DBPath)
	s.Require().NoError(err)
	s.DB = db

	var version int
	s.Require().NoError(s.DB.QueryRowContext(s.Ctx, `SELECT MAX(version_id) FROM goose_db_version`).Scan(&version))
	s.Equal(migrationsCount, version)
}

func (s *StoreTestSuite) TestForeignKeysAreOn() {
	_, err := s.articles.Insert(s.Ctx, testsrepository.ArticleRow{
		SourceID: unknownSourceID, URL: "https://example.com/a", PublishedAt: s.Clock.Now(), Now: s.Clock.Now(),
	})

	s.Error(err)
}

func (s *StoreTestSuite) TestSecondPostForSameArticleFails() {
	sourceID := s.insertSource(feedURL, "active")
	articleID := s.insertArticle(sourceID, "https://example.com/a", s.Clock.Now())
	runID := s.insertRun("running")
	post := domain.Post{ArticleID: domain.ArticleID(articleID), RunID: domain.RunID(runID), ChannelID: "C1", MessageTS: "1.1"}
	repo := repository.NewPost(s.DB)
	_, err := repo.Create(s.Ctx, post, s.Clock.Now())
	s.Require().NoError(err)

	post.MessageTS = "2.2"
	_, err = repo.Create(s.Ctx, post, s.Clock.Now())

	s.Error(err)
	s.Equal(1, s.CountRows("post"))
}

func (s *StoreTestSuite) TestCreateRevivesDeletedSource() {
	repo := repository.NewSource(s.DB)
	source := domain.Source{
		SiteURL: "https://example.com", FeedURL: feedURL, Title: "Example",
		Status: domain.SourceActive, Origin: domain.OriginUser,
	}
	created, err := repo.Create(s.Ctx, source, s.Clock.Now())
	s.Require().NoError(err)
	s.Require().NoError(repo.Delete(s.Ctx, created.ID, s.Clock.Now()))

	revived, err := repo.Create(s.Ctx, source, s.Clock.Now())

	s.Require().NoError(err)
	s.Equal(created.ID, revived.ID)
	deletedAt, err := s.sources.DeletedAt(s.Ctx, int64(revived.ID))
	s.Require().NoError(err)
	s.False(deletedAt.Valid)
}

func (s *StoreTestSuite) TestCreateKnownSourceFails() {
	repo := repository.NewSource(s.DB)
	source := domain.Source{
		SiteURL: "https://example.com", FeedURL: feedURL, Title: "Example",
		Status: domain.SourceActive, Origin: domain.OriginUser,
	}
	_, err := repo.Create(s.Ctx, source, s.Clock.Now())
	s.Require().NoError(err)

	_, err = repo.Create(s.Ctx, source, s.Clock.Now())

	s.ErrorIs(err, domain.ErrAlreadyExists)
}

func (s *StoreTestSuite) TestArticleDuplicateURLIgnored() {
	sourceID := s.insertSource(feedURL, "active")
	repo := repository.NewArticle(s.DB)
	article := domain.Article{SourceID: domain.SourceID(sourceID), URL: "https://example.com/a", Title: "A", PublishedAt: s.Clock.Now()}
	created, err := repo.CreateIgnoringDuplicate(s.Ctx, article, s.Clock.Now())
	s.Require().NoError(err)
	s.Require().True(created)

	created, err = repo.CreateIgnoringDuplicate(s.Ctx, article, s.Clock.Now())

	s.Require().NoError(err)
	s.False(created)
	s.Equal(1, s.CountRows("article"))
}

func (s *StoreTestSuite) TestListNotPostedSkipsPausedDeletedPostedAndOld() {
	const (
		activeNewURL  = "https://example.com/active-new"
		trialNewURL   = "https://example.com/trial-new"
		maxAge        = 14 * 24 * time.Hour
		oldAge        = 15 * 24 * time.Hour
		trialNewAge   = time.Hour
		activeNewAge  = 2 * time.Hour
		otherArticleAge = 3 * time.Hour
	)
	now := s.Clock.Now()
	active := s.insertSource("https://active.example.com/feed", "active")
	trial := s.insertSource("https://trial.example.com/feed", "trial")
	paused := s.insertSource("https://paused.example.com/feed", "paused")
	deleted := s.insertSource("https://deleted.example.com/feed", "active")
	_, err := s.DB.ExecContext(s.Ctx, `UPDATE source SET deleted_at = ? WHERE id = ?`, now.Format(time.RFC3339), deleted)
	s.Require().NoError(err)
	s.insertArticle(active, activeNewURL, now.Add(-activeNewAge))
	s.insertArticle(trial, trialNewURL, now.Add(-trialNewAge))
	s.insertArticle(paused, "https://example.com/paused-new", now.Add(-otherArticleAge))
	s.insertArticle(deleted, "https://example.com/deleted-new", now.Add(-otherArticleAge))
	s.insertArticle(active, "https://example.com/old", now.Add(-oldAge))
	posted := s.insertArticle(active, "https://example.com/posted", now.Add(-otherArticleAge))
	s.insertPost(posted, s.insertRun("ok"))

	articles, err := repository.NewArticle(s.DB).ListNotPostedOfActiveOrTrialPublishedAfter(s.Ctx, now.Add(-maxAge))

	s.Require().NoError(err)
	urls := make([]domain.URL, 0, len(articles))
	for _, article := range articles {
		urls = append(urls, article.URL)
	}
	s.Equal([]domain.URL{trialNewURL, activeNewURL}, urls)
}

func (s *StoreTestSuite) TestSourceVotesCountOnlyNotRemoved() {
	sourceID := s.insertSource(feedURL, "active")
	runID := s.insertRun("ok")
	var postIDs []int64
	for _, url := range []string{"https://example.com/1", "https://example.com/2", "https://example.com/3"} {
		postIDs = append(postIDs, s.insertPost(s.insertArticle(sourceID, url, s.Clock.Now()), runID))
	}
	s.insertReaction(postIDs[0], userOne, domain.VoteUp, false)
	s.insertReaction(postIDs[1], userOne, domain.VoteUp, false)
	s.insertReaction(postIDs[1], userTwo, domain.VoteDown, false)
	s.insertReaction(postIDs[2], userOne, domain.VoteUp, true)

	source, err := repository.NewSource(s.DB).FindNotDeletedByID(s.Ctx, domain.SourceID(sourceID))

	s.Require().NoError(err)
	s.Equal(domain.Votes{Up: 2, Down: 1}, source.Votes)
}

func (s *StoreTestSuite) TestFailRunningMarksInterrupted() {
	runID := s.insertRun("running")
	s.insertRun("ok")

	failed, err := repository.NewRun(s.DB).FailRunning(s.Ctx, s.Clock.Now())

	s.Require().NoError(err)
	s.Equal(int64(1), failed)
	status, err := s.runs.Status(s.Ctx, runID)
	s.Require().NoError(err)
	s.Equal("failed", status)
}

func (s *StoreTestSuite) TestLastLearnedIgnoresNotesVersions() {
	t1 := s.Clock.Now()
	s.Require().NoError(s.profileVersions.Insert(s.Ctx, "init", t1))
	s.Clock.Advance(time.Hour)
	s.Require().NoError(s.profileVersions.Insert(s.Ctx, "notes", s.Clock.Now()))

	last, err := repository.NewProfileVersion(s.DB).LastCreatedAtOfInitOrUpdate(s.Ctx)

	s.Require().NoError(err)
	s.True(t1.Equal(last))
}
