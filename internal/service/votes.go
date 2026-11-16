package service

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/rs/zerolog/log"

	"github.com/sadensmol/blogger/internal/domain"
	"github.com/sadensmol/blogger/internal/repository"
)

type VotesData struct {
	Reactions *repository.Reaction
	Posts     *Posts
	Sources   *Source
	Now       func() time.Time
}

type Votes struct {
	reactions *repository.Reaction
	posts     *Posts
	sources   *Source
	now       func() time.Time
}

func NewVotes(d VotesData) *Votes {
	return &Votes{reactions: d.Reactions, posts: d.Posts, sources: d.Sources, now: d.Now}
}

func (v *Votes) Add(ctx context.Context, e domain.ReactionEvent) error {
	return v.apply(ctx, e, v.reactions.Upsert)
}

func (v *Votes) Remove(ctx context.Context, e domain.ReactionEvent) error {
	return v.apply(ctx, e, v.reactions.MarkRemoved)
}

func (v *Votes) ChangedSince(ctx context.Context, t time.Time) (bool, error) {
	changed, err := v.reactions.ExistsChangedAfter(ctx, t)
	if err != nil {
		return false, fmt.Errorf("cannot check votes: %w", err)
	}
	return changed, nil
}

func (v *Votes) Recent(ctx context.Context, limit int) ([]domain.VotedArticle, error) {
	voted, err := v.reactions.ListLatestNotRemoved(ctx, limit)
	if err != nil {
		return nil, fmt.Errorf("cannot list recent votes: %w", err)
	}
	return voted, nil
}

func (v *Votes) apply(
	ctx context.Context,
	e domain.ReactionEvent,
	write func(context.Context, domain.ReactionEvent, domain.PostID, time.Time) error,
) error {
	post, err := v.posts.FindByMessage(ctx, e.ChannelID, e.MessageTS)
	if errors.Is(err, domain.ErrNotFound) {
		log.Ctx(ctx).Debug().Msgf("ignoring reaction on unknown message %s %s", e.ChannelID, e.MessageTS)
		return nil
	}
	if err != nil {
		return err
	}
	if err := write(ctx, e, post.ID, v.now()); err != nil {
		return fmt.Errorf("cannot store vote: %w", err)
	}
	_, err = v.sources.ApplyStatusRule(ctx, post.SourceID)
	if errors.Is(err, domain.ErrNotFound) {
		log.Ctx(ctx).Debug().Msgf("skipping status rule: blog %d of post %d is removed", post.SourceID, post.ID)
		return nil
	}
	if err != nil {
		return fmt.Errorf("cannot update blog %d after vote: %w", post.SourceID, err)
	}
	return nil
}

func (v *Votes) Themes(ctx context.Context) (domain.Themes, error) {
	return domain.Themes{}, nil
}
