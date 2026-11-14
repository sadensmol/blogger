package controllerslack

import (
	"context"
	"strconv"

	"github.com/rs/zerolog/log"
	"github.com/slack-go/slack"

	controllertext "github.com/sadensmol/blogger/internal/controller/text"
	"github.com/sadensmol/blogger/internal/domain"
	"github.com/sadensmol/blogger/internal/slackpost"
)

const alreadyInListText = "Already in the list."

// BlockActions takes a button click after the ack; it trusts only the proposal id from slack.
func (h *Handler) BlockActions(ctx context.Context, cb slack.InteractionCallback) {
	if cb.User.ID != h.ownerID || cb.Channel.ID != h.channelID {
		log.Ctx(ctx).Info().Msgf("ignored a click of user %s in %s", cb.User.ID, cb.Channel.ID)
		return
	}
	for _, action := range cb.ActionCallback.BlockActions {
		if action == nil || action.ActionID != slackpost.AddBlogAction {
			continue
		}
		id, err := strconv.ParseInt(action.Value, 10, 64)
		if err != nil {
			log.Ctx(ctx).Warn().Err(err).Msgf("ignored an add click with value %q", action.Value)
			continue
		}
		h.addProposalLater(cb.ResponseURL, domain.ProposalID(id))
	}
}

func (h *Handler) addProposalLater(responseURL string, id domain.ProposalID) {
	h.pending.Go(func() {
		ctx, cancel := context.WithTimeout(h.background, addTimeout)
		defer cancel()

		added, err := h.suggest.Add(ctx, id)
		if err == nil && added {
			return
		}
		text := alreadyInListText
		if err != nil {
			log.Ctx(ctx).Error().Err(err).Msgf("add click on proposal %d failed", id)
			text = controllertext.Failed
		}
		if err := h.poster.Reply(ctx, responseURL, text); err != nil {
			log.Ctx(ctx).Error().Err(err).Msgf("cannot answer the add click on proposal %d", id)
		}
	})
}
