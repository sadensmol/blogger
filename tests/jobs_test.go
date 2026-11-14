package tests_test

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/suite"

	"github.com/sadensmol/blogger/internal/domain"
	"github.com/sadensmol/blogger/internal/llm"
	"github.com/sadensmol/blogger/internal/usecase"
	"github.com/sadensmol/blogger/tests"
	testsrepository "github.com/sadensmol/blogger/tests/repository"
)

const (
	jobsFeedPath    = "/jobs.xml"
	jobsArticle     = "a"
	jobsQueueSize   = 3
	jobsWaitTimeout = 10 * time.Second
	jobsTick        = 20 * time.Millisecond
	jobsStopWithin  = 3 * time.Second
)

type JobsTestSuite struct {
	tests.Suite

	feeds  *tests.FakeFeeds
	slack  *tests.FakeSlack
	agents *tests.FakeAgents
	runs   *testsrepository.Run
	jobs   *usecase.Jobs
}

func TestJobsTestSuite(t *testing.T) {
	suite.Run(t, &JobsTestSuite{})
}

func (s *JobsTestSuite) SetupTest() {
	s.Suite.SetupTest()
	s.feeds = tests.NewFakeFeeds()
	s.slack = tests.NewFakeSlack()
	s.agents = tests.NewFakeAgents(s.T())
	s.runs = testsrepository.NewRun(s.DB)
	s.jobs = usecase.NewJobs(s.jobsData(jobsQueueSize))

	s.feeds.Set(jobsFeedPath, "application/rss+xml", fmt.Sprintf(itemsFmt, fmt.Sprintf(itemFmt, jobsArticle, s.articleURL())))
	_, err := testsrepository.NewSource(s.DB).Insert(s.Ctx, testsrepository.SourceRow{
		FeedURL: fmt.Sprintf("%s%s", s.feeds.URL(), jobsFeedPath),
		Status:  string(domain.SourceActive),
	}, s.Clock.Now())
	s.Require().NoError(err)
	s.Require().NoError(testsrepository.NewProfileVersion(s.DB).Insert(s.Ctx, string(domain.ProfileInit), s.Clock.Now().Add(-2*time.Hour)))
	s.agents.Answer(llm.Claude, llm.SkillPickArticles, map[string]any{"picks": []map[string]any{{
		"url": s.articleURL(), "description": "A post.", "topics": []string{"go"}, "reason": "Matches Go.",
	}}})
	s.agents.Answer(llm.Claude, llm.SkillDiscoverBlogs, map[string]any{"blogs": []map[string]any{}})
}

func (s *JobsTestSuite) TearDownTest() {
	s.feeds.Close()
	s.slack.Close()
	s.Suite.TearDownTest()
}

func (s *JobsTestSuite) articleURL() string {
	return fmt.Sprintf("%s/%s", s.feeds.URL(), jobsArticle)
}

func (s *JobsTestSuite) jobsData(size int) usecase.JobsData {
	location, err := time.LoadLocation("Europe/Berlin")
	s.Require().NoError(err)
	return tests.NewApp(s.T(), tests.AppData{
		DB:          s.DB,
		Now:         s.Clock.Now,
		Agents:      s.agents,
		Backends:    []llm.Backend{llm.Claude},
		SlackURL:    s.slack.URL,
		ChannelID:   digestChannelID,
		ProfilePath: writeLearnedProfile(s.T()),
		Location:    location,
		DigestSize:  digestSize,
	}).JobsData(size)
}

// startWorker returns the cancel of the worker and a channel closed when Run returns; cleanup stops it.
func (s *JobsTestSuite) startWorker() (cancel func(), stopped <-chan struct{}) {
	ctx, cancelCtx := context.WithCancel(s.Ctx)
	done := make(chan struct{})
	go func() {
		defer close(done)
		s.jobs.Run(ctx)
	}()
	s.T().Cleanup(func() {
		cancelCtx()
		<-done
	})
	return cancelCtx, done
}

func (s *JobsTestSuite) runStatus(kind domain.RunKind) string {
	result, err := s.runs.LatestByKind(s.Ctx, string(kind))
	if err != nil {
		return ""
	}
	return result.Status
}

func (s *JobsTestSuite) okRuns(kind domain.RunKind) int {
	var n int
	s.Require().NoError(s.DB.QueryRowContext(s.Ctx, "SELECT COUNT(*) FROM run WHERE kind = ? AND status = ?",
		string(kind), string(domain.RunStatusOK)).Scan(&n))
	return n
}

func (s *JobsTestSuite) TestJobsRunOneAtATime() {
	s.agents.Sleep(llm.Claude, 1)
	s.Equal(usecase.Queued, s.jobs.Submit(usecase.JobShowDigest))
	s.Equal(usecase.Queued, s.jobs.Submit(usecase.JobDiscovery))

	s.startWorker()

	s.Eventually(func() bool {
		return s.runStatus(domain.RunKindDigestShow) == string(domain.RunStatusOK) &&
			s.runStatus(domain.RunKindDiscovery) == string(domain.RunStatusOK)
	}, jobsWaitTimeout, jobsTick)
	s.False(s.agents.Overlapped())
}

func (s *JobsTestSuite) TestSubmitSameKindWhileWaitingOrRunningIsMerged() {
	s.Equal(usecase.Queued, s.jobs.Submit(usecase.JobShowDigest))
	s.Equal(usecase.AlreadyQueued, s.jobs.Submit(usecase.JobShowDigest))

	s.startWorker()

	s.Eventually(func() bool { return s.runStatus(domain.RunKindDigestShow) == string(domain.RunStatusOK) }, jobsWaitTimeout, jobsTick)
	s.Equal(1, s.okRuns(domain.RunKindDigestShow))
}

func (s *JobsTestSuite) TestSubmitSameKindWhileRunningIsMerged() {
	s.agents.Sleep(llm.Claude, 1)
	s.Equal(usecase.Queued, s.jobs.Submit(usecase.JobShowDigest))
	s.startWorker()
	s.Eventually(func() bool { return len(s.agents.Args(llm.Claude)) > 0 }, jobsWaitTimeout, jobsTick)

	s.Equal(usecase.AlreadyQueued, s.jobs.Submit(usecase.JobShowDigest))
}

func (s *JobsTestSuite) TestSubmitAfterFinishQueuesAgain() {
	s.Equal(usecase.Queued, s.jobs.Submit(usecase.JobShowDigest))
	s.startWorker()
	s.Eventually(func() bool { return s.runStatus(domain.RunKindDigestShow) == string(domain.RunStatusOK) }, jobsWaitTimeout, jobsTick)

	s.Eventually(func() bool { return s.jobs.Submit(usecase.JobShowDigest) == usecase.Queued }, jobsWaitTimeout, jobsTick)
	s.Eventually(func() bool { return s.okRuns(domain.RunKindDigestShow) == 2 }, jobsWaitTimeout, jobsTick)
}

func (s *JobsTestSuite) TestFullQueueAnswersBusy() {
	jobs := usecase.NewJobs(s.jobsData(1))

	s.Equal(usecase.Queued, jobs.Submit(usecase.JobShowDigest))
	s.Equal(usecase.Busy, jobs.Submit(usecase.JobDiscovery))
}

func (s *JobsTestSuite) TestWorkerStopsAfterCancel() {
	s.agents.Sleep(llm.Claude, 1)
	s.Equal(usecase.Queued, s.jobs.Submit(usecase.JobShowDigest))
	cancel, stopped := s.startWorker()
	s.Eventually(func() bool { return len(s.agents.Args(llm.Claude)) > 0 }, jobsWaitTimeout, jobsTick)

	cancel()

	select {
	case <-stopped:
	case <-time.After(jobsStopWithin):
		s.FailNow("worker did not stop")
	}
	s.Equal(string(domain.RunStatusOK), s.runStatus(domain.RunKindDigestShow))
}

func (s *JobsTestSuite) TestStoppedWorkerStartsNoQueuedJob() {
	s.agents.Sleep(llm.Claude, 1)
	s.Equal(usecase.Queued, s.jobs.Submit(usecase.JobShowDigest))
	s.Equal(usecase.Queued, s.jobs.Submit(usecase.JobDiscovery))
	cancel, stopped := s.startWorker()
	s.Eventually(func() bool { return len(s.agents.Args(llm.Claude)) > 0 }, jobsWaitTimeout, jobsTick)

	cancel()

	select {
	case <-stopped:
	case <-time.After(jobsStopWithin):
		s.FailNow("worker did not stop")
	}
	s.Equal(1, s.okRuns(domain.RunKindDigestShow))
	s.Empty(s.runStatus(domain.RunKindDiscovery))
}

func (s *JobsTestSuite) TestWorkerSurvivesPanickingJob() {
	data := s.jobsData(jobsQueueSize)
	data.Digest = nil
	s.jobs = usecase.NewJobs(data)
	s.Equal(usecase.Queued, s.jobs.Submit(usecase.JobShowDigest))
	s.Equal(usecase.Queued, s.jobs.Submit(usecase.JobDiscovery))

	s.startWorker()

	s.Eventually(func() bool { return s.runStatus(domain.RunKindDiscovery) == string(domain.RunStatusOK) }, jobsWaitTimeout, jobsTick)
	s.Eventually(func() bool { return s.jobs.Submit(usecase.JobShowDigest) == usecase.Queued }, jobsWaitTimeout, jobsTick)
}
