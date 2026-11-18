package tests_test

import (
	"fmt"
	"strings"

	"github.com/sadensmol/blogger/internal/llm"
	"github.com/sadensmol/blogger/tests"
)

func (s *SlackTestSuite) TestDiscoverFailureWhosePrivateMessageIsRefusedAnswersTheReasonThroughTheReplyURL() {
	const channelNotFoundLine = "Could not post: the channel was not found. Check SLACK_CHANNEL_ID."
	s.insertBlogWithArticle()
	good := fmt.Sprintf(goodFeedFmt, 1)
	s.setGoodFeed(good)
	s.answerBlogs(good)
	s.slack.PostFaults = []tests.PostFault{tests.PostRejected}
	s.slack.EphemeralError = notInChannelCode
	ctx, logs := loggedContext(s.Ctx)

	err := s.newApp(llm.Claude).Suggest.Run(ctx, s.replyURL())

	s.Require().Error(err)
	s.Equal([]string{"chat.postMessage", ephemeralCall, tests.ResponseCall}, callMethods(s.slack.Calls()))
	s.Empty(s.slack.Ephemerals())
	s.Require().Len(s.slack.Replies(), 1)
	requireTextThenTokenLine(s.T(), s.slack.Replies()[0], channelNotFoundLine, tokenLineOneClaudeRun)
	s.Contains(logs.String(), notInChannelCode)
	s.NotContains(logs.String(), discoverReplyFailedLog)
}

func (s *SlackTestSuite) TestBlogsAddAnswersOnlyThroughTheReplyURL() {
	s.feeds.Set(blogPath, htmlType, pageHTML)
	s.feeds.Set(feedPath, rssType, fmt.Sprintf(feedXMLFmt, blogTitle, s.feeds.URL()+blogPath, s.feeds.URL()))

	s.ack(s.ownerCommand("/blogs", fmt.Sprintf("add <%s%s>", s.feeds.URL(), blogPath)))

	s.Require().Eventually(func() bool { return len(s.slack.Replies()) == 1 }, slackWait, slackPoll)
	s.True(strings.HasPrefix(parseSlackMessage(s.T(), s.slack.Replies()[0]).Text, fmt.Sprintf("Added #1 %s (", blogTitle)))
	s.Empty(s.slack.Ephemerals())
}

func (s *SlackTestSuite) TestDiscoverFailureWhoseAnswersAreBothRefusedReturnsTheRunError() {
	s.insertBlogWithArticle()
	good := fmt.Sprintf(goodFeedFmt, 1)
	s.setGoodFeed(good)
	s.answerBlogs(good)
	s.slack.PostFaults = []tests.PostFault{tests.PostDropped}
	s.slack.EphemeralError = notInChannelCode
	ctx, logs := loggedContext(s.Ctx)

	err := s.newApp(llm.Claude).Suggest.Run(ctx, rejectingReplyURL(s.T()))

	s.Require().ErrorContains(err, "cannot suggest blogs")
	s.NotContains(err.Error(), "cannot reply to slash command")
	s.Equal([]string{"chat.postMessage", ephemeralCall}, callMethods(s.slack.Calls()))
	s.Empty(s.slack.Ephemerals())
	s.Contains(logs.String(), discoverReplyFailedLog)
}
