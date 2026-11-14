package usecase

import (
	"context"
	"sync"

	"github.com/rs/zerolog/log"

	"github.com/sadensmol/blogger/internal/domain"
)

type JobKind string

const (
	JobMorningDigest JobKind = "digest-morning"
	JobShowDigest    JobKind = "digest-show"
	JobDiscovery     JobKind = "discovery"
	JobSuggest       JobKind = "suggest"
)

type Job struct {
	Kind     JobKind
	ReplyURL string // the response url of the slash command that asked for the job
}

type SubmitResult string

const (
	Queued        SubmitResult = "queued"
	AlreadyQueued SubmitResult = "already-queued"
	Busy          SubmitResult = "busy"
)

type JobsData struct {
	Digest    *Digest
	Discovery *Discovery
	Suggest   *Suggest
	Size      int
}

type Jobs struct {
	digest    *Digest
	discovery *Discovery
	suggest   *Suggest
	queue     chan Job

	mu      sync.Mutex
	waiting map[JobKind]struct{}
}

func NewJobs(d JobsData) *Jobs {
	return &Jobs{
		digest:    d.Digest,
		discovery: d.Discovery,
		suggest:   d.Suggest,
		queue:     make(chan Job, d.Size),
		waiting:   map[JobKind]struct{}{},
	}
}

// Submit never blocks: a kind that waits or runs is merged, a full queue answers Busy.
func (j *Jobs) Submit(job Job) SubmitResult {
	j.mu.Lock()
	defer j.mu.Unlock()

	if _, ok := j.waiting[job.Kind]; ok {
		return AlreadyQueued
	}
	select {
	case j.queue <- job:
		j.waiting[job.Kind] = struct{}{}
		return Queued
	default:
		log.Warn().Msgf("job queue is full: %s refused", job.Kind)
		return Busy
	}
}

// Run is the one worker. It returns when ctx ends, after the current job.
func (j *Jobs) Run(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case job := <-j.queue:
			// select picks at random when both are ready: a stopped worker starts no new job
			if ctx.Err() != nil {
				j.forget(job.Kind)
				return
			}
			j.execute(ctx, job)
		}
	}
}

// execute lets a started job finish after ctx ends: it must not kill its agent halfway.
func (j *Jobs) execute(ctx context.Context, job Job) {
	jobCtx := context.WithoutCancel(ctx)
	defer j.forget(job.Kind)
	defer func() {
		if r := recover(); r != nil {
			log.Ctx(jobCtx).Error().Msgf("job %s panicked: %v", job.Kind, r)
		}
	}()
	var err error
	switch job.Kind {
	case JobMorningDigest:
		err = j.digest.Run(jobCtx, domain.RunKindDigestMorning)
	case JobShowDigest:
		err = j.digest.Show(jobCtx, job.ReplyURL)
	case JobDiscovery:
		_, err = j.discovery.Run(jobCtx)
	case JobSuggest:
		err = j.suggest.Run(jobCtx, job.ReplyURL)
	}
	if err != nil {
		log.Ctx(jobCtx).Error().Err(err).Msgf("job %s failed", job.Kind)
	}
}

func (j *Jobs) forget(kind JobKind) {
	j.mu.Lock()
	defer j.mu.Unlock()
	delete(j.waiting, kind)
}
