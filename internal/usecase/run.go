package usecase

import (
	"context"
	"fmt"

	"github.com/rs/zerolog/log"

	"github.com/sadensmol/blogger/internal/domain"
	"github.com/sadensmol/blogger/internal/llm"
	"github.com/sadensmol/blogger/internal/service"
)

// withRun records work as one run of kind; the run fails when work returns an error.
func withRun(ctx context.Context, runs *service.Runs, kind domain.RunKind, work func(domain.RunID) (llm.Backend, error)) error {
	runID, err := runs.Start(ctx, kind)
	if err != nil {
		return fmt.Errorf("cannot start %s run: %w", kind, err)
	}
	backend, workErr := work(runID)
	if err := runs.Finish(ctx, service.FinishData{ID: runID, Kind: kind, Backend: string(backend), Err: workErr}); err != nil {
		log.Ctx(ctx).Error().Err(err).Msgf("cannot record the end of %s run %d", kind, runID)
	}
	if workErr != nil {
		return fmt.Errorf("%s run %d failed: %w", kind, runID, workErr)
	}
	return nil
}
