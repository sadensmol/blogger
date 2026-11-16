package tests_test

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/suite"

	"github.com/sadensmol/blogger/internal/domain"
	"github.com/sadensmol/blogger/internal/llm"
	"github.com/sadensmol/blogger/tests"
	testsrepository "github.com/sadensmol/blogger/tests/repository"
)

const (
	themesFeedPath   = "/themes.xml"
	themesChannelID  = "C1"
	themesVoterID    = "U1"
	themesDigestSize = 5
	themesItemFmt    = `<item><title>%s</title><link>%s</link><description>About %s.</description><pubDate>%s</pubDate></item>`
	tagPrompt        = "-p /tag-articles"
	themesLinePrefix = "Themes: "
)

type ThemesTestSuite struct {
	tests.Suite

	feeds       *tests.FakeFeeds
	slack       *tests.FakeSlack
	agents      *tests.FakeAgents
	profilePath string
	app         *tests.App
	sources     *testsrepository.Source
	articles    *testsrepository.Article
	tags        *testsrepository.ArticleTag
	posts       *testsrepository.Post
	reactions   *testsrepository.Reaction
	runs        *testsrepository.Run
	versions    *testsrepository.ProfileVersion
}

func TestThemesTestSuite(t *testing.T) {
	suite.Run(t, &ThemesTestSuite{})
}

func (s *ThemesTestSuite) SetupTest() {
	s.Suite.SetupTest()
	s.feeds = tests.NewFakeFeeds()
	s.slack = tests.NewFakeSlack()
	s.agents = tests.NewFakeAgents(s.T())
	s.profilePath = filepath.Join(s.T().TempDir(), "interests.md")
	s.sources = testsrepository.NewSource(s.DB)
	s.articles = testsrepository.NewArticle(s.DB)
	s.tags = testsrepository.NewArticleTag(s.DB)
	s.posts = testsrepository.NewPost(s.DB)
	s.reactions = testsrepository.NewReaction(s.DB)
	s.runs = testsrepository.NewRun(s.DB)
	s.versions = testsrepository.NewProfileVersion(s.DB)

	location, err := time.LoadLocation("Europe/Berlin")
	s.Require().NoError(err)
	s.app = tests.NewApp(s.T(), tests.AppData{
		DB:          s.DB,
		Now:         s.Clock.Now,
		Agents:      s.agents,
		Backends:    []llm.Backend{llm.Claude, llm.Codex},
		SlackURL:    s.slack.URL,
		ChannelID:   themesChannelID,
		ProfilePath: s.profilePath,
		Location:    location,
		DigestSize:  themesDigestSize,
	})
}

func (s *ThemesTestSuite) TearDownTest() {
	s.feeds.Close()
	s.slack.Close()
	s.Suite.TearDownTest()
}

func (s *ThemesTestSuite) articleURL(name string) string {
	return fmt.Sprintf("%s/%s", s.feeds.URL(), name)
}

func (s *ThemesTestSuite) addBlog(status domain.SourceStatus, names ...string) int64 {
	var items strings.Builder
	for _, name := range names {
		fmt.Fprintf(&items, themesItemFmt, name, s.articleURL(name), name, s.Clock.Now().Add(-time.Hour).Format(time.RFC1123Z))
	}
	s.feeds.Set(themesFeedPath, "application/rss+xml", fmt.Sprintf(itemsFmt, items.String()))
	id, err := s.sources.Insert(s.Ctx, testsrepository.SourceRow{
		FeedURL: fmt.Sprintf("%s%s", s.feeds.URL(), themesFeedPath),
		Status:  string(status),
	}, s.Clock.Now())
	s.Require().NoError(err)
	return id
}

func (s *ThemesTestSuite) learnedProfile() {
	s.Require().NoError(os.WriteFile(s.profilePath, fmt.Appendf(nil, digestProfileFmt, digestNotes, digestLearned), 0o644))
	s.Require().NoError(s.versions.Insert(s.Ctx, string(domain.ProfileInit), s.Clock.Now().Add(-2*time.Hour)))
}

func (s *ThemesTestSuite) answerTags(tags map[string][]string) {
	articles := make([]map[string]any, 0, len(tags))
	for name, t := range tags {
		articles = append(articles, map[string]any{"url": s.articleURL(name), "tags": t})
	}
	s.agents.Answer(llm.Claude, llm.SkillTagArticles, map[string]any{"articles": articles})
}

func (s *ThemesTestSuite) answerPicks(names ...string) {
	picks := make([]map[string]any, 0, len(names))
	for _, name := range names {
		picks = append(picks, map[string]any{
			"url": s.articleURL(name), "description": "A post.", "topics": []string{"go"}, "reason": "Matches Go.",
		})
	}
	s.agents.Answer(llm.Claude, llm.SkillPickArticles, map[string]any{"picks": picks})
}

func (s *ThemesTestSuite) answerProfileUpdate() {
	s.agents.Answer(llm.Claude, llm.SkillUpdateProfile,
		map[string]any{"interests_md": fmt.Sprintf(digestProfileFmt, digestNotes, answeredLearned)})
}

func (s *ThemesTestSuite) insertArticle(sourceID int64, name string, age time.Duration, tags ...string) int64 {
	id, err := s.articles.Insert(s.Ctx, testsrepository.ArticleRow{
		SourceID: sourceID, URL: s.articleURL(name), PublishedAt: s.Clock.Now().Add(-age), Now: s.Clock.Now(),
	})
	s.Require().NoError(err)
	if len(tags) > 0 {
		s.Require().NoError(s.tags.Insert(s.Ctx, id, s.Clock.Now(), tags...))
	}
	return id
}

func (s *ThemesTestSuite) insertPost(sourceID int64, name string, tags ...string) domain.ReactionEvent {
	articleID := s.insertArticle(sourceID, name, 3*time.Hour, tags...)
	postedAt := s.Clock.Now().Add(-2 * time.Hour)
	runID, err := s.runs.Insert(s.Ctx, string(domain.RunKindDigestShow), string(domain.RunStatusOK), postedAt)
	s.Require().NoError(err)
	_, err = s.posts.Insert(s.Ctx, articleID, runID, postedAt)
	s.Require().NoError(err)
	return domain.ReactionEvent{ChannelID: themesChannelID, MessageTS: fmt.Sprintf("ts-%d", articleID), UserID: themesVoterID}
}

func (s *ThemesTestSuite) insertVotedPost(sourceID int64, name string, vote domain.Vote, tags ...string) {
	articleID := s.insertArticle(sourceID, name, 3*time.Hour, tags...)
	postedAt := s.Clock.Now().Add(-2 * time.Hour)
	runID, err := s.runs.Insert(s.Ctx, string(domain.RunKindDigestShow), string(domain.RunStatusOK), postedAt)
	s.Require().NoError(err)
	postID, err := s.posts.Insert(s.Ctx, articleID, runID, postedAt)
	s.Require().NoError(err)
	s.Require().NoError(s.reactions.Insert(s.Ctx, testsrepository.ReactionRow{
		PostID: postID, UserID: themesVoterID, Vote: int(vote), CreatedAt: s.Clock.Now().Add(-time.Hour),
	}))
}

func (s *ThemesTestSuite) vote(event domain.ReactionEvent, v domain.Vote) {
	event.Vote = v
	s.Require().NoError(s.app.Votes.Add(s.Ctx, event))
}

func (s *ThemesTestSuite) status(id int64) string {
	status, err := s.sources.Status(s.Ctx, id)
	s.Require().NoError(err)
	return status
}

func (s *ThemesTestSuite) agentRuns(prompt string) int {
	n := 0
	for _, args := range s.agents.Args(llm.Claude) {
		if strings.HasPrefix(args, prompt) {
			n++
		}
	}
	return n
}

func (s *ThemesTestSuite) tagsOf(name string) []string {
	tags, err := s.tags.ListByURL(s.Ctx, s.articleURL(name))
	s.Require().NoError(err)
	return tags
}

func (s *ThemesTestSuite) tagInput() (urls, knownTags []string) {
	var input struct {
		Articles []struct {
			URL string `json:"url"`
		} `json:"articles"`
		KnownTags []string `json:"known_tags"`
	}
	s.Require().NoError(json.Unmarshal(s.agents.Input(llm.Claude, llm.SkillTagArticles), &input))
	for _, a := range input.Articles {
		urls = append(urls, a.URL)
	}
	return urls, input.KnownTags
}

type themeJSON struct {
	Tag  string `json:"tag"`
	Up   int    `json:"up"`
	Down int    `json:"down"`
}

// themeScores keeps only themes with votes: a theme without votes may be listed or left out.
func themeScores(themes []themeJSON) map[string]domain.Votes {
	scores := map[string]domain.Votes{}
	for _, t := range themes {
		if t.Up != 0 || t.Down != 0 {
			scores[t.Tag] = domain.Votes{Up: t.Up, Down: t.Down}
		}
	}
	return scores
}

func (s *ThemesTestSuite) serviceThemeScores() map[string]domain.Votes {
	themes, err := s.app.Votes.Themes(s.Ctx)
	s.Require().NoError(err)
	scores := map[string]domain.Votes{}
	for _, t := range themes {
		if t.Votes.Up != 0 || t.Votes.Down != 0 {
			scores[string(t.Tag)] = t.Votes
		}
	}
	return scores
}

type pickJobInput struct {
	Candidates []struct {
		URL  string   `json:"url"`
		Tags []string `json:"tags"`
	} `json:"candidates"`
	Themes []themeJSON `json:"themes"`
}

func (s *ThemesTestSuite) pickInput() pickJobInput {
	var input pickJobInput
	s.Require().NoError(json.Unmarshal(s.agents.Input(llm.Claude, llm.SkillPickArticles), &input))
	return input
}

func (s *ThemesTestSuite) pickCandidates() []string {
	urls := []string{}
	for _, c := range s.pickInput().Candidates {
		urls = append(urls, c.URL)
	}
	return urls
}

type updateJobInput struct {
	Votes []struct {
		URL  string   `json:"url"`
		Vote string   `json:"vote"`
		Tags []string `json:"tags"`
	} `json:"votes"`
	Themes []themeJSON `json:"themes"`
}

func (s *ThemesTestSuite) updateInput() updateJobInput {
	var input updateJobInput
	s.Require().NoError(json.Unmarshal(s.agents.Input(llm.Claude, llm.SkillUpdateProfile), &input))
	return input
}

type discoverJobInput struct {
	Sources []map[string]any `json:"sources"`
	Themes  []themeJSON      `json:"themes"`
}

func (s *ThemesTestSuite) discoverInput() discoverJobInput {
	var input discoverJobInput
	s.Require().NoError(json.Unmarshal(s.agents.Input(llm.Claude, llm.SkillDiscoverBlogs), &input))
	return input
}

func (s *ThemesTestSuite) TestDigestTagsTheCandidatesBeforeThePick() {
	s.addBlog(domain.SourceActive, "a", "b")
	s.learnedProfile()
	s.answerTags(map[string][]string{"a": {"go"}, "b": {"sqlite"}})
	s.answerPicks("a")

	err := s.app.Digest.Run(s.Ctx, domain.RunKindDigestMorning)

	s.Require().NoError(err)
	args := s.agents.Args(llm.Claude)
	s.Require().Len(args, 2)
	s.True(strings.HasPrefix(args[0], tagPrompt))
	s.True(strings.HasPrefix(args[1], pickPrompt))
	urls, _ := s.tagInput()
	s.ElementsMatch([]string{s.articleURL("a"), s.articleURL("b")}, urls)
}

func (s *ThemesTestSuite) TestShowTagsTheCandidatesBeforeThePick() {
	s.addBlog(domain.SourceActive, "a")
	s.learnedProfile()
	s.answerTags(map[string][]string{"a": {"go"}})
	s.answerPicks("a")

	err := s.app.Digest.Run(s.Ctx, domain.RunKindDigestShow)

	s.Require().NoError(err)
	args := s.agents.Args(llm.Claude)
	s.Require().Len(args, 2)
	s.True(strings.HasPrefix(args[0], tagPrompt))
	s.True(strings.HasPrefix(args[1], pickPrompt))
}

func (s *ThemesTestSuite) TestDigestStoresTheAnsweredTags() {
	s.addBlog(domain.SourceActive, "a", "b")
	s.learnedProfile()
	s.answerTags(map[string][]string{"a": {"go", "databases"}, "b": {"sqlite"}})
	s.answerPicks("a")

	err := s.app.Digest.Run(s.Ctx, domain.RunKindDigestMorning)

	s.Require().NoError(err)
	s.Require().Equal(1, s.agentRuns(tagPrompt))
	s.Equal([]string{"databases", "go"}, s.tagsOf("a"))
	s.Equal([]string{"sqlite"}, s.tagsOf("b"))
}

func (s *ThemesTestSuite) TestLaterDigestNeverAsksAgainForATaggedArticle() {
	s.addBlog(domain.SourceActive, "a", "b")
	s.learnedProfile()
	s.answerTags(map[string][]string{"a": {"go"}, "b": {"sqlite"}})
	s.answerPicks("a")
	s.Require().NoError(s.app.Digest.Run(s.Ctx, domain.RunKindDigestMorning))
	s.answerPicks("b")

	err := s.app.Digest.Run(s.Ctx, domain.RunKindDigestShow)

	s.Require().NoError(err)
	s.Equal(1, s.agentRuns(tagPrompt))
	s.Equal(2, s.agentRuns(pickPrompt))
}

func (s *ThemesTestSuite) TestDigestAsksOnlyForTheUntaggedCandidates() {
	sourceID := s.addBlog(domain.SourceActive)
	s.learnedProfile()
	s.insertArticle(sourceID, "a", time.Hour, "go")
	s.insertArticle(sourceID, "b", 2*time.Hour)
	s.answerTags(map[string][]string{"b": {"sqlite"}})
	s.answerPicks("a")

	err := s.app.Digest.Run(s.Ctx, domain.RunKindDigestShow)

	s.Require().NoError(err)
	s.Require().Equal(1, s.agentRuns(tagPrompt))
	urls, _ := s.tagInput()
	s.Equal([]string{s.articleURL("b")}, urls)
}

func (s *ThemesTestSuite) TestTagInputHoldsTheKnownTags() {
	sourceID := s.addBlog(domain.SourceActive)
	s.learnedProfile()
	s.insertArticle(sourceID, "a", time.Hour, "go", "databases")
	s.insertArticle(sourceID, "b", 2*time.Hour)
	s.answerTags(map[string][]string{"b": {"go"}})
	s.answerPicks("a")

	err := s.app.Digest.Run(s.Ctx, domain.RunKindDigestShow)

	s.Require().NoError(err)
	s.Require().Equal(1, s.agentRuns(tagPrompt))
	_, known := s.tagInput()
	s.ElementsMatch([]string{"go", "databases"}, known)
}

func (s *ThemesTestSuite) TestArticleWithoutAValidTagIsAskedAgain() {
	sourceID := s.addBlog(domain.SourceActive)
	s.learnedProfile()
	s.insertArticle(sourceID, "a", time.Hour)
	s.answerTags(map[string][]string{"a": {"", "   "}})
	s.answerPicks()
	s.Require().NoError(s.app.Digest.Run(s.Ctx, domain.RunKindDigestMorning))

	err := s.app.Digest.Run(s.Ctx, domain.RunKindDigestMorning)

	s.Require().NoError(err)
	s.Require().Equal(2, s.agentRuns(tagPrompt))
	urls, _ := s.tagInput()
	s.Equal([]string{s.articleURL("a")}, urls)
	s.Empty(s.tagsOf("a"))
}

func (s *ThemesTestSuite) TestFailedTagJobStillPostsTheDigest() {
	s.addBlog(domain.SourceActive, "a")
	s.learnedProfile()
	s.answerPicks("a")

	err := s.app.Digest.Run(s.Ctx, domain.RunKindDigestMorning)

	s.Require().NoError(err)
	s.Require().Equal(1, s.agentRuns(tagPrompt))
	posted := s.slack.Posted()
	s.Require().Len(posted, 2)
	s.Contains(posted[1], s.articleURL("a"))
	s.Empty(s.tagsOf("a"))
}

func (s *ThemesTestSuite) TestPostedPickEndsWithItsThemes() {
	s.addBlog(domain.SourceActive, "a")
	s.learnedProfile()
	s.answerTags(map[string][]string{"a": {"go", "sqlite"}})
	s.answerPicks("a")

	err := s.app.Digest.Run(s.Ctx, domain.RunKindDigestMorning)

	s.Require().NoError(err)
	posted := s.slack.Posted()
	s.Require().Len(posted, 2)
	lines := strings.Split(posted[1], "\n")
	s.Equal("Themes: go, sqlite", lines[len(lines)-1])
}

func (s *ThemesTestSuite) TestThemesLineEscapesTheTags() {
	s.addBlog(domain.SourceActive, "a")
	s.learnedProfile()
	s.answerTags(map[string][]string{"a": {"q&a <tips>"}})
	s.answerPicks("a")

	err := s.app.Digest.Run(s.Ctx, domain.RunKindDigestMorning)

	s.Require().NoError(err)
	posted := s.slack.Posted()
	s.Require().Len(posted, 2)
	lines := strings.Split(posted[1], "\n")
	s.Equal(fmt.Sprintf("%sq&amp;a &lt;tips&gt;", themesLinePrefix), lines[len(lines)-1])
}

func (s *ThemesTestSuite) TestVoteCountsForEachTagOfTheArticle() {
	sourceID := s.addBlog(domain.SourceActive)
	s.vote(s.insertPost(sourceID, "p1", "go", "sqlite"), domain.VoteUp)
	s.vote(s.insertPost(sourceID, "p2", "go"), domain.VoteDown)
	s.vote(s.insertPost(sourceID, "p3", "rust"), domain.VoteUp)

	scores := s.serviceThemeScores()

	s.Equal(map[string]domain.Votes{
		"go":     {Up: 1, Down: 1},
		"sqlite": {Up: 1},
		"rust":   {Up: 1},
	}, scores)
}

func (s *ThemesTestSuite) TestRemovedVoteLeavesTheThemeScore() {
	sourceID := s.addBlog(domain.SourceActive)
	s.vote(s.insertPost(sourceID, "p1", "go"), domain.VoteUp)
	removed := s.insertPost(sourceID, "p2", "go")
	s.vote(removed, domain.VoteUp)
	removed.Vote = domain.VoteUp
	s.Require().NoError(s.app.Votes.Remove(s.Ctx, removed))

	scores := s.serviceThemeScores()

	s.Equal(map[string]domain.Votes{"go": {Up: 1}}, scores)
}

func (s *ThemesTestSuite) TestBothThumbsOnOnePostCountForItsTags() {
	sourceID := s.addBlog(domain.SourceActive)
	event := s.insertPost(sourceID, "p1", "go")
	s.vote(event, domain.VoteUp)
	s.vote(event, domain.VoteDown)

	scores := s.serviceThemeScores()

	s.Equal(map[string]domain.Votes{"go": {Up: 1, Down: 1}}, scores)
}

func (s *ThemesTestSuite) themedShow() {
	s.learnedProfile()
	s.answerProfileUpdate()
	err := s.app.Digest.Run(s.Ctx, domain.RunKindDigestShow)
	s.Require().NoError(err)
}

func (s *ThemesTestSuite) arrangeScoredCandidates() {
	sourceID := s.addBlog(domain.SourceActive)
	s.insertVotedPost(sourceID, "x", domain.VoteUp, "go")
	s.insertVotedPost(sourceID, "y", domain.VoteDown, "sqlite")
	s.insertArticle(sourceID, "a", time.Hour, "sqlite")
	s.insertArticle(sourceID, "b", 2*time.Hour, "go")
	s.insertArticle(sourceID, "c", 2*time.Hour+30*time.Minute, "rust")
	s.answerPicks("b")
}

func (s *ThemesTestSuite) TestPickGetsTheCandidatesBestThemeFirst() {
	s.arrangeScoredCandidates()

	s.themedShow()

	s.Equal([]string{s.articleURL("b"), s.articleURL("c"), s.articleURL("a")}, s.pickCandidates())
}

func (s *ThemesTestSuite) TestPickGetsTheTagsOfEachCandidate() {
	s.arrangeScoredCandidates()

	s.themedShow()

	tags := map[string][]string{}
	for _, c := range s.pickInput().Candidates {
		tags[c.URL] = c.Tags
	}
	s.Equal(map[string][]string{
		s.articleURL("a"): {"sqlite"},
		s.articleURL("b"): {"go"},
		s.articleURL("c"): {"rust"},
	}, tags)
}

func (s *ThemesTestSuite) TestPickGetsTheThemeScores() {
	s.arrangeScoredCandidates()

	s.themedShow()

	s.Equal(map[string]domain.Votes{"go": {Up: 1}, "sqlite": {Down: 1}}, themeScores(s.pickInput().Themes))
}

func (s *ThemesTestSuite) TestPickKeepsTheOrderOfEqualThemeScores() {
	sourceID := s.addBlog(domain.SourceActive)
	s.insertVotedPost(sourceID, "x", domain.VoteUp, "go")
	s.insertArticle(sourceID, "a", time.Hour, "rust")
	s.insertArticle(sourceID, "b", 2*time.Hour, "go")
	s.insertArticle(sourceID, "c", 2*time.Hour+30*time.Minute, "zig")
	s.answerPicks("b")

	s.themedShow()

	s.Equal([]string{s.articleURL("b"), s.articleURL("a"), s.articleURL("c")}, s.pickCandidates())
}

func (s *ThemesTestSuite) TestUpdateInputListsTheTagsOfEachVote() {
	s.addBlog(domain.SourceActive, "a", "b")
	s.learnedProfile()
	s.answerTags(map[string][]string{"a": {"go", "sqlite"}, "b": {"rust"}})
	s.answerPicks("a")
	s.Require().NoError(s.app.Digest.Run(s.Ctx, domain.RunKindDigestMorning))
	messages, err := s.posts.MessagesByURL(s.Ctx, s.articleURL("a"))
	s.Require().NoError(err)
	s.Require().Len(messages, 1)
	s.vote(domain.ReactionEvent{ChannelID: themesChannelID, MessageTS: messages[0], UserID: themesVoterID}, domain.VoteUp)
	s.answerProfileUpdate()
	s.answerPicks("b")

	err = s.app.Digest.Run(s.Ctx, domain.RunKindDigestShow)

	s.Require().NoError(err)
	votes := s.updateInput().Votes
	s.Require().Len(votes, 1)
	s.Equal(s.articleURL("a"), votes[0].URL)
	s.ElementsMatch([]string{"go", "sqlite"}, votes[0].Tags)
}

func (s *ThemesTestSuite) TestUpdateInputHoldsTheThemeScores() {
	sourceID := s.addBlog(domain.SourceActive)
	s.insertVotedPost(sourceID, "x", domain.VoteUp, "go")
	s.insertVotedPost(sourceID, "y", domain.VoteDown, "go", "sqlite")
	s.insertArticle(sourceID, "a", time.Hour, "rust")
	s.answerPicks("a")

	s.themedShow()

	s.Equal(map[string]domain.Votes{"go": {Up: 1, Down: 1}, "sqlite": {Down: 1}}, themeScores(s.updateInput().Themes))
}

func (s *ThemesTestSuite) TestDownVotesKeepAnActiveBlogActive() {
	sourceID := s.addBlog(domain.SourceActive)

	for i := range 5 {
		s.vote(s.insertPost(sourceID, fmt.Sprintf("p%d", i)), domain.VoteDown)
	}

	s.Equal(string(domain.SourceActive), s.status(sourceID))
}

func (s *ThemesTestSuite) TestUpVotesKeepATrialBlogTrial() {
	sourceID := s.addBlog(domain.SourceTrial)

	for i := range 4 {
		s.vote(s.insertPost(sourceID, fmt.Sprintf("p%d", i)), domain.VoteUp)
	}

	s.Equal(string(domain.SourceTrial), s.status(sourceID))
}

func (s *ThemesTestSuite) answerNoBlogs() {
	s.agents.Answer(llm.Claude, llm.SkillDiscoverBlogs, map[string]any{"blogs": []map[string]any{}})
}

func (s *ThemesTestSuite) arrangeTaggedVotes() {
	sourceID := s.addBlog(domain.SourceActive)
	s.insertVotedPost(sourceID, "x", domain.VoteUp, "go")
	s.insertVotedPost(sourceID, "y", domain.VoteDown, "sqlite")
	s.answerNoBlogs()
}

func (s *ThemesTestSuite) arrangeBlogVotes() {
	sourceID := s.addBlog(domain.SourceActive)
	s.insertVotedPost(sourceID, "x", domain.VoteUp)
	s.insertVotedPost(sourceID, "y", domain.VoteDown)
	s.answerNoBlogs()
}

func (s *ThemesTestSuite) TestWeeklyDiscoveryGetsTheThemeScores() {
	s.arrangeTaggedVotes()

	_, err := s.app.Discovery.Run(s.Ctx)

	s.Require().NoError(err)
	s.Equal(map[string]domain.Votes{"go": {Up: 1}, "sqlite": {Down: 1}}, themeScores(s.discoverInput().Themes))
}

func (s *ThemesTestSuite) TestWeeklyDiscoveryGetsNoBlogVotes() {
	s.arrangeBlogVotes()

	_, err := s.app.Discovery.Run(s.Ctx)

	s.Require().NoError(err)
	sources := s.discoverInput().Sources
	s.Require().Len(sources, 1)
	s.NotContains(sources[0], "up")
	s.NotContains(sources[0], "down")
}

func (s *ThemesTestSuite) TestDiscoverCommandGetsTheThemeScores() {
	s.arrangeTaggedVotes()

	err := s.app.Suggest.Run(s.Ctx, fmt.Sprintf("%s/response/themes", s.slack.URL))

	s.Require().NoError(err)
	s.Equal(map[string]domain.Votes{"go": {Up: 1}, "sqlite": {Down: 1}}, themeScores(s.discoverInput().Themes))
}

func (s *ThemesTestSuite) TestDiscoverCommandGetsNoBlogVotes() {
	s.arrangeBlogVotes()

	err := s.app.Suggest.Run(s.Ctx, fmt.Sprintf("%s/response/themes", s.slack.URL))

	s.Require().NoError(err)
	sources := s.discoverInput().Sources
	s.Require().Len(sources, 1)
	s.NotContains(sources[0], "up")
	s.NotContains(sources[0], "down")
}
