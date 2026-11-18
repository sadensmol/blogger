package tests_test

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/slack-go/slack"
	"github.com/slack-go/slack/slackevents"
	"github.com/slack-go/slack/socketmode"
	"github.com/stretchr/testify/require"
	"github.com/stretchr/testify/suite"

	"github.com/sadensmol/blogger/internal/app"
	"github.com/sadensmol/blogger/internal/config"
	controllerslack "github.com/sadensmol/blogger/internal/controller/slack"
	"github.com/sadensmol/blogger/internal/domain"
	"github.com/sadensmol/blogger/internal/llm"
	"github.com/sadensmol/blogger/internal/slackpost"
	"github.com/sadensmol/blogger/internal/usecase"
	"github.com/sadensmol/blogger/tests"
	testsrepository "github.com/sadensmol/blogger/tests/repository"
)

const (
	slackOwnerID    = tests.OwnerID
	slackFeedPath   = "/slack.xml"
	slackArticle    = "a"
	slackWait       = 10 * time.Second
	slackPoll       = 20 * time.Millisecond
	slackShowHeader = "On request: 1 pick"
	slackPicking    = "Picking articles…"
	slackOwnerOnly  = "Only the owner can use this bot."
	slackLooking    = "Looking for blogs…"
	slackOtherUser  = "UOTHER"
	slackBlogTitle  = "Tom & <Jerry>"
	slackAbout      = "Writes about Go & <SQL>."
	slackAdded      = "Added ✓"
	slackBusy       = "Busy right now, try again in a few minutes."

	tokenLinePrefix       = "Tokens used:"
	tokenLineNoCall       = "Tokens used: 0 (no LLM call)"
	tokenLineOneClaudeRun = "Tokens used: 41,181 (claude: 41,055 in, 126 out)"
	tokenLineTwoClaudeRuns = "Tokens used: 82,362 (claude: 82,110 in, 252 out)"
	maxSectionRunes       = 3000
)

type slackMessage struct {
	Text   string       `json:"text"`
	Blocks []slackBlock `json:"blocks"`
}

type slackBlock struct {
	Type string `json:"type"`
	Text struct {
		Text string `json:"text"`
	} `json:"text"`
	Elements []struct {
		Text string `json:"text"`
	} `json:"elements"`
}

func parseSlackMessage(t *testing.T, raw string) slackMessage {
	t.Helper()

	var msg slackMessage
	require.NoError(t, json.Unmarshal([]byte(raw), &msg))
	return msg
}

func requireTokenLineBlock(t *testing.T, block slackBlock, tokenLine string) {
	t.Helper()

	require.Equal(t, string(slack.MBTContext), block.Type)
	require.Len(t, block.Elements, 1)
	require.Equal(t, tokenLine, block.Elements[0].Text)
}

// requireTextThenTokenLine checks a message that keeps text, shows it in sections of at most 3000 characters cut at
// line ends, and ends with one context block that holds the token line.
func requireTextThenTokenLine(t *testing.T, raw, text, tokenLine string) {
	t.Helper()

	msg := parseSlackMessage(t, raw)
	require.Equal(t, text, msg.Text)
	require.GreaterOrEqual(t, len(msg.Blocks), 2, "the text sections and the token line")
	sections := make([]string, 0, len(msg.Blocks)-1)
	for _, block := range msg.Blocks[:len(msg.Blocks)-1] {
		require.Equal(t, string(slack.MBTSection), block.Type)
		require.LessOrEqual(t, utf8.RuneCountInString(block.Text.Text), maxSectionRunes)
		sections = append(sections, block.Text.Text)
	}
	require.Equal(t, text, strings.Join(sections, "\n"))
	requireTokenLineBlock(t, msg.Blocks[len(msg.Blocks)-1], tokenLine)
}

func requireOnlyTokenLine(t *testing.T, raw, tokenLine string) {
	t.Helper()

	msg := parseSlackMessage(t, raw)
	require.Len(t, msg.Blocks, 1)
	requireTokenLineBlock(t, msg.Blocks[0], tokenLine)
	require.Contains(t, []string{"", tokenLine}, msg.Text)
}

const ephemeralCall = "chat.postEphemeral"

// ownerAnswer checks that the run answered with one private channel message to the owner and returns that message.
func ownerAnswer(t *testing.T, fake *tests.FakeSlack) string {
	t.Helper()

	ephemerals := fake.Ephemerals()
	require.Len(t, ephemerals, 1)
	require.Equal(t, digestChannelID, ephemerals[0].Channel)
	require.Equal(t, slackOwnerID, ephemerals[0].User)
	return ephemerals[0].Message
}

func callMethods(calls []tests.SlackCall) []string {
	methods := make([]string, 0, len(calls))
	for _, call := range calls {
		methods = append(methods, call.Method)
	}
	return methods
}

func requireNoTokenLine(t *testing.T, raw string) {
	t.Helper()

	msg := parseSlackMessage(t, raw)
	require.Empty(t, msg.Blocks)
	require.NotContains(t, msg.Text, tokenLinePrefix)
}

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
	proposals *testsrepository.Proposal
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
	s.proposals = testsrepository.NewProposal(s.DB)

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

func (s *SlackTestSuite) newApp(backend llm.Backend) *tests.App {
	return tests.NewApp(s.T(), tests.AppData{
		DB:          s.DB,
		Now:         s.Clock.Now,
		Agents:      s.agents,
		Backends:    []llm.Backend{backend},
		SlackURL:    s.slack.URL,
		ChannelID:   digestChannelID,
		ProfilePath: writeLearnedProfile(s.T()),
		Location:    time.UTC,
		DigestSize:  digestSize,
	})
}

func (s *SlackTestSuite) newHandlerAndJobs() (*controllerslack.Handler, *usecase.Jobs) {
	app := s.newApp(llm.Claude)
	jobs := usecase.NewJobs(app.JobsData(3))
	handler := controllerslack.NewHandler(controllerslack.HandlerData{
		Jobs:       jobs,
		Suggest:    app.Suggest,
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

func (s *SlackTestSuite) ackPayload(handler *controllerslack.Handler, cmd slack.SlashCommand) string {
	raw, err := json.Marshal(handler.SlashCommand(s.Ctx, cmd))
	s.Require().NoError(err)
	return string(raw)
}

func (s *SlackTestSuite) replyURL() string {
	return fmt.Sprintf("%s/response/r1", s.slack.URL)
}

func (s *SlackTestSuite) insertBlogWithArticle() int64 {
	s.feeds.Set(slackFeedPath, rssType, fmt.Sprintf(itemsFmt, fmt.Sprintf(itemFmt, slackArticle, s.articleURL())))
	answerGoTags(s.agents, s.articleURL())
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

func (s *SlackTestSuite) feedURL(path string) string {
	return fmt.Sprintf("%s%s", s.feeds.URL(), path)
}

func (s *SlackTestSuite) siteURL(path string) string {
	return fmt.Sprintf("%s%s", s.feeds.URL(), strings.TrimSuffix(path, ".xml"))
}

func (s *SlackTestSuite) setGoodFeed(path string) {
	s.feeds.Set(path, rssType, fmt.Sprintf(itemsFmt, fmt.Sprintf(itemFmt, "Post", s.feedURL("/post"))))
}

func (s *SlackTestSuite) answerBlogs(paths ...string) {
	blogs := make([]map[string]any, 0, len(paths))
	for _, path := range paths {
		blogs = append(blogs, map[string]any{
			"site_url": s.siteURL(path), "feed_url": s.feedURL(path), "title": slackBlogTitle, "about": slackAbout, "reason": goodReason,
		})
	}
	s.agents.Answer(llm.Claude, llm.SkillDiscoverBlogs, map[string]any{"blogs": blogs})
}

// discover waits for the posted proposals too: the run stores their message ts after the post.
func (s *SlackTestSuite) discover() {
	s.Equal(slackLooking, s.ack(s.ownerCommand("/discover", "")))
	s.Eventually(func() bool {
		if s.runsOf(domain.RunKindSuggest) != 1 || len(s.slack.Posted()) != 1 {
			return false
		}
		proposals, err := s.proposals.List(s.Ctx)
		if err != nil {
			return false
		}
		for _, p := range proposals {
			if p.MessageTS == "" {
				return false
			}
		}
		return true
	}, slackWait, slackPoll)
}

func (s *SlackTestSuite) listProposals() []testsrepository.ProposalRow {
	proposals, err := s.proposals.List(s.Ctx)
	s.Require().NoError(err)
	return proposals
}

func (s *SlackTestSuite) sections(rawBlocks string) []*slack.SectionBlock {
	var blocks slack.Blocks
	s.Require().NoError(json.Unmarshal([]byte(rawBlocks), &blocks))
	s.Require().NotEmpty(blocks.BlockSet)
	s.Equal(slack.MBTHeader, blocks.BlockSet[0].BlockType())
	var sections []*slack.SectionBlock
	for _, block := range blocks.BlockSet[1:] {
		section, ok := block.(*slack.SectionBlock)
		s.Require().True(ok)
		sections = append(sections, section)
	}
	return sections
}

func (s *SlackTestSuite) clickPayload(userID, channelID string, proposalID int64) string {
	return fmt.Sprintf(`{"type":"block_actions","user":{"id":%q},"channel":{"id":%q},"response_url":%q,`+
		`"actions":[{"type":"button","block_id":"b1","action_id":%q,"value":"%d"}]}`,
		userID, channelID, fmt.Sprintf("%s/response/click", s.slack.URL), slackpost.AddBlogAction, proposalID)
}

func (s *SlackTestSuite) click(userID, channelID string, proposalID int64) {
	var cb slack.InteractionCallback
	s.Require().NoError(json.Unmarshal([]byte(s.clickPayload(userID, channelID, proposalID)), &cb))

	s.handler.BlockActions(s.Ctx, cb)
	s.handler.Wait()
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
	s.Require().Eventually(func() bool { return len(s.slack.Ephemerals()) > 0 }, slackWait, slackPoll)
	s.Contains(ownerAnswer(s.T(), s.slack), "the bot is not in this channel")
	s.Empty(s.slack.Replies())
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

func (s *SlackTestSuite) TestDiscoverPostsNewBlogsWithAddButtons() {
	s.insertBlogWithArticle()
	first, second := fmt.Sprintf(goodFeedFmt, 1), fmt.Sprintf(goodFeedFmt, 2)
	s.setGoodFeed(first)
	s.setGoodFeed(second)
	s.feeds.Set(emptyFeedPath, rssType, fmt.Sprintf(itemsFmt, ""))
	s.answerBlogs(first, slackFeedPath, deadFeedPath, emptyFeedPath, second, first)

	s.discover()

	proposals := s.listProposals()
	s.Require().Len(proposals, 2)
	form := s.slack.PostedForms()[0]
	s.Equal("false", form.Get("unfurl_links"))
	s.Equal("false", form.Get("unfurl_media"))
	sections := s.sections(form.Get("blocks"))
	s.Require().Len(sections, 2)
	for i, path := range []string{first, second} {
		s.Equal(s.feedURL(path), proposals[i].FeedURL)
		s.Equal(string(domain.ProposalProposed), proposals[i].Status)
		s.NotEmpty(proposals[i].MessageTS)
		s.Equal(fmt.Sprintf("*<%s|Tom &amp; &lt;Jerry&gt;>*\nWrites about Go &amp; &lt;SQL&gt;.", s.siteURL(path)), sections[i].Text.Text)
		s.Require().NotNil(sections[i].Accessory)
		s.Require().NotNil(sections[i].Accessory.ButtonElement)
		s.Equal(slackpost.AddBlogAction, sections[i].Accessory.ButtonElement.ActionID)
		s.Equal(strconv.FormatInt(proposals[i].ID, 10), sections[i].Accessory.ButtonElement.Value)
	}
}

func (s *SlackTestSuite) TestDiscoverWithoutNewBlogsSaysSo() {
	s.insertBlogWithArticle()
	s.answerBlogs(slackFeedPath, deadFeedPath)

	s.discover()

	s.Equal([]string{"Found no new blogs that can be read. Try again later."}, s.slack.Posted())
	s.Empty(s.listProposals())
}

func (s *SlackTestSuite) TestOwnerAddClickAddsBlogAndMarksIt() {
	s.insertBlogWithArticle()
	first, second := fmt.Sprintf(goodFeedFmt, 1), fmt.Sprintf(goodFeedFmt, 2)
	s.setGoodFeed(first)
	s.setGoodFeed(second)
	s.answerBlogs(first, second)
	s.discover()
	s.Require().Eventually(func() bool { return len(s.slack.Ephemerals()) == 1 }, slackWait, slackPoll)

	s.click(slackOwnerID, digestChannelID, s.listProposals()[0].ID)

	detail, err := s.sources.Detail(s.Ctx, s.feedURL(first))
	s.Require().NoError(err)
	s.Equal(testsrepository.SourceDetail{Status: string(domain.SourceActive), Origin: string(domain.OriginUser)}, detail)
	proposals := s.listProposals()
	s.Equal(string(domain.ProposalAdded), proposals[0].Status)
	s.Equal(string(domain.ProposalProposed), proposals[1].Status)
	updates := s.slack.Updates()
	s.Require().Len(updates, 1)
	s.Equal(proposals[0].MessageTS, updates[0].TS)
	sections := s.sections(updates[0].Blocks)
	s.Require().Len(sections, 2)
	s.Nil(sections[0].Accessory)
	s.Contains(sections[0].Text.Text, slackAdded)
	s.Require().NotNil(sections[1].Accessory)
	s.NotContains(sections[1].Text.Text, slackAdded)
	s.Empty(s.slack.Replies())
}

func (s *SlackTestSuite) TestSecondAddClickAddsNoDuplicate() {
	s.insertBlogWithArticle()
	good := fmt.Sprintf(goodFeedFmt, 1)
	s.setGoodFeed(good)
	s.answerBlogs(good)
	s.discover()
	s.Require().Eventually(func() bool { return len(s.slack.Ephemerals()) == 1 }, slackWait, slackPoll)
	proposalID := s.listProposals()[0].ID
	s.click(slackOwnerID, digestChannelID, proposalID)

	s.click(slackOwnerID, digestChannelID, proposalID)

	s.Equal(2, s.CountRows("source"))
	s.Require().Len(s.slack.Replies(), 1)
	s.Contains(s.slack.Replies()[0], "Already in the list.")
}

func (s *SlackTestSuite) TestAddClickOfAnotherUserIsIgnored() {
	s.insertBlogWithArticle()
	good := fmt.Sprintf(goodFeedFmt, 1)
	s.setGoodFeed(good)
	s.answerBlogs(good)
	s.discover()
	s.Require().Eventually(func() bool { return len(s.slack.Ephemerals()) == 1 }, slackWait, slackPoll)

	s.click(slackOtherUser, digestChannelID, s.listProposals()[0].ID)

	s.Equal(1, s.CountRows("source"))
	s.Equal(string(domain.ProposalProposed), s.listProposals()[0].Status)
	s.Empty(s.slack.Updates())
	s.Empty(s.slack.Replies())
}

func (s *SlackTestSuite) TestDiscoverNeverUsesCodexOSS() {
	s.insertBlogWithArticle()
	s.agents.Answer(llm.CodexOSS, llm.SkillDiscoverBlogs, map[string]any{"blogs": []map[string]any{}})

	err := s.newApp(llm.CodexOSS).Suggest.Run(s.Ctx, fmt.Sprintf("%s/response/r1", s.slack.URL))

	s.Require().ErrorIs(err, llm.ErrAllBackendsFailed)
	s.Empty(s.agents.Args(llm.CodexOSS))
	s.Empty(s.slack.Posted())
	s.Contains(ownerAnswer(s.T(), s.slack), "no agent with web search answered")
	s.Empty(s.slack.Replies())
}

func (s *SlackTestSuite) TestAddClickInAnotherChannelIsIgnored() {
	s.insertBlogWithArticle()
	good := fmt.Sprintf(goodFeedFmt, 1)
	s.setGoodFeed(good)
	s.answerBlogs(good)
	s.discover()

	s.click(slackOwnerID, "C2", s.listProposals()[0].ID)

	s.Equal(1, s.CountRows("source"))
	s.Equal(string(domain.ProposalProposed), s.listProposals()[0].Status)
	s.Empty(s.slack.Updates())
}

func (s *SlackTestSuite) TestDiscoverCutsLongTitleAndAbout() {
	const (
		maxTitle = 150
		maxAbout = 300
	)
	s.insertBlogWithArticle()
	good := fmt.Sprintf(goodFeedFmt, 1)
	s.setGoodFeed(good)
	s.agents.Answer(llm.Claude, llm.SkillDiscoverBlogs, map[string]any{"blogs": []map[string]any{{
		"site_url": s.siteURL(good), "feed_url": s.feedURL(good), "reason": goodReason,
		"title": strings.Repeat("t", 1000), "about": strings.Repeat("a&", 2500),
	}}})

	s.discover()

	proposals := s.listProposals()
	s.Require().Len(proposals, 1)
	s.Equal(fmt.Sprintf("%s…", strings.Repeat("t", maxTitle-1)), proposals[0].Title)
	s.Len([]rune(proposals[0].About), maxAbout)
	s.True(strings.HasSuffix(proposals[0].About, "…"))
	sections := s.sections(s.slack.PostedForms()[0].Get("blocks"))
	s.Require().Len(sections, 1)
	s.Contains(sections[0].Text.Text, strings.ReplaceAll(proposals[0].About, "&", "&amp;"))
}

func (s *SlackTestSuite) TestDiscoverPostRejectedKeepsNoProposals() {
	s.insertBlogWithArticle()
	good := fmt.Sprintf(goodFeedFmt, 1)
	s.setGoodFeed(good)
	s.answerBlogs(good)
	s.slack.PostFaults = []tests.PostFault{tests.PostRejected}

	s.Equal(slackLooking, s.ack(s.ownerCommand("/discover", "")))

	s.Require().Eventually(func() bool { return len(s.slack.Ephemerals()) == 1 }, slackWait, slackPoll)
	s.Contains(ownerAnswer(s.T(), s.slack), "the channel was not found")
	s.Empty(s.slack.Replies())
	s.Empty(s.slack.Posted())
	s.Empty(s.listProposals())
}

func (s *SlackTestSuite) TestClickThroughSocketModeIsAckedAndAddsBlog() {
	const envelopeID = "env-1"
	s.insertBlogWithArticle()
	good := fmt.Sprintf(goodFeedFmt, 1)
	s.setGoodFeed(good)
	s.answerBlogs(good)
	s.discover()
	api := slack.New("xoxb-test", slack.OptionAPIURL(fmt.Sprintf("%s/api/", s.slack.URL)), slack.OptionAppLevelToken("xapp-test"))
	ctx, cancel := context.WithCancel(s.Ctx)
	listenDone := make(chan error, 1)
	go func() {
		listenDone <- controllerslack.Listen(ctx, socketmode.New(api), s.handler)
	}()

	s.slack.SendEnvelope(fmt.Sprintf(`{"envelope_id":%q,"type":"interactive","accepts_response_payload":false,"payload":%s}`,
		envelopeID, s.clickPayload(slackOwnerID, digestChannelID, s.listProposals()[0].ID)))

	s.Eventually(func() bool { return slices.Contains(s.slack.Acks(), envelopeID) }, slackWait, slackPoll)
	s.Eventually(func() bool { return len(s.slack.Updates()) == 1 }, slackWait, slackPoll)
	cancel()
	s.Require().NoError(<-listenDone)
	s.handler.Wait()
	detail, err := s.sources.Detail(s.Ctx, s.feedURL(good))
	s.Require().NoError(err)
	s.Equal(string(domain.SourceActive), detail.Status)
	s.Equal(string(domain.ProposalAdded), s.listProposals()[0].Status)
}

func (s *SlackTestSuite) TestShowThroughCommandRepliesWithOnlyTheTokenLine() {
	s.insertBlogWithArticle()

	s.Equal(slackPicking, s.ack(s.ownerCommand("/show", "")))

	s.Require().Eventually(func() bool { return len(s.slack.Ephemerals()) == 1 }, slackWait, slackPoll)
	s.Contains(s.slack.Posted(), slackShowHeader)
	requireOnlyTokenLine(s.T(), ownerAnswer(s.T(), s.slack), tokenLineTwoClaudeRuns)
	s.Empty(s.slack.Replies())
}

func (s *SlackTestSuite) TestDiscoverThroughCommandRepliesWithOnlyTheTokenLine() {
	s.insertBlogWithArticle()
	good := fmt.Sprintf(goodFeedFmt, 1)
	s.setGoodFeed(good)
	s.answerBlogs(good)

	s.discover()

	s.Require().Eventually(func() bool { return len(s.slack.Ephemerals()) == 1 }, slackWait, slackPoll)
	s.Require().Len(s.listProposals(), 1)
	requireOnlyTokenLine(s.T(), ownerAnswer(s.T(), s.slack), tokenLineOneClaudeRun)
	s.Empty(s.slack.Replies())
}

func (s *SlackTestSuite) TestDiscoverWithoutNewBlogsRepliesWithTheTokenLine() {
	s.insertBlogWithArticle()
	s.answerBlogs(slackFeedPath, deadFeedPath)

	err := s.newApp(llm.Claude).Suggest.Run(s.Ctx, s.replyURL())

	s.Require().NoError(err)
	s.Equal([]string{"Found no new blogs that can be read. Try again later."}, s.slack.Posted())
	requireOnlyTokenLine(s.T(), ownerAnswer(s.T(), s.slack), tokenLineOneClaudeRun)
}

func (s *SlackTestSuite) TestDiscoverWithoutBlogsRepliesWithZeroTokens() {
	err := s.newApp(llm.Claude).Suggest.Run(s.Ctx, s.replyURL())

	s.Require().NoError(err)
	s.Equal([]string{"No blogs yet. Add one with /blogs add <url>."}, s.slack.Posted())
	requireOnlyTokenLine(s.T(), ownerAnswer(s.T(), s.slack), tokenLineNoCall)
}

func (s *SlackTestSuite) TestDiscoverPostFailureRepliesWithTheReasonAboveTheTokenLine() {
	s.insertBlogWithArticle()
	good := fmt.Sprintf(goodFeedFmt, 1)
	s.setGoodFeed(good)
	s.answerBlogs(good)
	s.slack.PostFaults = []tests.PostFault{tests.PostRejected}

	err := s.newApp(llm.Claude).Suggest.Run(s.Ctx, s.replyURL())

	s.Require().Error(err)
	requireTextThenTokenLine(s.T(), ownerAnswer(s.T(), s.slack), "Could not post: the channel was not found. Check SLACK_CHANNEL_ID.", tokenLineOneClaudeRun)
}

func (s *SlackTestSuite) TestDiscoverWithoutWebAgentRepliesWithZeroTokens() {
	s.insertBlogWithArticle()

	err := s.newApp(llm.CodexOSS).Suggest.Run(s.Ctx, s.replyURL())

	s.Require().ErrorIs(err, llm.ErrAllBackendsFailed)
	requireTextThenTokenLine(s.T(), ownerAnswer(s.T(), s.slack),
		"Could not look for blogs: no agent with web search answered. See the logs.", tokenLineNoCall)
}

func (s *SlackTestSuite) TestInstantAnswersAddTheZeroTokenLine() {
	otherUser := s.ownerCommand("/blogs", "list")
	otherUser.UserID = slackOtherUser
	showElsewhere := s.ownerCommand("/show", "")
	showElsewhere.ChannelID = "C2"
	discoverElsewhere := s.ownerCommand("/discover", "")
	discoverElsewhere.ChannelID = "C2"
	usage := "usage: list | add &lt;url&gt; | remove &lt;id|url&gt; | pause &lt;id|url&gt; | resume &lt;id|url&gt;"
	cases := []struct {
		name string
		cmd  slack.SlashCommand
		text string
	}{
		{name: "owner only refusal", cmd: otherUser, text: slackOwnerOnly},
		{name: "show in another channel", cmd: showElsewhere, text: fmt.Sprintf("Use /show in <#%s>.", digestChannelID)},
		{name: "discover in another channel", cmd: discoverElsewhere, text: fmt.Sprintf("Use /discover in <#%s>.", digestChannelID)},
		{name: "blogs help", cmd: s.ownerCommand("/blogs", "help"), text: usage},
		{name: "blogs without text", cmd: s.ownerCommand("/blogs", ""), text: usage},
		{name: "bad blogs text", cmd: s.ownerCommand("/blogs", "frobnicate now"), text: usage},
		{name: "empty blogs list", cmd: s.ownerCommand("/blogs", "list"), text: "No blogs yet."},
		{name: "remove of an unknown blog", cmd: s.ownerCommand("/blogs", "remove foo"), text: "No blog matches foo."},
		{name: "unknown command", cmd: s.ownerCommand("/frob", ""), text: "Unknown command /frob."},
	}
	for _, tt := range cases {
		s.Run(tt.name, func() {
			requireTextThenTokenLine(s.T(), s.ackPayload(s.handler, tt.cmd), tt.text, tokenLineNoCall)
		})
	}
}

func (s *SlackTestSuite) TestBlogsListAndChangesAddTheZeroTokenLine() {
	sourceID, _ := s.insertPost()

	list := s.ackPayload(s.handler, s.ownerCommand("/blogs", "list"))
	pause := s.ackPayload(s.handler, s.ownerCommand("/blogs", fmt.Sprintf("pause %d", sourceID)))
	resume := s.ackPayload(s.handler, s.ownerCommand("/blogs", fmt.Sprintf("resume %d", sourceID)))
	remove := s.ackPayload(s.handler, s.ownerCommand("/blogs", fmt.Sprintf("remove %d", sourceID)))

	listText := parseSlackMessage(s.T(), list).Text
	s.Contains(listText, blogTitle)
	requireTextThenTokenLine(s.T(), list, listText, tokenLineNoCall)
	requireTextThenTokenLine(s.T(), pause, fmt.Sprintf("Paused #%d %s.", sourceID, blogTitle), tokenLineNoCall)
	requireTextThenTokenLine(s.T(), resume, fmt.Sprintf("Resumed #%d %s.", sourceID, blogTitle), tokenLineNoCall)
	requireTextThenTokenLine(s.T(), remove, fmt.Sprintf("Removed #%d %s.", sourceID, blogTitle), tokenLineNoCall)
}

func (s *SlackTestSuite) TestLongBlogsListIsCutIntoSectionsAtLineEnds() {
	const blogs = 40
	for i := range blogs {
		_, err := s.sources.Insert(s.Ctx, testsrepository.SourceRow{
			FeedURL: fmt.Sprintf("%s/long-%d.xml", s.feeds.URL(), i),
			Title:   fmt.Sprintf("%d %s & co", i, strings.Repeat("t", 120)),
			Status:  string(domain.SourceActive),
		}, s.Clock.Now())
		s.Require().NoError(err)
	}

	raw := s.ackPayload(s.handler, s.ownerCommand("/blogs", "list"))

	text := parseSlackMessage(s.T(), raw).Text
	s.Greater(utf8.RuneCountInString(text), 2*maxSectionRunes)
	s.Len(strings.Split(text, "\n"), blogs)
	requireTextThenTokenLine(s.T(), raw, text, tokenLineNoCall)
	s.GreaterOrEqual(len(parseSlackMessage(s.T(), raw).Blocks), 4)
}

func (s *SlackTestSuite) TestQueuedAnswersAddTheZeroTokenLine() {
	s.insertBlogWithArticle()
	s.answerBlogs(slackFeedPath)
	s.agents.Sleep(llm.Claude, 2)
	s.Equal(slackPicking, s.ack(s.ownerCommand("/show", "")))

	onItsWay := s.ackPayload(s.handler, s.ownerCommand("/show", ""))
	s.Equal(slackLooking, s.ack(s.ownerCommand("/discover", "")))
	alreadyLooking := s.ackPayload(s.handler, s.ownerCommand("/discover", ""))

	requireTextThenTokenLine(s.T(), onItsWay, "A digest is already on its way.", tokenLineNoCall)
	requireTextThenTokenLine(s.T(), alreadyLooking, "Already looking for blogs.", tokenLineNoCall)
}

func (s *SlackTestSuite) TestBusyAnswerAddsTheZeroTokenLine() {
	app := s.newApp(llm.Claude)
	handler := controllerslack.NewHandler(controllerslack.HandlerData{
		Jobs:       usecase.NewJobs(app.JobsData(0)),
		Suggest:    app.Suggest,
		Sources:    app.Sources,
		Votes:      app.Votes,
		Poster:     app.Poster,
		ChannelID:  digestChannelID,
		OwnerID:    slackOwnerID,
		Background: s.Ctx,
	})

	raw := s.ackPayload(handler, s.ownerCommand("/show", ""))

	requireTextThenTokenLine(s.T(), raw, slackBusy, tokenLineNoCall)
}

func (s *SlackTestSuite) TestBlogsAddReplyEndsWithTheTokenLine() {
	s.feeds.Set(blogPath, htmlType, pageHTML)
	s.feeds.Set(feedPath, rssType, fmt.Sprintf(feedXMLFmt, blogTitle, s.feeds.URL()+blogPath, s.feeds.URL()))

	s.ack(s.ownerCommand("/blogs", fmt.Sprintf("add <%s%s>", s.feeds.URL(), blogPath)))

	s.Require().Eventually(func() bool { return len(s.slack.Replies()) == 1 }, slackWait, slackPoll)
	text := parseSlackMessage(s.T(), s.slack.Replies()[0]).Text
	s.True(strings.HasPrefix(text, fmt.Sprintf("Added #1 %s (", blogTitle)), text)
	requireTextThenTokenLine(s.T(), s.slack.Replies()[0], text, tokenLineNoCall)
}

func (s *SlackTestSuite) TestBlogsAddOfAKnownBlogRepliesAlreadyInTheListWithTheTokenLine() {
	s.feeds.Set(blogPath, htmlType, pageHTML)
	s.feeds.Set(feedPath, rssType, fmt.Sprintf(feedXMLFmt, blogTitle, s.feeds.URL()+blogPath, s.feeds.URL()))
	_, err := s.sources.Insert(s.Ctx, testsrepository.SourceRow{
		FeedURL: fmt.Sprintf("%s%s", s.feeds.URL(), feedPath), Title: blogTitle, Status: string(domain.SourceActive),
	}, s.Clock.Now())
	s.Require().NoError(err)

	s.ack(s.ownerCommand("/blogs", fmt.Sprintf("add <%s%s>", s.feeds.URL(), blogPath)))

	s.Require().Eventually(func() bool { return len(s.slack.Replies()) == 1 }, slackWait, slackPoll)
	requireTextThenTokenLine(s.T(), s.slack.Replies()[0], "Already in the list.", tokenLineNoCall)
}

func (s *SlackTestSuite) TestBlogsAddOfAPageWithoutFeedRepliesNotFoundWithTheTokenLine() {
	const barePath = "/bare"
	s.feeds.Set(barePath, htmlType, "<html></html>")
	bareURL := fmt.Sprintf("%s%s", s.feeds.URL(), barePath)

	s.ack(s.ownerCommand("/blogs", fmt.Sprintf("add <%s>", bareURL)))

	s.Require().Eventually(func() bool { return len(s.slack.Replies()) == 1 }, slackWait, slackPoll)
	requireTextThenTokenLine(s.T(), s.slack.Replies()[0],
		fmt.Sprintf("No feed and too few articles found at %s.", bareURL), tokenLineNoCall)
}

func (s *SlackTestSuite) TestAddClickReplySendsNoTokenLine() {
	s.insertBlogWithArticle()
	good := fmt.Sprintf(goodFeedFmt, 1)
	s.setGoodFeed(good)
	s.answerBlogs(good)
	s.discover()
	s.Require().Eventually(func() bool { return len(s.slack.Ephemerals()) == 1 }, slackWait, slackPoll)
	requireOnlyTokenLine(s.T(), ownerAnswer(s.T(), s.slack), tokenLineOneClaudeRun)
	proposalID := s.listProposals()[0].ID
	s.click(slackOwnerID, digestChannelID, proposalID)

	s.click(slackOwnerID, digestChannelID, proposalID)

	replies := s.slack.Replies()
	s.Require().Len(replies, 1)
	s.Equal("Already in the list.", parseSlackMessage(s.T(), replies[0]).Text)
	requireNoTokenLine(s.T(), replies[0])
}

func (s *SlackTestSuite) TestDiscoverPostsTheTokenLineToTheOwnerAfterTheBlogList() {
	s.insertBlogWithArticle()
	good := fmt.Sprintf(goodFeedFmt, 1)
	s.setGoodFeed(good)
	s.answerBlogs(good)

	err := s.newApp(llm.Claude).Suggest.Run(s.Ctx, s.replyURL())

	s.Require().NoError(err)
	s.Equal([]string{"chat.postMessage", ephemeralCall}, callMethods(s.slack.Calls()))
	requireOnlyTokenLine(s.T(), ownerAnswer(s.T(), s.slack), tokenLineOneClaudeRun)
	s.Empty(s.slack.Replies())
}

func (s *SlackTestSuite) TestDiscoverWithoutNewBlogsPostsTheTokenLineAfterThatLine() {
	const noNewBlogsLine = "Found no new blogs that can be read. Try again later."
	s.insertBlogWithArticle()
	s.answerBlogs(slackFeedPath, deadFeedPath)

	err := s.newApp(llm.Claude).Suggest.Run(s.Ctx, s.replyURL())

	s.Require().NoError(err)
	s.Equal([]tests.SlackCall{
		{Method: "chat.postMessage", Text: noNewBlogsLine},
		{Method: ephemeralCall, Text: tokenLineOneClaudeRun},
	}, s.slack.Calls())
	s.Empty(s.slack.Replies())
}
