package tests_test

import (
	"fmt"
	"strconv"
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

func (s *FeedbackTestSuite) insertPost(sourceID int64, n int) domain.ReactionEvent {
	articleID, err := s.articles.Insert(s.Ctx, testsrepository.ArticleRow{
		SourceID: sourceID, URL: fmt.Sprintf("%s/article-%d", feedbackURL, n), PublishedAt: s.Clock.Now(), Now: s.Clock.Now(),
	})
	s.Require().NoError(err)
	runID, err := s.runs.Insert(s.Ctx, string(domain.RunKindFetch), string(domain.RunStatusOK), s.Clock.Now())
	s.Require().NoError(err)
	_, err = s.posts.Insert(s.Ctx, articleID, runID, s.Clock.Now())
	s.Require().NoError(err)
	return domain.ReactionEvent{ChannelID: channelID, MessageTS: fmt.Sprintf("ts-%d", articleID), UserID: voterID}
}

func (s *FeedbackTestSuite) vote(event domain.ReactionEvent, v domain.Vote) {
	event.Vote = v
	s.Require().NoError(s.votes.Add(s.Ctx, event))
}

func (s *FeedbackTestSuite) sourceVotes(id int64) domain.Votes {
	src, err := s.sourceSv.List(s.Ctx)
	s.Require().NoError(err)
	for _, candidate := range src {
		if int64(candidate.ID) == id {
			return candidate.Votes
		}
	}
	s.FailNow("source not listed")
	return domain.Votes{}
}

func (s *FeedbackTestSuite) status(id int64) string {
	status, err := s.sources.Status(s.Ctx, id)
	s.Require().NoError(err)
	return status
}

func (s *FeedbackTestSuite) TestAddStoresUpVote() {
	id := s.insertSource(domain.SourceActive)

	s.vote(s.insertPost(id, 1), domain.VoteUp)

	s.Equal(domain.Votes{Up: 1}, s.sourceVotes(id))
}

func (s *FeedbackTestSuite) TestBothThumbsOnOnePostCountPlusAndMinusOne() {
	id := s.insertSource(domain.SourceActive)
	event := s.insertPost(id, 1)

	s.vote(event, domain.VoteUp)
	s.vote(event, domain.VoteDown)

	s.Equal(domain.Votes{Up: 1, Down: 1}, s.sourceVotes(id))
}

func (s *FeedbackTestSuite) TestAddForUnknownMessageIsIgnored() {
	s.insertSource(domain.SourceActive)

	err := s.votes.Add(s.Ctx, domain.ReactionEvent{ChannelID: channelID, MessageTS: "nope", UserID: voterID, Vote: domain.VoteUp})

	s.Require().NoError(err)
	s.Equal(0, s.CountRows("reaction"))
}

func (s *FeedbackTestSuite) TestAddTwiceStoresOneRow() {
	event := s.insertPost(s.insertSource(domain.SourceActive), 1)

	s.vote(event, domain.VoteUp)
	s.vote(event, domain.VoteUp)

	s.Equal(1, s.CountRows("reaction"))
}

func (s *FeedbackTestSuite) TestRemoveDropsVote() {
	id := s.insertSource(domain.SourceActive)
	event := s.insertPost(id, 1)
	s.vote(event, domain.VoteUp)
	before := s.Clock.Now()
	s.Clock.Advance(time.Minute)

	event.Vote = domain.VoteUp
	err := s.votes.Remove(s.Ctx, event)
	changed, changedErr := s.votes.ChangedSince(s.Ctx, before)

	s.Require().NoError(err)
	s.Require().NoError(changedErr)
	s.Equal(domain.Votes{}, s.sourceVotes(id))
	s.True(changed)
}

func (s *FeedbackTestSuite) TestThirdNetDownVotePausesActiveSource() {
	id := s.insertSource(domain.SourceActive)

	for n := 1; n <= 3; n++ {
		s.vote(s.insertPost(id, n), domain.VoteDown)
	}

	s.Equal(string(domain.SourcePaused), s.status(id))
}

func (s *FeedbackTestSuite) TestTwoNetUpVotesPromoteTrialSource() {
	id := s.insertSource(domain.SourceTrial)

	for n := 1; n <= 2; n++ {
		s.vote(s.insertPost(id, n), domain.VoteUp)
	}

	s.Equal(string(domain.SourceActive), s.status(id))
}

func (s *FeedbackTestSuite) TestResumedSourceStartsFromZero() {
	id := s.insertSource(domain.SourceActive)
	for n := 1; n <= 3; n++ {
		s.vote(s.insertPost(id, n), domain.VoteDown)
	}
	s.Require().Equal(string(domain.SourcePaused), s.status(id))
	s.Clock.Advance(time.Hour)
	_, err := s.sourceSv.Resume(s.Ctx, strconv.FormatInt(id, 10))
	s.Require().NoError(err)
	s.Clock.Advance(time.Hour)

	s.vote(s.insertPost(id, 4), domain.VoteDown)

	s.Equal(string(domain.SourceActive), s.status(id))
}
