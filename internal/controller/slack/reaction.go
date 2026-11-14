package controllerslack

import (
	"context"

	"github.com/slack-go/slack/slackevents"

	"github.com/sadensmol/blogger/internal/domain"
)

type slackReaction slackevents.ReactionAddedEvent

func (r slackReaction) toDomain() (domain.ReactionEvent, bool) {
	vote, ok := domain.ParseVote(r.Reaction)
	if !ok {
		return domain.ReactionEvent{}, false
	}
	return domain.ReactionEvent{ChannelID: r.Item.Channel, MessageTS: r.Item.Timestamp, UserID: r.User, Vote: vote}, true
}

func (h *Handler) ReactionAdded(ctx context.Context, ev *slackevents.ReactionAddedEvent) error {
	if ev == nil {
		return nil
	}
	vote, ok := h.ownerVote(slackReaction(*ev))
	if !ok {
		return nil
	}
	return h.votes.Add(ctx, vote)
}

func (h *Handler) ReactionRemoved(ctx context.Context, ev *slackevents.ReactionRemovedEvent) error {
	if ev == nil {
		return nil
	}
	vote, ok := h.ownerVote(slackReaction(*ev))
	if !ok {
		return nil
	}
	return h.votes.Remove(ctx, vote)
}

// ownerVote drops reactions of other users (the bot too), other channels and other emoji.
func (h *Handler) ownerVote(r slackReaction) (domain.ReactionEvent, bool) {
	if r.User != h.ownerID || r.Item.Type != "message" || r.Item.Channel != h.channelID {
		return domain.ReactionEvent{}, false
	}
	return r.toDomain()
}
