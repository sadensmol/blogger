package slackpost_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/sadensmol/blogger/internal/domain"
	"github.com/sadensmol/blogger/internal/slackpost"
)

const (
	claudeName = "claude"
	codexName  = "codex"
)

func TestUsageTextOneBackend(t *testing.T) {
	u := domain.TokenUsage{}.WithRun(claudeName, 41055, 126)

	require.Equal(t, "Tokens used: 41,181 (claude: 41,055 in, 126 out)", slackpost.UsageText(u))
}

func TestUsageTextTwoBackendsInFirstRunOrder(t *testing.T) {
	u := domain.TokenUsage{}.
		WithRun(codexName, 24710, 25).
		WithRun(claudeName, 41055, 126)

	require.Equal(t, "Tokens used: 65,916 (codex: 24,710 in, 25 out; claude: 41,055 in, 126 out)", slackpost.UsageText(u))
}

func TestUsageTextSumsRunsOfOneBackend(t *testing.T) {
	u := domain.TokenUsage{}.
		WithRun(claudeName, 41055, 126).
		WithRun(claudeName, 41055, 126)

	require.Equal(t, "Tokens used: 82,362 (claude: 82,110 in, 252 out)", slackpost.UsageText(u))
}

func TestUsageTextMarksBackendWithAnUnreportedRunIncomplete(t *testing.T) {
	u := domain.TokenUsage{}.
		WithRun(claudeName, 41055, 126).
		WithUnreportedRun(claudeName)

	require.Equal(t, "Tokens used: 41,181 (claude: 41,055 in, 126 out (incomplete))", slackpost.UsageText(u))
}

func TestUsageTextWritesBackendWithoutReportedRunsNotReported(t *testing.T) {
	u := domain.TokenUsage{}.
		WithUnreportedRun(claudeName).
		WithRun(codexName, 24710, 25)

	require.Equal(t, "Tokens used: 24,735 (claude: not reported; codex: 24,710 in, 25 out)", slackpost.UsageText(u))
}

func TestUsageTextUnknownWhenNoRunReported(t *testing.T) {
	u := domain.TokenUsage{}.
		WithUnreportedRun(claudeName).
		WithUnreportedRun(codexName)

	require.Equal(t, "Tokens used: unknown (claude: not reported; codex: not reported)", slackpost.UsageText(u))
}

func TestUsageTextZeroWhenNoBackendRan(t *testing.T) {
	require.Equal(t, "Tokens used: 0 (no LLM call)", slackpost.UsageText(domain.TokenUsage{}))
}

func TestUsageTextThousandsSeparators(t *testing.T) {
	tests := []struct {
		name          string
		input, output int64
		want          string
	}{
		{name: "below a thousand", input: 999, want: "Tokens used: 999 (codex: 999 in, 0 out)"},
		{name: "exactly a thousand", input: 1000, output: 1, want: "Tokens used: 1,001 (codex: 1,000 in, 1 out)"},
		{name: "millions", input: 1234567, output: 1000000, want: "Tokens used: 2,234,567 (codex: 1,234,567 in, 1,000,000 out)"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			u := domain.TokenUsage{}.WithRun(codexName, tt.input, tt.output)

			require.Equal(t, tt.want, slackpost.UsageText(u))
		})
	}
}
