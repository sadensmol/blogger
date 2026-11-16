package llm

import (
	"context"

	"github.com/sadensmol/blogger/internal/domain"
)

func (r *Runner) TagArticles(ctx context.Context, articles []domain.Article, known []domain.Tag) (map[domain.URL][]domain.Tag, Backend, error) {
	return map[domain.URL][]domain.Tag{}, "", nil
}
