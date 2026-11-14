package service

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"slices"
	"sync"
	"time"

	"github.com/rs/zerolog/log"

	"github.com/sadensmol/blogger/internal/domain"
	"github.com/sadensmol/blogger/internal/feed"
	"github.com/sadensmol/blogger/internal/llm"
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
	backfillFailures  = 5
	sitemapEntriesMax = 3 * backfillMax
	pageArticleReads  = 20
)

type Articles struct {
	sources  *Source
	repo     *repository.Article
	verdicts *repository.LinkVerdict
	feeds    *feed.Client
	llm      *llm.Runner
	runs     *Runs
	history time.Duration
	repeatAfter time.Duration
	repeatMax   int
	now     func() time.Time

	mu   sync.Mutex
	busy map[domain.SourceID]bool // blogs being fetched or backfilled now
}

type ArticlesData struct {
	Sources  *Source
	Repo     *repository.Article
	Verdicts *repository.LinkVerdict
	Feeds    *feed.Client
	LLM      *llm.Runner // asks which links of a blog page are articles when no rule tells
	Runs     *Runs
	History time.Duration // how far back an article is still worth reading
	// an article the owner never voted on comes back RepeatAfter its last post, until it was posted RepeatMax times
	RepeatAfter time.Duration
	RepeatMax   int
	Now         func() time.Time
}

func NewArticles(d ArticlesData) *Articles {
	return &Articles{
		sources:  d.Sources,
		repo:     d.Repo,
		verdicts: d.Verdicts,
		feeds:    d.Feeds,
		llm:      d.LLM,
		runs:     d.Runs,
		history: d.History,
		repeatAfter: d.RepeatAfter,
		repeatMax:   d.RepeatMax,
		now:     d.Now,
		busy:    map[domain.SourceID]bool{},
	}
}

// FetchAll reads every feed in a fetch run.
func (a *Articles) FetchAll(ctx context.Context) (int, error) {
	runID, err := a.runs.Start(ctx, domain.RunKindFetch)
	if err != nil {
		return 0, err
	}
	created, fetchErr := a.fetchAllFeeds(ctx)
	if err := a.runs.Finish(ctx, FinishData{ID: runID, Kind: domain.RunKindFetch, Err: fetchErr}); err != nil {
		log.Ctx(ctx).Error().Err(err).Msgf("cannot record the end of fetch run %d", runID)
	}
	return created, fetchErr
}

// FetchFeedsIfOlderThan reads every feed when the last finished fetch of either kind is older than age,
// else only the feeds never read. Its run is a fetch-feeds run, so the scheduled fetch keeps its interval.
func (a *Articles) FetchFeedsIfOlderThan(ctx context.Context, age time.Duration) error {
	lastFetch, err := a.runs.LastFinishedStarted(ctx, domain.RunKindFetch)
	if err != nil {
		return err
	}
	lastFeeds, err := a.runs.LastFinishedStarted(ctx, domain.RunKindFetchFeeds)
	if err != nil {
		return err
	}
	sources, err := a.sources.ListActiveOrTrial(ctx)
	if err != nil {
		return err
	}
	last := lastFetch
	if lastFeeds.After(last) {
		last = lastFeeds
	}
	if !last.IsZero() && a.now().Sub(last) < age {
		neverFetched := slices.DeleteFunc(sources, func(src domain.Source) bool { return !src.NeverFetched() })
		if len(neverFetched) > 0 {
			a.fetchFeeds(ctx, neverFetched)
		}
		return nil
	}

	runID, err := a.runs.Start(ctx, domain.RunKindFetchFeeds)
	if err != nil {
		return err
	}
	a.fetchFeeds(ctx, sources)
	if err := a.runs.Finish(ctx, FinishData{ID: runID, Kind: domain.RunKindFetchFeeds}); err != nil {
		log.Ctx(ctx).Error().Err(err).Msgf("cannot record the end of fetch-feeds run %d", runID)
	}
	return nil
}

// Backfill collects the older articles of each blog whose backfill is due.
func (a *Articles) Backfill(ctx context.Context) (int, error) {
	sources, err := a.sources.ListActiveOrTrial(ctx)
	if err != nil {
		return 0, err
	}
	created := 0
	for _, src := range sources {
		if ctx.Err() != nil {
			break
		}
		if src.BackfillDue(a.now()) {
			created += a.backfill(ctx, src)
		}
	}
	return created, nil
}

// Candidates takes the not posted articles and the unvoted ones due again of the last two weeks.
func (a *Articles) Candidates(ctx context.Context) ([]domain.Article, error) {
	articles, err := a.repo.ListNotVotedPostedFewerThanLastBeforeOfActiveOrTrialPublishedAfter(ctx,
		a.repeatMax, a.now().Add(-a.repeatAfter), a.now().Add(-min(candidateWindow, a.history)))
	if err != nil {
		return nil, fmt.Errorf("cannot list candidates: %w", err)
	}
	return domain.CutCandidates(articles, candidatesTotal, candidatesPerBlog), nil
}

// CandidatesOverHistory takes the not posted articles and the unvoted ones due again of the whole history,
// spread over time so older ones come up too. Undated articles come last.
func (a *Articles) CandidatesOverHistory(ctx context.Context) ([]domain.Article, error) {
	articles, err := a.repo.ListNotVotedPostedFewerThanLastBeforeOfActiveOrTrialPublishedAfterOrUndated(ctx,
		a.repeatMax, a.now().Add(-a.repeatAfter), a.now().Add(-a.history))
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

func (a *Articles) fetchAllFeeds(ctx context.Context) (int, error) {
	sources, err := a.sources.ListActiveOrTrial(ctx)
	if err != nil {
		return 0, err
	}
	return a.fetchFeeds(ctx, sources), nil
}

// claim keeps two fetches of one blog from running at once; the caller calls release when done.
func (a *Articles) claim(id domain.SourceID) (func(), bool) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.busy[id] {
		return nil, false
	}
	a.busy[id] = true
	return func() {
		a.mu.Lock()
		defer a.mu.Unlock()
		delete(a.busy, id)
	}, true
}

func (a *Articles) fetchFeeds(ctx context.Context, sources []domain.Source) int {
	total := 0
	for _, src := range sources {
		if src.FeedBackedOff(a.now()) {
			log.Ctx(ctx).Debug().Msgf("blog %d failed lately, its feed waits", src.ID)
			continue
		}
		release, ok := a.claim(src.ID)
		if !ok {
			log.Ctx(ctx).Debug().Msgf("blog %d is being fetched already", src.ID)
			continue
		}
		created, err := a.fetchSource(ctx, src)
		release()
		if err != nil {
			log.Ctx(ctx).Warn().Err(err).Msgf("skipping blog %d (%s)", src.ID, src.SiteURL)
			if err := a.sources.MarkFetchFailed(ctx, src.ID); err != nil {
				log.Ctx(ctx).Warn().Err(err).Msgf("blog %d will not wait before its next fetch", src.ID)
			}
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

// fetchSource reads the blog's feed and its page. A blog fails only when nothing of it can be read:
// its feed, or its page when it has no feed.
// The browser and the agent help only a blog without a feed: a feed that gives nothing now is a passing fault.
func (a *Articles) fetchSource(ctx context.Context, src domain.Source) (int, error) {
	origin, err := a.repo.FindLatestURLBySource(ctx, src.ID)
	if err != nil {
		if !errors.Is(err, domain.ErrNotFound) {
			log.Ctx(ctx).Warn().Err(err).Msgf("blog %d page articles use the page url for their urls", src.ID)
		}
		origin = src.SiteURL
	}

	created := 0
	var feedErr error
	if src.HasFeed() {
		items, err := a.feeds.Fetch(ctx, src.FeedURL)
		if err != nil {
			feedErr = fmt.Errorf("cannot read feed %s: %w", src.FeedURL, err)
		}
		for _, item := range items {
			isNew, err := a.store(ctx, src.ID, item)
			if err != nil {
				return created, err
			}
			if isNew {
				created++
			}
		}
	}

	fromPage, pageErr := a.fetchPage(ctx, src, origin, !src.HasFeed())
	switch {
	case pageErr != nil && !src.HasFeed():
		return created + fromPage, pageErr
	case pageErr != nil && feedErr != nil:
		log.Ctx(ctx).Info().Err(pageErr).Msgf("blog %d page cannot be read either", src.ID)
		return created + fromPage, feedErr
	case pageErr != nil:
		log.Ctx(ctx).Debug().Err(pageErr).Msgf("blog %d is read from its feed only", src.ID)
	case feedErr != nil:
		log.Ctx(ctx).Warn().Err(feedErr).Msgf("blog %d is read from its page only", src.ID)
	}
	return created + fromPage, nil
}

// fetchPage stores the new articles that the blog page lists. With fallbacks, a page that the rules find no article on
// is rendered in the browser, and its links are judged by the agent: only the links it never judged.
func (a *Articles) fetchPage(ctx context.Context, src domain.Source, origin domain.URL, fallbacks bool) (int, error) {
	scan, err := a.feeds.ScanPage(ctx, src.SiteURL, fallbacks)
	if err != nil {
		return 0, fmt.Errorf("cannot read blog page %s: %w", src.SiteURL, err)
	}
	links := scan.Articles
	if len(links) == 0 && fallbacks && len(scan.Unclear) > 0 {
		links, err = a.judgeLinks(ctx, src, origin, scan.Unclear)
		if err != nil {
			log.Ctx(ctx).Warn().Err(err).Msgf("blog %d goes on with the links judged before", src.ID)
		}
	}
	return a.storePageArticles(ctx, src.ID, origin, links), nil
}

// judgeLinks returns the links judged as articles, before or now. It asks the agent about the links it never judged
// and that are not stored articles, in one job.
func (a *Articles) judgeLinks(ctx context.Context, src domain.Source, origin domain.URL, links []domain.PageLink) ([]domain.PageLink, error) {
	verdicts, err := a.verdicts.ListBySource(ctx, src.ID)
	if err != nil {
		return nil, fmt.Errorf("cannot list the judged links of blog %d: %w", src.ID, err)
	}
	var articles, fresh []domain.PageLink
	for _, link := range links {
		article, judged := verdicts[link.URL]
		if judged {
			if article {
				articles = append(articles, link)
			}
			continue
		}
		known, err := a.repo.ExistsByURL(ctx, link.URL.WithOriginOf(origin))
		if err != nil {
			return articles, fmt.Errorf("cannot check link %s: %w", link.URL, err)
		}
		if !known {
			fresh = append(fresh, link)
		}
	}
	if len(fresh) == 0 {
		return articles, nil
	}

	answered, backend, err := a.llm.ClassifyLinks(ctx, src.SiteURL, fresh)
	if err != nil {
		return articles, fmt.Errorf("cannot judge %d links of blog %d: %w", len(fresh), src.ID, err)
	}
	if err := a.verdicts.CreateIgnoringDuplicates(ctx, src.ID, answered, a.now()); err != nil {
		log.Ctx(ctx).Warn().Err(err).Msgf("blog %d: its links are judged again next time", src.ID)
	}
	judged := len(articles)
	for _, link := range fresh {
		if answered[link.URL] {
			articles = append(articles, link)
		}
	}
	log.Ctx(ctx).Info().Msgf("blog %d: %s judged %d links, %d are articles", src.ID, backend, len(answered), len(articles)-judged)
	return articles, nil
}

// storePageArticles reads a few new articles per fetch: a long first page fills up over the next fetches.
// It stops after a few unreadable articles in a row: the site is likely down.
func (a *Articles) storePageArticles(ctx context.Context, sourceID domain.SourceID, origin domain.URL, links []domain.PageLink) int {
	created, reads, failures := 0, 0, 0
	for _, link := range links {
		if reads >= pageArticleReads || failures >= backfillFailures || ctx.Err() != nil {
			break
		}
		pageURL := link.URL.WithOriginOf(origin)
		known, err := a.repo.ExistsByURL(ctx, pageURL)
		if err != nil {
			log.Ctx(ctx).Warn().Err(err).Msgf("blog %d page articles stop at %s", sourceID, pageURL)
			return created
		}
		if known {
			continue
		}
		reads++
		info, err := a.feeds.ReadArticle(ctx, pageURL)
		if err != nil {
			failures++
			log.Ctx(ctx).Info().Err(err).Msgf("blog %d skips page article %s", sourceID, pageURL)
			continue
		}
		failures = 0
		title := cmp.Or(info.Title, link.Text)
		if title == "" {
			log.Ctx(ctx).Debug().Msgf("blog %d: %s has no title, skipped", sourceID, pageURL)
			continue
		}
		isNew, err := a.store(ctx, sourceID, feed.Item{URL: pageURL, Title: title, Excerpt: info.Description, PublishedAt: info.PublishedAt})
		if err != nil {
			log.Ctx(ctx).Warn().Err(err).Msgf("blog %d page articles stop at %s", sourceID, pageURL)
			return created
		}
		if isNew {
			created++
		}
	}
	return created
}

// backfill collects articles older than the feed holds. A backfill that collects nothing counts as a try.
// Each url takes the scheme and host of the blog's stored articles: http or www variants are one article.
func (a *Articles) backfill(ctx context.Context, src domain.Source) int {
	release, ok := a.claim(src.ID)
	if !ok {
		return 0
	}
	defer release()

	origin, err := a.repo.FindLatestURLBySource(ctx, src.ID)
	if err != nil {
		if !errors.Is(err, domain.ErrNotFound) {
			log.Ctx(ctx).Warn().Err(err).Msgf("blog %d backfill uses the site url for its urls", src.ID)
		}
		origin = src.SiteURL
	}
	after := a.now().Add(-a.history)
	fromFeed := 0
	if src.HasFeed() {
		fromFeed = a.backfillFeedPages(ctx, src, origin, after)
	}
	fromSitemaps := a.backfillSitemaps(ctx, src, origin, after, backfillMax-fromFeed)
	if ctx.Err() != nil {
		return fromFeed + fromSitemaps
	}
	if fromFeed+fromSitemaps == 0 {
		if err := a.sources.CountBackfillTry(ctx, src.ID); err != nil {
			log.Ctx(ctx).Warn().Err(err).Msgf("blog %d backfill is tried again at once", src.ID)
		}
		log.Ctx(ctx).Info().Msgf("blog %d backfill found nothing: try %d", src.ID, src.BackfillTries+1)
		return 0
	}
	if err := a.sources.MarkBackfilled(ctx, src.ID); err != nil {
		log.Ctx(ctx).Warn().Err(err).Msgf("blog %d will be backfilled again", src.ID)
	}
	log.Ctx(ctx).Info().Msgf("blog %d backfilled: %d articles from older feed pages, %d from sitemaps", src.ID, fromFeed, fromSitemaps)
	return fromFeed + fromSitemaps
}

// backfillFeedPages stops at a page that adds nothing: a feed that ignores ?paged= repeats its first page.
func (a *Articles) backfillFeedPages(ctx context.Context, src domain.Source, origin domain.URL, after time.Time) int {
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
			item.URL = item.URL.WithOriginOf(origin)
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

// backfillSitemaps skips a page it cannot read; it stops after a few failures in a row: the site is likely down.
func (a *Articles) backfillSitemaps(ctx context.Context, src domain.Source, origin domain.URL, after time.Time, room int) int {
	if room <= 0 {
		return 0
	}
	entries, err := a.feeds.ReadSitemaps(ctx, src.SiteURL, after, sitemapEntriesMax)
	if err != nil {
		log.Ctx(ctx).Warn().Err(err).Msgf("blog %d goes on with %d sitemap entries read before the error", src.ID, len(entries))
	}
	created, failures := 0, 0
	for _, entry := range entries {
		if created >= room || failures >= backfillFailures || ctx.Err() != nil {
			break
		}
		pageURL := entry.URL.WithOriginOf(origin)
		known, err := a.repo.ExistsByURL(ctx, pageURL)
		if err != nil {
			log.Ctx(ctx).Warn().Err(err).Msgf("blog %d backfill stops at %s", src.ID, pageURL)
			return created
		}
		if known {
			if err := a.repo.UpdateUndatedPublishedAt(ctx, pageURL, entry.LastMod, a.now()); err != nil {
				log.Ctx(ctx).Warn().Err(err).Msgf("blog %d backfill stops at %s", src.ID, pageURL)
				return created
			}
			continue
		}
		info, err := a.feeds.ReadArticle(ctx, pageURL)
		if err != nil {
			failures++
			log.Ctx(ctx).Info().Err(err).Msgf("blog %d backfill skips %s", src.ID, pageURL)
			continue
		}
		failures = 0
		if info.Title == "" {
			log.Ctx(ctx).Debug().Msgf("blog %d: %s has no title, skipped", src.ID, pageURL)
			continue
		}
		publishedAt := info.PublishedAt
		if publishedAt.IsZero() {
			publishedAt = entry.LastMod
		}
		isNew, err := a.store(ctx, src.ID, feed.Item{URL: pageURL, Title: info.Title, Excerpt: info.Description, PublishedAt: publishedAt})
		if err != nil {
			log.Ctx(ctx).Warn().Err(err).Msgf("blog %d backfill stops at %s", src.ID, pageURL)
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
