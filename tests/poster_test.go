package tests_test

import (
	"testing"

	"github.com/slack-go/slack"
	"github.com/stretchr/testify/suite"

	"github.com/sadensmol/blogger/internal/domain"
	"github.com/sadensmol/blogger/internal/slackpost"
	"github.com/sadensmol/blogger/tests"
)

const (
	posterChannelID = "C1"
	posterURL       = "https://example.com/a"
	posterTitle     = "First post"
	posterSource    = "Example Blog"
	posterReason    = "fits your interests"
)

type PosterTestSuite struct {
	suite.Suite

	fake   *tests.FakeSlack
	poster *slackpost.Poster
}

func TestPosterTestSuite(t *testing.T) {
	suite.Run(t, &PosterTestSuite{})
}

func (s *PosterTestSuite) SetupTest() {
	s.fake = tests.NewFakeSlack()
	api := slack.New("xoxb-test", slack.OptionAPIURL(s.fake.URL+"/api/"))
	s.poster = slackpost.NewPoster(api, posterChannelID)
}

func (s *PosterTestSuite) TearDownTest() {
	s.fake.Close()
}

func (s *PosterTestSuite) article(title string) domain.Article {
	return domain.Article{URL: posterURL, Title: title, SourceTitle: posterSource}
}

func (s *PosterTestSuite) TestPickPostsLinkAndAddsBothReactions() {
	ts, channel, err := s.poster.Pick(s.T().Context(), s.article(posterTitle), domain.Pick{Description: "About a thing", Reason: posterReason})

	s.Require().NoError(err)
	s.Equal("1.000100", ts)
	s.Equal(posterChannelID, channel)
	s.Require().Len(s.fake.Posted(), 1)
	s.Contains(s.fake.Posted()[0], "<https://example.com/a|First post>")
	s.Contains(s.fake.Posted()[0], posterSource)
	s.Equal([]string{"1.000100:+1", "1.000100:-1"}, s.fake.Reactions())
}

func (s *PosterTestSuite) TestPickEscapesTitle() {
	_, _, err := s.poster.Pick(s.T().Context(), s.article("A <b> & C"), domain.Pick{Description: "d", Reason: "r"})

	s.Require().NoError(err)
	s.Contains(s.fake.Posted()[0], "A &lt;b&gt; &amp; C")
}

func (s *PosterTestSuite) TestReplyIsEphemeral() {
	err := s.poster.Reply(s.T().Context(), s.fake.URL+"/response/r1", "hello there")

	s.Require().NoError(err)
	s.Require().Len(s.fake.Replies(), 1)
	s.Contains(s.fake.Replies()[0], `"response_type":"ephemeral"`)
	s.Contains(s.fake.Replies()[0], "hello there")
}

func (s *PosterTestSuite) TestTextFailsWhenSlackFails() {
	s.fake.FailMethod = "chat.postMessage"

	s.Require().Error(s.poster.Text(s.T().Context(), "hi"))
}
