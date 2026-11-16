package llm_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/sadensmol/blogger/internal/domain"
	"github.com/sadensmol/blogger/internal/llm"
)

const (
	ClaudeToken = "test-claude-token"
	AgentName   = "agent"
	LocalModel  = "qwen3:4b"
	OllamaURL   = "http://ollama:11434/v1"

	URLA = domain.URL("https://example.com/a")
	URLB = domain.URL("https://example.com/b")

	CurrentProfile = domain.Profile("# Interests\n\n## My notes\nGo and SQLite.\n\n## Learned\n\n")
)

type fakeAgents struct {
	t       *testing.T
	home    string
	jobsDir string
	cfg     llm.Config
}

func newFakeAgents(t *testing.T, order ...llm.Backend) *fakeAgents {
	t.Helper()

	claudeBin, err := filepath.Abs("testdata/fake-claude.sh")
	require.NoError(t, err)
	codexBin, err := filepath.Abs("testdata/fake-codex.sh")
	require.NoError(t, err)

	home := t.TempDir()
	return &fakeAgents{
		t:       t,
		home:    home,
		jobsDir: t.TempDir(),
		cfg: llm.Config{
			Order:       order,
			ClaudeBin:   claudeBin,
			CodexBin:    codexBin,
			ClaudeToken: ClaudeToken,
			RunAs:       llm.RunAs{Name: AgentName, Home: home},
			AgentDir:    t.TempDir(),
			Timeout:     10 * time.Second,
		},
	}
}

func (f *fakeAgents) runner() *llm.Runner {
	f.t.Helper()

	backends, err := llm.NewBackends(f.cfg)
	require.NoError(f.t, err)
	return llm.NewRunner(backends, f.jobsDir, llm.RunAs{})
}

func (f *fakeAgents) write(name, content string) {
	f.t.Helper()

	require.NoError(f.t, os.WriteFile(filepath.Join(f.home, name), []byte(content), 0o600))
}

// answer sets the canned answer of program "claude" or "codex" for one skill.
func (f *fakeAgents) answer(program string, skill llm.Skill, obj any) {
	f.t.Helper()

	if program == "codex" {
		out, err := json.Marshal(obj)
		require.NoError(f.t, err)
		f.write(fmt.Sprintf("codex.%s.output", skill), string(out))
		return
	}

	raw, err := os.ReadFile("testdata/claude-envelope.json")
	require.NoError(f.t, err)
	var envelope map[string]any
	require.NoError(f.t, json.Unmarshal(raw, &envelope))
	envelope["structured_output"] = obj
	out, err := json.Marshal(envelope)
	require.NoError(f.t, err)
	f.write(fmt.Sprintf("claude.%s.stdout", skill), string(out))
}

func (f *fakeAgents) fail(program string) {
	f.write(fmt.Sprintf("%s.exit", program), "1")
}

func (f *fakeAgents) read(name string) string {
	f.t.Helper()

	raw, err := os.ReadFile(filepath.Join(f.home, name))
	require.NoError(f.t, err)
	return string(raw)
}

func (f *fakeAgents) envKeys(program string) []string {
	return strings.Fields(f.read(fmt.Sprintf("%s.env", program)))
}

func (f *fakeAgents) requireNotRun(program string) {
	f.t.Helper()

	require.NoFileExists(f.t, filepath.Join(f.home, fmt.Sprintf("%s.args", program)))
}

func pickInput() llm.PickInput {
	return llm.PickInput{
		Candidates: []domain.Article{
			{ID: 1, SourceID: 1, SourceTitle: "Example", URL: URLA, Title: "A", PublishedAt: time.Date(2026, 11, 13, 9, 0, 0, 0, time.UTC)},
			{ID: 2, SourceID: 1, SourceTitle: "Example", URL: URLB, Title: "B", PublishedAt: time.Date(2026, 11, 12, 9, 0, 0, 0, time.UTC)},
		},
		Profile: CurrentProfile,
		Count:   domain.PickCount{Max: 5},
	}
}

func picksAnswer(urls ...domain.URL) map[string]any {
	picks := make([]map[string]any, 0, len(urls))
	for _, u := range urls {
		picks = append(picks, map[string]any{"url": u, "description": "A post.", "topics": []string{"go"}, "reason": "Matches Go."})
	}
	return map[string]any{"picks": picks}
}

func blogsAnswer() map[string]any {
	return map[string]any{"blogs": []map[string]any{{
		"site_url": "https://new.example.com",
		"feed_url": "https://new.example.com/feed.xml",
		"title":    "New",
		"reason":   "Writes about Go.",
	}}}
}

func pickURLs(picks []domain.Pick) []domain.URL {
	urls := make([]domain.URL, 0, len(picks))
	for _, p := range picks {
		urls = append(urls, p.URL)
	}
	return urls
}

func TestNewBackendsKeepsOrder(t *testing.T) {
	backends, err := llm.NewBackends(llm.Config{Order: []llm.Backend{llm.Codex, llm.Claude}})

	require.NoError(t, err)
	names := make([]llm.Backend, 0, len(backends))
	for _, b := range backends {
		names = append(names, b.Name())
	}
	require.Equal(t, []llm.Backend{llm.Codex, llm.Claude}, names)
}

func TestNewBackendsRejectsUnknownName(t *testing.T) {
	_, err := llm.NewBackends(llm.Config{Order: []llm.Backend{llm.Claude, "local"}})

	require.ErrorContains(t, err, "local")
}

func TestPickArticlesUsesClaudeFirst(t *testing.T) {
	f := newFakeAgents(t, llm.Claude, llm.Codex)
	f.answer("claude", llm.SkillPickArticles, picksAnswer(URLA, URLB))
	f.answer("codex", llm.SkillPickArticles, picksAnswer(URLA))

	picks, backend, err := f.runner().PickArticles(context.Background(), pickInput())

	require.NoError(t, err)
	require.Equal(t, llm.Claude, backend)
	require.Equal(t, []domain.URL{URLA, URLB}, pickURLs(picks))
	f.requireNotRun("codex")
}

func TestFallsBackToCodexWhenClaudeExits1(t *testing.T) {
	f := newFakeAgents(t, llm.Claude, llm.Codex)
	f.answer("claude", llm.SkillPickArticles, picksAnswer(URLA, URLB))
	f.fail("claude")
	f.answer("codex", llm.SkillPickArticles, picksAnswer(URLA))

	picks, backend, err := f.runner().PickArticles(context.Background(), pickInput())

	require.NoError(t, err)
	require.Equal(t, llm.Codex, backend)
	require.Len(t, picks, 1)
}

func TestFallsBackWhenPickURLNotInCandidates(t *testing.T) {
	f := newFakeAgents(t, llm.Claude, llm.Codex)
	f.answer("claude", llm.SkillPickArticles, picksAnswer("https://evil.example/x"))
	f.answer("codex", llm.SkillPickArticles, picksAnswer(URLA))

	_, backend, err := f.runner().PickArticles(context.Background(), pickInput())

	require.NoError(t, err)
	require.Equal(t, llm.Codex, backend)
}

func TestFallsBackWhenPicksAreBelowMin(t *testing.T) {
	f := newFakeAgents(t, llm.Claude, llm.Codex)
	f.answer("claude", llm.SkillPickArticles, picksAnswer())
	f.answer("codex", llm.SkillPickArticles, picksAnswer(URLA))
	in := pickInput()
	in.Count.Min = 1

	picks, backend, err := f.runner().PickArticles(context.Background(), in)

	require.NoError(t, err)
	require.Equal(t, llm.Codex, backend)
	require.Equal(t, []domain.URL{URLA}, pickURLs(picks))
}

func TestAgentPathStartsWithTheAgentHomeBin(t *testing.T) {
	f := newFakeAgents(t, llm.Claude)
	f.answer("claude", llm.SkillPickArticles, picksAnswer(URLA))

	_, _, err := f.runner().PickArticles(context.Background(), pickInput())

	require.NoError(t, err)
	require.Equal(t, filepath.Join(f.home, ".local", "bin"), strings.TrimSpace(f.read("claude.path")))
}

func TestFallsBackOnTimeout(t *testing.T) {
	f := newFakeAgents(t, llm.Claude, llm.Codex)
	f.cfg.Timeout = time.Second
	f.answer("claude", llm.SkillPickArticles, picksAnswer(URLA))
	f.write("claude.sleep", "5")
	f.answer("codex", llm.SkillPickArticles, picksAnswer(URLA))

	start := time.Now()
	_, backend, err := f.runner().PickArticles(context.Background(), pickInput())

	require.NoError(t, err)
	require.Equal(t, llm.Codex, backend)
	require.Less(t, time.Since(start), 4*time.Second)
	sleepPID, err := strconv.Atoi(strings.TrimSpace(f.read("claude.sleep.pid")))
	require.NoError(t, err)
	require.Eventually(t, func() bool { return errors.Is(syscall.Kill(sleepPID, 0), syscall.ESRCH) },
		2*time.Second, 20*time.Millisecond, "the agent's child process outlived the timeout")
}

func TestAllBackendsFailReturnsErrAllBackendsFailed(t *testing.T) {
	f := newFakeAgents(t, llm.Claude, llm.Codex)
	f.fail("claude")
	f.fail("codex")

	_, _, err := f.runner().PickArticles(context.Background(), pickInput())

	require.ErrorIs(t, err, llm.ErrAllBackendsFailed)
}

func TestCancelDuringLastBackendReturnsContextError(t *testing.T) {
	f := newFakeAgents(t, llm.Claude)
	f.answer("claude", llm.SkillPickArticles, picksAnswer(URLA))
	f.write("claude.sleep", "5")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	time.AfterFunc(300*time.Millisecond, cancel)

	_, _, err := f.runner().PickArticles(ctx, pickInput())

	require.ErrorIs(t, err, context.Canceled)
	require.NotErrorIs(t, err, llm.ErrAllBackendsFailed)
}

func TestCodexOSSSkippedWithoutModel(t *testing.T) {
	f := newFakeAgents(t, llm.CodexOSS)
	f.answer("codex", llm.SkillPickArticles, picksAnswer(URLA))

	_, _, err := f.runner().PickArticles(context.Background(), pickInput())

	require.ErrorIs(t, err, llm.ErrAllBackendsFailed)
	f.requireNotRun("codex")
}

func TestCodexOSSPassesFlagsAndBaseURL(t *testing.T) {
	f := newFakeAgents(t, llm.CodexOSS)
	f.cfg.LocalModel = LocalModel
	f.cfg.OllamaURL = OllamaURL
	f.answer("codex", llm.SkillPickArticles, picksAnswer(URLA))

	_, backend, err := f.runner().PickArticles(context.Background(), pickInput())

	require.NoError(t, err)
	require.Equal(t, llm.CodexOSS, backend)
	require.Contains(t, f.read("codex.args"), "--oss --local-provider ollama -m qwen3:4b")
	require.Contains(t, f.envKeys("codex"), "CODEX_OSS_BASE_URL")
}

func TestDiscoverUsesWebToolsAndNeverCodexOSS(t *testing.T) {
	sources := []domain.Source{{ID: 1, SiteURL: "https://example.com/", FeedURL: "https://example.com/feed.xml", Title: "Example", Status: domain.SourceActive}}

	claude := newFakeAgents(t, llm.Claude)
	claude.answer("claude", llm.SkillDiscoverBlogs, blogsAnswer())
	_, backend, err := claude.runner().DiscoverBlogs(context.Background(), sources, nil, 3)
	require.NoError(t, err)
	require.Equal(t, llm.Claude, backend)
	require.Contains(t, claude.read("claude.args"), "WebSearch")

	local := newFakeAgents(t, llm.CodexOSS)
	local.cfg.LocalModel = LocalModel
	local.answer("codex", llm.SkillDiscoverBlogs, blogsAnswer())
	_, _, err = local.runner().DiscoverBlogs(context.Background(), sources, nil, 3)
	require.ErrorIs(t, err, llm.ErrAllBackendsFailed)
	local.requireNotRun("codex")
}

func TestClaudeGetsOnlyItsLogin(t *testing.T) {
	t.Setenv("SLACK_BOT_TOKEN", "x")
	f := newFakeAgents(t, llm.Claude)
	f.answer("claude", llm.SkillPickArticles, picksAnswer(URLA))

	_, _, err := f.runner().PickArticles(context.Background(), pickInput())

	require.NoError(t, err)
	require.Contains(t, f.envKeys("claude"), "CLAUDE_CODE_OAUTH_TOKEN")
	require.NotContains(t, f.envKeys("claude"), "SLACK_BOT_TOKEN")
}

func TestAgentGetsUser(t *testing.T) {
	f := newFakeAgents(t, llm.Claude)
	f.answer("claude", llm.SkillPickArticles, picksAnswer(URLA))

	_, _, err := f.runner().PickArticles(context.Background(), pickInput())

	require.NoError(t, err)
	require.Contains(t, f.envKeys("claude"), "USER")
}

func TestCodexNeverGetsClaudeToken(t *testing.T) {
	t.Setenv("CLAUDE_CODE_OAUTH_TOKEN", "x")
	f := newFakeAgents(t, llm.Claude, llm.Codex)
	f.fail("claude")
	f.answer("codex", llm.SkillPickArticles, picksAnswer(URLA))

	_, backend, err := f.runner().PickArticles(context.Background(), pickInput())

	require.NoError(t, err)
	require.Equal(t, llm.Codex, backend)
	require.NotContains(t, f.envKeys("codex"), "CLAUDE_CODE_OAUTH_TOKEN")
}

func TestUpdateProfileWithoutLearnedFallsBack(t *testing.T) {
	f := newFakeAgents(t, llm.Claude, llm.Codex)
	f.answer("claude", llm.SkillUpdateProfile, map[string]any{"interests_md": "# Interests\n\n## My notes\nGo and SQLite.\n"})
	f.answer("codex", llm.SkillUpdateProfile, map[string]any{"interests_md": "# Interests\n\n## My notes\nGo and SQLite.\n\n## Learned\nLikes databases.\n"})
	votes := []domain.VotedArticle{{Article: pickInput().Candidates[0], Tags: []domain.Tag{"go"}, Vote: domain.VoteUp}}

	_, backend, err := f.runner().UpdateProfile(context.Background(), CurrentProfile, votes, nil)

	require.NoError(t, err)
	require.Equal(t, llm.Codex, backend)
}

func TestJobFolderRemovedAfterSuccessKeptAfterFailure(t *testing.T) {
	f := newFakeAgents(t, llm.Claude)
	f.answer("claude", llm.SkillPickArticles, picksAnswer(URLA))
	runner := f.runner()

	_, _, err := runner.PickArticles(context.Background(), pickInput())
	require.NoError(t, err)
	entries, err := os.ReadDir(f.jobsDir)
	require.NoError(t, err)
	require.Empty(t, entries)

	f.fail("claude")
	_, _, err = runner.PickArticles(context.Background(), pickInput())
	require.ErrorIs(t, err, llm.ErrAllBackendsFailed)
	entries, err = os.ReadDir(f.jobsDir)
	require.NoError(t, err)
	require.Len(t, entries, 1)
	require.FileExists(t, filepath.Join(f.jobsDir, entries[0].Name(), "input.json"))
	require.FileExists(t, filepath.Join(f.jobsDir, entries[0].Name(), "stderr.log"))
}

func TestClaudeAutoUpdateIsOff(t *testing.T) {
	f := newFakeAgents(t, llm.Claude)
	f.answer("claude", llm.SkillPickArticles, picksAnswer(URLA))

	_, _, err := f.runner().PickArticles(context.Background(), pickInput())

	require.NoError(t, err)
	require.Contains(t, f.envKeys("claude"), "DISABLE_AUTOUPDATER")
}
