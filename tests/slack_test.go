package tests_test

import (
	"context"
	"fmt"
	"slices"
	"testing"
	"time"

	"github.com/slack-go/slack"
	"github.com/slack-go/slack/slackevents"
	"github.com/stretchr/testify/suite"

	"github.com/sadensmol/blogger/internal/app"
	"github.com/sadensmol/blogger/internal/config"
	controllerslack "github.com/sadensmol/blogger/internal/controller/slack"
	"github.com/sadensmol/blogger/internal/domain"
	"github.com/sadensmol/blogger/internal/llm"
	"github.com/sadensmol/blogger/internal/usecase"
	"github.com/sadensmol/blogger/tests"
	testsrepository "github.com/sadensmol/blogger/tests/repository"
)

const (
	slackOwnerID    = "UOWNER"
	slackFeedPath   = "/slack.xml"
	slackArticle    = "a"
	slackWait       = 10 * time.Second
	slackPoll       = 20 * time.Millisecond
	slackShowHeader = "On request: 1 pick"
	slackPicking    = "Picking articles…"
	slackOwnerOnly  = "Only the owner can use this bot."
)

type SlackTestSuite struct {
	tests.Suite

	feeds     *tests.FakeFeeds
	slack     *tests.FakeSlack
	agents    *tests.FakeAgents
	sources   *testsrepository.Source
	articles  *testsrepository.Article
	runs      *testsrepository.Run
	posts     *testsrepository.Post
	reactions *testsrepository.Reaction
	handler   *controllerslack.Handler

	stopWorker func()
	workerDone chan struct{}
}

func TestSlackTestSuite(t *testing.T) {
	suite.Run(t, &SlackTestSuite{})
}

func (s *SlackTestSuite) SetupTest() {
	s.Suite.SetupTest()
	s.feeds = tests.NewFakeFeeds()
	s.slack = tests.NewFakeSlack()
	s.agents = tests.NewFakeAgents(s.T())
	s.sources = testsrepository.NewSource(s.DB)
	s.articles = testsrepository.NewArticle(s.DB)
	s.runs = testsrepository.NewRun(s.DB)
	s.posts = testsrepository.NewPost(s.DB)
	s.reactions = testsrepository.NewReaction(s.DB)

	s.Require().NoError(testsrepository.NewProfileVersion(s.DB).Insert(s.Ctx, string(domain.ProfileInit), s.Clock.Now().Add(-2*time.Hour)))
	s.agents.Answer(llm.Claude, llm.SkillPickArticles, map[string]any{"picks": []map[string]any{{
		"url": s.articleURL(), "description": "A post.", "topics": []string{"go"}, "reason": "Matches Go.",
	}}})

	var jobs *usecase.Jobs
	s.handler, jobs = s.newHandlerAndJobs()
	ctx, cancel := context.WithCancel(s.Ctx)
	s.stopWorker = cancel
	s.workerDone = make(chan struct{})
	go func() {
		defer close(s.workerDone)
		jobs.Run(ctx)
	}()
}

func (s *SlackTestSuite) TearDownTest() {
	s.stopWorker()
	<-s.workerDone
	s.handler.Wait()
	s.feeds.Close()
	s.slack.Close()
	s.Suite.TearDownTest()
}

func (s *SlackTestSuite) articleURL() string {
	return fmt.Sprintf("%s/%s", s.feeds.URL(), slackArticle)
}

func (s *SlackTestSuite) newHandlerAndJobs() (*controllerslack.Handler, *usecase.Jobs) {
	app := tests.NewApp(s.T(), tests.AppData{
		DB:          s.DB,
		Now:         s.Clock.Now,
		Agents:      s.agents,
		Backends:    []llm.Backend{llm.Claude},
		SlackURL:    s.slack.URL,
		ChannelID:   digestChannelID,
		ProfilePath: writeLearnedProfile(s.T()),
		Location:    time.UTC,
		DigestSize:  digestSize,
	})
	jobs := usecase.NewJobs(app.JobsData(3))
	handler := controllerslack.NewHandler(controllerslack.HandlerData{
		Jobs:       jobs,
		Sources:    app.Sources,
		Votes:      app.Votes,
		Poster:     app.Poster,
		ChannelID:  digestChannelID,
		OwnerID:    slackOwnerID,
		Background: s.Ctx,
	})
	return handler, jobs
}

func (s *SlackTestSuite) ownerCommand(command, text string) slack.SlashCommand {
	return slack.SlashCommand{
		Command:     command,
		Text:        text,
		UserID:      slackOwnerID,
		ChannelID:   digestChannelID,
		ResponseURL: fmt.Sprintf("%s/response/r1", s.slack.URL),
	}
}

func (s *SlackTestSuite) ack(cmd slack.SlashCommand) string {
	text, ok := s.handler.SlashCommand(s.Ctx, cmd)["text"].(string)
	s.Require().True(ok)
	return text
}

func (s *SlackTestSuite) insertBlogWithArticle() int64 {
	s.feeds.Set(slackFeedPath, rssType, fmt.Sprintf(itemsFmt, fmt.Sprintf(itemFmt, slackArticle, s.articleURL())))
	sourceID, err := s.sources.Insert(s.Ctx, testsrepository.SourceRow{
		FeedURL: fmt.Sprintf("%s%s", s.feeds.URL(), slackFeedPath),
		Title:   blogTitle,
		Status:  string(domain.SourceActive),
	}, s.Clock.Now())
	s.Require().NoError(err)
	return sourceID
}

func (s *SlackTestSuite) insertPost() (int64, string) {
	sourceID := s.insertBlogWithArticle()
	articleID, err := s.articles.Insert(s.Ctx, testsrepository.ArticleRow{
		SourceID: sourceID, URL: s.articleURL(), PublishedAt: s.Clock.Now(), Now: s.Clock.Now(),
	})
	s.Require().NoError(err)
	runID, err := s.runs.Insert(s.Ctx, string(domain.RunKindDigestShow), string(domain.RunStatusOK), s.Clock.Now())
	s.Require().NoError(err)
	_, err = s.posts.Insert(s.Ctx, articleID, runID, s.Clock.Now())
	s.Require().NoError(err)
	return sourceID, fmt.Sprintf("ts-%d", articleID)
}

func (s *SlackTestSuite) ownerThumbsUp(ts string) *slackevents.ReactionAddedEvent {
	return &slackevents.ReactionAddedEvent{
		User:     slackOwnerID,
		Reaction: "+1",
		Item:     slackevents.Item{Type: "message", Channel: digestChannelID, Timestamp: ts},
	}
}

func (s *SlackTestSuite) react(ev *slackevents.ReactionAddedEvent) {
	s.Require().NoError(s.handler.ReactionAdded(s.Ctx, ev))
}

func (s *SlackTestSuite) runsOf(kind domain.RunKind) int {
	var n int
	s.Require().NoError(s.DB.QueryRowContext(s.Ctx, "SELECT COUNT(*) FROM run WHERE kind = ?", string(kind)).Scan(&n))
	return n
}

func (s *SlackTestSuite) TestShowAcksFastAndPostsLater() {
	const ackWithin = time.Second
	s.insertBlogWithArticle()
	s.agents.Sleep(llm.Claude, 2)

	started := time.Now()
	text := s.ack(s.ownerCommand("/show", ""))

	s.Less(time.Since(started), ackWithin)
	s.Equal(slackPicking, text)
	s.Eventually(func() bool { return slices.Contains(s.slack.Posted(), slackShowHeader) }, slackWait, slackPoll)
}

func (s *SlackTestSuite) TestShowWhileDigestQueuedSaysOnItsWay() {
	s.insertBlogWithArticle()
	s.agents.Sleep(llm.Claude, 2)

	first := s.ack(s.ownerCommand("/show", ""))
	second := s.ack(s.ownerCommand("/show", ""))

	s.Equal(slackPicking, first)
	s.Equal("A digest is already on its way.", second)
	s.Eventually(func() bool { return slices.Contains(s.slack.Posted(), slackShowHeader) }, slackWait, slackPoll)
	s.Equal(1, s.runsOf(domain.RunKindDigestShow))
}

func (s *SlackTestSuite) TestShowThatCannotPostRepliesWhyThroughResponseURL() {
	s.insertBlogWithArticle()
	s.slack.PostFaults = []tests.PostFault{tests.PostNotInChannel}

	text := s.ack(s.ownerCommand("/show", ""))

	s.Equal(slackPicking, text)
	s.Eventually(func() bool { return len(s.slack.Replies()) > 0 }, slackWait, slackPoll)
	s.Contains(s.slack.Replies()[0], "the bot is not in this channel")
	s.Empty(s.slack.Posted())
}

func (s *SlackTestSuite) TestShowOutsideChannelIsRefused() {
	cmd := s.ownerCommand("/show", "")
	cmd.ChannelID = "C2"

	text := s.ack(cmd)

	s.Equal(fmt.Sprintf("Use /show in <#%s>.", digestChannelID), text)
	s.Equal(slackPicking, s.ack(s.ownerCommand("/show", "")))
}

func (s *SlackTestSuite) TestCommandFromAnotherUserIsRefused() {
	cmd := s.ownerCommand("/blogs", "list")
	cmd.UserID = "UOTHER"

	text := s.ack(cmd)

	s.Equal(slackOwnerOnly, text)
}

func (s *SlackTestSuite) TestBlogsListShowsVotes() {
	_, ts := s.insertPost()
	s.react(s.ownerThumbsUp(ts))

	text := s.ack(s.ownerCommand("/blogs", "list"))

	s.Contains(text, "+1/-0")
	s.Contains(text, blogTitle)
}

func (s *SlackTestSuite) TestBlogsListWithoutBlogs() {
	text := s.ack(s.ownerCommand("/blogs", "list"))

	s.Equal("No blogs yet.", text)
}

func (s *SlackTestSuite) TestBlogsHelpShowsEscapedUsage() {
	text := s.ack(s.ownerCommand("/blogs", ""))

	s.Equal("usage: list | add &lt;url&gt; | remove &lt;id|url&gt; | pause &lt;id|url&gt; | resume &lt;id|url&gt;", text)
}

func (s *SlackTestSuite) TestBlogsRemoveUnknownRefIsEscaped() {
	text := s.ack(s.ownerCommand("/blogs", "remove https://x.com/?a&b"))

	s.Equal("No blog matches https://x.com/?a&amp;b.", text)
}

func (s *SlackTestSuite) TestBlogsRemoveUnparseableRefMatchesNoBlog() {
	text := s.ack(s.ownerCommand("/blogs", "remove foo"))

	s.Equal("No blog matches foo.", text)
}

func (s *SlackTestSuite) TestBlogsPauseAnswersInAck() {
	sourceID, _ := s.insertPost()

	text := s.ack(s.ownerCommand("/blogs", fmt.Sprintf("pause %d", sourceID)))

	s.Equal(fmt.Sprintf("Paused #%d %s.", sourceID, blogTitle), text)
	status, err := s.sources.Status(s.Ctx, sourceID)
	s.Require().NoError(err)
	s.Equal(string(domain.SourcePaused), status)
}

func (s *SlackTestSuite) TestBlogsAddRepliesThroughResponseURL() {
	s.feeds.Set(blogPath, htmlType, pageHTML)
	s.feeds.Set(feedPath, rssType, fmt.Sprintf(feedXMLFmt, blogTitle, s.feeds.URL()+blogPath, s.feeds.URL()))
	blogURL := s.feeds.URL() + blogPath

	text := s.ack(s.ownerCommand("/blogs", fmt.Sprintf("add <%s>", blogURL)))

	s.Equal(fmt.Sprintf("Checking %s…", blogURL), text)
	s.Eventually(func() bool { return len(s.slack.Replies()) > 0 }, slackWait, slackPoll)
	s.Contains(s.slack.Replies()[0], "Added #1")
}

func (s *SlackTestSuite) TestReactionFromBotIsIgnored() {
	_, ts := s.insertPost()
	ev := s.ownerThumbsUp(ts)
	ev.User = "UBOT"

	s.react(ev)

	s.Equal(0, s.CountRows("reaction"))
}

func (s *SlackTestSuite) TestReactionFromAnotherUserIsIgnored() {
	_, ts := s.insertPost()
	ev := s.ownerThumbsUp(ts)
	ev.User = "UOTHER"

	s.react(ev)

	s.Equal(0, s.CountRows("reaction"))
}

func (s *SlackTestSuite) TestReactionInAnotherChannelIsIgnored() {
	_, ts := s.insertPost()
	ev := s.ownerThumbsUp(ts)
	ev.Item.Channel = "C2"

	s.react(ev)

	s.Equal(0, s.CountRows("reaction"))
}

func (s *SlackTestSuite) TestReactionOnFileIsIgnored() {
	_, ts := s.insertPost()
	ev := s.ownerThumbsUp(ts)
	ev.Item.Type = "file"

	s.react(ev)

	s.Equal(0, s.CountRows("reaction"))
}

func (s *SlackTestSuite) TestSkinToneThumbsUpCounts() {
	sourceID, ts := s.insertPost()
	ev := s.ownerThumbsUp(ts)
	ev.Reaction = "+1::skin-tone-4"

	s.react(ev)

	up, err := s.sources.UpVotes(s.Ctx, sourceID)
	s.Require().NoError(err)
	s.Equal(1, up)
}

func (s *SlackTestSuite) TestHeartIsIgnored() {
	_, ts := s.insertPost()
	ev := s.ownerThumbsUp(ts)
	ev.Reaction = "heart"

	s.react(ev)

	s.Equal(0, s.CountRows("reaction"))
}

func (s *SlackTestSuite) TestOwnerReactionRemovedDropsVote() {
	sourceID, ts := s.insertPost()
	added := s.ownerThumbsUp(ts)
	s.react(added)

	err := s.handler.ReactionRemoved(s.Ctx, (*slackevents.ReactionRemovedEvent)(added))

	s.Require().NoError(err)
	up, err := s.sources.UpVotes(s.Ctx, sourceID)
	s.Require().NoError(err)
	s.Equal(0, up)
}

func (s *SlackTestSuite) TestServeWithBadTimeZoneFails() {
	err := app.Serve(s.Ctx, config.Serve{LogLevel: "info", TimeZone: "Mars/Base", DigestTime: "10:00"}, "dev")

	s.ErrorContains(err, "BLOGGER_TIME_ZONE")
}

func (s *SlackTestSuite) TestServeWithBadDigestTimeFails() {
	err := app.Serve(s.Ctx, config.Serve{LogLevel: "info", TimeZone: "UTC", DigestTime: "25:61"}, "dev")

	s.ErrorContains(err, "BLOGGER_DIGEST_TIME")
}
