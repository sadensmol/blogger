package tests_test

import (
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/suite"

	"github.com/sadensmol/blogger/internal/domain"
	"github.com/sadensmol/blogger/internal/feed"
	"github.com/sadensmol/blogger/internal/repository"
	"github.com/sadensmol/blogger/internal/service"
	"github.com/sadensmol/blogger/tests"
	testsrepository "github.com/sadensmol/blogger/tests/repository"
)

const (
	voterID     = "U1"
	channelID   = "C1"
	feedbackURL = "https://example.com/feed"
)

type FeedbackTestSuite struct {
	tests.Suite

	sources  *testsrepository.Source
	articles *testsrepository.Article
	runs     *testsrepository.Run
	posts    *testsrepository.Post
	reactions *testsrepository.Reaction
	sourceSv *service.Source
	votes    *service.Votes
}

func TestFeedbackTestSuite(t *testing.T) {
	suite.Run(t, &FeedbackTestSuite{})
}

func (s *FeedbackTestSuite) SetupTest() {
	s.Suite.SetupTest()
	s.sources = testsrepository.NewSource(s.DB)
	s.articles = testsrepository.NewArticle(s.DB)
	s.runs = testsrepository.NewRun(s.DB)
	s.posts = testsrepository.NewPost(s.DB)
	s.reactions = testsrepository.NewReaction(s.DB)
	s.sourceSv = service.NewSource(repository.NewSource(s.DB), feed.NewClient(time.Second), s.Clock.Now)
	s.votes = service.NewVotes(service.VotesData{
		Reactions: repository.NewReaction(s.DB),
		Posts:     service.NewPosts(repository.NewPost(s.DB), s.Clock.Now),
		Sources:   s.sourceSv,
		Now:       s.Clock.Now,
	})
}

func (s *FeedbackTestSuite) insertSource(status domain.SourceStatus) int64 {
	id, err := s.sources.Insert(s.Ctx, testsrepository.SourceRow{FeedURL: feedbackURL, Status: string(status)}, s.Clock.Now())
	s.Require().NoError(err)
	return id
}

func (s *FeedbackTestSuite) insertPost(sourceID int64, n int) (int64, domain.ReactionEvent) {
	articleID, err := s.articles.Insert(s.Ctx, testsrepository.ArticleRow{
		SourceID: sourceID, URL: fmt.Sprintf("%s/article-%d", feedbackURL, n), PublishedAt: s.Clock.Now(), Now: s.Clock.Now(),
	})
	s.Require().NoError(err)
	runID, err := s.runs.Insert(s.Ctx, string(domain.RunKindFetch), string(domain.RunStatusOK), s.Clock.Now())
	s.Require().NoError(err)
	postID, err := s.posts.Insert(s.Ctx, articleID, runID, s.Clock.Now())
	s.Require().NoError(err)
	return postID, domain.ReactionEvent{ChannelID: channelID, MessageTS: fmt.Sprintf("ts-%d", articleID), UserID: voterID}
}

func (s *FeedbackTestSuite) vote(event domain.ReactionEvent, v domain.Vote) {
	event.Vote = v
	s.Require().NoError(s.votes.Add(s.Ctx, event))
}

func (s *FeedbackTestSuite) votesOf(id int64) (up, down int) {
	up, err := s.sources.UpVotes(s.Ctx, id)
	s.Require().NoError(err)
	down, err = s.sources.DownVotes(s.Ctx, id)
	s.Require().NoError(err)
	return up, down
}

func (s *FeedbackTestSuite) status(id int64) string {
	status, err := s.sources.Status(s.Ctx, id)
	s.Require().NoError(err)
	return status
}

func (s *FeedbackTestSuite) TestAddStoresUpVote() {
	id := s.insertSource(domain.SourceActive)
	_, event := s.insertPost(id, 1)

	s.vote(event, domain.VoteUp)

	up, down := s.votesOf(id)
	s.Equal(1, up)
	s.Equal(0, down)
}

func (s *FeedbackTestSuite) TestBothThumbsOnOnePostCountPlusAndMinusOne() {
	id := s.insertSource(domain.SourceActive)
	_, event := s.insertPost(id, 1)

	s.vote(event, domain.VoteUp)
	s.vote(event, domain.VoteDown)

	up, down := s.votesOf(id)
	s.Equal(1, up)
	s.Equal(1, down)
}

func (s *FeedbackTestSuite) TestAddForUnknownMessageIsIgnored() {
	s.insertSource(domain.SourceActive)

	err := s.votes.Add(s.Ctx, domain.ReactionEvent{ChannelID: channelID, MessageTS: "nope", UserID: voterID, Vote: domain.VoteUp})

	s.Require().NoError(err)
	s.Equal(0, s.CountRows("reaction"))
}

func (s *FeedbackTestSuite) TestAddTwiceStoresOneRow() {
	_, event := s.insertPost(s.insertSource(domain.SourceActive), 1)

	s.vote(event, domain.VoteUp)
	s.vote(event, domain.VoteUp)

	s.Equal(1, s.CountRows("reaction"))
}

func (s *FeedbackTestSuite) TestAddForRemovedBlogStoresVoteWithoutError() {
	id := s.insertSource(domain.SourceActive)
	_, event := s.insertPost(id, 1)
	s.Require().NoError(s.sources.MarkDeleted(s.Ctx, id, s.Clock.Now()))
	event.Vote = domain.VoteUp

	err := s.votes.Add(s.Ctx, event)

	s.Require().NoError(err)
	s.Equal(1, s.CountRows("reaction"))
}

func (s *FeedbackTestSuite) TestRemoveDropsVote() {
	id := s.insertSource(domain.SourceActive)
	postID, event := s.insertPost(id, 1)
	s.Require().NoError(s.reactions.Insert(s.Ctx, testsrepository.ReactionRow{
		PostID: postID, UserID: voterID, Vote: int(domain.VoteUp), CreatedAt: s.Clock.Now(),
	}))
	before := s.Clock.Now()
	s.Clock.Advance(time.Minute)

	event.Vote = domain.VoteUp
	err := s.votes.Remove(s.Ctx, event)
	changed, changedErr := s.votes.ChangedSince(s.Ctx, before)

	s.Require().NoError(err)
	s.Require().NoError(changedErr)
	up, down := s.votesOf(id)
	s.Equal(0, up)
	s.Equal(0, down)
	s.True(changed)
}

func (s *FeedbackTestSuite) TestThirdNetDownVotePausesActiveSource() {
	id := s.insertSource(domain.SourceActive)

	for n := 1; n <= 3; n++ {
		_, event := s.insertPost(id, n)
		s.vote(event, domain.VoteDown)
	}

	s.Equal(string(domain.SourcePaused), s.status(id))
}

func (s *FeedbackTestSuite) TestTwoNetUpVotesPromoteTrialSource() {
	id := s.insertSource(domain.SourceTrial)

	for n := 1; n <= 2; n++ {
		_, event := s.insertPost(id, n)
		s.vote(event, domain.VoteUp)
	}

	s.Equal(string(domain.SourceActive), s.status(id))
}

func (s *FeedbackTestSuite) TestResumedSourceStartsFromZero() {
	id := s.insertSource(domain.SourceActive)
	for n := 1; n <= 3; n++ {
		postID, _ := s.insertPost(id, n)
		s.Require().NoError(s.reactions.Insert(s.Ctx, testsrepository.ReactionRow{
			PostID: postID, UserID: voterID, Vote: int(domain.VoteDown), CreatedAt: s.Clock.Now(),
		}))
	}
	s.Clock.Advance(time.Hour)
	s.Require().NoError(s.sources.SetStatusChangedAt(s.Ctx, id, s.Clock.Now()))
	s.Clock.Advance(time.Hour)
	_, event := s.insertPost(id, 4)

	s.vote(event, domain.VoteDown)

	s.Equal(string(domain.SourceActive), s.status(id))
}
