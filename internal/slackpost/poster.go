package slackpost

import (
	"context"
	"fmt"
	"strings"

	"github.com/slack-go/slack"

	"github.com/sadensmol/blogger/internal/domain"
)

var (
	escaper    = strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;")
	urlEscaper = strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;", "|", "%7C", " ", "%20", "\t", "%09", "\n", "%0A", "\r", "%0D")
)

func Escape(s string) string {
	return escaper.Replace(s)
}

type Poster struct {
	api       *slack.Client
	channelID string
}

func NewPoster(api *slack.Client, channelID string) *Poster {
	return &Poster{api: api, channelID: channelID}
}

func (p *Poster) Text(ctx context.Context, text string) error {
	_, _, err := p.api.PostMessageContext(ctx, p.channelID, slack.MsgOptionText(text, false))
	if err != nil {
		return fmt.Errorf("cannot post text to slack: %w", err)
	}
	return nil
}

func (p *Poster) Pick(ctx context.Context, a domain.Article, pk domain.Pick) (string, string, error) {
	if !strings.HasPrefix(string(a.URL), "http://") && !strings.HasPrefix(string(a.URL), "https://") {
		return "", "", fmt.Errorf("cannot post pick %q: url scheme must be http or https", a.URL)
	}
	text := fmt.Sprintf("*<%s|%s>*\n%s\n_%s · %s_",
		urlEscaper.Replace(string(a.URL)), Escape(a.Title), Escape(pk.Description), Escape(a.SourceTitle), Escape(pk.Reason))

	channel, ts, err := p.api.PostMessageContext(ctx, p.channelID,
		slack.MsgOptionText(text, false),
		slack.MsgOptionDisableLinkUnfurl(),
		slack.MsgOptionDisableMediaUnfurl(),
	)
	if err != nil {
		return "", "", fmt.Errorf("cannot post pick %s to slack: %w", a.URL, err)
	}

	ref := slack.NewRefToMessage(channel, ts)
	for _, name := range []string{"+1", "-1"} {
		if err := p.api.AddReactionContext(ctx, name, ref); err != nil {
			return ts, channel, fmt.Errorf("cannot add reaction %s to pick %s: %w", name, a.URL, err)
		}
	}
	return ts, channel, nil
}

func (p *Poster) Reply(ctx context.Context, responseURL, text string) error {
	msg := &slack.WebhookMessage{ResponseType: slack.ResponseTypeEphemeral, Text: text}
	if err := slack.PostWebhookContext(ctx, responseURL, msg); err != nil {
		return fmt.Errorf("cannot reply to slash command: %w", err)
	}
	return nil
}
