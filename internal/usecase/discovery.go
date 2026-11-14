package usecase

import (
	"context"
	"fmt"

	"github.com/rs/zerolog/log"

	"github.com/sadensmol/blogger/internal/domain"
	"github.com/sadensmol/blogger/internal/feed"
	"github.com/sadensmol/blogger/internal/llm"
	"github.com/sadensmol/blogger/internal/service"
)

const (
	discoveryAsked = 5
	discoveryAdds  = 3
)

type DiscoveryData struct {
	Sources *service.Source
	Feeds   *feed.Client
	Runs    *service.Runs
	LLM     *llm.Runner
}

type Discovery struct {
	sources *service.Source
	feeds   *feed.Client
	runs    *service.Runs
	llm     *llm.Runner
}

func NewDiscovery(d DiscoveryData) *Discovery {
	return &Discovery{sources: d.Sources, feeds: d.Feeds, runs: d.Runs, llm: d.LLM}
}

// Run returns how many trial blogs it added.
func (d *Discovery) Run(ctx context.Context) (int, error) {
	runID, err := d.runs.Start(ctx, domain.RunKindDiscovery)
	if err != nil {
		return 0, fmt.Errorf("cannot start %s run: %w", domain.RunKindDiscovery, err)
	}
	added, backend, workErr := d.discover(ctx)
	finishErr := d.runs.Finish(ctx, service.FinishData{ID: runID, Kind: domain.RunKindDiscovery, Backend: string(backend), Err: workErr})
	if finishErr != nil {
		log.Ctx(ctx).Error().Err(finishErr).Msgf("cannot record the end of %s run %d", domain.RunKindDiscovery, runID)
	}
	if workErr != nil {
		return added, fmt.Errorf("%s run %d failed: %w", domain.RunKindDiscovery, runID, workErr)
	}
	log.Ctx(ctx).Info().Msgf("%s run %d added %d trial blogs", domain.RunKindDiscovery, runID, added)
	return added, nil
}

func (d *Discovery) discover(ctx context.Context) (int, llm.Backend, error) {
	sources, err := d.sources.List(ctx)
	if err != nil {
		return 0, "", fmt.Errorf("cannot list blogs: %w", err)
	}
	if len(sources) == 0 {
		return 0, "", nil
	}

	proposals, backend, err := d.llm.DiscoverBlogs(ctx, sources, discoveryAsked)
	if err != nil {
		return 0, "", fmt.Errorf("cannot ask for blogs: %w", err)
	}

	added := 0
	for _, p := range proposals {
		if added == discoveryAdds {
			break
		}
		known, err := d.sources.Known(ctx, p.FeedURL)
		if err != nil {
			return added, backend, fmt.Errorf("cannot check proposed blog: %w", err)
		}
		if known {
			continue
		}
		items, err := d.feeds.Fetch(ctx, p.FeedURL)
		if err != nil {
			log.Ctx(ctx).Warn().Err(err).Msgf("skip proposed blog %s: its feed does not load", p.FeedURL)
			continue
		}
		if len(items) == 0 {
			log.Ctx(ctx).Warn().Msgf("skip proposed blog %s: its feed has no item", p.FeedURL)
			continue
		}
		if _, err := d.sources.Create(ctx, domain.Source{
			SiteURL: p.SiteURL,
			FeedURL: p.FeedURL,
			Title:   p.Title,
			Reason:  p.Reason,
			Status:  domain.SourceTrial,
			Origin:  domain.OriginDiscovered,
		}); err != nil {
			return added, backend, fmt.Errorf("cannot add proposed blog %s: %w", p.FeedURL, err)
		}
		added++
	}
	return added, backend, nil
}
