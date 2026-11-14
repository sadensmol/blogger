package usecase

import (
	"context"
	"errors"
	"fmt"

	"github.com/rs/zerolog/log"

	"github.com/sadensmol/blogger/internal/domain"
	"github.com/sadensmol/blogger/internal/feed"
	"github.com/sadensmol/blogger/internal/llm"
	"github.com/sadensmol/blogger/internal/service"
	"github.com/sadensmol/blogger/internal/slackpost"
)

const (
	suggestMax = 10

	noNewBlogsText    = "Found no new blogs with a working feed. Try again later."
	suggestAgentText  = "Could not look for blogs: no agent with web search answered. See the logs."
	suggestFailedText = "Could not look for blogs. See the logs."
)

type SuggestData struct {
	Sources   *service.Source
	Proposals *service.Proposals
	Feeds     *feed.Client
	Runs      *service.Runs
	LLM       *llm.Runner
	Poster    *slackpost.Poster
}

// Suggest answers /discover: it shows new blogs with add buttons and adds the one the owner clicks.
type Suggest struct {
	sources   *service.Source
	proposals *service.Proposals
	feeds     *feed.Client
	runs      *service.Runs
	llm       *llm.Runner
	poster    *slackpost.Poster
}

func NewSuggest(d SuggestData) *Suggest {
	return &Suggest{
		sources:   d.Sources,
		proposals: d.Proposals,
		feeds:     d.Feeds,
		runs:      d.Runs,
		llm:       d.LLM,
		poster:    d.Poster,
	}
}

// Run answers the slash command privately when it fails: a failure never reaches the channel.
func (s *Suggest) Run(ctx context.Context, replyURL string) error {
	err := withRun(ctx, s.runs, domain.RunKindSuggest, func(runID domain.RunID) (llm.Backend, error) {
		return s.suggest(ctx, runID)
	})
	if err == nil {
		return nil
	}
	if replyErr := s.poster.Reply(ctx, replyURL, slackpost.Escape(suggestFailure(err))); replyErr != nil {
		log.Ctx(ctx).Error().Err(replyErr).Msg("cannot tell the user why /discover failed")
	}
	return fmt.Errorf("cannot suggest blogs: %w", err)
}

// suggestFailure never quotes the error: it may hold a url or a path.
func suggestFailure(err error) string {
	switch code := slackpost.ErrorCode(err); {
	case code == "not_in_channel":
		return notInChannelText
	case code == "channel_not_found":
		return channelNotFoundText
	case code != "":
		return fmt.Sprintf(slackFailedFmt, code)
	case errors.Is(err, llm.ErrAllBackendsFailed):
		return suggestAgentText
	default:
		return suggestFailedText
	}
}

func (s *Suggest) suggest(ctx context.Context, runID domain.RunID) (llm.Backend, error) {
	sources, err := s.sources.List(ctx)
	if err != nil {
		return "", fmt.Errorf("cannot list blogs: %w", err)
	}
	if len(sources) == 0 {
		if err := s.poster.Text(ctx, noBlogsText); err != nil {
			return "", fmt.Errorf("cannot post the no-blogs hint: %w", err)
		}
		return "", nil
	}

	blogs, backend, err := s.llm.DiscoverBlogs(ctx, sources, suggestMax)
	if err != nil {
		return "", fmt.Errorf("cannot ask for blogs: %w", err)
	}

	proposals := make([]domain.Proposal, 0, suggestMax)
	seen := make(map[domain.URL]bool, len(blogs))
	for _, blog := range blogs {
		if len(proposals) == suggestMax {
			break
		}
		if seen[blog.FeedURL] {
			continue
		}
		seen[blog.FeedURL] = true
		known, err := s.sources.Known(ctx, blog.FeedURL)
		if err != nil {
			return backend, fmt.Errorf("cannot check proposed blog: %w", err)
		}
		if known {
			continue
		}
		items, err := s.feeds.Fetch(ctx, blog.FeedURL)
		if err != nil {
			log.Ctx(ctx).Warn().Err(err).Msgf("skip proposed blog %s: its feed does not load", blog.FeedURL)
			continue
		}
		if len(items) == 0 {
			log.Ctx(ctx).Warn().Msgf("skip proposed blog %s: its feed has no item", blog.FeedURL)
			continue
		}
		proposal := domain.Proposal{RunID: runID, Blog: blog, Status: domain.ProposalProposed}
		if proposal.ID, err = s.proposals.Create(ctx, proposal); err != nil {
			return backend, fmt.Errorf("cannot keep proposed blog %s: %w", blog.FeedURL, err)
		}
		proposals = append(proposals, proposal)
	}
	if len(proposals) == 0 {
		if err := s.poster.Text(ctx, noNewBlogsText); err != nil {
			return backend, fmt.Errorf("cannot post that no new blog was found: %w", err)
		}
		return backend, nil
	}

	ts, channelID, err := s.poster.Proposals(ctx, proposals)
	if err != nil {
		return backend, fmt.Errorf("cannot post proposed blogs: %w", err)
	}
	// the message is in slack: a cancelled job must still store it
	if err := s.proposals.SetMessage(context.WithoutCancel(ctx), runID, channelID, ts); err != nil {
		return backend, fmt.Errorf("cannot store message %s %s of proposed blogs: %w", channelID, ts, err)
	}
	log.Ctx(ctx).Info().Msgf("%s run %d proposed %d blogs (%s %s)", domain.RunKindSuggest, runID, len(proposals), channelID, ts)
	return backend, nil
}

// Add adds the blog of a proposal and marks its button; it returns false when the blog was already in the list.
func (s *Suggest) Add(ctx context.Context, id domain.ProposalID) (bool, error) {
	proposal, err := s.proposals.Find(ctx, id)
	if err != nil {
		return false, fmt.Errorf("cannot add proposed blog: %w", err)
	}

	added := false
	if proposal.Status == domain.ProposalProposed {
		src, err := s.sources.Create(ctx, domain.Source{
			SiteURL: proposal.Blog.SiteURL,
			FeedURL: proposal.Blog.FeedURL,
			Title:   proposal.Blog.Title,
			Status:  domain.SourceActive,
			Origin:  domain.OriginUser,
		})
		switch {
		case err == nil:
			added = true
			log.Ctx(ctx).Info().Msgf("proposal %d added blog %d %s", id, src.ID, src.FeedURL)
		case !errors.Is(err, domain.ErrAlreadyExists):
			return false, fmt.Errorf("cannot add the blog of proposal %d: %w", id, err)
		}
		if err := s.proposals.MarkAdded(ctx, id); err != nil {
			return false, fmt.Errorf("cannot finish proposal %d: %w", id, err)
		}
		proposal.Status = domain.ProposalAdded
	}

	if proposal.MessageTS == "" {
		log.Ctx(ctx).Warn().Msgf("proposal %d has no stored message: its button stays", id)
		return added, nil
	}
	shown, err := s.proposals.ListByMessage(ctx, proposal.ChannelID, proposal.MessageTS)
	if err != nil {
		log.Ctx(ctx).Error().Err(err).Msgf("proposal %d: cannot redraw message %s %s", id, proposal.ChannelID, proposal.MessageTS)
		return added, nil
	}
	if err := s.poster.UpdateProposals(ctx, proposal.ChannelID, proposal.MessageTS, shown); err != nil {
		log.Ctx(ctx).Error().Err(err).Msgf("proposal %d: the blog is in the list, its button stays", id)
	}
	return added, nil
}
