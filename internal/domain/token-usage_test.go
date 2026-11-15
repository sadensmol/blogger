package domain_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/sadensmol/blogger/internal/domain"
)

const (
	claudeName  = "claude"
	codexName   = "codex"
	claudeIn    = int64(41055)
	claudeOut   = int64(126)
	codexIn     = int64(23710)
	codexOut    = int64(5)
	claudeTotal = int64(41181)
)

func TestTokenUsageWithRunCountsOneBackend(t *testing.T) {
	u := domain.TokenUsage{}.WithRun(claudeName, claudeIn, claudeOut)

	require.Equal(t, []domain.BackendTokens{{Backend: claudeName, Input: claudeIn, Output: claudeOut, Reported: 1}}, u.Backends())
	require.Equal(t, claudeTotal, u.Total())
}

func TestTokenUsageSumsRunsPerBackendInFirstUseOrder(t *testing.T) {
	u := domain.TokenUsage{}.
		WithRun(codexName, codexIn, codexOut).
		WithRun(claudeName, claudeIn, claudeOut).
		WithRun(codexName, 1000, 20)

	require.Equal(t, []domain.BackendTokens{
		{Backend: codexName, Input: codexIn + 1000, Output: codexOut + 20, Reported: 2},
		{Backend: claudeName, Input: claudeIn, Output: claudeOut, Reported: 1},
	}, u.Backends())
	require.Equal(t, codexIn+1000+codexOut+20+claudeIn+claudeOut, u.Total())
}

func TestTokenUsageCountsUnreportedRunsApart(t *testing.T) {
	u := domain.TokenUsage{}.
		WithRun(claudeName, claudeIn, claudeOut).
		WithUnreportedRun(claudeName).
		WithUnreportedRun(codexName)

	require.Equal(t, []domain.BackendTokens{
		{Backend: claudeName, Input: claudeIn, Output: claudeOut, Reported: 1, Unreported: 1},
		{Backend: codexName, Unreported: 1},
	}, u.Backends())
	require.Equal(t, claudeTotal, u.Total())
}

func TestTokenUsageUnreportedFirstKeepsItsPlace(t *testing.T) {
	u := domain.TokenUsage{}.
		WithUnreportedRun(claudeName).
		WithRun(codexName, codexIn, codexOut)

	require.Equal(t, []domain.BackendTokens{
		{Backend: claudeName, Unreported: 1},
		{Backend: codexName, Input: codexIn, Output: codexOut, Reported: 1},
	}, u.Backends())
	require.Equal(t, codexIn+codexOut, u.Total())
}

func TestTokenUsageRunWithZeroTokensIsReported(t *testing.T) {
	u := domain.TokenUsage{}.WithRun(codexName, 0, 0)

	require.Equal(t, []domain.BackendTokens{{Backend: codexName, Reported: 1}}, u.Backends())
	require.Zero(t, u.Total())
}

func TestTokenUsageWithRunLeavesTheOldValueUnchanged(t *testing.T) {
	first := domain.TokenUsage{}.WithRun(claudeName, claudeIn, claudeOut)

	second := first.WithRun(codexName, codexIn, codexOut)
	third := first.WithUnreportedRun(claudeName)

	require.Equal(t, []domain.BackendTokens{{Backend: claudeName, Input: claudeIn, Output: claudeOut, Reported: 1}}, first.Backends())
	require.Equal(t, claudeTotal, first.Total())
	require.Len(t, second.Backends(), 2)
	require.Equal(t, []domain.BackendTokens{{Backend: claudeName, Input: claudeIn, Output: claudeOut, Reported: 1, Unreported: 1}}, third.Backends())
}
