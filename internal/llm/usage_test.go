package llm_test

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/sadensmol/blogger/internal/domain"
	"github.com/sadensmol/blogger/internal/llm"
)

const (
	SampleClaudeIn  = int64(41055)
	SampleClaudeOut = int64(126)
	CodexIn         = int64(23710)
	CodexCached     = int64(6784)
	CodexOut        = int64(5)
)

func claudeTokens(runs int) domain.BackendTokens {
	return domain.BackendTokens{Backend: string(llm.Claude), Input: SampleClaudeIn * int64(runs), Output: SampleClaudeOut * int64(runs), Reported: runs}
}

func codexTokens(backend llm.Backend) domain.BackendTokens {
	return domain.BackendTokens{Backend: string(backend), Input: CodexIn, Output: CodexOut, Reported: 1}
}

func turnCompleted(input, cached, output int64) string {
	return fmt.Sprintf(`{"type":"turn.completed","usage":{"input_tokens":%d,"cached_input_tokens":%d,"cache_write_input_tokens":0,"output_tokens":%d,"reasoning_output_tokens":0}}`,
		input, cached, output)
}

// events sets the JSONL lines that the fake codex prints for one skill when it runs with --json.
func (f *fakeAgents) events(skill llm.Skill, lines ...string) {
	f.t.Helper()

	f.write(fmt.Sprintf("codex.%s.events", skill), fmt.Sprintf("%s\n", strings.Join(lines, "\n")))
}

func (f *fakeAgents) codexSample(skill llm.Skill) {
	f.events(skill,
		`{"type":"thread.started","thread_id":"t1"}`,
		`{"type":"turn.started"}`,
		`{"type":"item.completed","item":{"id":"item_0","type":"agent_message","text":"{}"}}`,
		turnCompleted(CodexIn, CodexCached, CodexOut),
	)
}

// claudeStdout sets the claude output of one skill: the sample envelope with answer obj, changed by edit.
func (f *fakeAgents) claudeStdout(skill llm.Skill, obj any, edit func(envelope map[string]any)) {
	f.t.Helper()

	raw, err := os.ReadFile("testdata/claude-envelope.json")
	require.NoError(f.t, err)
	var envelope map[string]any
	require.NoError(f.t, json.Unmarshal(raw, &envelope))
	envelope["structured_output"] = obj
	edit(envelope)
	out, err := json.Marshal(envelope)
	require.NoError(f.t, err)
	f.write(fmt.Sprintf("claude.%s.stdout", skill), string(out))
}

func TestClaudeRunAddsItsUsageToTheMeter(t *testing.T) {
	f := newFakeAgents(t, llm.Claude)
	f.answer("claude", llm.SkillPickArticles, picksAnswer(URLA))
	ctx, meter := llm.WithMeter(context.Background())

	_, backend, err := f.runner().PickArticles(ctx, pickInput())

	require.NoError(t, err)
	require.Equal(t, llm.Claude, backend)
	require.Equal(t, []domain.BackendTokens{claudeTokens(1)}, meter.Usage().Backends())
	require.Equal(t, SampleClaudeIn+SampleClaudeOut, meter.Usage().Total())
}

func TestClaudeUsageSumsEveryModel(t *testing.T) {
	f := newFakeAgents(t, llm.Claude)
	f.claudeStdout(llm.SkillPickArticles, picksAnswer(URLA), func(envelope map[string]any) {
		models := envelope["modelUsage"].(map[string]any)
		models["claude-haiku-4-5"] = map[string]any{
			"inputTokens": 100, "outputTokens": 50, "cacheReadInputTokens": 1000, "cacheCreationInputTokens": 10,
		}
	})
	ctx, meter := llm.WithMeter(context.Background())

	_, _, err := f.runner().PickArticles(ctx, pickInput())

	require.NoError(t, err)
	require.Equal(t, []domain.BackendTokens{{Backend: string(llm.Claude), Input: SampleClaudeIn + 1110, Output: SampleClaudeOut + 50, Reported: 1}},
		meter.Usage().Backends())
}

func TestClaudeUsageWithoutModelUsageReadsTopLevelUsage(t *testing.T) {
	f := newFakeAgents(t, llm.Claude)
	f.claudeStdout(llm.SkillPickArticles, picksAnswer(URLA), func(envelope map[string]any) {
		delete(envelope, "modelUsage")
		envelope["usage"] = map[string]any{
			"input_tokens": 2, "cache_creation_input_tokens": 41053, "cache_read_input_tokens": 500, "output_tokens": 126,
		}
	})
	ctx, meter := llm.WithMeter(context.Background())

	_, _, err := f.runner().PickArticles(ctx, pickInput())

	require.NoError(t, err)
	require.Equal(t, []domain.BackendTokens{{Backend: string(llm.Claude), Input: SampleClaudeIn + 500, Output: SampleClaudeOut, Reported: 1}},
		meter.Usage().Backends())
}

func TestClaudeErrorEnvelopeStillCountsItsUsage(t *testing.T) {
	f := newFakeAgents(t, llm.Claude, llm.Codex)
	f.claudeStdout(llm.SkillPickArticles, picksAnswer(URLA), func(envelope map[string]any) {
		envelope["is_error"] = true
		envelope["subtype"] = "error_max_turns"
	})
	f.answer("codex", llm.SkillPickArticles, picksAnswer(URLA))
	f.codexSample(llm.SkillPickArticles)
	ctx, meter := llm.WithMeter(context.Background())

	_, backend, err := f.runner().PickArticles(ctx, pickInput())

	require.NoError(t, err)
	require.Equal(t, llm.Codex, backend)
	require.Equal(t, []domain.BackendTokens{claudeTokens(1), codexTokens(llm.Codex)}, meter.Usage().Backends())
}

func TestCodexRunsWithJSONAndCountsTurnCompleted(t *testing.T) {
	f := newFakeAgents(t, llm.Codex)
	f.answer("codex", llm.SkillPickArticles, picksAnswer(URLA, URLB))
	f.codexSample(llm.SkillPickArticles)
	ctx, meter := llm.WithMeter(context.Background())

	picks, backend, err := f.runner().PickArticles(ctx, pickInput())

	require.NoError(t, err)
	require.Equal(t, llm.Codex, backend)
	require.Equal(t, []domain.URL{URLA, URLB}, pickURLs(picks))
	require.Contains(t, strings.Fields(f.read("codex.args")), "--json")
	require.Equal(t, []domain.BackendTokens{codexTokens(llm.Codex)}, meter.Usage().Backends())
}

func TestCodexSumsEveryTurnCompletedEvent(t *testing.T) {
	f := newFakeAgents(t, llm.Codex)
	f.answer("codex", llm.SkillPickArticles, picksAnswer(URLA))
	f.events(llm.SkillPickArticles,
		`{"type":"thread.started","thread_id":"t1"}`,
		`{"type":"turn.started"}`,
		turnCompleted(CodexIn, CodexCached, CodexOut),
		`{"type":"turn.started"}`,
		`{"type":"item.completed","item":{"id":"item_1","type":"agent_message","text":"{}"}}`,
		turnCompleted(1000, 0, 20),
	)
	ctx, meter := llm.WithMeter(context.Background())

	_, _, err := f.runner().PickArticles(ctx, pickInput())

	require.NoError(t, err)
	require.Equal(t, []domain.BackendTokens{{Backend: string(llm.Codex), Input: CodexIn + 1000, Output: CodexOut + 20, Reported: 1}},
		meter.Usage().Backends())
}

func TestCodexOSSRunsWithJSONAndCountsAsCodexOSS(t *testing.T) {
	f := newFakeAgents(t, llm.CodexOSS)
	f.cfg.LocalModel = LocalModel
	f.cfg.OllamaURL = OllamaURL
	f.answer("codex", llm.SkillPickArticles, picksAnswer(URLA))
	f.codexSample(llm.SkillPickArticles)
	ctx, meter := llm.WithMeter(context.Background())

	_, backend, err := f.runner().PickArticles(ctx, pickInput())

	require.NoError(t, err)
	require.Equal(t, llm.CodexOSS, backend)
	require.Contains(t, strings.Fields(f.read("codex.args")), "--json")
	require.Equal(t, []domain.BackendTokens{codexTokens(llm.CodexOSS)}, meter.Usage().Backends())
}

func TestFailedClaudeAttemptCountsBesideTheCodexAnswer(t *testing.T) {
	f := newFakeAgents(t, llm.Claude, llm.Codex)
	f.answer("claude", llm.SkillPickArticles, picksAnswer(URLA))
	f.fail("claude")
	f.answer("codex", llm.SkillPickArticles, picksAnswer(URLA))
	f.codexSample(llm.SkillPickArticles)
	ctx, meter := llm.WithMeter(context.Background())

	_, backend, err := f.runner().PickArticles(ctx, pickInput())

	require.NoError(t, err)
	require.Equal(t, llm.Codex, backend)
	require.Equal(t, []domain.BackendTokens{claudeTokens(1), codexTokens(llm.Codex)}, meter.Usage().Backends())
}

func TestRejectedAnswerStillCounts(t *testing.T) {
	f := newFakeAgents(t, llm.Claude, llm.Codex)
	f.answer("claude", llm.SkillPickArticles, picksAnswer("https://evil.example/x"))
	f.answer("codex", llm.SkillPickArticles, picksAnswer(URLA))
	f.codexSample(llm.SkillPickArticles)
	ctx, meter := llm.WithMeter(context.Background())

	_, backend, err := f.runner().PickArticles(ctx, pickInput())

	require.NoError(t, err)
	require.Equal(t, llm.Codex, backend)
	require.Equal(t, []domain.BackendTokens{claudeTokens(1), codexTokens(llm.Codex)}, meter.Usage().Backends())
}

func TestEveryJobOfOneMeterCounts(t *testing.T) {
	f := newFakeAgents(t, llm.Claude)
	f.answer("claude", llm.SkillUpdateProfile, map[string]any{"interests_md": "# Interests\n\n## My notes\nGo and SQLite.\n\n## Learned\nLikes databases.\n"})
	f.answer("claude", llm.SkillPickArticles, picksAnswer(URLA))
	votes := []domain.VotedArticle{{Article: pickInput().Candidates[0], Tags: []domain.Tag{"go"}, Vote: domain.VoteUp}}
	runner := f.runner()
	ctx, meter := llm.WithMeter(context.Background())

	_, _, err := runner.UpdateProfile(ctx, CurrentProfile, votes, nil)
	require.NoError(t, err)
	_, _, err = runner.PickArticles(ctx, pickInput())
	require.NoError(t, err)

	require.Equal(t, []domain.BackendTokens{claudeTokens(2)}, meter.Usage().Backends())
}

func TestJobWhereAllBackendsFailStillCountsEveryAttempt(t *testing.T) {
	f := newFakeAgents(t, llm.Claude, llm.Codex)
	f.answer("claude", llm.SkillPickArticles, picksAnswer(URLA))
	f.fail("claude")
	f.answer("codex", llm.SkillPickArticles, picksAnswer(URLA))
	f.codexSample(llm.SkillPickArticles)
	f.fail("codex")
	ctx, meter := llm.WithMeter(context.Background())

	_, _, err := f.runner().PickArticles(ctx, pickInput())

	require.ErrorIs(t, err, llm.ErrAllBackendsFailed)
	require.Equal(t, []domain.BackendTokens{claudeTokens(1), codexTokens(llm.Codex)}, meter.Usage().Backends())
}

func TestJobWithoutMeterLeavesAConcurrentMeterAlone(t *testing.T) {
	f := newFakeAgents(t, llm.Claude)
	f.answer("claude", llm.SkillPickArticles, picksAnswer(URLA))
	runner := f.runner()
	ctx, meter := llm.WithMeter(context.Background())

	var wg sync.WaitGroup
	errs := make([]error, 3)
	for i, jobCtx := range []context.Context{context.Background(), ctx, context.Background()} {
		wg.Go(func() {
			_, _, errs[i] = runner.PickArticles(jobCtx, pickInput())
		})
	}
	wg.Wait()

	for _, err := range errs {
		require.NoError(t, err)
	}
	require.Equal(t, []domain.BackendTokens{claudeTokens(1)}, meter.Usage().Backends())
}

func TestTwoMetersCountOnlyTheirOwnJobs(t *testing.T) {
	f := newFakeAgents(t, llm.Claude)
	f.answer("claude", llm.SkillPickArticles, picksAnswer(URLA))
	runner := f.runner()
	oneCtx, one := llm.WithMeter(context.Background())
	twoCtx, two := llm.WithMeter(context.Background())

	var wg sync.WaitGroup
	errs := make([]error, 3)
	for i, jobCtx := range []context.Context{oneCtx, twoCtx, twoCtx} {
		wg.Go(func() {
			_, _, errs[i] = runner.PickArticles(jobCtx, pickInput())
		})
	}
	wg.Wait()

	for _, err := range errs {
		require.NoError(t, err)
	}
	require.Equal(t, []domain.BackendTokens{claudeTokens(1)}, one.Usage().Backends())
	require.Equal(t, []domain.BackendTokens{claudeTokens(2)}, two.Usage().Backends())
}

func TestCodexWithoutEventsIsUnreportedAndItsAnswerAccepted(t *testing.T) {
	f := newFakeAgents(t, llm.Codex)
	f.answer("codex", llm.SkillPickArticles, picksAnswer(URLA))
	ctx, meter := llm.WithMeter(context.Background())

	picks, backend, err := f.runner().PickArticles(ctx, pickInput())

	require.NoError(t, err)
	require.Equal(t, llm.Codex, backend)
	require.Equal(t, []domain.URL{URLA}, pickURLs(picks))
	require.Equal(t, []domain.BackendTokens{{Backend: string(llm.Codex), Unreported: 1}}, meter.Usage().Backends())
}

func TestCodexOutputThatIsNotJSONIsUnreported(t *testing.T) {
	f := newFakeAgents(t, llm.Codex)
	f.answer("codex", llm.SkillPickArticles, picksAnswer(URLA))
	f.events(llm.SkillPickArticles, "not json", "{\"type\":\"turn.compl")
	ctx, meter := llm.WithMeter(context.Background())

	_, backend, err := f.runner().PickArticles(ctx, pickInput())

	require.NoError(t, err)
	require.Equal(t, llm.Codex, backend)
	require.Equal(t, []domain.BackendTokens{{Backend: string(llm.Codex), Unreported: 1}}, meter.Usage().Backends())
}

func TestCodexEventsWithoutTurnCompletedAreUnreported(t *testing.T) {
	f := newFakeAgents(t, llm.Codex)
	f.answer("codex", llm.SkillPickArticles, picksAnswer(URLA))
	f.events(llm.SkillPickArticles,
		`{"type":"thread.started","thread_id":"t1"}`,
		`{"type":"turn.started"}`,
		`{"type":"item.completed","item":{"id":"item_0","type":"agent_message","text":"{}"}}`,
	)
	ctx, meter := llm.WithMeter(context.Background())

	_, backend, err := f.runner().PickArticles(ctx, pickInput())

	require.NoError(t, err)
	require.Equal(t, llm.Codex, backend)
	require.Equal(t, []domain.BackendTokens{{Backend: string(llm.Codex), Unreported: 1}}, meter.Usage().Backends())
}

func TestClaudeEnvelopeWithoutUsageIsUnreportedAndItsAnswerAccepted(t *testing.T) {
	f := newFakeAgents(t, llm.Claude)
	f.claudeStdout(llm.SkillPickArticles, picksAnswer(URLA), func(envelope map[string]any) {
		delete(envelope, "modelUsage")
		delete(envelope, "usage")
	})
	ctx, meter := llm.WithMeter(context.Background())

	picks, backend, err := f.runner().PickArticles(ctx, pickInput())

	require.NoError(t, err)
	require.Equal(t, llm.Claude, backend)
	require.Equal(t, []domain.URL{URLA}, pickURLs(picks))
	require.Equal(t, []domain.BackendTokens{{Backend: string(llm.Claude), Unreported: 1}}, meter.Usage().Backends())
}

func TestClaudeEmptyOutputIsUnreportedAndFallsBack(t *testing.T) {
	f := newFakeAgents(t, llm.Claude, llm.Codex)
	f.answer("codex", llm.SkillPickArticles, picksAnswer(URLA))
	f.codexSample(llm.SkillPickArticles)
	ctx, meter := llm.WithMeter(context.Background())

	_, backend, err := f.runner().PickArticles(ctx, pickInput())

	require.NoError(t, err)
	require.Equal(t, llm.Codex, backend)
	require.Equal(t, []domain.BackendTokens{{Backend: string(llm.Claude), Unreported: 1}, codexTokens(llm.Codex)}, meter.Usage().Backends())
}

func TestClaudeOutputCutShortIsUnreported(t *testing.T) {
	f := newFakeAgents(t, llm.Claude, llm.Codex)
	raw, err := os.ReadFile("testdata/claude-envelope.json")
	require.NoError(t, err)
	f.write(fmt.Sprintf("claude.%s.stdout", llm.SkillPickArticles), string(raw[:len(raw)/2]))
	f.answer("codex", llm.SkillPickArticles, picksAnswer(URLA))
	f.codexSample(llm.SkillPickArticles)
	ctx, meter := llm.WithMeter(context.Background())

	_, backend, err := f.runner().PickArticles(ctx, pickInput())

	require.NoError(t, err)
	require.Equal(t, llm.Codex, backend)
	require.Equal(t, []domain.BackendTokens{{Backend: string(llm.Claude), Unreported: 1}, codexTokens(llm.Codex)}, meter.Usage().Backends())
}

func TestClaudeTimeoutIsUnreportedAndFallsBack(t *testing.T) {
	f := newFakeAgents(t, llm.Claude, llm.Codex)
	f.cfg.Timeout = time.Second
	f.answer("claude", llm.SkillPickArticles, picksAnswer(URLA))
	f.write("claude.sleep", "5")
	f.answer("codex", llm.SkillPickArticles, picksAnswer(URLA))
	f.codexSample(llm.SkillPickArticles)
	ctx, meter := llm.WithMeter(context.Background())

	_, backend, err := f.runner().PickArticles(ctx, pickInput())

	require.NoError(t, err)
	require.Equal(t, llm.Codex, backend)
	require.Equal(t, []domain.BackendTokens{{Backend: string(llm.Claude), Unreported: 1}, codexTokens(llm.Codex)}, meter.Usage().Backends())
}
