package domain

import "slices"

type BackendTokens struct {
	Backend    string
	Input      int64
	Output     int64
	Reported   int
	Unreported int
}

// TokenUsage keeps one entry per backend, in the order of the backend's first run.
type TokenUsage struct {
	backends []BackendTokens
}

func (u TokenUsage) WithRun(backend string, input, output int64) TokenUsage {
	return u.withChange(backend, func(b *BackendTokens) {
		b.Input += input
		b.Output += output
		b.Reported++
	})
}

func (u TokenUsage) WithUnreportedRun(backend string) TokenUsage {
	return u.withChange(backend, func(b *BackendTokens) {
		b.Unreported++
	})
}

func (u TokenUsage) Backends() []BackendTokens {
	return append([]BackendTokens{}, u.backends...)
}

func (u TokenUsage) Total() int64 {
	var total int64
	for _, b := range u.backends {
		total += b.Input + b.Output
	}
	return total
}

func (u TokenUsage) withChange(backend string, change func(b *BackendTokens)) TokenUsage {
	backends := slices.Clone(u.backends)
	i := slices.IndexFunc(backends, func(b BackendTokens) bool { return b.Backend == backend })
	if i < 0 {
		backends = append(backends, BackendTokens{Backend: backend})
		i = len(backends) - 1
	}
	change(&backends[i])
	return TokenUsage{backends: backends}
}
