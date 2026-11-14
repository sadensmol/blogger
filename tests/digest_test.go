package tests_test

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/stretchr/testify/suite"

	"github.com/sadensmol/blogger/internal/domain"
	"github.com/sadensmol/blogger/internal/llm"
	"github.com/sadensmol/blogger/internal/usecase"
	"github.com/sadensmol/blogger/tests"
	testsrepository "github.com/sadensmol/blogger/tests/repository"
)

const (
	digestFeedPath   = "/digest.xml"
	digestChannelID  = "C1"
	digestSize       = 5
	digestNotes      = "Go"
	digestLearned    = "Likes small tools."
	answeredLearned  = "Likes Go."
	digestProfileFmt = "## My notes\n%s\n\n## Learned\n%s\n"
	nothingLine      = "Nothing worth reading today."
	failureLine      = "Digest failed: no agent could pick articles. See the logs."
	noBlogsLine      = "No blogs yet. Add one with /blogs add <url>."
	initPrompt       = "-p /init-profile"
	updatePrompt     = "-p /update-profile"
	pickPrompt       = "-p /pick-articles"
)

type DigestTestSuite struct {
	tests.Suite

	feeds       *tests.FakeFeeds
	slack       *tests.FakeSlack
	agents      *tests.FakeAgents
	profilePath string
	sources     *testsrepository.Source
	articles    *testsrepository.Article
	posts       *testsrepository.Post
	reactions   *testsrepository.Reaction
	runs        *testsrepository.Run
	versions    *testsrepository.ProfileVersion
	digest      *usecase.Digest
}

func TestDigestTestSuite(t *testing.T) {
	suite.Run(t, &DigestTestSuite{})
}

func (s *DigestTestSuite) SetupTest() {
	s.Suite.SetupTest()
	s.feeds = tests.NewFakeFeeds()
	s.slack = tests.NewFakeSlack()
	s.agents = tests.NewFakeAgents(s.T())
	s.profilePath = filepath.Join(s.T().TempDir(), "interests.md")
	s.sources = testsrepository.NewSource(s.DB)
	s.articles = testsrepository.NewArticle(s.DB)
	s.posts = testsrepository.NewPost(s.DB)
	s.reactions = testsrepository.NewReaction(s.DB)
	s.runs = testsrepository.NewRun(s.DB)
	s.versions = testsrepository.NewProfileVersion(s.DB)

	location, err := time.LoadLocation("Europe/Berlin")
	s.Require().NoError(err)
	s.digest = tests.NewApp(s.T(), tests.AppData{
		DB:          s.DB,
		Now:         s.Clock.Now,
		Agents:      s.agents,
		Backends:    []llm.Backend{llm.Claude, llm.Codex},
		SlackURL:    s.slack.URL,
		ChannelID:   digestChannelID,
		ProfilePath: s.profilePath,
		Location:    location,
		DigestSize:  digestSize,
	}).Digest
}

func writeLearnedProfile(t *testing.T) string {
	path := filepath.Join(t.TempDir(), "interests.md")
	require.NoError(t, os.WriteFile(path, fmt.Appendf(nil, digestProfileFmt, digestNotes, digestLearned), 0o644))
	return path
}

func (s *DigestTestSuite) TearDownTest() {
	s.feeds.Close()
	s.slack.Close()
	s.Suite.TearDownTest()
}

func (s *DigestTestSuite) articleURL(name string) string {
	return fmt.Sprintf("%s/%s", s.feeds.URL(), name)
}

func (s *DigestTestSuite) addBlog(names ...string) int64 {
	var items strings.Builder
	for _, name := range names {
		fmt.Fprintf(&items, itemFmt, name, s.articleURL(name))
	}
	s.feeds.Set(digestFeedPath, "application/rss+xml", fmt.Sprintf(itemsFmt, items.String()))
	id, err := s.sources.Insert(s.Ctx, testsrepository.SourceRow{
		FeedURL: fmt.Sprintf("%s%s", s.feeds.URL(), digestFeedPath),
		Status:  string(domain.SourceActive),
	}, s.Clock.Now())
	s.Require().NoError(err)
	return id
}

func (s *DigestTestSuite) writeProfile(notes, learned string) {
	s.Require().NoError(os.WriteFile(s.profilePath, fmt.Appendf(nil, digestProfileFmt, notes, learned), 0o644))
}

func (s *DigestTestSuite) learnedProfile() {
	s.writeProfile(digestNotes, digestLearned)
	s.Require().NoError(s.versions.Insert(s.Ctx, string(domain.ProfileInit), s.Clock.Now().Add(-2*time.Hour)))
}

func (s *DigestTestSuite) answerPicks(names ...string) {
	picks := make([]map[string]any, 0, len(names))
	for _, name := range names {
		picks = append(picks, map[string]any{
			"url": s.articleURL(name), "description": "A post.", "topics": []string{"go"}, "reason": "Matches Go.",
		})
	}
	s.agents.Answer(llm.Claude, llm.SkillPickArticles, map[string]any{"picks": picks})
}

func (s *DigestTestSuite) answerProfile(skill llm.Skill, notes string) {
	s.agents.Answer(llm.Claude, skill, map[string]any{"interests_md": fmt.Sprintf(digestProfileFmt, notes, answeredLearned)})
}

func (s *DigestTestSuite) pickCandidates() []string {
	var input struct {
		Candidates []struct {
			URL string `json:"url"`
		} `json:"candidates"`
	}
	s.Require().NoError(json.Unmarshal(s.agents.Input(llm.Claude, llm.SkillPickArticles), &input))
	urls := make([]string, 0, len(input.Candidates))
	for _, c := range input.Candidates {
		urls = append(urls, c.URL)
	}
	return urls
}

func (s *DigestTestSuite) latestRun(kind domain.RunKind) testsrepository.RunResult {
	result, err := s.runs.LatestByKind(s.Ctx, string(kind))
	s.Require().NoError(err)
	return result
}

func (s *DigestTestSuite) TestMorningDigestPostsHeaderAndPicks() {
	s.addBlog("a", "b", "c")
	s.learnedProfile()
	s.answerPicks("a", "b")

	err := s.digest.Run(s.Ctx, domain.RunKindDigestMorning)

	s.Require().NoError(err)
	posted := s.slack.Posted()
	s.Require().Len(posted, 3)
	s.Equal("Your reading for Sat, 14 Nov: 2 picks", posted[0])
	s.Contains(posted[1], s.articleURL("a"))
	s.Contains(posted[2], s.articleURL("b"))
	s.Len(s.slack.Reactions(), 4)
	s.Equal(2, s.CountRows("post"))
	s.Equal(testsrepository.RunResult{Status: string(domain.RunStatusOK), Backend: string(llm.Claude)},
		s.latestRun(domain.RunKindDigestMorning))
}

func (s *DigestTestSuite) TestShowHeader() {
	s.addBlog("a", "b")
	s.learnedProfile()
	s.answerPicks("a")

	err := s.digest.Run(s.Ctx, domain.RunKindDigestShow)

	s.Require().NoError(err)
	s.Require().NotEmpty(s.slack.Posted())
	s.Equal("On request: 1 pick", s.slack.Posted()[0])
}

func (s *DigestTestSuite) TestNothingFitsPostsOneLine() {
	s.addBlog("a", "b")
	s.learnedProfile()
	s.answerPicks()

	err := s.digest.Run(s.Ctx, domain.RunKindDigestShow)

	s.Require().NoError(err)
	s.Equal([]string{nothingLine}, s.slack.Posted())
}

func (s *DigestTestSuite) TestNoCandidatesSkipsAgent() {
	sourceID := s.addBlog()
	s.learnedProfile()
	articleID, err := s.articles.Insert(s.Ctx, testsrepository.ArticleRow{
		SourceID: sourceID, URL: s.articleURL("a"), PublishedAt: s.Clock.Now().Add(-time.Hour), Now: s.Clock.Now(),
	})
	s.Require().NoError(err)
	runID, err := s.runs.Insert(s.Ctx, string(domain.RunKindDigestShow), string(domain.RunStatusOK), s.Clock.Now().Add(-time.Hour))
	s.Require().NoError(err)
	_, err = s.posts.Insert(s.Ctx, articleID, runID, s.Clock.Now().Add(-time.Hour))
	s.Require().NoError(err)

	err = s.digest.Run(s.Ctx, domain.RunKindDigestShow)

	s.Require().NoError(err)
	s.Equal([]string{nothingLine}, s.slack.Posted())
	s.Empty(s.agents.Args(llm.Claude))
}

func (s *DigestTestSuite) TestSecondShowNeverRepeatsAnArticle() {
	s.addBlog("a", "b", "c")
	s.learnedProfile()
	s.answerPicks("a", "b")
	s.Require().NoError(s.digest.Run(s.Ctx, domain.RunKindDigestShow))
	s.answerPicks("c")

	err := s.digest.Run(s.Ctx, domain.RunKindDigestShow)

	s.Require().NoError(err)
	s.Equal([]string{s.articleURL("c")}, s.pickCandidates())
}

func (s *DigestTestSuite) TestAllBackendsFailPostsFailureLine() {
	s.addBlog("a", "b")
	s.learnedProfile()
	s.agents.Fail(llm.Claude)
	s.agents.Fail(llm.Codex)

	err := s.digest.Run(s.Ctx, domain.RunKindDigestMorning)

	s.Require().ErrorIs(err, llm.ErrAllBackendsFailed)
	s.Equal([]string{failureLine}, s.slack.Posted())
	s.Equal(string(domain.RunStatusFailed), s.latestRun(domain.RunKindDigestMorning).Status)
}

func (s *DigestTestSuite) TestFirstRunBuildsProfileFromSources() {
	s.addBlog("a", "b", "c", "d", "e", "f")
	s.answerProfile(llm.SkillInitProfile, digestNotes)
	s.answerPicks("f")

	err := s.digest.Run(s.Ctx, domain.RunKindDigestMorning)

	s.Require().NoError(err)
	args := s.agents.Args(llm.Claude)
	s.Require().NotEmpty(args)
	s.True(strings.HasPrefix(args[0], initPrompt))
	s.FileExists(s.profilePath)
	change, err := s.versions.LatestChange(s.Ctx)
	s.Require().NoError(err)
	s.Equal(string(domain.ProfileInit), change)
	s.Equal(string(domain.RunStatusOK), s.latestRun(domain.RunKindProfileInit).Status)
	var input struct {
		Sources []struct {
			RecentTitles []string `json:"recent_titles"`
		} `json:"sources"`
	}
	s.Require().NoError(json.Unmarshal(s.agents.Input(llm.Claude, llm.SkillInitProfile), &input))
	s.Require().Len(input.Sources, 1)
	s.Equal([]string{"f", "e", "d", "c", "b"}, input.Sources[0].RecentTitles)
}

func (s *DigestTestSuite) TestFailedProfileInitKeepsOldProfile() {
	s.addBlog("a", "b")
	s.writeProfile(digestNotes, "")
	s.answerPicks("a")

	err := s.digest.Run(s.Ctx, domain.RunKindDigestMorning)

	s.Require().NoError(err)
	s.Equal(string(domain.RunStatusFailed), s.latestRun(domain.RunKindProfileInit).Status)
	s.Equal(string(domain.RunStatusOK), s.latestRun(domain.RunKindDigestMorning).Status)
	var input struct {
		InterestsMD string `json:"interests_md"`
	}
	s.Require().NoError(json.Unmarshal(s.agents.Input(llm.Claude, llm.SkillPickArticles), &input))
	s.Equal(fmt.Sprintf(digestProfileFmt, digestNotes, ""), input.InterestsMD)
	s.Equal(1, s.CountRows("post"))
}

func (s *DigestTestSuite) TestInitRunsWhenOnlyNotesExist() {
	s.addBlog("a", "b")
	s.writeProfile(digestNotes, "")
	s.answerProfile(llm.SkillInitProfile, "changed")
	s.answerPicks("a")

	err := s.digest.Run(s.Ctx, domain.RunKindDigestMorning)

	s.Require().NoError(err)
	args := s.agents.Args(llm.Claude)
	s.Require().NotEmpty(args)
	s.True(strings.HasPrefix(args[0], initPrompt))
	var input struct {
		InterestsMD string `json:"interests_md"`
	}
	s.Require().NoError(json.Unmarshal(s.agents.Input(llm.Claude, llm.SkillInitProfile), &input))
	s.Contains(input.InterestsMD, digestNotes)
	saved, err := os.ReadFile(s.profilePath)
	s.Require().NoError(err)
	s.Equal(fmt.Sprintf(digestProfileFmt, digestNotes, answeredLearned), string(saved))
}

func (s *DigestTestSuite) TestNewVotesUpdateProfileBeforePick() {
	sourceID := s.addBlog("a", "b")
	s.learnedProfile()
	votedAt := s.Clock.Now().Add(-time.Hour)
	articleID, err := s.articles.Insert(s.Ctx, testsrepository.ArticleRow{
		SourceID: sourceID, URL: s.articleURL("old"), PublishedAt: votedAt, Now: votedAt,
	})
	s.Require().NoError(err)
	runID, err := s.runs.Insert(s.Ctx, string(domain.RunKindDigestShow), string(domain.RunStatusOK), votedAt)
	s.Require().NoError(err)
	postID, err := s.posts.Insert(s.Ctx, articleID, runID, votedAt)
	s.Require().NoError(err)
	s.Require().NoError(s.reactions.Insert(s.Ctx, testsrepository.ReactionRow{
		PostID: postID, UserID: "U1", Vote: int(domain.VoteUp), CreatedAt: votedAt,
	}))
	s.answerProfile(llm.SkillUpdateProfile, digestNotes)
	s.answerPicks("a")

	err = s.digest.Run(s.Ctx, domain.RunKindDigestMorning)

	s.Require().NoError(err)
	args := s.agents.Args(llm.Claude)
	s.Require().Len(args, 2)
	s.True(strings.HasPrefix(args[0], updatePrompt))
	s.True(strings.HasPrefix(args[1], pickPrompt))
	s.Equal(string(domain.RunStatusOK), s.latestRun(domain.RunKindProfileUpdate).Status)
}

func (s *DigestTestSuite) TestNoNewVotesSkipsProfileUpdate() {
	s.addBlog("a", "b")
	s.learnedProfile()
	s.answerPicks("a")

	err := s.digest.Run(s.Ctx, domain.RunKindDigestMorning)

	s.Require().NoError(err)
	args := s.agents.Args(llm.Claude)
	s.Require().Len(args, 1)
	s.True(strings.HasPrefix(args[0], pickPrompt))
}

func (s *DigestTestSuite) TestMorningWithoutBlogsPostsNothing() {
	err := s.digest.Run(s.Ctx, domain.RunKindDigestMorning)

	s.Require().NoError(err)
	s.Empty(s.slack.Posted())
	s.Equal(string(domain.RunStatusOK), s.latestRun(domain.RunKindDigestMorning).Status)
	s.Empty(s.agents.Args(llm.Claude))
}

func (s *DigestTestSuite) TestShowWithoutBlogsPostsHint() {
	err := s.digest.Run(s.Ctx, domain.RunKindDigestShow)

	s.Require().NoError(err)
	s.Equal([]string{noBlogsLine}, s.slack.Posted())
}

func (s *DigestTestSuite) TestReactionFailureStillStoresPost() {
	s.addBlog("a", "b")
	s.learnedProfile()
	s.answerPicks("a")
	s.slack.FailMethod = "reactions.add"

	err := s.digest.Run(s.Ctx, domain.RunKindDigestShow)

	s.Require().NoError(err)
	s.Len(s.slack.Posted(), 2)
	s.Equal(1, s.CountRows("post"))
}

func (s *DigestTestSuite) TestCancelWhileAddingReactionsStillStoresPost() {
	s.addBlog("a", "b")
	s.learnedProfile()
	s.answerPicks("a")
	ctx, cancel := context.WithCancel(s.Ctx)
	defer cancel()
	s.slack.BlockMethod = "reactions.add"
	s.slack.OnBlock = cancel

	err := s.digest.Run(ctx, domain.RunKindDigestShow)

	s.Require().NoError(err)
	s.Equal(1, s.CountRows("post"))
}
