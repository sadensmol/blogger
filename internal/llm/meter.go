package llm

import (
	"context"

	"github.com/sadensmol/blogger/internal/domain"
)

type Meter struct {
	usage domain.TokenUsage
}

func WithMeter(ctx context.Context) (context.Context, *Meter) {
	return ctx, &Meter{}
}

func (m *Meter) Usage() domain.TokenUsage {
	return domain.TokenUsage{}
}
