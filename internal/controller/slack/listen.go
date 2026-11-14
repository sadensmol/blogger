package controllerslack

import (
	"context"
	"errors"
	"fmt"

	"github.com/rs/zerolog/log"
	"github.com/slack-go/slack"
	"github.com/slack-go/slack/slackevents"
	"github.com/slack-go/slack/socketmode"
)

func Listen(ctx context.Context, client *socketmode.Client, h *Handler) error {
	runErr := make(chan error, 1)
	go func() {
		runErr <- client.RunContext(ctx)
	}()
	for {
		select {
		case <-ctx.Done():
			<-runErr
			return nil
		case err := <-runErr:
			if ctx.Err() != nil {
				return nil
			}
			if err == nil {
				return errors.New("slack connection ended")
			}
			return fmt.Errorf("slack connection ended: %w", err)
		case evt := <-client.Events:
			dispatch(ctx, client, h, evt)
		}
	}
}

func dispatch(ctx context.Context, client *socketmode.Client, h *Handler, evt socketmode.Event) {
	switch evt.Type {
	case socketmode.EventTypeConnected:
		log.Ctx(ctx).Info().Msg("connected to Slack")
	case socketmode.EventTypeConnectionError, socketmode.EventTypeInvalidAuth, socketmode.EventTypeIncomingError,
		socketmode.EventTypeErrorBadMessage, socketmode.EventTypeErrorWriteFailed:
		log.Ctx(ctx).Warn().Msgf("slack socket %s: %v", evt.Type, evt.Data)
	case socketmode.EventTypeEventsAPI:
		if evt.Request == nil {
			return
		}
		if err := client.AckCtx(ctx, evt.Request.EnvelopeID, nil); err != nil {
			log.Ctx(ctx).Error().Err(err).Msgf("cannot ack slack event %s", evt.Request.EnvelopeID)
		}
		apiEvent, ok := evt.Data.(slackevents.EventsAPIEvent)
		if !ok {
			log.Ctx(ctx).Warn().Msgf("unexpected slack event data %T", evt.Data)
			return
		}
		var err error
		switch inner := apiEvent.InnerEvent.Data.(type) {
		case *slackevents.ReactionAddedEvent:
			err = h.ReactionAdded(ctx, inner)
		case *slackevents.ReactionRemovedEvent:
			err = h.ReactionRemoved(ctx, inner)
		}
		if err != nil {
			log.Ctx(ctx).Error().Err(err).Msgf("cannot record slack %s", apiEvent.InnerEvent.Type)
		}
	case socketmode.EventTypeInteractive:
		cb, ok := evt.Data.(slack.InteractionCallback)
		if !ok || evt.Request == nil {
			log.Ctx(ctx).Warn().Msgf("unexpected interaction data %T", evt.Data)
			return
		}
		if err := client.AckCtx(ctx, evt.Request.EnvelopeID, nil); err != nil {
			log.Ctx(ctx).Error().Err(err).Msgf("cannot ack slack interaction %s", evt.Request.EnvelopeID)
		}
		if cb.Type == slack.InteractionTypeBlockActions {
			h.BlockActions(ctx, cb)
		}
	case socketmode.EventTypeSlashCommand:
		cmd, ok := evt.Data.(slack.SlashCommand)
		if !ok || evt.Request == nil {
			log.Ctx(ctx).Warn().Msgf("unexpected slash command data %T", evt.Data)
			return
		}
		if err := client.AckCtx(ctx, evt.Request.EnvelopeID, h.SlashCommand(ctx, cmd)); err != nil {
			log.Ctx(ctx).Error().Err(err).Msgf("cannot ack %s %s", cmd.Command, evt.Request.EnvelopeID)
		}
	}
}
