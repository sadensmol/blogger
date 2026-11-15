package tests_test

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"unicode/utf8"

	"github.com/rs/zerolog"

	controllerslack "github.com/sadensmol/blogger/internal/controller/slack"
	"github.com/sadensmol/blogger/internal/domain"
	"github.com/sadensmol/blogger/internal/llm"
	"github.com/sadensmol/blogger/internal/usecase"
	"github.com/sadensmol/blogger/tests"
)

const (
	showReplyFailedLog     = "cannot answer /show"
	discoverReplyFailedLog = "cannot answer /discover"
)

type syncLog struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (l *syncLog) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.buf.Write(p)
}

func (l *syncLog) String() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.buf.String()
}

func loggedContext(ctx context.Context) (context.Context, *syncLog) {
	logs := &syncLog{}
	return zerolog.New(logs).WithContext(ctx), logs
}

func rejectingReplyURL(t *testing.T) string {
	t.Helper()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	t.Cleanup(server.Close)
	return fmt.Sprintf("%s/response/rejected", server.URL)
}

func (s *DigestTestSuite) TestShowWhoseTokenReplyIsRejectedLogsItAndSucceeds() {
	s.addBlog("a")
	s.learnedProfile()
	s.answerPicks("a")
	ctx, logs := loggedContext(s.Ctx)

	err := s.digest.Show(ctx, rejectingReplyURL(s.T()))

	s.Require().NoError(err)
	s.Len(s.slack.Posted(), 2)
	s.Contains(logs.String(), showReplyFailedLog)
}

func (s *DigestTestSuite) TestShowFailureWhoseTokenReplyIsRejectedReturnsTheRunError() {
	s.addBlog("a")
	s.learnedProfile()
	s.agents.Fail(llm.Claude)
	s.agents.Fail(llm.Codex)
	ctx, logs := loggedContext(s.Ctx)

	err := s.digest.Show(ctx, rejectingReplyURL(s.T()))

	s.Require().ErrorIs(err, llm.ErrAllBackendsFailed)
	s.Contains(logs.String(), showReplyFailedLog)
}

func (s *DigestTestSuite) TestShowSumsCodexTurnsAcrossBlankEventLines() {
	const events = "{\"type\":\"thread.started\"}\n\n" +
		"{\"type\":\"turn.completed\",\"usage\":{\"input_tokens\":1200,\"output_tokens\":30}}\n" +
		"\n" +
		"{\"type\":\"turn.completed\",\"usage\":{\"input_tokens\":800,\"output_tokens\":20}}\n"
	s.addBlog("a")
	s.learnedProfile()
	s.agents.Fail(llm.Claude)
	s.agents.Answer(llm.Codex, llm.SkillPickArticles, map[string]any{"picks": []map[string]any{{
		"url": s.articleURL("a"), "description": "A post.", "topics": []string{"go"}, "reason": "Matches Go.",
	}}})
	s.Require().NoError(os.WriteFile(filepath.Join(s.agents.Home, fmt.Sprintf("codex.%s.events", llm.SkillPickArticles)), []byte(events), 0o600))

	err := s.digest.Show(s.Ctx, fmt.Sprintf("%s%s", s.slack.URL, replyURLPath))

	s.Require().NoError(err)
	replies := s.slack.Replies()
	s.Require().Len(replies, 1)
	requireOnlyTokenLine(s.T(), replies[0], "Tokens used: 2,050 (claude: not reported; codex: 2,000 in, 50 out)")
}

func (s *SlackTestSuite) TestDiscoverWhoseTokenReplyIsRejectedLogsItAndSucceeds() {
	s.insertBlogWithArticle()
	s.answerBlogs(slackFeedPath, deadFeedPath)
	ctx, logs := loggedContext(s.Ctx)

	err := s.newApp(llm.Claude).Suggest.Run(ctx, rejectingReplyURL(s.T()))

	s.Require().NoError(err)
	s.Equal([]string{"Found no new blogs that can be read. Try again later."}, s.slack.Posted())
	s.Contains(logs.String(), discoverReplyFailedLog)
}

func (s *SlackTestSuite) TestDiscoverWhileBusyAddsTheZeroTokenLine() {
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

	raw := s.ackPayload(handler, s.ownerCommand("/discover", ""))

	requireTextThenTokenLine(s.T(), raw, slackBusy, tokenLineNoCall)
}

func (s *SlackTestSuite) TestJobStartedAnswersCarryNoTokenLine() {
	s.insertBlogWithArticle()
	s.answerBlogs(slackFeedPath)
	s.feeds.Set(blogPath, htmlType, pageHTML)
	s.feeds.Set(feedPath, rssType, fmt.Sprintf(feedXMLFmt, blogTitle, s.feeds.URL()+blogPath, s.feeds.URL()))
	blogURL := fmt.Sprintf("%s%s", s.feeds.URL(), blogPath)

	show := s.ackPayload(s.handler, s.ownerCommand("/show", ""))
	discover := s.ackPayload(s.handler, s.ownerCommand("/discover", ""))
	add := s.ackPayload(s.handler, s.ownerCommand("/blogs", fmt.Sprintf("add <%s>", blogURL)))

	s.Equal(slackPicking, parseSlackMessage(s.T(), show).Text)
	requireNoTokenLine(s.T(), show)
	s.Equal(slackLooking, parseSlackMessage(s.T(), discover).Text)
	requireNoTokenLine(s.T(), discover)
	s.Equal(fmt.Sprintf("Checking %s…", blogURL), parseSlackMessage(s.T(), add).Text)
	requireNoTokenLine(s.T(), add)
}

func (s *PosterTestSuite) TestReplyWithUsageErrorHidesResponseURL() {
	const responsePath = "/response/secret-token"
	gone := tests.NewFakeSlack()
	gone.Close()

	err := s.poster.ReplyWithUsage(s.T().Context(), gone.URL+responsePath, "hello there", domain.TokenUsage{})

	s.Require().Error(err)
	s.NotContains(err.Error(), "secret-token")
}

func (s *PosterTestSuite) TestReplyWithUsageCutsALineLongerThanASectionInsideTheLine() {
	const longLineRunes, tail = 3500, "short line"
	text := fmt.Sprintf("%s\n%s", strings.Repeat("é", longLineRunes), tail)

	err := s.poster.ReplyWithUsage(s.T().Context(), s.fake.URL+"/response/r1", text, domain.TokenUsage{})

	s.Require().NoError(err)
	s.Require().Len(s.fake.Replies(), 1)
	msg := parseSlackMessage(s.T(), s.fake.Replies()[0])
	s.Equal(text, msg.Text)
	s.Require().Len(msg.Blocks, 3)
	var joined strings.Builder
	for _, block := range msg.Blocks[:2] {
		s.Equal("section", block.Type)
		s.LessOrEqual(utf8.RuneCountInString(block.Text.Text), maxSectionRunes)
		joined.WriteString(block.Text.Text)
	}
	s.Equal(text, joined.String())
	requireTokenLineBlock(s.T(), msg.Blocks[2], tokenLineNoCall)
}
