package service

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/rs/zerolog/log"

	"github.com/sadensmol/blogger/internal/domain"
	"github.com/sadensmol/blogger/internal/feed"
	"github.com/sadensmol/blogger/internal/repository"
)

// a blog without a feed must list at least this many articles on its page
const minPageArticles = 3

type Source struct {
	repo  *repository.Source
	feeds *feed.Client
	now   func() time.Time
}

func NewSource(repo *repository.Source, feeds *feed.Client, now func() time.Time) *Source {
	return &Source{repo: repo, feeds: feeds, now: now}
}

// Add takes rawURL as the blog page. A blog without a feed is added when its page lists at least three articles.
// When rawURL is a feed, the feed's site link is the blog page.
func (s *Source) Add(ctx context.Context, rawURL string) (domain.Source, error) {
	pageURL, err := domain.ParseURL(rawURL)
	if err != nil {
		return domain.Source{}, fmt.Errorf("cannot add blog: %w", err)
	}
	info, err := s.feeds.Resolve(ctx, pageURL)
	switch {
	case err == nil:
		if info.FeedURL == pageURL {
			pageURL = info.SiteURL
		}
		return s.Create(ctx, domain.Source{
			SiteURL: pageURL,
			FeedURL: info.FeedURL,
			Title:   info.Title,
			Status:  domain.SourceActive,
			Origin:  domain.OriginUser,
		})
	case !errors.Is(err, domain.ErrNotFound):
		return domain.Source{}, fmt.Errorf("cannot add blog %s: %w", pageURL, err)
	}

	scan, err := s.feeds.ScanPage(ctx, pageURL, true)
	if err != nil {
		return domain.Source{}, fmt.Errorf("cannot add blog %s: %w", pageURL, err)
	}
	if len(scan.Articles) < minPageArticles {
		return domain.Source{}, fmt.Errorf("cannot add blog %s: no feed, and its page lists %d articles: %w", pageURL, len(scan.Articles), domain.ErrNotFound)
	}
	return s.Create(ctx, domain.Source{
		SiteURL: pageURL,
		Title:   cmp.Or(scan.Title, string(pageURL)),
		Status:  domain.SourceActive,
		Origin:  domain.OriginUser,
	})
}

// ReadableFeed returns the feed to keep for a blog: feedURL when it has items, else none when the blog page
// lists at least three articles. It is ErrNotFound when the blog cannot be read either way.
func (s *Source) ReadableFeed(ctx context.Context, pageURL, feedURL domain.URL) (domain.URL, error) {
	if feedURL != "" {
		items, err := s.feeds.Fetch(ctx, feedURL)
		if err == nil && len(items) > 0 {
			return feedURL, nil
		}
		log.Ctx(ctx).Info().Err(err).Msgf("blog %s: feed %s gives no items, its page is read instead", pageURL, feedURL)
	}
	scan, err := s.feeds.ScanPage(ctx, pageURL, true)
	if err != nil {
		return "", fmt.Errorf("cannot read blog %s: %w", pageURL, err)
	}
	if len(scan.Articles) < minPageArticles {
		return "", fmt.Errorf("blog %s has no working feed, and its page lists %d articles: %w", pageURL, len(scan.Articles), domain.ErrNotFound)
	}
	return "", nil
}

func (s *Source) Create(ctx context.Context, src domain.Source) (domain.Source, error) {
	created, err := s.repo.Create(ctx, src, s.now())
	if err != nil {
		return domain.Source{}, fmt.Errorf("cannot create blog: %w", err)
	}
	return created, nil
}

func (s *Source) Remove(ctx context.Context, ref string) (domain.Source, error) {
	src, err := s.find(ctx, ref)
	if err != nil {
		return domain.Source{}, err
	}
	if err := s.repo.Delete(ctx, src.ID, s.now()); err != nil {
		return domain.Source{}, fmt.Errorf("cannot remove blog %d: %w", src.ID, err)
	}
	return src, nil
}

func (s *Source) Pause(ctx context.Context, ref string) (domain.Source, error) {
	src, err := s.find(ctx, ref)
	if err != nil {
		return domain.Source{}, err
	}
	return s.changeStatus(ctx, src, domain.SourcePaused)
}

func (s *Source) Resume(ctx context.Context, ref string) (domain.Source, error) {
	src, err := s.find(ctx, ref)
	if err != nil {
		return domain.Source{}, err
	}
	return s.changeStatus(ctx, src, domain.SourceActive)
}

func (s *Source) List(ctx context.Context) ([]domain.Source, error) {
	sources, err := s.repo.ListNotDeleted(ctx)
	if err != nil {
		return nil, fmt.Errorf("cannot list blogs: %w", err)
	}
	return sources, nil
}

func (s *Source) ListActiveOrTrial(ctx context.Context) ([]domain.Source, error) {
	sources, err := s.repo.ListActiveOrTrial(ctx)
	if err != nil {
		return nil, fmt.Errorf("cannot list active blogs: %w", err)
	}
	return sources, nil
}

func (s *Source) MarkFetched(ctx context.Context, id domain.SourceID) error {
	if err := s.repo.UpdateFetchedAt(ctx, id, s.now()); err != nil {
		return fmt.Errorf("cannot mark blog %d fetched: %w", id, err)
	}
	return nil
}

func (s *Source) MarkFetchFailed(ctx context.Context, id domain.SourceID) error {
	if err := s.repo.UpdateFetchFailedAt(ctx, id, s.now()); err != nil {
		return fmt.Errorf("cannot mark the fetch of blog %d failed: %w", id, err)
	}
	return nil
}

func (s *Source) CountBackfillTry(ctx context.Context, id domain.SourceID) error {
	if err := s.repo.IncBackfillTries(ctx, id, s.now()); err != nil {
		return fmt.Errorf("cannot count a backfill try of blog %d: %w", id, err)
	}
	return nil
}

func (s *Source) MarkBackfilled(ctx context.Context, id domain.SourceID) error {
	if err := s.repo.UpdateBackfilledAt(ctx, id, s.now()); err != nil {
		return fmt.Errorf("cannot mark blog %d backfilled: %w", id, err)
	}
	return nil
}

// Known reports a blog with this page or this feed, removed ones too.
func (s *Source) Known(ctx context.Context, pageURL, feedURL domain.URL) (bool, error) {
	known, err := s.repo.ExistsBySiteURLOrFeedURL(ctx, pageURL, feedURL)
	if err != nil {
		return false, fmt.Errorf("cannot check blog %s: %w", pageURL, err)
	}
	return known, nil
}

// Diff matches an entry to a blog by its page, else by its feed: an entry can change the page of a blog.
// An entry without a feed gets the feed found at its page when it matches no blog by page;
// a blog without a feed is fine. An entry with only a feed keeps the page of its blog, or takes the feed url as its page.
func (s *Source) Diff(ctx context.Context, wanted []domain.SourceSpec) (domain.SourcesDiff, error) {
	current, err := s.List(ctx)
	if err != nil {
		return domain.SourcesDiff{}, err
	}
	byPage := make(map[domain.URL]domain.Source, len(current))
	byFeed := make(map[domain.URL]domain.Source, len(current))
	for _, src := range current {
		byPage[src.SiteURL] = src
		if src.HasFeed() {
			byFeed[src.FeedURL] = src
		}
	}

	var diff domain.SourcesDiff
	kept := make(map[domain.SourceID]bool, len(wanted))
	added := make(map[domain.URL]bool, len(wanted))
	addedFeeds := make(map[domain.URL]bool, len(wanted))
	for _, spec := range wanted {
		var before domain.Source
		exists := false
		if spec.URL == "" {
			before, exists = byFeed[spec.FeedURL]
			spec.URL = cmp.Or(before.SiteURL, spec.FeedURL)
		} else {
			before, exists = byPage[spec.URL]
		}
		fallbackTitle := string(cmp.Or(spec.FeedURL, spec.URL))
		if !exists && spec.FeedURL == "" {
			info, err := s.feeds.Resolve(ctx, spec.URL)
			switch {
			case err == nil:
				spec.FeedURL = info.FeedURL
				fallbackTitle = info.Title
			case !errors.Is(err, domain.ErrNotFound):
				return domain.SourcesDiff{}, fmt.Errorf("cannot look for the feed of %s: %w", spec.URL, err)
			}
		}
		if !exists && spec.FeedURL != "" {
			before, exists = byFeed[spec.FeedURL]
		}

		if !exists {
			if added[spec.URL] {
				return domain.SourcesDiff{}, fmt.Errorf("cannot import: blog %s is listed more than once", spec.URL)
			}
			if spec.FeedURL != "" && addedFeeds[spec.FeedURL] {
				return domain.SourcesDiff{}, fmt.Errorf("cannot import: feed %s is listed more than once", spec.FeedURL)
			}
			added[spec.URL] = true
			addedFeeds[spec.FeedURL] = true
			diff.Add = append(diff.Add, newSourceFromSpec(spec, fallbackTitle))
			continue
		}
		if kept[before.ID] {
			return domain.SourcesDiff{}, fmt.Errorf("cannot import: blog %s is listed more than once", before.SiteURL)
		}
		kept[before.ID] = true

		after := before
		if spec.Status != "" && spec.Status != before.Status {
			after.Status = spec.Status
			after.StatusChangedAt = s.now()
		}
		if spec.Title != "" {
			after.Title = spec.Title
		}
		after.SiteURL = spec.URL
		if spec.FeedURL != "" {
			after.FeedURL = spec.FeedURL
		}
		if after != before {
			diff.Change = append(diff.Change, domain.SourceChange{Before: before, After: after})
		}
	}
	for _, src := range current {
		if !kept[src.ID] {
			diff.Remove = append(diff.Remove, src)
		}
	}
	return diff, nil
}

func (s *Source) Apply(ctx context.Context, d domain.SourcesDiff) error {
	now := s.now()
	for _, src := range d.Remove {
		if err := s.repo.Delete(ctx, src.ID, now); err != nil {
			return fmt.Errorf("cannot apply removal of blog %d: %w", src.ID, err)
		}
	}
	for _, change := range d.Change {
		if err := s.repo.Update(ctx, change.After, now); err != nil {
			return fmt.Errorf("cannot apply change of blog %d: %w", change.After.ID, err)
		}
	}
	for _, src := range d.Add {
		if _, err := s.repo.Create(ctx, src, now); err != nil {
			return fmt.Errorf("cannot apply addition of blog %s: %w", src.SiteURL, err)
		}
	}
	return nil
}

func (s *Source) find(ctx context.Context, ref string) (domain.Source, error) {
	if id, err := strconv.ParseInt(ref, 10, 64); err == nil {
		src, err := s.repo.FindNotDeletedByID(ctx, domain.SourceID(id))
		if err != nil {
			return domain.Source{}, fmt.Errorf("cannot find blog %q: %w", ref, err)
		}
		return src, nil
	}
	refURL, err := domain.ParseURL(ref)
	if err != nil {
		return domain.Source{}, fmt.Errorf("cannot find blog %q: not an id or a url: %w", ref, domain.ErrNotFound)
	}
	src, err := s.repo.FindNotDeletedByURL(ctx, refURL)
	if err != nil {
		return domain.Source{}, fmt.Errorf("cannot find blog %q: %w", ref, err)
	}
	return src, nil
}

func (s *Source) changeStatus(ctx context.Context, src domain.Source, status domain.SourceStatus) (domain.Source, error) {
	now := s.now()
	src.Status = status
	src.StatusChangedAt = now
	if err := s.repo.Update(ctx, src, now); err != nil {
		return domain.Source{}, fmt.Errorf("cannot change status of blog %d: %w", src.ID, err)
	}
	updated, err := s.repo.FindNotDeletedByID(ctx, src.ID)
	if err != nil {
		return domain.Source{}, fmt.Errorf("cannot reload blog %d: %w", src.ID, err)
	}
	return updated, nil
}

func newSourceFromSpec(spec domain.SourceSpec, fallbackTitle string) domain.Source {
	return domain.Source{
		SiteURL: spec.URL,
		FeedURL: spec.FeedURL,
		Title:   cmp.Or(spec.Title, fallbackTitle),
		Status:  cmp.Or(spec.Status, domain.SourceActive),
		Origin:  domain.OriginUser,
	}
}
