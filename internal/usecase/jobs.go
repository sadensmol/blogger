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
)

type SubmitResult string

const (
	Queued        SubmitResult = "queued"
	AlreadyQueued SubmitResult = "already-queued"
	Busy          SubmitResult = "busy"
)

type JobsData struct {
	Digest    *Digest
	Discovery *Discovery
	Size      int
}

type Jobs struct {
	digest    *Digest
	discovery *Discovery
	queue     chan JobKind

	mu      sync.Mutex
	waiting map[JobKind]struct{}
}

func NewJobs(d JobsData) *Jobs {
	return &Jobs{
		digest:    d.Digest,
		discovery: d.Discovery,
		queue:     make(chan JobKind, d.Size),
		waiting:   map[JobKind]struct{}{},
	}
}

// Submit never blocks: a kind that waits or runs is merged, a full queue answers Busy.
func (j *Jobs) Submit(kind JobKind) SubmitResult {
	j.mu.Lock()
	defer j.mu.Unlock()

	if _, ok := j.waiting[kind]; ok {
		return AlreadyQueued
	}
	select {
	case j.queue <- kind:
		j.waiting[kind] = struct{}{}
		return Queued
	default:
		log.Warn().Msgf("job queue is full: %s refused", kind)
		return Busy
	}
}

// Run is the one worker. It returns when ctx ends, after the current job.
func (j *Jobs) Run(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case kind := <-j.queue:
			j.execute(ctx, kind)
		}
	}
}

// execute lets a started job finish after ctx ends: it must not kill its agent halfway.
func (j *Jobs) execute(ctx context.Context, kind JobKind) {
	jobCtx := context.WithoutCancel(ctx)
	var err error
	switch kind {
	case JobMorningDigest:
		err = j.digest.Run(jobCtx, domain.RunKindDigestMorning)
	case JobShowDigest:
		err = j.digest.Run(jobCtx, domain.RunKindDigestShow)
	case JobDiscovery:
		_, err = j.discovery.Run(jobCtx)
	}
	if err != nil {
		log.Ctx(jobCtx).Error().Err(err).Msgf("job %s failed", kind)
	}

	j.mu.Lock()
	delete(j.waiting, kind)
	j.mu.Unlock()
}
