package service

import (
	"context"
	"fmt"
	"strconv"
	"time"

	"github.com/sadensmol/blogger/internal/domain"
	"github.com/sadensmol/blogger/internal/feed"
	"github.com/sadensmol/blogger/internal/repository"
)

type Source struct {
	repo  *repository.Source
	feeds *feed.Client
	now   func() time.Time
}

func NewSource(repo *repository.Source, feeds *feed.Client, now func() time.Time) *Source {
	return &Source{repo: repo, feeds: feeds, now: now}
}

func (s *Source) Add(ctx context.Context, rawURL string) (domain.Source, error) {
	pageURL, err := domain.ParseURL(rawURL)
	if err != nil {
		return domain.Source{}, fmt.Errorf("cannot add blog: %w", err)
	}
	info, err := s.feeds.Resolve(ctx, pageURL)
	if err != nil {
		return domain.Source{}, fmt.Errorf("cannot add blog %s: %w", pageURL, err)
	}
	return s.Create(ctx, domain.Source{
		SiteURL: info.SiteURL,
		FeedURL: info.FeedURL,
		Title:   info.Title,
		Status:  domain.SourceActive,
		Origin:  domain.OriginUser,
	})
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

func (s *Source) Known(ctx context.Context, feedURL domain.URL) (bool, error) {
	known, err := s.repo.ExistsByFeedURL(ctx, feedURL)
	if err != nil {
		return false, fmt.Errorf("cannot check blog %s: %w", feedURL, err)
	}
	return known, nil
}

func (s *Source) ApplyStatusRule(ctx context.Context, id domain.SourceID) (domain.Source, error) {
	src, err := s.repo.FindNotDeletedByID(ctx, id)
	if err != nil {
		return domain.Source{}, fmt.Errorf("cannot apply status rule: %w", err)
	}
	status := src.StatusAfterVotes()
	if status == src.Status {
		return src, nil
	}
	return s.changeStatus(ctx, src, status)
}

func (s *Source) Diff(ctx context.Context, wanted []domain.SourceSpec) (domain.SourcesDiff, error) {
	current, err := s.List(ctx)
	if err != nil {
		return domain.SourcesDiff{}, err
	}
	currentByFeed := make(map[domain.URL]domain.Source, len(current))
	for _, src := range current {
		currentByFeed[src.FeedURL] = src
	}

	var diff domain.SourcesDiff
	kept := make(map[domain.URL]bool, len(wanted))
	for _, spec := range wanted {
		fallbackTitle := string(spec.FeedURL)
		if spec.FeedURL == "" {
			info, err := s.feeds.Resolve(ctx, spec.URL)
			if err != nil {
				return domain.SourcesDiff{}, fmt.Errorf("cannot resolve feed of %s: %w", spec.URL, err)
			}
			spec.FeedURL = info.FeedURL
			fallbackTitle = info.Title
		}
		if kept[spec.FeedURL] {
			return domain.SourcesDiff{}, fmt.Errorf("cannot import: feed %s is listed more than once", spec.FeedURL)
		}
		kept[spec.FeedURL] = true

		before, exists := currentByFeed[spec.FeedURL]
		if !exists {
			diff.Add = append(diff.Add, newSourceFromSpec(spec, fallbackTitle))
			continue
		}
		after := before
		if spec.Status != "" && spec.Status != before.Status {
			after.Status = spec.Status
			after.StatusChangedAt = s.now()
		}
		if spec.Title != "" {
			after.Title = spec.Title
		}
		if spec.URL != "" {
			after.SiteURL = spec.URL
		}
		if after != before {
			diff.Change = append(diff.Change, domain.SourceChange{Before: before, After: after})
		}
	}
	for _, src := range current {
		if !kept[src.FeedURL] {
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
			return fmt.Errorf("cannot apply addition of blog %s: %w", src.FeedURL, err)
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
	src := domain.Source{
		SiteURL: spec.URL,
		FeedURL: spec.FeedURL,
		Title:   spec.Title,
		Status:  spec.Status,
		Origin:  domain.OriginUser,
	}
	if src.Status == "" {
		src.Status = domain.SourceActive
	}
	if src.SiteURL == "" {
		src.SiteURL = spec.FeedURL
	}
	if src.Title == "" {
		src.Title = fallbackTitle
	}
	return src
}
