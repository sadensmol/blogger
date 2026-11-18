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
	noUnreadText   = "No unread articles in your blogs' feeds yet."
	pickFailedText = "Digest failed: no agent could pick articles. See the logs."
	morningDateFmt = "Mon, 2 Jan"

	notInChannelText    = "Could not post: the bot is not in this channel. Invite it with /invite @<bot name>."
	channelNotFoundText = "Could not post: the channel was not found. Check SLACK_CHANNEL_ID."
	slackFailedFmt      = "Could not post to Slack: %s."
	showFailedText      = "Could not make the digest. See the logs."
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
	OwnerID  string
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
	ownerID  string
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
		ownerID:  d.OwnerID,
	}
}

// Show runs digest-show and always answers the slash command privately with the tokens it used.
// A failure puts its reason above them: a failed post never reaches the channel.
func (d *Digest) Show(ctx context.Context, replyURL string) error {
	ctx, meter := llm.WithMeter(ctx)
	err := d.Run(ctx, domain.RunKindDigestShow)
	usage := meter.Usage()
	log.Ctx(ctx).Info().Msgf("%s run: %s", domain.RunKindDigestShow, slackpost.UsageText(usage))
	text := ""
	if err != nil {
		text = slackpost.Escape(showFailure(err))
	}
	if replyErr := d.poster.ReplyWithUsage(ctx, replyURL, text, usage); replyErr != nil {
		log.Ctx(ctx).Error().Err(replyErr).Msg("cannot answer /show")
	}
	return err
}

// showFailure never quotes the error: it may hold a url or a path.
func showFailure(err error) string {
	switch code := slackpost.ErrorCode(err); {
	case code == "not_in_channel":
		return notInChannelText
	case code == "channel_not_found":
		return channelNotFoundText
	case code != "":
		return fmt.Sprintf(slackFailedFmt, code)
	case errors.Is(err, llm.ErrAllBackendsFailed):
		return pickFailedText
	default:
		return showFailedText
	}
}

// Run takes kind digest-morning or digest-show.
func (d *Digest) Run(ctx context.Context, kind domain.RunKind) error {
	if err := withRun(ctx, d.runs, kind, func(runID domain.RunID) (llm.Backend, error) {
		return d.digest(ctx, kind, runID)
	}); err != nil {
		return fmt.Errorf("cannot post %s: %w", kind, err)
	}
	return nil
}

func (d *Digest) digest(ctx context.Context, kind domain.RunKind, runID domain.RunID) (llm.Backend, error) {
	sources, err := d.sources.ListActiveOrTrial(ctx)
	if err != nil {
		return "", fmt.Errorf("cannot list blogs: %w", err)
	}
	if len(sources) == 0 {
		if kind == domain.RunKindDigestMorning {
			log.Ctx(ctx).Info().Msgf("no blogs: morning digest skipped")
			return "", nil
		}
		if err := d.poster.Text(ctx, noBlogsText); err != nil {
			return "", fmt.Errorf("cannot post the no-blogs hint: %w", err)
		}
		return "", nil
	}

	if err := d.articles.FetchFeedsIfOlderThan(ctx, fetchMaxAge); err != nil {
		log.Ctx(ctx).Warn().Err(err).Msgf("digest run %d goes on with the stored articles", runID)
	}

	profile, err := d.learnedProfile(ctx, sources)
	if err != nil {
		return "", fmt.Errorf("cannot prepare the interests file: %w", err)
	}

	// /show asks for something to read now: any unread article will do; the morning digest keeps its quality bar
	readCandidates, emptyText, count := d.articles.Candidates, nothingText, domain.PickCount{Max: d.size}
	if kind == domain.RunKindDigestShow {
		readCandidates, emptyText, count = d.articles.CandidatesOverHistory, noUnreadText, domain.PickCount{Min: 1, Max: d.size}
	}
	candidates, err := readCandidates(ctx)
	if err != nil {
		return "", fmt.Errorf("cannot read candidates: %w", err)
	}
	if len(candidates) == 0 {
		if err := d.poster.Text(ctx, emptyText); err != nil {
			return "", fmt.Errorf("cannot post that no article is unread: %w", err)
		}
		return "", nil
	}

	tagged, err := d.articles.Tag(ctx, candidates)
	if err != nil {
		log.Ctx(ctx).Warn().Err(err).Msgf("digest run %d picks from candidates without new tags", runID)
	}
	// a failed tag job still returns the candidates with their stored tags
	if tagged != nil {
		candidates = tagged
	}
	themes, err := d.votes.Themes(ctx)
	if err != nil {
		return "", fmt.Errorf("cannot read the theme scores: %w", err)
	}
	candidates = themes.Rank(candidates)

	picks, backend, err := d.llm.PickArticles(ctx, llm.PickInput{Candidates: candidates, Profile: profile, Count: count, Themes: themes})
	// /show tells the user privately (see Show); the morning digest has nobody to answer but the channel
	if errors.Is(err, llm.ErrAllBackendsFailed) && kind == domain.RunKindDigestMorning {
		if postErr := d.poster.Text(ctx, pickFailedText); postErr != nil {
			log.Ctx(ctx).Error().Err(postErr).Msgf("cannot tell slack that digest run %d failed", runID)
		}
	}
	if err != nil {
		return "", fmt.Errorf("cannot pick articles: %w", err)
	}
	if len(picks) == 0 {
		if err := d.poster.Text(ctx, nothingText); err != nil {
			return backend, fmt.Errorf("cannot post that nothing fits: %w", err)
		}
		return backend, nil
	}

	if err := d.poster.Text(ctx, d.header(kind, len(picks))); err != nil {
		return backend, fmt.Errorf("cannot post the digest header: %w", err)
	}
	articles := make(map[domain.URL]domain.Article, len(candidates))
	for _, a := range candidates {
		articles[a.URL] = a
	}
	for i, pick := range picks {
		if err := d.postPick(ctx, runID, articles[pick.URL], pick); err != nil {
			return backend, fmt.Errorf("cannot post pick %d of %d: %w", i+1, len(picks), err)
		}
	}
	return backend, nil
}

// postPick stores the post before it posts: an article that may be in slack counts as posted, so it repeats only when due.
func (d *Digest) postPick(ctx context.Context, runID domain.RunID, a domain.Article, pick domain.Pick) error {
	postID, err := d.posts.Create(ctx, domain.Post{ArticleID: a.ID, RunID: runID, Pick: pick})
	if err != nil {
		return fmt.Errorf("cannot reserve the post of article %d: %w", a.ID, err)
	}

	ts, channelID, err := d.poster.Pick(ctx, a, pick)
	// a ts with an error means the message is in slack and only a reaction failed
	if err != nil && ts == "" {
		if !slackpost.NotPosted(err) {
			return fmt.Errorf("cannot post article %d, post %d keeps it out of digests until it is due again: %w", a.ID, postID, err)
		}
		if delErr := d.posts.Delete(context.WithoutCancel(ctx), postID); delErr != nil {
			log.Ctx(ctx).Error().Err(delErr).Msgf("article %d stays out of digests until it is due again: cannot remove post %d that slack rejected", a.ID, postID)
		}
		return fmt.Errorf("cannot post article %d: %w", a.ID, err)
	}
	if err != nil {
		log.Ctx(ctx).Warn().Err(err).Msgf("article %d posted as %s %s without its reactions", a.ID, channelID, ts)
	}
	// the message is in slack: a cancelled job must still store it
	if err := d.posts.SetMessage(context.WithoutCancel(ctx), postID, channelID, ts); err != nil {
		return fmt.Errorf("cannot store message %s %s of article %d: %w", channelID, ts, a.ID, err)
	}
	log.Ctx(ctx).Info().Msgf("run %d posted article %d as post %d (%s %s)", runID, a.ID, postID, channelID, ts)
	// an article the tag job missed takes its pick topics as tags, so votes on this post count for themes
	if err := d.articles.CreateTagsIfUntagged(context.WithoutCancel(ctx), a.ID, domain.ParseTags(pick.Topics)); err != nil {
		log.Ctx(ctx).Warn().Err(err).Msgf("votes on post %d count for no theme until article %d is tagged", postID, a.ID)
	}
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
		return "", fmt.Errorf("cannot read the interests file: %w", err)
	}
	if current.LearnedEmpty() {
		return d.initProfile(ctx, current, sources), nil
	}

	lastLearnedAt, err := d.profile.LastLearnedAt(ctx)
	if err != nil {
		return "", fmt.Errorf("cannot read when the interests file was learned: %w", err)
	}
	changed, err := d.votes.ChangedSince(ctx, lastLearnedAt)
	if err != nil {
		return "", fmt.Errorf("cannot check for new votes: %w", err)
	}
	if !changed {
		return current, nil
	}
	return d.updateProfile(ctx, current), nil
}

func (d *Digest) initProfile(ctx context.Context, current domain.Profile, sources []domain.Source) domain.Profile {
	learned := current
	if err := withRun(ctx, d.runs, domain.RunKindProfileInit, func(domain.RunID) (llm.Backend, error) {
		summaries := make([]llm.SourceSummary, 0, len(sources))
		for _, src := range sources {
			titles, err := d.articles.RecentTitles(ctx, src.ID, initTitlesPerBlog)
			if err != nil {
				return "", fmt.Errorf("cannot read recent titles of blog %d: %w", src.ID, err)
			}
			summaries = append(summaries, llm.SourceSummary{Source: src, RecentTitles: titles})
		}
		profile, backend, err := d.llm.InitProfile(ctx, current, summaries)
		if err != nil {
			return "", fmt.Errorf("cannot make the interests file: %w", err)
		}
		if err := d.profile.Save(ctx, profile, domain.ProfileInit); err != nil {
			return backend, fmt.Errorf("cannot save the new interests file: %w", err)
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
	if err := withRun(ctx, d.runs, domain.RunKindProfileUpdate, func(domain.RunID) (llm.Backend, error) {
		votes, err := d.votes.Recent(ctx, updateVotes)
		if err != nil {
			return "", fmt.Errorf("cannot read recent votes: %w", err)
		}
		themes, err := d.votes.Themes(ctx)
		if err != nil {
			return "", fmt.Errorf("cannot read the theme scores: %w", err)
		}
		profile, backend, err := d.llm.UpdateProfile(ctx, current, votes, themes)
		if err != nil {
			return "", fmt.Errorf("cannot update the interests file: %w", err)
		}
		if err := d.profile.Save(ctx, profile, domain.ProfileUpdate); err != nil {
			return backend, fmt.Errorf("cannot save the updated interests file: %w", err)
		}
		learned = profile
		return backend, nil
	}); err != nil {
		log.Ctx(ctx).Warn().Err(err).Msgf("digest goes on with the old interests file")
	}
	return learned
}
