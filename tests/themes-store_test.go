package tests_test

import (
	"database/sql"
	"fmt"
	"io/fs"
	"path/filepath"
	"testing"
	"time"

	"github.com/pressly/goose/v3"
	"github.com/stretchr/testify/suite"

	migrations "github.com/sadensmol/blogger/db"
	"github.com/sadensmol/blogger/internal/domain"
	"github.com/sadensmol/blogger/internal/llm"
	"github.com/sadensmol/blogger/internal/repository"
	"github.com/sadensmol/blogger/tests"
	testsrepository "github.com/sadensmol/blogger/tests/repository"
)

const (
	versionBeforeTags = 13
	oldPostFeedURL    = "https://old.example.com/feed.xml"
	oldPostVoterID    = "U1"
)

type ThemesStoreTestSuite struct {
	tests.Suite

	oldPath string
	oldDB   *sql.DB
}

func TestThemesStoreTestSuite(t *testing.T) {
	suite.Run(t, &ThemesStoreTestSuite{})
}

func (s *ThemesStoreTestSuite) SetupTest() {
	s.Suite.SetupTest()
	s.oldPath = filepath.Join(s.T().TempDir(), "old.db")
	db, err := sql.Open("sqlite", fmt.Sprintf("file:%s?_pragma=foreign_keys(1)", s.oldPath))
	s.Require().NoError(err)
	s.oldDB = db
	dir, err := fs.Sub(migrations.Migrations, "migrations")
	s.Require().NoError(err)
	provider, err := goose.NewProvider(goose.DialectSQLite3, s.oldDB, dir)
	s.Require().NoError(err)
	_, err = provider.UpTo(s.Ctx, versionBeforeTags)
	s.Require().NoError(err)
}

func (s *ThemesStoreTestSuite) insertOldArticle(name string) int64 {
	sourceID, err := testsrepository.NewSource(s.oldDB).Insert(s.Ctx, testsrepository.SourceRow{
		FeedURL: fmt.Sprintf("%s/%s", oldPostFeedURL, name), Status: string(domain.SourceActive),
	}, s.Clock.Now())
	s.Require().NoError(err)
	articleID, err := testsrepository.NewArticle(s.oldDB).Insert(s.Ctx, testsrepository.ArticleRow{
		SourceID: sourceID, URL: fmt.Sprintf("https://old.example.com/%s", name), PublishedAt: s.Clock.Now(), Now: s.Clock.Now(),
	})
	s.Require().NoError(err)
	return articleID
}

func (s *ThemesStoreTestSuite) insertOldPost(articleID int64, n int, topics []string) int64 {
	runID, err := testsrepository.NewRun(s.oldDB).Insert(s.Ctx, string(domain.RunKindDigestShow), string(domain.RunStatusOK), s.Clock.Now())
	s.Require().NoError(err)
	postID, err := testsrepository.NewPost(s.oldDB).InsertMessage(s.Ctx, testsrepository.PostRow{
		ArticleID: articleID, RunID: runID, MessageTS: fmt.Sprintf("ts-%d-%d", articleID, n), Now: s.Clock.Now(),
	})
	s.Require().NoError(err)
	s.Require().NoError(testsrepository.NewArticleTag(s.oldDB).SetPostTopics(s.Ctx, postID, topics))
	return postID
}

func (s *ThemesStoreTestSuite) voteUp(postID int64) {
	s.Require().NoError(testsrepository.NewReaction(s.oldDB).Insert(s.Ctx, testsrepository.ReactionRow{
		PostID: postID, UserID: oldPostVoterID, Vote: int(domain.VoteUp), CreatedAt: s.Clock.Now(),
	}))
}

func (s *ThemesStoreTestSuite) upgrade() *sql.DB {
	s.Require().NoError(s.oldDB.Close())
	upgraded, err := repository.Open(s.Ctx, s.oldPath)
	s.Require().NoError(err)
	return upgraded
}

func (s *ThemesStoreTestSuite) themeScores(db *sql.DB) map[string]domain.Votes {
	app := tests.NewApp(s.T(), tests.AppData{
		DB:          db,
		Now:         s.Clock.Now,
		Agents:      tests.NewFakeAgents(s.T()),
		Backends:    []llm.Backend{llm.Claude},
		SlackURL:    "http://127.0.0.1:1",
		ChannelID:   "C1",
		ProfilePath: filepath.Join(s.T().TempDir(), "interests.md"),
		Location:    time.UTC,
		DigestSize:  1,
	})
	themes, err := app.Votes.Themes(s.Ctx)
	s.Require().NoError(err)
	scores := map[string]domain.Votes{}
	for _, t := range themes {
		if t.Votes.Up != 0 || t.Votes.Down != 0 {
			scores[string(t.Tag)] = t.Votes
		}
	}
	return scores
}

func (s *ThemesStoreTestSuite) TestUpgradeTurnsOldPostTopicsIntoThemes() {
	articleID := s.insertOldArticle("a")
	s.voteUp(s.insertOldPost(articleID, 1, []string{"Go", "SQLite"}))

	upgraded := s.upgrade()
	defer func() { s.Require().NoError(upgraded.Close()) }()

	s.Equal(map[string]domain.Votes{"go": {Up: 1}, "sqlite": {Up: 1}}, s.themeScores(upgraded))
}

func (s *ThemesStoreTestSuite) TestUpgradeTrimsLowersAndDedupesOldTopics() {
	articleID := s.insertOldArticle("a")
	s.voteUp(s.insertOldPost(articleID, 1, []string{" Go ", "", "go"}))
	s.insertOldPost(articleID, 2, []string{"GO", "Rust", "   "})

	upgraded := s.upgrade()
	defer func() { s.Require().NoError(upgraded.Close()) }()

	s.Equal(map[string]domain.Votes{"go": {Up: 1}, "rust": {Up: 1}}, s.themeScores(upgraded))
	tags, err := testsrepository.NewArticleTag(upgraded).ListByURL(s.Ctx, "https://old.example.com/a")
	s.Require().NoError(err)
	s.Equal([]string{"go", "rust"}, tags)
}
