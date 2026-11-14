package scheduler

import (
	"context"
	"time"

	"github.com/rs/zerolog/log"

	"github.com/sadensmol/blogger/internal/domain"
	"github.com/sadensmol/blogger/internal/service"
	"github.com/sadensmol/blogger/internal/usecase"
)

const (
	tickEvery      = time.Minute
	discoveryEvery = 7 * 24 * time.Hour
)

type Data struct {
	Jobs       *usecase.Jobs
	Articles   *service.Articles
	Runs       *service.Runs
	At         DailyTime
	Location   *time.Location
	FetchEvery time.Duration
	Now        func() time.Time
}

type Scheduler struct {
	d           Data
	clockWarned bool
}

func New(d Data) *Scheduler {
	return &Scheduler{d: d}
}

func (s *Scheduler) Tick(ctx context.Context) {
	now := s.d.Now()
	if !ClockSane(now) {
		if !s.clockWarned {
			log.Ctx(ctx).Warn().Msgf("board clock is wrong (%s): scheduler waits", now.Format(time.RFC3339))
			s.clockWarned = true
		}
		return
	}
	s.clockWarned = false
	local := now.In(s.d.Location)

	s.submitMorning(ctx, local)
	s.submitDiscovery(ctx, local)
	s.fetch(ctx, now)
}

func (s *Scheduler) Run(ctx context.Context) {
	ticker := time.NewTicker(tickEvery)
	defer ticker.Stop()
	for {
		s.Tick(ctx)
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func (s *Scheduler) submitMorning(ctx context.Context, local time.Time) {
	last, err := s.d.Runs.LastStarted(ctx, domain.RunKindDigestMorning)
	if err != nil {
		log.Ctx(ctx).Error().Err(err).Msg("scheduler cannot check the morning digest")
		return
	}
	if MorningDue(local, s.d.At, last.In(s.d.Location)) {
		log.Ctx(ctx).Debug().Msgf("morning digest due: %s", s.d.Jobs.Submit(usecase.JobMorningDigest))
	}
}

func (s *Scheduler) submitDiscovery(ctx context.Context, local time.Time) {
	if local.Before(s.d.At.slot(local)) {
		return
	}
	last, err := s.d.Runs.LastStarted(ctx, domain.RunKindDiscovery)
	if err != nil {
		log.Ctx(ctx).Error().Err(err).Msg("scheduler cannot check discovery")
		return
	}
	if IntervalDue(local, last, discoveryEvery) {
		log.Ctx(ctx).Debug().Msgf("discovery due: %s", s.d.Jobs.Submit(usecase.JobDiscovery))
	}
}

func (s *Scheduler) fetch(ctx context.Context, now time.Time) {
	last, err := s.d.Runs.LastStarted(ctx, domain.RunKindFetch)
	if err != nil {
		log.Ctx(ctx).Error().Err(err).Msg("scheduler cannot check the last fetch")
		return
	}
	if !IntervalDue(now, last, s.d.FetchEvery) {
		return
	}
	if _, err := s.d.Articles.FetchAll(ctx); err != nil {
		log.Ctx(ctx).Error().Err(err).Msg("scheduled fetch failed")
	}
}
