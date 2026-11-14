package tests_test

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/suite"

	controllercli "github.com/sadensmol/blogger/internal/controller/cli"
	"github.com/sadensmol/blogger/internal/domain"
	"github.com/sadensmol/blogger/internal/feed"
	"github.com/sadensmol/blogger/internal/repository"
	"github.com/sadensmol/blogger/internal/service"
	"github.com/sadensmol/blogger/tests"
	testsrepository "github.com/sadensmol/blogger/tests/repository"
)

const (
	userNotes    = "Go and SQLite"
	learnedText  = "Likes small tools."
	newLearned   = "Likes databases."
	profileFmt   = "## My notes\n%s\n\n## Learned\n%s\n"
	noInterests  = "No interests file yet.\n"
	notesSaved   = "Notes saved.\n"
	changedNotes = "changed"
)

type ProfileTestSuite struct {
	tests.Suite

	path     string
	versions *testsrepository.ProfileVersion
	svc      *service.Profile
}

func TestProfileTestSuite(t *testing.T) {
	suite.Run(t, &ProfileTestSuite{})
}

func (s *ProfileTestSuite) SetupTest() {
	s.Suite.SetupTest()
	s.path = filepath.Join(s.T().TempDir(), "interests.md")
	s.versions = testsrepository.NewProfileVersion(s.DB)
	s.svc = service.NewProfile(s.path, repository.NewProfileVersion(s.DB), s.Clock.Now)
}

func (s *ProfileTestSuite) writeFile(content string) {
	s.Require().NoError(os.WriteFile(s.path, []byte(content), 0o644))
}

func (s *ProfileTestSuite) readFile() domain.Profile {
	data, err := os.ReadFile(s.path)
	s.Require().NoError(err)
	return domain.Profile(data)
}

func (s *ProfileTestSuite) runCLI(stdin string, args ...string) (string, error) {
	var out bytes.Buffer
	err := controllercli.Profile(s.Ctx, controllercli.ProfileData{
		Svc: s.svc, Args: args, In: strings.NewReader(stdin), Out: &out,
	})
	return out.String(), err
}

func (s *ProfileTestSuite) TestSaveRestoresUserNotes() {
	s.writeFile(fmt.Sprintf(profileFmt, userNotes, learnedText))

	err := s.svc.Save(s.Ctx, domain.Profile(fmt.Sprintf(profileFmt, changedNotes, newLearned)), domain.ProfileUpdate)

	s.Require().NoError(err)
	saved := s.readFile()
	s.Equal(userNotes, saved.Notes())
	s.Contains(string(saved), newLearned)
	s.Equal(1, s.CountRows("profile_version"))
}

func (s *ProfileTestSuite) TestSaveKeepsNotesEditedDuringRun() {
	s.writeFile(fmt.Sprintf(profileFmt, userNotes, learnedText))
	current, err := s.svc.Current(s.Ctx)
	s.Require().NoError(err)
	s.writeFile(fmt.Sprintf(profileFmt, changedNotes, learnedText))

	err = s.svc.Save(s.Ctx, domain.Profile(fmt.Sprintf(profileFmt, current.Notes(), newLearned)), domain.ProfileUpdate)

	s.Require().NoError(err)
	s.Equal(changedNotes, s.readFile().Notes())
}

func (s *ProfileTestSuite) TestCurrentRestoresFileFromLatestVersion() {
	s.Require().NoError(s.versions.Insert(s.Ctx, string(domain.ProfileInit), s.Clock.Now()))

	current, err := s.svc.Current(s.Ctx)

	s.Require().NoError(err)
	s.Equal(domain.Profile("content"), current)
	s.Equal(current, s.readFile())
}

func (s *ProfileTestSuite) TestCurrentPrefersFileOverVersion() {
	s.Require().NoError(s.versions.Insert(s.Ctx, string(domain.ProfileInit), s.Clock.Now()))
	fileText := fmt.Sprintf(profileFmt, userNotes, learnedText)
	s.writeFile(fileText)

	current, err := s.svc.Current(s.Ctx)

	s.Require().NoError(err)
	s.Equal(domain.Profile(fileText), current)
}

func (s *ProfileTestSuite) TestCurrentWithoutFileOrVersionIsNotFound() {
	_, err := s.svc.Current(s.Ctx)

	s.ErrorIs(err, domain.ErrNotFound)
}

func (s *ProfileTestSuite) TestSetNotesReplacesOnlyNotes() {
	s.writeFile(fmt.Sprintf(profileFmt, userNotes, learnedText))

	err := s.svc.SetNotes(s.Ctx, changedNotes)

	s.Require().NoError(err)
	s.Equal(domain.Profile(fmt.Sprintf(profileFmt, changedNotes, learnedText)), s.readFile())
	s.Equal(1, s.CountRows("profile_version"))
	change, err := s.versions.LatestChange(s.Ctx)
	s.Require().NoError(err)
	s.Equal(string(domain.ProfileNotes), change)
}

func (s *ProfileTestSuite) TestSaveWithoutFileOrVersionDropsLLMNotes() {
	err := s.svc.Save(s.Ctx, domain.Profile(fmt.Sprintf(profileFmt, changedNotes, newLearned)), domain.ProfileInit)

	s.Require().NoError(err)
	s.Empty(s.readFile().Notes())
	s.Equal(1, s.CountRows("profile_version"))
}

func (s *ProfileTestSuite) TestSaveWithoutFileUsesLatestVersionNotes() {
	s.Require().NoError(s.svc.SetNotes(s.Ctx, userNotes))
	s.Require().NoError(os.Remove(s.path))

	err := s.svc.Save(s.Ctx, domain.Profile(fmt.Sprintf(profileFmt, changedNotes, newLearned)), domain.ProfileUpdate)

	s.Require().NoError(err)
	s.Equal(userNotes, s.readFile().Notes())
}

func (s *ProfileTestSuite) TestLastLearnedAtWithoutVersionsIsZero() {
	at, err := s.svc.LastLearnedAt(s.Ctx)

	s.Require().NoError(err)
	s.True(at.IsZero())
}

func (s *ProfileTestSuite) TestSetNotesWithoutFileCreatesOne() {
	err := s.svc.SetNotes(s.Ctx, userNotes)

	s.Require().NoError(err)
	created := s.readFile()
	s.Equal(userNotes, created.Notes())
	s.True(created.LearnedEmpty())
}

func (s *ProfileTestSuite) TestNotesEditDoesNotHideNewVotes() {
	learnedAt := s.Clock.Now()
	s.Require().NoError(s.versions.Insert(s.Ctx, string(domain.ProfileInit), learnedAt))
	s.Clock.Advance(time.Hour)
	s.insertVote(s.Clock.Now())
	s.Clock.Advance(time.Hour)
	s.Require().NoError(s.svc.SetNotes(s.Ctx, userNotes))
	votes := service.NewVotes(service.VotesData{
		Reactions: repository.NewReaction(s.DB),
		Posts:     service.NewPosts(repository.NewPost(s.DB), s.Clock.Now),
		Sources:   service.NewSource(repository.NewSource(s.DB), feed.NewClient(time.Second), s.Clock.Now),
		Now:       s.Clock.Now,
	})

	since, err := s.svc.LastLearnedAt(s.Ctx)
	s.Require().NoError(err)
	changed, err := votes.ChangedSince(s.Ctx, since)

	s.Require().NoError(err)
	s.True(since.Equal(learnedAt))
	s.True(changed)
}

func (s *ProfileTestSuite) TestCLIShowPrintsFile() {
	fileText := fmt.Sprintf(profileFmt, userNotes, learnedText)
	s.writeFile(fileText)

	out, err := s.runCLI("", "show")

	s.Require().NoError(err)
	s.Equal(fileText, out)
}

func (s *ProfileTestSuite) TestCLIShowWithoutFile() {
	out, err := s.runCLI("", "show")

	s.Require().NoError(err)
	s.Equal(noInterests, out)
}

func (s *ProfileTestSuite) TestCLISetNotesFromStdin() {
	out, err := s.runCLI("Go", "set-notes", "-")

	s.Require().NoError(err)
	s.Equal(notesSaved, out)
	s.Equal("Go", s.readFile().Notes())
}

func (s *ProfileTestSuite) TestCLISetNotesFromFile() {
	notesPath := filepath.Join(s.T().TempDir(), "notes.md")
	s.Require().NoError(os.WriteFile(notesPath, []byte(userNotes), 0o644))

	out, err := s.runCLI("", "set-notes", notesPath)

	s.Require().NoError(err)
	s.Equal(notesSaved, out)
	s.Equal(userNotes, s.readFile().Notes())
}

func (s *ProfileTestSuite) TestCLISetNotesRejectsHeadingLine() {
	_, err := s.runCLI("Go\n## Learned\nhacked\n", "set-notes", "-")

	s.Require().Error(err)
	s.NoFileExists(s.path)
	s.Equal(0, s.CountRows("profile_version"))
}

func (s *ProfileTestSuite) insertVote(at time.Time) {
	sourceID, err := testsrepository.NewSource(s.DB).Insert(s.Ctx, testsrepository.SourceRow{
		FeedURL: "https://example.com/feed", Status: string(domain.SourceActive),
	}, at)
	s.Require().NoError(err)
	articleID, err := testsrepository.NewArticle(s.DB).Insert(s.Ctx, testsrepository.ArticleRow{
		SourceID: sourceID, URL: "https://example.com/a", PublishedAt: at, Now: at,
	})
	s.Require().NoError(err)
	runID, err := testsrepository.NewRun(s.DB).Insert(s.Ctx, string(domain.RunKindFetch), string(domain.RunStatusOK), at)
	s.Require().NoError(err)
	postID, err := testsrepository.NewPost(s.DB).Insert(s.Ctx, articleID, runID, at)
	s.Require().NoError(err)
	s.Require().NoError(testsrepository.NewReaction(s.DB).Insert(s.Ctx, testsrepository.ReactionRow{
		PostID: postID, UserID: "U1", Vote: int(domain.VoteUp), CreatedAt: at,
	}))
}
