package tests_test

import (
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

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
	posterTimeout   = 5 * time.Second
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
	s.poster = slackpost.NewPoster(api, &http.Client{Timeout: posterTimeout}, posterChannelID)
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

func (s *PosterTestSuite) TestPickLinksTheSourceToTheBlogPage() {
	const blogPage = "https://example.com/writing"
	article := s.article(posterTitle)
	article.SourceURL = blogPage

	_, _, err := s.poster.Pick(s.T().Context(), article, domain.Pick{Description: "d", Reason: posterReason})

	s.Require().NoError(err)
	s.Contains(s.fake.Posted()[0], fmt.Sprintf("_<%s|%s> · %s_", blogPage, posterSource, posterReason))
}

func (s *PosterTestSuite) TestPickEscapesTitle() {
	_, _, err := s.poster.Pick(s.T().Context(), s.article("A <b> & C"), domain.Pick{Description: "d", Reason: "r"})

	s.Require().NoError(err)
	s.Contains(s.fake.Posted()[0], "A &lt;b&gt; &amp; C")
}

func (s *PosterTestSuite) TestPickEndsWithThemesLine() {
	article := s.article(posterTitle)
	article.Tags = []domain.Tag{"go", "sqlite"}

	_, _, err := s.poster.Pick(s.T().Context(), article, domain.Pick{Description: "d", Reason: posterReason})

	s.Require().NoError(err)
	s.Equal(fmt.Sprintf("*<%s|%s>*\nd\n_%s · %s_\nThemes: go, sqlite", posterURL, posterTitle, posterSource, posterReason), s.fake.Posted()[0])
}

func (s *PosterTestSuite) TestPickEscapesThemes() {
	article := s.article(posterTitle)
	article.Tags = []domain.Tag{"q&a", "<!channel>"}

	_, _, err := s.poster.Pick(s.T().Context(), article, domain.Pick{Description: "d", Reason: posterReason})

	s.Require().NoError(err)
	lines := strings.Split(s.fake.Posted()[0], "\n")
	s.Equal("Themes: q&amp;a, &lt;!channel&gt;", lines[len(lines)-1])
}

func (s *PosterTestSuite) TestPickWithoutTagsHasNoThemesLine() {
	_, _, err := s.poster.Pick(s.T().Context(), s.article(posterTitle), domain.Pick{Description: "d", Reason: posterReason})

	s.Require().NoError(err)
	s.Equal(fmt.Sprintf("*<%s|%s>*\nd\n_%s · %s_", posterURL, posterTitle, posterSource, posterReason), s.fake.Posted()[0])
}

func (s *PosterTestSuite) TestReplyIsEphemeral() {
	err := s.poster.Reply(s.T().Context(), s.fake.URL+"/response/r1", "hello there")

	s.Require().NoError(err)
	s.Require().Len(s.fake.Replies(), 1)
	s.Contains(s.fake.Replies()[0], `"response_type":"ephemeral"`)
	s.Contains(s.fake.Replies()[0], "hello there")
}

func (s *PosterTestSuite) TestReplyErrorHidesResponseURL() {
	const responsePath = "/response/secret-token"
	gone := tests.NewFakeSlack()
	gone.Close()

	err := s.poster.Reply(s.T().Context(), gone.URL+responsePath, "hello there")

	s.Require().Error(err)
	s.NotContains(err.Error(), "secret-token")
}

func (s *PosterTestSuite) TestTextFailsWhenSlackFails() {
	s.fake.FailMethod = "chat.postMessage"

	s.Require().Error(s.poster.Text(s.T().Context(), "hi"))
}

func (s *PosterTestSuite) TestPickEscapesSourceDescriptionAndReason() {
	a := domain.Article{URL: posterURL, Title: posterTitle, SourceTitle: "<!channel>"}

	_, _, err := s.poster.Pick(s.T().Context(), a, domain.Pick{Description: "<!here> & more", Reason: "<@U123>"})

	s.Require().NoError(err)
	text := s.fake.Posted()[0]
	s.Contains(text, "&lt;!channel&gt;")
	s.Contains(text, "&lt;!here&gt; &amp; more")
	s.Contains(text, "&lt;@U123&gt;")
	s.NotContains(text, "<!channel>")
	s.NotContains(text, "<!here>")
	s.NotContains(text, "<@U123>")
}

func (s *PosterTestSuite) TestPickKeepsUrlInsideItsLink() {
	a := domain.Article{URL: "https://example.com/a|b>c d?x=1&y=2", Title: posterTitle, SourceTitle: posterSource}

	_, _, err := s.poster.Pick(s.T().Context(), a, domain.Pick{Description: "d", Reason: "r"})

	s.Require().NoError(err)
	s.Contains(s.fake.Posted()[0], "*<https://example.com/a%7Cb&gt;c%20d?x=1&amp;y=2|First post>*")
}

func (s *PosterTestSuite) TestPickRefusesNonHttpUrl() {
	a := domain.Article{URL: "javascript:alert(1)", Title: posterTitle, SourceTitle: posterSource}

	_, _, err := s.poster.Pick(s.T().Context(), a, domain.Pick{Description: "d", Reason: "r"})

	s.Require().Error(err)
	s.Empty(s.fake.Posted())
}

func (s *PosterTestSuite) TestPickReturnsPostWhenReactionFails() {
	s.fake.FailMethod = "reactions.add"

	ts, channel, err := s.poster.Pick(s.T().Context(), s.article(posterTitle), domain.Pick{Description: "d", Reason: "r"})

	s.Require().Error(err)
	s.Equal("1.000100", ts)
	s.Equal(posterChannelID, channel)
	s.Len(s.fake.Posted(), 1)
}
