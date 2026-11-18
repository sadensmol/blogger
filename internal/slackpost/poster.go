package slackpost

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/slack-go/slack"

	"github.com/sadensmol/blogger/internal/domain"
)

const (
	AddBlogAction  = "add-blog"
	proposalsTitle = "Blogs you may like"
	addButtonText  = "+ Add"
	addedText      = "Added ✓"
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
	http      *http.Client
	channelID string
}

// NewPoster takes httpClient for replies to a response URL, which the api client does not send.
func NewPoster(api *slack.Client, httpClient *http.Client, channelID string) *Poster {
	return &Poster{api: api, http: httpClient, channelID: channelID}
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
	source := Escape(a.SourceTitle)
	if strings.HasPrefix(string(a.SourceURL), "http://") || strings.HasPrefix(string(a.SourceURL), "https://") {
		source = fmt.Sprintf("<%s|%s>", urlEscaper.Replace(string(a.SourceURL)), source)
	}
	text := fmt.Sprintf("*<%s|%s>*\n%s\n_%s · %s_",
		urlEscaper.Replace(string(a.URL)), Escape(a.Title), Escape(pk.Description), source, Escape(pk.Reason))
	if len(a.Tags) > 0 {
		themes := make([]string, 0, len(a.Tags))
		for _, tag := range a.Tags {
			themes = append(themes, Escape(string(tag)))
		}
		text = fmt.Sprintf("%s\nThemes: %s", text, strings.Join(themes, ", "))
	}

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

// Proposals posts one message with an add button per blog; the button value is the proposal id.
func (p *Poster) Proposals(ctx context.Context, proposals []domain.Proposal) (string, string, error) {
	blocks, err := proposalBlocks(proposals)
	if err != nil {
		return "", "", err
	}
	channel, ts, err := p.api.PostMessageContext(ctx, p.channelID,
		slack.MsgOptionText(proposalsTitle, false),
		slack.MsgOptionBlocks(blocks...),
		slack.MsgOptionDisableLinkUnfurl(),
		slack.MsgOptionDisableMediaUnfurl(),
	)
	if err != nil {
		return "", "", fmt.Errorf("cannot post %d proposed blogs to slack: %w", len(proposals), err)
	}
	return ts, channel, nil
}

// UpdateProposals redraws a Proposals message: an added blog shows a mark instead of its button.
func (p *Poster) UpdateProposals(ctx context.Context, channelID, ts string, proposals []domain.Proposal) error {
	blocks, err := proposalBlocks(proposals)
	if err != nil {
		return err
	}
	if _, _, _, err := p.api.UpdateMessageContext(ctx, channelID, ts,
		slack.MsgOptionText(proposalsTitle, false),
		slack.MsgOptionBlocks(blocks...),
	); err != nil {
		return fmt.Errorf("cannot update proposed blogs message %s %s: %w", channelID, ts, err)
	}
	return nil
}

func proposalBlocks(proposals []domain.Proposal) ([]slack.Block, error) {
	blocks := []slack.Block{slack.NewHeaderBlock(slack.NewTextBlockObject(slack.PlainTextType, proposalsTitle, false, false))}
	for _, proposal := range proposals {
		blog := proposal.Blog
		if !strings.HasPrefix(string(blog.SiteURL), "http://") && !strings.HasPrefix(string(blog.SiteURL), "https://") {
			return nil, fmt.Errorf("cannot show proposed blog %q: url scheme must be http or https", blog.SiteURL)
		}
		text := fmt.Sprintf("*<%s|%s>*\n%s", urlEscaper.Replace(string(blog.SiteURL)), Escape(blog.Title), Escape(blog.About))
		var accessory *slack.Accessory
		if proposal.Status == domain.ProposalAdded {
			text = fmt.Sprintf("%s\n*%s*", text, addedText)
		} else {
			accessory = slack.NewAccessory(slack.NewButtonBlockElement(AddBlogAction, strconv.FormatInt(int64(proposal.ID), 10),
				slack.NewTextBlockObject(slack.PlainTextType, addButtonText, false, false)))
		}
		blocks = append(blocks, slack.NewSectionBlock(slack.NewTextBlockObject(slack.MarkdownType, text, false, false), nil, accessory))
	}
	return blocks, nil
}

// NotPosted reports whether slack answered a post with a clear rejection, so the message is surely not in the channel.
// A transport error or a timeout is not one: slack may have posted the message before the answer got lost.
func NotPosted(err error) bool {
	var apiErr slack.SlackErrorResponse
	var rateErr *slack.RateLimitedError
	return errors.As(err, &apiErr) || errors.As(err, &rateErr)
}

// ErrorCode returns the code of a call that slack rejected, such as not_in_channel; it is empty for any other error.
func ErrorCode(err error) string {
	var apiErr slack.SlackErrorResponse
	if errors.As(err, &apiErr) {
		return apiErr.Err
	}
	return ""
}

func (p *Poster) Reply(ctx context.Context, responseURL, text string) error {
	return p.reply(ctx, responseURL, &slack.WebhookMessage{ResponseType: slack.ResponseTypeEphemeral, Text: text})
}

// ReplyWithUsage puts the token line as small text below text; an empty text sends the token line alone.
func (p *Poster) ReplyWithUsage(ctx context.Context, responseURL, text string, u domain.TokenUsage) error {
	fallback := text
	if fallback == "" {
		fallback = UsageText(u)
	}
	return p.reply(ctx, responseURL, &slack.WebhookMessage{
		ResponseType: slack.ResponseTypeEphemeral,
		Text:         fallback,
		Blocks:       &slack.Blocks{BlockSet: UsageBlocks(text, u)},
	})
}

// Ephemeral posts text with the token line as a message in the channel that only userID sees.
func (p *Poster) Ephemeral(ctx context.Context, userID, text string, u domain.TokenUsage) error {
	fallback := text
	if fallback == "" {
		fallback = UsageText(u)
	}
	if _, err := p.api.PostEphemeralContext(ctx, p.channelID, userID,
		slack.MsgOptionText(fallback, false),
		slack.MsgOptionBlocks(UsageBlocks(text, u)...),
	); err != nil {
		return fmt.Errorf("cannot post private message to user %s: %w", userID, err)
	}
	return nil
}

func (p *Poster) reply(ctx context.Context, responseURL string, msg *slack.WebhookMessage) error {
	if err := slack.PostWebhookCustomHTTPContext(ctx, responseURL, p.http, msg); err != nil {
		// a *url.Error prints the response URL, which lets anyone post as the bot: keep only its cause
		var urlErr *url.Error
		if errors.As(err, &urlErr) {
			return fmt.Errorf("cannot reply to slash command: %s: %w", urlErr.Op, urlErr.Err)
		}
		return fmt.Errorf("cannot reply to slash command: %w", err)
	}
	return nil
}
