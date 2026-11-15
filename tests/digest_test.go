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
	"github.com/sadensmol/blogger/internal/service"
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
	noUnreadLine     = "No unread articles in your blogs' feeds yet."
	notInChannelLine = "Could not post: the bot is not in this channel. Invite it with /invite @&lt;bot name&gt;."
	replyURLPath     = "/response/show"
	oldArticleAge    = 100 * 24 * time.Hour
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
	votes       *service.Votes
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
	app := tests.NewApp(s.T(), tests.AppData{
		DB:          s.DB,
		Now:         s.Clock.Now,
		Agents:      s.agents,
		Backends:    []llm.Backend{llm.Claude, llm.Codex},
		SlackURL:    s.slack.URL,
		ChannelID:   digestChannelID,
		ProfilePath: s.profilePath,
		Location:    location,
		DigestSize:  digestSize,
	})
	s.digest = app.Digest
	s.votes = app.Votes
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
		fmt.Fprintf(&items, datedItemFmt, name, s.articleURL(name), s.Clock.Now().Add(-time.Hour).Format(time.RFC1123Z))
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

func (s *DigestTestSuite) pickShown() map[string]int {
	var input struct {
		Candidates []struct {
			URL   string `json:"url"`
			Shown int    `json:"shown"`
		} `json:"candidates"`
	}
	s.Require().NoError(json.Unmarshal(s.agents.Input(llm.Claude, llm.SkillPickArticles), &input))
	shown := make(map[string]int, len(input.Candidates))
	for _, c := range input.Candidates {
		shown[c.URL] = c.Shown
	}
	return shown
}

func (s *DigestTestSuite) insertArticle(sourceID int64, name string, publishedAt time.Time) int64 {
	id, err := s.articles.Insert(s.Ctx, testsrepository.ArticleRow{
		SourceID: sourceID, URL: s.articleURL(name), PublishedAt: publishedAt, Now: s.Clock.Now(),
	})
	s.Require().NoError(err)
	return id
}

func (s *DigestTestSuite) insertPostsAgo(articleID int64, ages ...time.Duration) []int64 {
	ids := make([]int64, 0, len(ages))
	for _, age := range ages {
		postedAt := s.Clock.Now().Add(-age)
		runID, err := s.runs.Insert(s.Ctx, string(domain.RunKindDigestShow), string(domain.RunStatusOK), postedAt)
		s.Require().NoError(err)
		id, err := s.posts.InsertMessage(s.Ctx, testsrepository.PostRow{
			ArticleID: articleID, RunID: runID, MessageTS: fmt.Sprintf("ts-%d-%d", articleID, postedAt.Unix()), Now: postedAt,
		})
		s.Require().NoError(err)
		ids = append(ids, id)
	}
	return ids
}

func (s *DigestTestSuite) replyTexts() []string {
	var texts []string
	for _, raw := range s.slack.Replies() {
		var reply struct {
			Text string `json:"text"`
		}
		s.Require().NoError(json.Unmarshal([]byte(raw), &reply))
		texts = append(texts, reply.Text)
	}
	return texts
}

func (s *DigestTestSuite) postsWithoutMessage() int {
	n, err := s.posts.CountWithoutMessage(s.Ctx)
	s.Require().NoError(err)
	return n
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
	s.Equal(0, s.postsWithoutMessage())
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

func (s *DigestTestSuite) TestMorningWhereNothingFitsPostsOneLine() {
	s.addBlog("a", "b")
	s.learnedProfile()
	s.answerPicks()

	err := s.digest.Run(s.Ctx, domain.RunKindDigestMorning)

	s.Require().NoError(err)
	s.Equal([]string{nothingLine}, s.slack.Posted())
}

func (s *DigestTestSuite) TestShowWithZeroPicksFallsBackToNextBackend() {
	s.addBlog("a", "b")
	s.learnedProfile()
	s.answerPicks()
	s.agents.Answer(llm.Codex, llm.SkillPickArticles, map[string]any{"picks": []map[string]any{{
		"url": s.articleURL("b"), "description": "A post.", "topics": []string{"go"}, "reason": "Closest to Go.",
	}}})

	err := s.digest.Run(s.Ctx, domain.RunKindDigestShow)

	s.Require().NoError(err)
	s.Require().Len(s.slack.Posted(), 2)
	s.Equal("On request: 1 pick", s.slack.Posted()[0])
	s.Contains(s.slack.Posted()[1], s.articleURL("b"))
	s.Equal(testsrepository.RunResult{Status: string(domain.RunStatusOK), Backend: string(llm.Codex)},
		s.latestRun(domain.RunKindDigestShow))
}

func (s *DigestTestSuite) TestShowWithZeroPicksFromEveryBackendFails() {
	s.addBlog("a")
	s.learnedProfile()
	s.answerPicks()
	s.agents.Answer(llm.Codex, llm.SkillPickArticles, map[string]any{"picks": []map[string]any{}})

	err := s.digest.Run(s.Ctx, domain.RunKindDigestShow)

	s.Require().ErrorIs(err, llm.ErrAllBackendsFailed)
	s.Empty(s.slack.Posted())
	s.Equal(0, s.CountRows("post"))
}

func (s *DigestTestSuite) TestShowWithoutAnyArticleSaysWhy() {
	s.addBlog()
	s.learnedProfile()

	err := s.digest.Run(s.Ctx, domain.RunKindDigestShow)

	s.Require().NoError(err)
	s.Equal([]string{noUnreadLine}, s.slack.Posted())
	s.Empty(s.agents.Args(llm.Claude))
}

func (s *DigestTestSuite) TestShowReadsNeverFetchedBlogDespiteFreshFetch() {
	_, err := s.runs.Insert(s.Ctx, string(domain.RunKindFetch), string(domain.RunStatusOK), s.Clock.Now().Add(-10*time.Minute))
	s.Require().NoError(err)
	s.addBlog("a")
	s.learnedProfile()
	s.answerPicks("a")

	err = s.digest.Run(s.Ctx, domain.RunKindDigestShow)

	s.Require().NoError(err)
	s.Equal(1, s.feeds.Requests(digestFeedPath))
	s.Require().Len(s.slack.Posted(), 2)
	s.Contains(s.slack.Posted()[1], s.articleURL("a"))
}

func (s *DigestTestSuite) TestShowPicksFromArticlesOlderThanTwoWeeks() {
	sourceID := s.addBlog()
	s.learnedProfile()
	_, err := s.articles.Insert(s.Ctx, testsrepository.ArticleRow{
		SourceID: sourceID, URL: s.articleURL("old"), PublishedAt: s.Clock.Now().Add(-oldArticleAge), Now: s.Clock.Now(),
	})
	s.Require().NoError(err)
	_, err = s.articles.Insert(s.Ctx, testsrepository.ArticleRow{
		SourceID: sourceID, URL: s.articleURL("too-old"), PublishedAt: s.Clock.Now().Add(-tests.HistoryWindow - time.Hour), Now: s.Clock.Now(),
	})
	s.Require().NoError(err)
	s.answerPicks("old")

	err = s.digest.Run(s.Ctx, domain.RunKindDigestShow)

	s.Require().NoError(err)
	s.Equal([]string{s.articleURL("old")}, s.pickCandidates())
	s.Require().Len(s.slack.Posted(), 2)
	s.Contains(s.slack.Posted()[1], s.articleURL("old"))
}

func (s *DigestTestSuite) TestMorningSkipsArticlesOlderThanTwoWeeksAndUndated() {
	sourceID := s.addBlog()
	s.learnedProfile()
	for _, row := range []testsrepository.ArticleRow{
		{SourceID: sourceID, URL: s.articleURL("old"), PublishedAt: s.Clock.Now().Add(-oldArticleAge), Now: s.Clock.Now()},
		{SourceID: sourceID, URL: s.articleURL("undated"), Now: s.Clock.Now()},
	} {
		_, err := s.articles.Insert(s.Ctx, row)
		s.Require().NoError(err)
	}

	err := s.digest.Run(s.Ctx, domain.RunKindDigestMorning)

	s.Require().NoError(err)
	s.Equal([]string{nothingLine}, s.slack.Posted())
	s.Empty(s.agents.Args(llm.Claude))
}

func (s *DigestTestSuite) TestShowOffersUndatedArticlesLast() {
	sourceID := s.addBlog("a")
	s.learnedProfile()
	_, err := s.articles.Insert(s.Ctx, testsrepository.ArticleRow{SourceID: sourceID, URL: s.articleURL("undated"), Now: s.Clock.Now()})
	s.Require().NoError(err)
	s.answerPicks("undated")

	err = s.digest.Run(s.Ctx, domain.RunKindDigestShow)

	s.Require().NoError(err)
	s.Equal([]string{s.articleURL("a"), s.articleURL("undated")}, s.pickCandidates())
}

func (s *DigestTestSuite) TestShowFailureRepliesWithTheReason() {
	s.addBlog()
	s.learnedProfile()
	s.slack.PostFaults = []tests.PostFault{tests.PostNotInChannel}

	err := s.digest.Show(s.Ctx, fmt.Sprintf("%s%s", s.slack.URL, replyURLPath))

	s.Require().Error(err)
	s.Equal([]string{notInChannelLine}, s.replyTexts())
}

func (s *DigestTestSuite) TestShowWhereAllBackendsFailRepliesWithTheReason() {
	s.addBlog("a")
	s.learnedProfile()
	s.agents.Fail(llm.Claude)
	s.agents.Fail(llm.Codex)

	err := s.digest.Show(s.Ctx, fmt.Sprintf("%s%s", s.slack.URL, replyURLPath))

	s.Require().ErrorIs(err, llm.ErrAllBackendsFailed)
	s.Equal([]string{failureLine}, s.replyTexts())
	s.Empty(s.slack.Posted())
}

func (s *DigestTestSuite) TestShowSuccessRepliesWithOnlyTheTokenLine() {
	s.addBlog("a")
	s.learnedProfile()
	s.answerPicks("a")

	err := s.digest.Show(s.Ctx, fmt.Sprintf("%s%s", s.slack.URL, replyURLPath))

	s.Require().NoError(err)
	s.Require().Len(s.slack.Posted(), 2)
	replies := s.slack.Replies()
	s.Require().Len(replies, 1)
	requireOnlyTokenLine(s.T(), replies[0], tokenLineOneClaudeRun)
}

func (s *DigestTestSuite) TestShowFailureRepliesWithTheReasonAboveTheTokenLine() {
	s.addBlog()
	s.learnedProfile()
	s.slack.PostFaults = []tests.PostFault{tests.PostNotInChannel}

	err := s.digest.Show(s.Ctx, fmt.Sprintf("%s%s", s.slack.URL, replyURLPath))

	s.Require().Error(err)
	replies := s.slack.Replies()
	s.Require().Len(replies, 1)
	requireTextThenTokenLine(s.T(), replies[0], notInChannelLine, tokenLineNoCall)
}

func (s *DigestTestSuite) TestShowWhereEveryAttemptFailsCountsEachAttempt() {
	const codexIn, codexOut = 23710, 5
	s.addBlog("a")
	s.learnedProfile()
	s.answerPicks()
	s.agents.Answer(llm.Codex, llm.SkillPickArticles, map[string]any{"picks": []map[string]any{}})
	s.agents.CodexUsage(llm.SkillPickArticles, codexIn, codexOut)

	err := s.digest.Show(s.Ctx, fmt.Sprintf("%s%s", s.slack.URL, replyURLPath))

	s.Require().ErrorIs(err, llm.ErrAllBackendsFailed)
	replies := s.slack.Replies()
	s.Require().Len(replies, 1)
	requireTextThenTokenLine(s.T(), replies[0], failureLine,
		"Tokens used: 64,896 (claude: 41,055 in, 126 out; codex: 23,710 in, 5 out)")
}

func (s *DigestTestSuite) TestShowWhereNoAgentReportsUsageSaysUnknown() {
	s.addBlog("a")
	s.learnedProfile()
	s.agents.Fail(llm.Claude)
	s.agents.Fail(llm.Codex)

	err := s.digest.Show(s.Ctx, fmt.Sprintf("%s%s", s.slack.URL, replyURLPath))

	s.Require().ErrorIs(err, llm.ErrAllBackendsFailed)
	replies := s.slack.Replies()
	s.Require().Len(replies, 1)
	requireTextThenTokenLine(s.T(), replies[0], failureLine, "Tokens used: unknown (claude: not reported; codex: not reported)")
}

func (s *DigestTestSuite) TestShowWithNothingToReadRepliesWithZeroTokens() {
	s.addBlog()
	s.learnedProfile()

	err := s.digest.Show(s.Ctx, fmt.Sprintf("%s%s", s.slack.URL, replyURLPath))

	s.Require().NoError(err)
	s.Equal([]string{noUnreadLine}, s.slack.Posted())
	replies := s.slack.Replies()
	s.Require().Len(replies, 1)
	requireOnlyTokenLine(s.T(), replies[0], tokenLineNoCall)
}

func (s *DigestTestSuite) TestShowWithoutBlogsRepliesWithZeroTokens() {
	err := s.digest.Show(s.Ctx, fmt.Sprintf("%s%s", s.slack.URL, replyURLPath))

	s.Require().NoError(err)
	s.Equal([]string{noBlogsLine}, s.slack.Posted())
	replies := s.slack.Replies()
	s.Require().Len(replies, 1)
	requireOnlyTokenLine(s.T(), replies[0], tokenLineNoCall)
}

func (s *DigestTestSuite) TestShowCountsTheProfileUpdateAndThePick() {
	sourceID := s.addBlog("a", "b")
	s.learnedProfile()
	votedAt := s.Clock.Now().Add(-time.Hour)
	articleID := s.insertArticle(sourceID, "old", votedAt)
	runID, err := s.runs.Insert(s.Ctx, string(domain.RunKindDigestShow), string(domain.RunStatusOK), votedAt)
	s.Require().NoError(err)
	postID, err := s.posts.Insert(s.Ctx, articleID, runID, votedAt)
	s.Require().NoError(err)
	s.Require().NoError(s.reactions.Insert(s.Ctx, testsrepository.ReactionRow{
		PostID: postID, UserID: "U1", Vote: int(domain.VoteUp), CreatedAt: votedAt,
	}))
	s.answerProfile(llm.SkillUpdateProfile, digestNotes)
	s.answerPicks("a")

	err = s.digest.Show(s.Ctx, fmt.Sprintf("%s%s", s.slack.URL, replyURLPath))

	s.Require().NoError(err)
	s.Require().Len(s.agents.Args(llm.Claude), 2)
	replies := s.slack.Replies()
	s.Require().Len(replies, 1)
	requireOnlyTokenLine(s.T(), replies[0], "Tokens used: 82,362 (claude: 82,110 in, 252 out)")
}

func (s *DigestTestSuite) TestShowAfterMorningDigestCountsOnlyItsOwnRun() {
	s.addBlog("a", "b")
	s.learnedProfile()
	s.answerPicks("a")
	s.Require().NoError(s.digest.Run(s.Ctx, domain.RunKindDigestMorning))
	s.answerPicks("b")

	err := s.digest.Show(s.Ctx, fmt.Sprintf("%s%s", s.slack.URL, replyURLPath))

	s.Require().NoError(err)
	s.Require().Len(s.agents.Args(llm.Claude), 2)
	replies := s.slack.Replies()
	s.Require().Len(replies, 1)
	requireOnlyTokenLine(s.T(), replies[0], tokenLineOneClaudeRun)
}

func (s *DigestTestSuite) TestMorningDigestSendsNoTokenLine() {
	s.addBlog("a", "b")
	s.learnedProfile()
	s.answerPicks("a")
	s.Require().NoError(s.digest.Show(s.Ctx, fmt.Sprintf("%s%s", s.slack.URL, replyURLPath)))
	s.Require().Len(s.slack.Replies(), 1)
	requireOnlyTokenLine(s.T(), s.slack.Replies()[0], tokenLineOneClaudeRun)
	s.answerPicks("b")

	err := s.digest.Run(s.Ctx, domain.RunKindDigestMorning)

	s.Require().NoError(err)
	posted := s.slack.Posted()
	s.Require().Len(posted, 4)
	s.Equal("Your reading for Sat, 14 Nov: 1 pick", posted[2])
	s.Contains(posted[3], s.articleURL("b"))
	for _, form := range s.slack.PostedForms() {
		s.NotContains(form.Get("text"), tokenLinePrefix)
		s.NotContains(form.Get("blocks"), tokenLinePrefix)
	}
	s.Len(s.slack.Replies(), 1)
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

	err = s.digest.Run(s.Ctx, domain.RunKindDigestMorning)

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

func (s *DigestTestSuite) TestLostSlackAnswerNeverRepostsArticle() {
	s.addBlog("a", "b")
	s.learnedProfile()
	s.answerPicks("a")
	s.slack.PostFaults = []tests.PostFault{tests.PostOK, tests.PostDropped}
	s.Require().Error(s.digest.Run(s.Ctx, domain.RunKindDigestShow))
	s.answerPicks("b")

	err := s.digest.Run(s.Ctx, domain.RunKindDigestShow)

	s.Require().NoError(err)
	s.Equal([]string{s.articleURL("b")}, s.pickCandidates())
	s.Len(s.slack.Posted(), 4)
	s.Equal(1, s.postsWithoutMessage())
}

func (s *DigestTestSuite) TestRejectedPickCanBePickedAgain() {
	s.addBlog("a", "b")
	s.learnedProfile()
	s.answerPicks("a")
	s.slack.PostFaults = []tests.PostFault{tests.PostOK, tests.PostRejected}
	s.Require().Error(s.digest.Run(s.Ctx, domain.RunKindDigestShow))
	s.Equal(0, s.CountRows("post"))

	err := s.digest.Run(s.Ctx, domain.RunKindDigestShow)

	s.Require().NoError(err)
	s.Contains(s.pickCandidates(), s.articleURL("a"))
	s.Equal(1, s.CountRows("post"))
	s.Equal(0, s.postsWithoutMessage())
}

func (s *DigestTestSuite) offersUnvotedArticleAgain(kind domain.RunKind) {
	const name = "unvoted"
	sourceID := s.addBlog()
	s.learnedProfile()
	articleID := s.insertArticle(sourceID, name, s.Clock.Now().Add(-tests.RepeatAfter-2*time.Hour))
	s.insertPostsAgo(articleID, tests.RepeatAfter+time.Hour)
	s.answerPicks(name)

	err := s.digest.Run(s.Ctx, kind)

	s.Require().NoError(err)
	s.Equal(map[string]int{s.articleURL(name): 1}, s.pickShown())
	messages, err := s.posts.MessagesByURL(s.Ctx, s.articleURL(name))
	s.Require().NoError(err)
	s.Len(messages, 2)
}

func (s *DigestTestSuite) TestShowOffersUnvotedArticleAgainAfterAWeek() {
	s.offersUnvotedArticleAgain(domain.RunKindDigestShow)
}

func (s *DigestTestSuite) TestMorningOffersUnvotedArticleAgainAfterAWeek() {
	s.offersUnvotedArticleAgain(domain.RunKindDigestMorning)
}

func (s *DigestTestSuite) TestVotedArticleNeverComesBack() {
	cases := []struct {
		name     string
		vote     domain.Vote
		removed  bool
		comeBack bool
	}{
		{name: "up vote", vote: domain.VoteUp},
		{name: "down vote", vote: domain.VoteDown},
		{name: "removed up vote", vote: domain.VoteUp, removed: true, comeBack: true},
	}
	sourceID := s.addBlog()
	s.learnedProfile()
	for i, tt := range cases {
		s.Run(tt.name, func() {
			voted, fresh := fmt.Sprintf("voted-%d", i), fmt.Sprintf("fresh-%d", i)
			articleID := s.insertArticle(sourceID, voted, s.Clock.Now().Add(-time.Hour))
			s.insertArticle(sourceID, fresh, s.Clock.Now().Add(-time.Hour))
			postIDs := s.insertPostsAgo(articleID, tests.RepeatAfter+time.Hour)
			s.Require().NoError(s.reactions.Insert(s.Ctx, testsrepository.ReactionRow{
				PostID: postIDs[0], UserID: "U1", Vote: int(tt.vote), Removed: tt.removed, CreatedAt: s.Clock.Now().Add(-time.Hour),
			}))
			s.answerPicks(fresh)

			err := s.digest.Run(s.Ctx, domain.RunKindDigestShow)

			s.Require().NoError(err)
			s.Contains(s.pickCandidates(), s.articleURL(fresh))
			if tt.comeBack {
				s.Contains(s.pickCandidates(), s.articleURL(voted))
				return
			}
			s.NotContains(s.pickCandidates(), s.articleURL(voted))
		})
	}
}

func (s *DigestTestSuite) TestArticleComesBackUntilPostedThreeTimes() {
	const (
		twice  = "twice"
		thrice = "thrice"
		fresh  = "fresh"
	)
	sourceID := s.addBlog()
	s.learnedProfile()
	s.insertPostsAgo(s.insertArticle(sourceID, twice, s.Clock.Now().Add(-time.Hour)), 3*tests.RepeatAfter, 2*tests.RepeatAfter)
	s.insertPostsAgo(s.insertArticle(sourceID, thrice, s.Clock.Now().Add(-time.Hour)), 4*tests.RepeatAfter, 3*tests.RepeatAfter, 2*tests.RepeatAfter)
	s.insertArticle(sourceID, fresh, s.Clock.Now().Add(-time.Hour))
	s.answerPicks(fresh)

	err := s.digest.Run(s.Ctx, domain.RunKindDigestShow)

	s.Require().NoError(err)
	s.Equal(map[string]int{s.articleURL(twice): 2, s.articleURL(fresh): 0}, s.pickShown())
}

func (s *DigestTestSuite) insertReservedPostAgo(articleID int64, age time.Duration) {
	runID, err := s.runs.Insert(s.Ctx, string(domain.RunKindDigestShow), string(domain.RunStatusOK), s.Clock.Now().Add(-age))
	s.Require().NoError(err)
	_, err = s.posts.InsertMessage(s.Ctx, testsrepository.PostRow{ArticleID: articleID, RunID: runID, Now: s.Clock.Now().Add(-age)})
	s.Require().NoError(err)
}

func (s *DigestTestSuite) TestPostWithoutMessageIsNoShowButDelaysTheRepeat() {
	const (
		lostOld     = "lost-old"
		lostRecent  = "lost-recent"
		twiceAndLost = "twice-and-lost"
	)
	sourceID := s.addBlog()
	s.learnedProfile()
	s.insertReservedPostAgo(s.insertArticle(sourceID, lostOld, s.Clock.Now().Add(-time.Hour)), tests.RepeatAfter+time.Hour)
	s.insertReservedPostAgo(s.insertArticle(sourceID, lostRecent, s.Clock.Now().Add(-time.Hour)), 2*24*time.Hour)
	twiceID := s.insertArticle(sourceID, twiceAndLost, s.Clock.Now().Add(-time.Hour))
	s.insertPostsAgo(twiceID, 3*tests.RepeatAfter, 2*tests.RepeatAfter)
	s.insertReservedPostAgo(twiceID, tests.RepeatAfter+time.Hour)
	s.answerPicks(lostOld)

	err := s.digest.Run(s.Ctx, domain.RunKindDigestShow)

	s.Require().NoError(err)
	s.Equal(map[string]int{s.articleURL(lostOld): 0, s.articleURL(twiceAndLost): 2}, s.pickShown())
}

func (s *DigestTestSuite) TestArticlePostedTwoDaysAgoWaits() {
	sourceID := s.addBlog()
	s.learnedProfile()
	s.insertPostsAgo(s.insertArticle(sourceID, "recent", s.Clock.Now().Add(-time.Hour)), 2*24*time.Hour)

	err := s.digest.Run(s.Ctx, domain.RunKindDigestShow)

	s.Require().NoError(err)
	s.Equal([]string{noUnreadLine}, s.slack.Posted())
	s.Empty(s.agents.Args(llm.Claude))
}

func (s *DigestTestSuite) TestUnvotedArticleOutsideHistoryNeverComesBack() {
	sourceID := s.addBlog()
	s.learnedProfile()
	s.insertPostsAgo(s.insertArticle(sourceID, "ancient", s.Clock.Now().Add(-tests.HistoryWindow-time.Hour)), tests.RepeatAfter+time.Hour)

	err := s.digest.Run(s.Ctx, domain.RunKindDigestShow)

	s.Require().NoError(err)
	s.Equal([]string{noUnreadLine}, s.slack.Posted())
	s.Empty(s.agents.Args(llm.Claude))
}

func (s *DigestTestSuite) repeatThenVoteOn(post int) {
	sourceID := s.addBlog("a")
	s.learnedProfile()
	s.answerPicks("a")
	s.Require().NoError(s.digest.Run(s.Ctx, domain.RunKindDigestShow))
	s.Clock.Advance(tests.RepeatAfter + time.Hour)
	s.Require().NoError(s.digest.Run(s.Ctx, domain.RunKindDigestShow))
	s.Require().Equal(map[string]int{s.articleURL("a"): 1}, s.pickShown())
	messages, err := s.posts.MessagesByURL(s.Ctx, s.articleURL("a"))
	s.Require().NoError(err)
	s.Require().Len(messages, 2)
	s.Require().NoError(s.votes.Add(s.Ctx, domain.ReactionEvent{
		ChannelID: digestChannelID, MessageTS: messages[post], UserID: "U1", Vote: domain.VoteUp,
	}))
	s.Clock.Advance(tests.RepeatAfter + time.Hour)

	err = s.digest.Run(s.Ctx, domain.RunKindDigestShow)

	s.Require().NoError(err)
	posted := s.slack.Posted()
	s.Equal(noUnreadLine, posted[len(posted)-1])
	up, err := s.sources.UpVotes(s.Ctx, sourceID)
	s.Require().NoError(err)
	s.Equal(1, up)
}

func (s *DigestTestSuite) TestVoteOnFirstPostOfRepeatedArticleEndsRepeats() {
	s.repeatThenVoteOn(0)
}

func (s *DigestTestSuite) TestVoteOnSecondPostOfRepeatedArticleEndsRepeats() {
	s.repeatThenVoteOn(1)
}
