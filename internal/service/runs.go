package service

import (
	"context"
	"fmt"
	"time"

	"github.com/rs/zerolog/log"

	"github.com/sadensmol/blogger/internal/domain"
	"github.com/sadensmol/blogger/internal/repository"
)

const failedDigestBlocksFor = time.Hour

type Runs struct {
	repo *repository.Run
	now  func() time.Time
}

func NewRuns(repo *repository.Run, now func() time.Time) *Runs {
	return &Runs{repo: repo, now: now}
}

type FinishData struct {
	ID      domain.RunID
	Kind    domain.RunKind
	Backend string
	Err     error
}

func (r *Runs) Start(ctx context.Context, kind domain.RunKind) (domain.RunID, error) {
	id, err := r.repo.Create(ctx, kind, r.now())
	if err != nil {
		return 0, fmt.Errorf("cannot start run: %w", err)
	}
	return id, nil
}

func (r *Runs) Finish(ctx context.Context, d FinishData) error {
	data := repository.FinishRunData{ID: d.ID, Status: domain.RunStatusOK, Backend: d.Backend, Now: r.now()}
	if d.Err != nil {
		data.Status = domain.RunStatusFailed
		data.Error = d.Err.Error()
	}
	if err := r.repo.Finish(ctx, data); err != nil {
		return fmt.Errorf("cannot finish run: %w", err)
	}
	log.Ctx(ctx).Info().Msgf("run %d finished: kind=%s status=%s backend=%q", d.ID, d.Kind, data.Status, d.Backend)
	return nil
}

func (r *Runs) LastStarted(ctx context.Context, kind domain.RunKind) (time.Time, error) {
	startedAt, err := r.repo.LastStartedAt(ctx, kind)
	if err != nil {
		return time.Time{}, fmt.Errorf("cannot read last run: %w", err)
	}
	return startedAt, nil
}

func (r *Runs) LastDigestStarted(ctx context.Context) (time.Time, error) {
	startedAt, err := r.repo.LastMorningOrShowProvidedStartedAt(ctx, r.now().Add(-failedDigestBlocksFor))
	if err != nil {
		return time.Time{}, fmt.Errorf("cannot read last digest run: %w", err)
	}
	return startedAt, nil
}

func (r *Runs) LastFinishedStarted(ctx context.Context, kind domain.RunKind) (time.Time, error) {
	startedAt, err := r.repo.LastFinishedStartedAt(ctx, kind)
	if err != nil {
		return time.Time{}, fmt.Errorf("cannot read last finished run: %w", err)
	}
	return startedAt, nil
}

func (r *Runs) FailInterrupted(ctx context.Context) (int64, error) {
	n, err := r.repo.FailRunning(ctx, r.now())
	if err != nil {
		return 0, fmt.Errorf("cannot fail interrupted runs: %w", err)
	}
	return n, nil
}
