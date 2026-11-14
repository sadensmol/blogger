package tests_test

import (
	"database/sql"
	"errors"
	"io/fs"
	"path/filepath"
	"testing"
	"time"

	"github.com/pressly/goose/v3"
	"github.com/stretchr/testify/suite"
	"modernc.org/sqlite"
	sqlite3 "modernc.org/sqlite/lib"

	blogdb "github.com/sadensmol/blogger/db"
	"github.com/sadensmol/blogger/internal/domain"
	"github.com/sadensmol/blogger/internal/repository"
	"github.com/sadensmol/blogger/tests"
	testsrepository "github.com/sadensmol/blogger/tests/repository"
)

const (
	migrationsCount = 11
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

func (s *StoreTestSuite) insertSource(feed string, status domain.SourceStatus) int64 {
	id, err := s.sources.Insert(s.Ctx, testsrepository.SourceRow{FeedURL: feed, Status: string(status)}, s.Clock.Now())
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

func (s *StoreTestSuite) insertRun(status domain.RunStatus) int64 {
	id, err := s.runs.Insert(s.Ctx, string(domain.RunKindFetch), string(status), s.Clock.Now())
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

func (s *StoreTestSuite) TestOptionalPublishedMigrationForgetsFetchTimeDates() {
	const lastVersionWithRequiredDate = 8
	path := filepath.Join(s.T().TempDir(), "old.db")
	old, err := sql.Open("sqlite", path)
	s.Require().NoError(err)
	migrations, err := fs.Sub(blogdb.Migrations, "migrations")
	s.Require().NoError(err)
	provider, err := goose.NewProvider(goose.DialectSQLite3, old, migrations)
	s.Require().NoError(err)
	_, err = provider.UpTo(s.Ctx, lastVersionWithRequiredDate)
	s.Require().NoError(err)
	sourceID, err := testsrepository.NewSource(old).Insert(s.Ctx, testsrepository.SourceRow{FeedURL: feedURL, Status: string(domain.SourceActive)}, s.Clock.Now())
	s.Require().NoError(err)
	realDate := s.Clock.Now().Add(-24 * time.Hour)
	for _, row := range []testsrepository.ArticleRow{
		{SourceID: sourceID, URL: "https://example.com/fetch-time", PublishedAt: s.Clock.Now(), Now: s.Clock.Now()},
		{SourceID: sourceID, URL: "https://example.com/real-date", PublishedAt: realDate, Now: s.Clock.Now()},
	} {
		_, err := testsrepository.NewArticle(old).Insert(s.Ctx, row)
		s.Require().NoError(err)
	}
	s.Require().NoError(old.Close())

	migrated, err := repository.Open(s.Ctx, path)
	s.Require().NoError(err)
	defer func() { s.Require().NoError(migrated.Close()) }()

	stored, err := testsrepository.NewArticle(migrated).ListBySource(s.Ctx, sourceID)
	s.Require().NoError(err)
	s.Equal([]testsrepository.StoredArticle{
		{URL: "https://example.com/real-date", Title: "https://example.com/real-date", PublishedAt: sql.NullString{String: realDate.Format(time.RFC3339), Valid: true}},
		{URL: "https://example.com/fetch-time", Title: "https://example.com/fetch-time"},
	}, stored)
}

func (s *StoreTestSuite) TestForeignKeysAreOn() {
	_, err := s.articles.Insert(s.Ctx, testsrepository.ArticleRow{
		SourceID: unknownSourceID, URL: "https://example.com/a", PublishedAt: s.Clock.Now(), Now: s.Clock.Now(),
	})

	var sqliteErr *sqlite.Error
	s.Require().True(errors.As(err, &sqliteErr))
	s.Equal(sqlite3.SQLITE_CONSTRAINT_FOREIGNKEY, sqliteErr.Code())
}

func (s *StoreTestSuite) TestSecondPostForSameArticleFails() {
	sourceID := s.insertSource(feedURL, domain.SourceActive)
	articleID := s.insertArticle(sourceID, "https://example.com/a", s.Clock.Now())
	runID := s.insertRun(domain.RunStatusRunning)
	s.insertPost(articleID, runID)
	post := domain.Post{ArticleID: domain.ArticleID(articleID), RunID: domain.RunID(runID), ChannelID: "C1", MessageTS: "2.2"}

	_, err := repository.NewPost(s.DB).Create(s.Ctx, post, s.Clock.Now())

	s.Error(err)
	s.Equal(1, s.CountRows("post"))
}

func (s *StoreTestSuite) TestCreateRevivesDeletedSource() {
	sourceID := s.insertSource(feedURL, domain.SourceActive)
	s.Require().NoError(s.sources.MarkDeleted(s.Ctx, sourceID, s.Clock.Now()))
	source := domain.Source{
		SiteURL: "https://example.com", FeedURL: feedURL, Title: "Example",
		Status: domain.SourceActive, Origin: domain.OriginUser,
	}

	revived, err := repository.NewSource(s.DB).Create(s.Ctx, source, s.Clock.Now())

	s.Require().NoError(err)
	s.Equal(domain.SourceID(sourceID), revived.ID)
	deletedAt, err := s.sources.DeletedAt(s.Ctx, int64(revived.ID))
	s.Require().NoError(err)
	s.False(deletedAt.Valid)
}

func (s *StoreTestSuite) TestCreateKnownSourceFails() {
	s.insertSource(feedURL, domain.SourceActive)
	source := domain.Source{
		SiteURL: "https://example.com", FeedURL: feedURL, Title: "Example",
		Status: domain.SourceActive, Origin: domain.OriginUser,
	}

	_, err := repository.NewSource(s.DB).Create(s.Ctx, source, s.Clock.Now())

	s.ErrorIs(err, domain.ErrAlreadyExists)
}

func (s *StoreTestSuite) TestArticleDuplicateURLIgnored() {
	sourceID := s.insertSource(feedURL, domain.SourceActive)
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
	active := s.insertSource("https://active.example.com/feed", domain.SourceActive)
	trial := s.insertSource("https://trial.example.com/feed", domain.SourceTrial)
	paused := s.insertSource("https://paused.example.com/feed", domain.SourcePaused)
	deleted := s.insertSource("https://deleted.example.com/feed", domain.SourceActive)
	_, err := s.DB.ExecContext(s.Ctx, `UPDATE source SET deleted_at = ? WHERE id = ?`, now.Format(time.RFC3339), deleted)
	s.Require().NoError(err)
	s.insertArticle(active, activeNewURL, now.Add(-activeNewAge))
	s.insertArticle(trial, trialNewURL, now.Add(-trialNewAge))
	s.insertArticle(paused, "https://example.com/paused-new", now.Add(-otherArticleAge))
	s.insertArticle(deleted, "https://example.com/deleted-new", now.Add(-otherArticleAge))
	s.insertArticle(active, "https://example.com/old", now.Add(-oldAge))
	posted := s.insertArticle(active, "https://example.com/posted", now.Add(-otherArticleAge))
	s.insertPost(posted, s.insertRun(domain.RunStatusOK))

	articles, err := repository.NewArticle(s.DB).ListNotPostedOfActiveOrTrialPublishedAfter(s.Ctx, now.Add(-maxAge))

	s.Require().NoError(err)
	urls := make([]domain.URL, 0, len(articles))
	for _, article := range articles {
		urls = append(urls, article.URL)
	}
	s.Equal([]domain.URL{trialNewURL, activeNewURL}, urls)
}

func (s *StoreTestSuite) TestSourceVotesCountOnlyNotRemoved() {
	sourceID := s.insertSource(feedURL, domain.SourceActive)
	runID := s.insertRun(domain.RunStatusOK)
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

func (s *StoreTestSuite) TestSourceVotesSinceStatusCountOnlyAfterStatusChange() {
	sourceID := s.insertSource(feedURL, domain.SourceTrial)
	runID := s.insertRun(domain.RunStatusOK)
	var postIDs []int64
	for _, url := range []string{"https://example.com/1", "https://example.com/2", "https://example.com/3", "https://example.com/4"} {
		postIDs = append(postIDs, s.insertPost(s.insertArticle(sourceID, url, s.Clock.Now()), runID))
	}
	s.insertReaction(postIDs[0], userOne, domain.VoteUp, false)
	s.Clock.Advance(time.Hour)
	statusChangedAt := s.Clock.Now()
	s.Require().NoError(s.sources.SetStatusChangedAt(s.Ctx, sourceID, statusChangedAt))
	s.Clock.Advance(time.Hour)
	s.insertReaction(postIDs[1], userOne, domain.VoteUp, false)
	s.insertReaction(postIDs[2], userOne, domain.VoteDown, false)
	s.insertReaction(postIDs[3], userOne, domain.VoteUp, true)

	source, err := repository.NewSource(s.DB).FindNotDeletedByID(s.Ctx, domain.SourceID(sourceID))

	s.Require().NoError(err)
	s.Equal(domain.Votes{Up: 1, Down: 1}, source.VotesSinceStatus)
	s.Equal(domain.Votes{Up: 2, Down: 1}, source.Votes)
}

func (s *StoreTestSuite) TestFailRunningMarksInterrupted() {
	runID := s.insertRun(domain.RunStatusRunning)
	s.insertRun(domain.RunStatusOK)

	failed, err := repository.NewRun(s.DB).FailRunning(s.Ctx, s.Clock.Now())

	s.Require().NoError(err)
	s.Equal(int64(1), failed)
	status, err := s.runs.Status(s.Ctx, runID)
	s.Require().NoError(err)
	s.Equal(string(domain.RunStatusFailed), status)
}

func (s *StoreTestSuite) TestLastLearnedIgnoresNotesVersions() {
	t1 := s.Clock.Now()
	s.Require().NoError(s.profileVersions.Insert(s.Ctx, string(domain.ProfileInit), t1))
	s.Clock.Advance(time.Hour)
	s.Require().NoError(s.profileVersions.Insert(s.Ctx, string(domain.ProfileNotes), s.Clock.Now()))

	last, err := repository.NewProfileVersion(s.DB).LastCreatedAtOfInitOrUpdate(s.Ctx)

	s.Require().NoError(err)
	s.True(t1.Equal(last))
}
