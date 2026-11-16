package tests_test

import (
	"time"

	"github.com/sadensmol/blogger/internal/domain"
	"github.com/sadensmol/blogger/internal/llm"
)

func (s *ThemesTestSuite) answerPickWithTopics(name string, topics ...string) {
	s.agents.Answer(llm.Claude, llm.SkillPickArticles, map[string]any{"picks": []map[string]any{{
		"url": s.articleURL(name), "description": "A post.", "topics": topics, "reason": "Matches Go.",
	}}})
}

func (s *ThemesTestSuite) voteOnPostOf(name string, v domain.Vote) {
	messages, err := s.posts.MessagesByURL(s.Ctx, s.articleURL(name))
	s.Require().NoError(err)
	s.Require().Len(messages, 1)
	s.vote(domain.ReactionEvent{ChannelID: themesChannelID, MessageTS: messages[0], UserID: themesVoterID}, v)
}

func (s *ThemesTestSuite) TestPostedUntaggedArticleTakesItsNormalisedPickTopicsAsTags() {
	s.addBlog(domain.SourceActive, "a")
	s.learnedProfile()
	s.answerPickWithTopics("a", " Go ", "Machine  Learning", "go", "sqlite", "rust")

	err := s.app.Digest.Run(s.Ctx, domain.RunKindDigestMorning)

	s.Require().NoError(err)
	s.Require().Equal(1, s.agentRuns(tagPrompt))
	s.Equal([]string{"go", "machine learning", "sqlite"}, s.tagsOf("a"))
}

func (s *ThemesTestSuite) TestVoteOnAPostTaggedFromItsPickTopicsCountsForThoseThemes() {
	s.addBlog(domain.SourceActive, "a")
	s.learnedProfile()
	s.answerPickWithTopics("a", "go", "sqlite")
	s.Require().NoError(s.app.Digest.Run(s.Ctx, domain.RunKindDigestMorning))

	s.voteOnPostOf("a", domain.VoteUp)

	s.Equal(map[string]domain.Votes{"go": {Up: 1}, "sqlite": {Up: 1}}, s.serviceThemeScores())
}

func (s *ThemesTestSuite) TestPostedArticleWithStoredTagsKeepsThem() {
	sourceID := s.addBlog(domain.SourceActive)
	s.insertArticle(sourceID, "a", time.Hour, "rust")
	s.learnedProfile()
	s.answerPickWithTopics("a", "go")

	err := s.app.Digest.Run(s.Ctx, domain.RunKindDigestMorning)

	s.Require().NoError(err)
	s.Equal(0, s.agentRuns(tagPrompt))
	s.Equal([]string{"rust"}, s.tagsOf("a"))
}

func (s *ThemesTestSuite) TestPostedArticleKeepsTheTagsOfTheTagJob() {
	s.addBlog(domain.SourceActive, "a")
	s.learnedProfile()
	s.answerTags(map[string][]string{"a": {"sqlite"}})
	s.answerPickWithTopics("a", "go")

	err := s.app.Digest.Run(s.Ctx, domain.RunKindDigestMorning)

	s.Require().NoError(err)
	s.Equal([]string{"sqlite"}, s.tagsOf("a"))
}

func (s *ThemesTestSuite) TestPostedArticleWithoutAValidPickTopicStaysUntagged() {
	s.addBlog(domain.SourceActive, "a")
	s.learnedProfile()
	s.answerPickWithTopics("a", "", "   ")

	err := s.app.Digest.Run(s.Ctx, domain.RunKindDigestMorning)

	s.Require().NoError(err)
	s.Require().Len(s.slack.Posted(), 2)
	s.Empty(s.tagsOf("a"))
}
