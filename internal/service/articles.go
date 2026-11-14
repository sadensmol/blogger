package service

import (
	"context"
	"fmt"
	"slices"
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
	newestPerBlog     = 3
	olderPerBlog      = 2
	backfillMax       = 200
	backfillFeedPages = 50
)

type Articles struct {
	sources *Source
	repo    *repository.Article
	feeds   *feed.Client
	runs    *Runs
	history time.Duration
	now     func() time.Time
}

type ArticlesData struct {
	Sources *Source
	Repo    *repository.Article
	Feeds   *feed.Client
	Runs    *Runs
	History time.Duration // how far back an article is still worth reading
	Now     func() time.Time
}

func NewArticles(d ArticlesData) *Articles {
	return &Articles{sources: d.Sources, repo: d.Repo, feeds: d.Feeds, runs: d.Runs, history: d.History, now: d.Now}
}

// FetchAll reads every feed, then collects the older articles of each blog that has none collected yet.
func (a *Articles) FetchAll(ctx context.Context) (int, error) {
	runID, err := a.runs.Start(ctx, domain.RunKindFetch)
	if err != nil {
		return 0, err
	}
	created, fetchErr := a.fetchAndBackfill(ctx)
	if err := a.runs.Finish(ctx, FinishData{ID: runID, Kind: domain.RunKindFetch, Err: fetchErr}); err != nil {
		log.Ctx(ctx).Error().Err(err).Msgf("cannot record the end of fetch run %d", runID)
	}
	return created, fetchErr
}

// FetchFeedsIfOlderThan reads every feed when the last finished fetch is older than age, else only the feeds never read.
// It never collects older articles: a digest must not wait for that.
func (a *Articles) FetchFeedsIfOlderThan(ctx context.Context, age time.Duration) error {
	last, err := a.runs.LastFinishedStarted(ctx, domain.RunKindFetch)
	if err != nil {
		return err
	}
	sources, err := a.sources.ListActiveOrTrial(ctx)
	if err != nil {
		return err
	}
	if !last.IsZero() && a.now().Sub(last) < age {
		neverFetched := slices.DeleteFunc(sources, func(src domain.Source) bool { return !src.NeverFetched() })
		if len(neverFetched) > 0 {
			a.fetchFeeds(ctx, neverFetched)
		}
		return nil
	}

	runID, err := a.runs.Start(ctx, domain.RunKindFetch)
	if err != nil {
		return err
	}
	a.fetchFeeds(ctx, sources)
	if err := a.runs.Finish(ctx, FinishData{ID: runID, Kind: domain.RunKindFetch}); err != nil {
		log.Ctx(ctx).Error().Err(err).Msgf("cannot record the end of fetch run %d", runID)
	}
	return nil
}

func (a *Articles) Candidates(ctx context.Context) ([]domain.Article, error) {
	articles, err := a.repo.ListNotPostedOfActiveOrTrialPublishedAfter(ctx, a.now().Add(-min(candidateWindow, a.history)))
	if err != nil {
		return nil, fmt.Errorf("cannot list candidates: %w", err)
	}
	return domain.CutCandidates(articles, candidatesTotal, candidatesPerBlog), nil
}

// CandidatesOverHistory takes the not posted articles of the whole history, spread over time so older ones come up too.
// Undated articles come last.
func (a *Articles) CandidatesOverHistory(ctx context.Context) ([]domain.Article, error) {
	articles, err := a.repo.ListNotPostedOfActiveOrTrialPublishedAfterOrUndated(ctx, a.now().Add(-a.history))
	if err != nil {
		return nil, fmt.Errorf("cannot list candidates over history: %w", err)
	}
	return domain.SpreadCandidates(articles, domain.SpreadLimits{
		Total:           candidatesTotal,
		NewestPerSource: newestPerBlog,
		OlderPerSource:  olderPerBlog,
	}), nil
}

func (a *Articles) RecentTitles(ctx context.Context, id domain.SourceID, limit int) ([]string, error) {
	titles, err := a.repo.ListTitlesBySource(ctx, id, limit)
	if err != nil {
		return nil, fmt.Errorf("cannot list recent titles: %w", err)
	}
	return titles, nil
}

func (a *Articles) fetchAndBackfill(ctx context.Context) (int, error) {
	sources, err := a.sources.ListActiveOrTrial(ctx)
	if err != nil {
		return 0, err
	}
	total := a.fetchFeeds(ctx, sources)
	for _, src := range sources {
		if !src.Backfilled() {
			total += a.backfill(ctx, src)
		}
	}
	return total, nil
}

func (a *Articles) fetchFeeds(ctx context.Context, sources []domain.Source) int {
	total := 0
	for _, src := range sources {
		created, err := a.fetchSource(ctx, src)
		if err != nil {
			log.Ctx(ctx).Warn().Err(err).Msgf("skipping blog %d (%s)", src.ID, src.FeedURL)
			continue
		}
		total += created
		if err := a.sources.MarkFetched(ctx, src.ID); err != nil {
			log.Ctx(ctx).Warn().Err(err).Msgf("blog %d counts as never fetched", src.ID)
		}
	}
	log.Ctx(ctx).Info().Msgf("fetched %d new articles from %d blogs", total, len(sources))
	return total
}

func (a *Articles) fetchSource(ctx context.Context, src domain.Source) (int, error) {
	items, err := a.feeds.Fetch(ctx, src.FeedURL)
	if err != nil {
		return 0, fmt.Errorf("cannot read feed %s: %w", src.FeedURL, err)
	}
	created := 0
	for _, item := range items {
		isNew, err := a.store(ctx, src.ID, item)
		if err != nil {
			return created, err
		}
		if isNew {
			created++
		}
	}
	return created, nil
}

// backfill collects articles older than the feed holds, once per blog; a partial result counts as done.
func (a *Articles) backfill(ctx context.Context, src domain.Source) int {
	after := a.now().Add(-a.history)
	fromFeed := a.backfillFeedPages(ctx, src, after)
	fromSitemaps := a.backfillSitemaps(ctx, src, after, backfillMax-fromFeed)
	if ctx.Err() != nil {
		return fromFeed + fromSitemaps
	}
	if err := a.sources.MarkBackfilled(ctx, src.ID); err != nil {
		log.Ctx(ctx).Warn().Err(err).Msgf("blog %d will be backfilled again", src.ID)
	}
	log.Ctx(ctx).Info().Msgf("blog %d backfilled: %d articles from older feed pages, %d from sitemaps", src.ID, fromFeed, fromSitemaps)
	return fromFeed + fromSitemaps
}

// backfillFeedPages stops at a page that adds nothing: a feed that ignores ?paged= repeats its first page.
func (a *Articles) backfillFeedPages(ctx context.Context, src domain.Source, after time.Time) int {
	created := 0
	for page := 2; page <= backfillFeedPages && created < backfillMax; page++ {
		items, err := a.feeds.FetchPaged(ctx, src.FeedURL, page)
		if err != nil {
			log.Ctx(ctx).Debug().Err(err).Msgf("blog %d has no feed page %d", src.ID, page)
			return created
		}
		added, reachedOld := 0, false
		for _, item := range items {
			if item.PublishedAt.IsZero() {
				continue
			}
			if !item.PublishedAt.After(after) {
				reachedOld = true
				continue
			}
			if created+added >= backfillMax {
				break
			}
			isNew, err := a.store(ctx, src.ID, item)
			if err != nil {
				log.Ctx(ctx).Warn().Err(err).Msgf("blog %d backfill stops at feed page %d", src.ID, page)
				return created + added
			}
			if isNew {
				added++
			}
		}
		created += added
		if added == 0 || reachedOld {
			return created
		}
	}
	return created
}

func (a *Articles) backfillSitemaps(ctx context.Context, src domain.Source, after time.Time, room int) int {
	if room <= 0 {
		return 0
	}
	entries, err := a.feeds.ReadSitemaps(ctx, src.SiteURL, after)
	if err != nil {
		log.Ctx(ctx).Warn().Err(err).Msgf("blog %d goes on with %d sitemap entries read before the error", src.ID, len(entries))
	}
	created := 0
	for _, entry := range entries {
		if created >= room {
			break
		}
		known, err := a.repo.ExistsByURL(ctx, entry.URL)
		if err != nil {
			log.Ctx(ctx).Warn().Err(err).Msgf("blog %d backfill stops at %s", src.ID, entry.URL)
			return created
		}
		if known {
			if err := a.repo.UpdateUndatedPublishedAt(ctx, entry.URL, entry.LastMod, a.now()); err != nil {
				log.Ctx(ctx).Warn().Err(err).Msgf("blog %d backfill stops at %s", src.ID, entry.URL)
				return created
			}
			continue
		}
		info, err := a.feeds.ReadArticle(ctx, entry.URL)
		if err != nil {
			log.Ctx(ctx).Warn().Err(err).Msgf("blog %d backfill stops at %s", src.ID, entry.URL)
			return created
		}
		if info.Title == "" {
			log.Ctx(ctx).Debug().Msgf("blog %d: %s has no title, skipped", src.ID, entry.URL)
			continue
		}
		publishedAt := info.PublishedAt
		if publishedAt.IsZero() {
			publishedAt = entry.LastMod
		}
		isNew, err := a.store(ctx, src.ID, feed.Item{URL: entry.URL, Title: info.Title, Excerpt: info.Description, PublishedAt: publishedAt})
		if err != nil {
			log.Ctx(ctx).Warn().Err(err).Msgf("blog %d backfill stops at %s", src.ID, entry.URL)
			return created
		}
		if isNew {
			created++
		}
	}
	return created
}

func (a *Articles) store(ctx context.Context, sourceID domain.SourceID, item feed.Item) (bool, error) {
	return a.repo.CreateIgnoringDuplicate(ctx, domain.Article{
		SourceID:    sourceID,
		URL:         item.URL,
		Title:       item.Title,
		Excerpt:     item.Excerpt,
		PublishedAt: item.PublishedAt,
	}, a.now())
}
