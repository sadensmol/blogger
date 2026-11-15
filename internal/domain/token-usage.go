package domain

type BackendTokens struct {
	Backend    string
	Input      int64
	Output     int64
	Reported   int
	Unreported int
}

type TokenUsage struct {
	backends []BackendTokens
}

func (u TokenUsage) WithRun(backend string, input, output int64) TokenUsage {
	return TokenUsage{}
}

func (u TokenUsage) WithUnreportedRun(backend string) TokenUsage {
	return TokenUsage{}
}

func (u TokenUsage) Backends() []BackendTokens {
	return []BackendTokens{}
}

func (u TokenUsage) Total() int64 {
	return 0
}
