package service

import (
	"context"
	"fmt"
	"time"

	"github.com/rs/zerolog/log"

	"github.com/sadensmol/blogger/internal/domain"
	"github.com/sadensmol/blogger/internal/feed"
	"github.com/sadensmol/blogger/internal/repository"
)

const (
	candidateWindow   = 14 * 24 * time.Hour
	candidatesTotal   = 60
	candidatesPerBlog  = 5
)

type Articles struct {
	sources *Source
	repo    *repository.Article
	feeds   *feed.Client
	runs    *Runs
	now     func() time.Time
}

type ArticlesData struct {
	Sources *Source
	Repo    *repository.Article
	Feeds   *feed.Client
	Runs    *Runs
	Now     func() time.Time
}

func NewArticles(d ArticlesData) *Articles {
	return &Articles{sources: d.Sources, repo: d.Repo, feeds: d.Feeds, runs: d.Runs, now: d.Now}
}

func (a *Articles) FetchAll(ctx context.Context) (int, error) {
	runID, err := a.runs.Start(ctx, domain.RunKindFetch)
	if err != nil {
		return 0, err
	}
	created, fetchErr := a.fetchSources(ctx)
	if err := a.runs.Finish(ctx, FinishData{ID: runID, Kind: domain.RunKindFetch, Err: fetchErr}); err != nil {
		log.Ctx(ctx).Error().Err(err).Msgf("cannot record the end of fetch run %d", runID)
	}
	return created, fetchErr
}

func (a *Articles) FetchIfOlderThan(ctx context.Context, age time.Duration) error {
	last, err := a.runs.LastStarted(ctx, domain.RunKindFetch)
	if err != nil {
		return err
	}
	if !last.IsZero() && a.now().Sub(last) < age {
		return nil
	}
	if _, err := a.FetchAll(ctx); err != nil {
		return err
	}
	return nil
}

func (a *Articles) Candidates(ctx context.Context) ([]domain.Article, error) {
	articles, err := a.repo.ListNotPostedOfActiveOrTrialPublishedAfter(ctx, a.now().Add(-candidateWindow))
	if err != nil {
		return nil, fmt.Errorf("cannot list candidates: %w", err)
	}
	return domain.CutCandidates(articles, candidatesTotal, candidatesPerBlog), nil
}

func (a *Articles) RecentTitles(ctx context.Context, id domain.SourceID, limit int) ([]string, error) {
	titles, err := a.repo.ListTitlesBySource(ctx, id, limit)
	if err != nil {
		return nil, fmt.Errorf("cannot list recent titles: %w", err)
	}
	return titles, nil
}

func (a *Articles) fetchSources(ctx context.Context) (int, error) {
	sources, err := a.sources.ListActiveOrTrial(ctx)
	if err != nil {
		return 0, err
	}
	total := 0
	for _, src := range sources {
		created, err := a.fetchSource(ctx, src)
		if err != nil {
			log.Ctx(ctx).Warn().Err(err).Msgf("skipping blog %d (%s)", src.ID, src.FeedURL)
			continue
		}
		total += created
	}
	log.Ctx(ctx).Info().Msgf("fetched %d new articles from %d blogs", total, len(sources))
	return total, nil
}

func (a *Articles) fetchSource(ctx context.Context, src domain.Source) (int, error) {
	items, err := a.feeds.Fetch(ctx, src.FeedURL)
	if err != nil {
		return 0, fmt.Errorf("cannot read feed %s: %w", src.FeedURL, err)
	}
	now := a.now()
	created := 0
	for _, item := range items {
		article := domain.Article{
			SourceID:    src.ID,
			URL:         item.URL,
			Title:       item.Title,
			Excerpt:     item.Excerpt,
			PublishedAt: item.PublishedAt,
		}
		if article.PublishedAt.IsZero() {
			article.PublishedAt = now
		}
		isNew, err := a.repo.CreateIgnoringDuplicate(ctx, article, now)
		if err != nil {
			return created, err
		}
		if isNew {
			created++
		}
	}
	return created, nil
}
