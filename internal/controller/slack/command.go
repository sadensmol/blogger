package controllerslack

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/rs/zerolog/log"
	"github.com/slack-go/slack"

	controllertext "github.com/sadensmol/blogger/internal/controller/text"
	"github.com/sadensmol/blogger/internal/domain"
	"github.com/sadensmol/blogger/internal/service"
	"github.com/sadensmol/blogger/internal/slackpost"
	"github.com/sadensmol/blogger/internal/usecase"
)

const (
	ownerOnlyText      = "Only the owner can use this bot."
	pickingText        = "Picking articles…"
	onItsWayText       = "A digest is already on its way."
	lookingText        = "Looking for blogs…"
	alreadyLookingText = "Already looking for blogs."
	busyText           = "Busy right now, try again in a few minutes."
	addTimeout         = time.Minute
)

type BlogsCommand struct{ Action, Arg string }

// ParseBlogsCommand takes the text after /blogs; Slack may wrap a URL as <url> or <url|label>.
func ParseBlogsCommand(text string) (BlogsCommand, error) {
	fields := strings.Fields(text)
	if len(fields) == 0 {
		return BlogsCommand{Action: "help"}, nil
	}
	action := strings.ToLower(fields[0])
	switch action {
	case "help", "list":
		if len(fields) != 1 {
			return BlogsCommand{}, fmt.Errorf("%s takes no argument", action)
		}
		return BlogsCommand{Action: action}, nil
	case "add", "remove", "pause", "resume":
		if len(fields) != 2 {
			return BlogsCommand{}, fmt.Errorf("%s takes one argument", action)
		}
		arg := fields[1]
		if strings.HasPrefix(arg, "<") && strings.HasSuffix(arg, ">") {
			arg, _, _ = strings.Cut(arg[1:len(arg)-1], "|")
		}
		return BlogsCommand{Action: action, Arg: arg}, nil
	default:
		return BlogsCommand{}, fmt.Errorf("unknown action %q", fields[0])
	}
}

type HandlerData struct {
	Jobs               *usecase.Jobs
	Suggest            *usecase.Suggest
	Sources            *service.Source
	Votes              *service.Votes
	Poster             *slackpost.Poster
	ChannelID, OwnerID string
	Background         context.Context // the serve context, for the work that answers after the ack
}

type Handler struct {
	jobs       *usecase.Jobs
	suggest    *usecase.Suggest
	sources    *service.Source
	votes      *service.Votes
	poster     *slackpost.Poster
	channelID  string
	ownerID    string
	background context.Context
	pending    sync.WaitGroup
}

func NewHandler(d HandlerData) *Handler {
	return &Handler{
		jobs:       d.Jobs,
		suggest:    d.Suggest,
		sources:    d.Sources,
		votes:      d.Votes,
		poster:     d.Poster,
		channelID:  d.ChannelID,
		ownerID:    d.OwnerID,
		background: d.Background,
	}
}

// SlashCommand returns the ack payload at once; it never waits on the network.
func (h *Handler) SlashCommand(ctx context.Context, cmd slack.SlashCommand) map[string]any {
	return map[string]any{"response_type": slack.ResponseTypeEphemeral, "text": h.answer(ctx, cmd)}
}

func (h *Handler) answer(ctx context.Context, cmd slack.SlashCommand) string {
	if cmd.UserID != h.ownerID {
		log.Ctx(ctx).Info().Msgf("refused %s from user %s", cmd.Command, cmd.UserID)
		return ownerOnlyText
	}
	switch cmd.Command {
	case "/show":
		return h.show(ctx, cmd)
	case "/blogs":
		return h.blogs(ctx, cmd)
	case "/discover":
		return h.discover(ctx, cmd)
	default:
		log.Ctx(ctx).Warn().Msgf("unknown slash command %s", cmd.Command)
		return fmt.Sprintf("Unknown command %s.", slackpost.Escape(cmd.Command))
	}
}

func (h *Handler) show(ctx context.Context, cmd slack.SlashCommand) string {
	if cmd.ChannelID != h.channelID {
		return fmt.Sprintf("Use /show in <#%s>.", h.channelID)
	}
	result := h.jobs.Submit(usecase.Job{Kind: usecase.JobShowDigest, ReplyURL: cmd.ResponseURL})
	log.Ctx(ctx).Info().Msgf("/show from %s in %s: %s", cmd.UserID, cmd.ChannelID, result)
	switch result {
	case usecase.Queued:
		return pickingText
	case usecase.AlreadyQueued:
		return onItsWayText
	case usecase.Busy:
		return busyText
	}
	return controllertext.Failed
}

func (h *Handler) discover(ctx context.Context, cmd slack.SlashCommand) string {
	if cmd.ChannelID != h.channelID {
		return fmt.Sprintf("Use /discover in <#%s>.", h.channelID)
	}
	result := h.jobs.Submit(usecase.Job{Kind: usecase.JobSuggest, ReplyURL: cmd.ResponseURL})
	log.Ctx(ctx).Info().Msgf("/discover from %s in %s: %s", cmd.UserID, cmd.ChannelID, result)
	switch result {
	case usecase.Queued:
		return lookingText
	case usecase.AlreadyQueued:
		return alreadyLookingText
	case usecase.Busy:
		return busyText
	}
	return controllertext.Failed
}

func (h *Handler) blogs(ctx context.Context, cmd slack.SlashCommand) string {
	c, err := ParseBlogsCommand(cmd.Text)
	if err != nil {
		log.Ctx(ctx).Debug().Err(err).Msgf("bad /blogs text %q", cmd.Text)
		return slackpost.Escape(controllertext.BlogsUsage)
	}
	switch c.Action {
	case "help":
		return slackpost.Escape(controllertext.BlogsUsage)
	case "list":
		return h.listBlogs(ctx)
	case "add":
		h.addBlogLater(cmd.ResponseURL, c.Arg)
		return slackpost.Escape(controllertext.Checking(c.Arg))
	default:
		return h.changeBlog(ctx, c)
	}
}

func (h *Handler) listBlogs(ctx context.Context) string {
	sources, err := h.sources.List(ctx)
	if err != nil {
		log.Ctx(ctx).Error().Err(err).Msg("/blogs list failed")
		return controllertext.Failed
	}
	if len(sources) == 0 {
		return controllertext.NoBlogsYet
	}
	lines := make([]string, 0, len(sources))
	for _, src := range sources {
		lines = append(lines, slackpost.Escape(controllertext.SourceLine(src)))
	}
	return strings.Join(lines, "\n")
}

func (h *Handler) changeBlog(ctx context.Context, c BlogsCommand) string {
	var src domain.Source
	var err error
	var answer func(domain.Source) string
	switch c.Action {
	case "remove":
		src, err = h.sources.Remove(ctx, c.Arg)
		answer = controllertext.SourceRemoved
	case "pause":
		src, err = h.sources.Pause(ctx, c.Arg)
		answer = controllertext.SourcePaused
	default:
		src, err = h.sources.Resume(ctx, c.Arg)
		answer = controllertext.SourceResumed
	}
	if err != nil {
		if text, ok := controllertext.RefFailed(err, c.Arg); ok {
			return slackpost.Escape(text)
		}
		log.Ctx(ctx).Error().Err(err).Msgf("/blogs %s %s failed", c.Action, c.Arg)
		return controllertext.Failed
	}
	log.Ctx(ctx).Info().Msgf("/blogs %s: blog %d", c.Action, src.ID)
	return slackpost.Escape(answer(src))
}

// Wait returns when every /blogs add and add button click started so far has answered.
func (h *Handler) Wait() {
	h.pending.Wait()
}

func (h *Handler) addBlogLater(responseURL, rawURL string) {
	h.pending.Go(func() {
		ctx, cancel := context.WithTimeout(h.background, addTimeout)
		defer cancel()

		src, err := h.sources.Add(ctx, rawURL)
		text, known := controllertext.AddFailed(err, rawURL)
		switch {
		case err == nil:
			log.Ctx(ctx).Info().Msgf("/blogs add: blog %d %s", src.ID, src.FeedURL)
			text = controllertext.SourceAdded(src)
		case !known:
			log.Ctx(ctx).Error().Err(err).Msgf("/blogs add %s failed", rawURL)
			text = controllertext.Failed
		}
		if err := h.poster.Reply(ctx, responseURL, slackpost.Escape(text)); err != nil {
			log.Ctx(ctx).Error().Err(err).Msgf("cannot answer /blogs add %s", rawURL)
		}
	})
}
