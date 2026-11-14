package service

import (
	"context"
	"fmt"
	"time"

	"github.com/sadensmol/blogger/internal/domain"
	"github.com/sadensmol/blogger/internal/repository"
)

type Posts struct {
	repo *repository.Post
	now  func() time.Time
}

func NewPosts(repo *repository.Post, now func() time.Time) *Posts {
	return &Posts{repo: repo, now: now}
}

func (p *Posts) Create(ctx context.Context, post domain.Post) (domain.PostID, error) {
	id, err := p.repo.Create(ctx, post, p.now())
	if err != nil {
		return 0, fmt.Errorf("cannot save post: %w", err)
	}
	return id, nil
}

func (p *Posts) SetMessage(ctx context.Context, id domain.PostID, channelID, ts string) error {
	if err := p.repo.SetMessage(ctx, id, channelID, ts, p.now()); err != nil {
		return fmt.Errorf("cannot save the message of post %d: %w", id, err)
	}
	return nil
}

func (p *Posts) Delete(ctx context.Context, id domain.PostID) error {
	if err := p.repo.Delete(ctx, id); err != nil {
		return fmt.Errorf("cannot remove post %d: %w", id, err)
	}
	return nil
}

func (p *Posts) FindByMessage(ctx context.Context, channelID, ts string) (domain.Post, error) {
	post, err := p.repo.FindByMessage(ctx, channelID, ts)
	if err != nil {
		return domain.Post{}, fmt.Errorf("cannot find post: %w", err)
	}
	return post, nil
}
