package llm

import (
	"context"
	"sync"

	"github.com/sadensmol/blogger/internal/domain"
)

type meterKey struct{}

// Meter collects the token usage of every backend run started with its context.
// Jobs of one context can run at the same time, so the mutex guards the usage.
type Meter struct {
	mu    sync.Mutex
	usage domain.TokenUsage
}

func WithMeter(ctx context.Context) (context.Context, *Meter) {
	meter := &Meter{}
	return context.WithValue(ctx, meterKey{}, meter), meter
}

func (m *Meter) Usage() domain.TokenUsage {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.usage
}

func (m *Meter) addRun(backend Backend, input, output int64) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.usage = m.usage.WithRun(string(backend), input, output)
}

func (m *Meter) addUnreportedRun(backend Backend) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.usage = m.usage.WithUnreportedRun(string(backend))
}
