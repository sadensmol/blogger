package usecase

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/rs/zerolog/log"

	"github.com/sadensmol/blogger/internal/domain"
	"github.com/sadensmol/blogger/internal/llm"
	"github.com/sadensmol/blogger/internal/service"
	"github.com/sadensmol/blogger/internal/slackpost"
)

const (
	fetchMaxAge       = time.Hour
	initTitlesPerBlog = 5
	updateVotes       = 50

	noBlogsText    = "No blogs yet. Add one with /blogs add <url>."
	nothingText    = "Nothing worth reading today."
	pickFailedText = "Digest failed: no agent could pick articles. See the logs."
	morningDateFmt = "Mon, 2 Jan"
)

type DigestData struct {
	Articles *service.Articles
	Sources  *service.Source
	Votes    *service.Votes
	Profile  *service.Profile
	Posts    *service.Posts
	Runs     *service.Runs
	LLM      *llm.Runner
	Poster   *slackpost.Poster
	Size     int
	Location *time.Location
	Now      func() time.Time
}

type Digest struct {
	articles *service.Articles
	sources  *service.Source
	votes    *service.Votes
	profile  *service.Profile
	posts    *service.Posts
	runs     *service.Runs
	llm      *llm.Runner
	poster   *slackpost.Poster
	size     int
	location *time.Location
	now      func() time.Time
}

func NewDigest(d DigestData) *Digest {
	return &Digest{
		articles: d.Articles,
		sources:  d.Sources,
		votes:    d.Votes,
		profile:  d.Profile,
		posts:    d.Posts,
		runs:     d.Runs,
		llm:      d.LLM,
		poster:   d.Poster,
		size:     d.Size,
		location: d.Location,
		now:      d.Now,
	}
}

// Run takes kind digest-morning or digest-show.
func (d *Digest) Run(ctx context.Context, kind domain.RunKind) error {
	if err := d.withRun(ctx, kind, func(runID domain.RunID) (llm.Backend, error) {
		return d.digest(ctx, kind, runID)
	}); err != nil {
		return fmt.Errorf("cannot post %s: %w", kind, err)
	}
	return nil
}

func (d *Digest) digest(ctx context.Context, kind domain.RunKind, runID domain.RunID) (llm.Backend, error) {
	sources, err := d.sources.ListActiveOrTrial(ctx)
	if err != nil {
		return "", err
	}
	if len(sources) == 0 {
		if kind == domain.RunKindDigestMorning {
			log.Ctx(ctx).Info().Msgf("no blogs: morning digest skipped")
			return "", nil
		}
		return "", d.poster.Text(ctx, noBlogsText)
	}

	if err := d.articles.FetchIfOlderThan(ctx, fetchMaxAge); err != nil {
		log.Ctx(ctx).Warn().Err(err).Msgf("digest run %d goes on with the stored articles", runID)
	}

	profile, err := d.learnedProfile(ctx, sources)
	if err != nil {
		return "", err
	}

	candidates, err := d.articles.Candidates(ctx)
	if err != nil {
		return "", err
	}
	if len(candidates) == 0 {
		return "", d.poster.Text(ctx, nothingText)
	}

	picks, backend, err := d.llm.PickArticles(ctx, llm.PickInput{Candidates: candidates, Profile: profile, Max: d.size})
	if errors.Is(err, llm.ErrAllBackendsFailed) {
		if postErr := d.poster.Text(ctx, pickFailedText); postErr != nil {
			log.Ctx(ctx).Error().Err(postErr).Msgf("cannot tell slack that digest run %d failed", runID)
		}
	}
	if err != nil {
		return "", fmt.Errorf("cannot pick articles: %w", err)
	}
	if len(picks) == 0 {
		return backend, d.poster.Text(ctx, nothingText)
	}

	if err := d.poster.Text(ctx, d.header(kind, len(picks))); err != nil {
		return backend, err
	}
	articles := make(map[domain.URL]domain.Article, len(candidates))
	for _, a := range candidates {
		articles[a.URL] = a
	}
	for _, pick := range picks {
		if err := d.postPick(ctx, runID, articles[pick.URL], pick); err != nil {
			return backend, err
		}
	}
	return backend, nil
}

func (d *Digest) postPick(ctx context.Context, runID domain.RunID, a domain.Article, pick domain.Pick) error {
	ts, channelID, err := d.poster.Pick(ctx, a, pick)
	// a ts with an error means the message is in slack and only a reaction failed
	if err != nil && ts == "" {
		return err
	}
	if err != nil {
		log.Ctx(ctx).Warn().Err(err).Msgf("article %d posted as %s %s without its reactions", a.ID, channelID, ts)
	}
	postID, err := d.posts.Create(ctx, domain.Post{
		ArticleID: a.ID,
		RunID:     runID,
		ChannelID: channelID,
		MessageTS: ts,
		Pick:      pick,
	})
	if err != nil {
		return err
	}
	log.Ctx(ctx).Info().Msgf("run %d posted article %d as post %d (%s %s)", runID, a.ID, postID, channelID, ts)
	return nil
}

func (d *Digest) header(kind domain.RunKind, count int) string {
	noun := "picks"
	if count == 1 {
		noun = "pick"
	}
	if kind == domain.RunKindDigestMorning {
		return fmt.Sprintf("Your reading for %s: %d %s", d.now().In(d.location).Format(morningDateFmt), count, noun)
	}
	return fmt.Sprintf("On request: %d %s", count, noun)
}

// learnedProfile keeps the old interests file when the agent cannot make or update it.
func (d *Digest) learnedProfile(ctx context.Context, sources []domain.Source) (domain.Profile, error) {
	current, err := d.profile.Current(ctx)
	if err != nil && !errors.Is(err, domain.ErrNotFound) {
		return "", err
	}
	if current.LearnedEmpty() {
		return d.initProfile(ctx, current, sources), nil
	}

	lastLearnedAt, err := d.profile.LastLearnedAt(ctx)
	if err != nil {
		return "", err
	}
	changed, err := d.votes.ChangedSince(ctx, lastLearnedAt)
	if err != nil {
		return "", err
	}
	if !changed {
		return current, nil
	}
	return d.updateProfile(ctx, current), nil
}

func (d *Digest) initProfile(ctx context.Context, current domain.Profile, sources []domain.Source) domain.Profile {
	learned := current
	if err := d.withRun(ctx, domain.RunKindProfileInit, func(domain.RunID) (llm.Backend, error) {
		summaries := make([]llm.SourceSummary, 0, len(sources))
		for _, src := range sources {
			titles, err := d.articles.RecentTitles(ctx, src.ID, initTitlesPerBlog)
			if err != nil {
				return "", err
			}
			summaries = append(summaries, llm.SourceSummary{Source: src, RecentTitles: titles})
		}
		profile, backend, err := d.llm.InitProfile(ctx, current, summaries)
		if err != nil {
			return "", fmt.Errorf("cannot make the interests file: %w", err)
		}
		if err := d.profile.Save(ctx, profile, domain.ProfileInit); err != nil {
			return backend, err
		}
		learned = profile
		return backend, nil
	}); err != nil {
		log.Ctx(ctx).Warn().Err(err).Msgf("digest goes on without a learned interests file")
	}
	return learned
}

func (d *Digest) updateProfile(ctx context.Context, current domain.Profile) domain.Profile {
	learned := current
	if err := d.withRun(ctx, domain.RunKindProfileUpdate, func(domain.RunID) (llm.Backend, error) {
		votes, err := d.votes.Recent(ctx, updateVotes)
		if err != nil {
			return "", err
		}
		profile, backend, err := d.llm.UpdateProfile(ctx, current, votes)
		if err != nil {
			return "", fmt.Errorf("cannot update the interests file: %w", err)
		}
		if err := d.profile.Save(ctx, profile, domain.ProfileUpdate); err != nil {
			return backend, err
		}
		learned = profile
		return backend, nil
	}); err != nil {
		log.Ctx(ctx).Warn().Err(err).Msgf("digest goes on with the old interests file")
	}
	return learned
}

// withRun records work as one run of kind; the run fails when work returns an error.
func (d *Digest) withRun(ctx context.Context, kind domain.RunKind, work func(domain.RunID) (llm.Backend, error)) error {
	runID, err := d.runs.Start(ctx, kind)
	if err != nil {
		return err
	}
	backend, workErr := work(runID)
	if err := d.runs.Finish(ctx, service.FinishData{ID: runID, Kind: kind, Backend: string(backend), Err: workErr}); err != nil {
		log.Ctx(ctx).Error().Err(err).Msgf("cannot record the end of %s run %d", kind, runID)
	}
	return workErr
}
