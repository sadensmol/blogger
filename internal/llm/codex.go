package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/sadensmol/blogger/internal/domain"
)

// CodexBackend is not named Codex: that name is the Backend value.
type CodexBackend struct {
	cfg Config
}

func NewCodex(cfg Config) *CodexBackend {
	return &CodexBackend{cfg: cfg}
}

func (c *CodexBackend) Name() Backend {
	return Codex
}

func (c *CodexBackend) Supports(Job) bool {
	return true
}

func (c *CodexBackend) Answer(ctx context.Context, job Job) ([]byte, error) {
	var flags []string
	if job.NeedsWeb {
		flags = append(flags, "-c", `web_search="live"`)
	}
	return c.answer(ctx, job, flags, c.login())
}

func (c *CodexBackend) login() []string {
	return []string{fmt.Sprintf("CODEX_HOME=%s", filepath.Join(c.cfg.RunAs.Home, ".codex"))}
}

type codexEvent struct {
	Type  string `json:"type"`
	Usage *struct {
		InputTokens  int64 `json:"input_tokens"`
		OutputTokens int64 `json:"output_tokens"`
	} `json:"usage"`
}

// Usage sums the turn.completed events that --json prints to stdout.log. A line that is not JSON
// means the event stream was cut, so its counts are not complete and the run counts as not reported.
func (c *CodexBackend) Usage(ctx context.Context, job Job) (int64, int64, error) {
	raw, err := readAgentFile(ctx, filepath.Join(job.Dir, stdoutFile))
	if err != nil {
		return 0, 0, fmt.Errorf("cannot read codex events: %w", err)
	}

	var input, output int64
	turns := 0
	for line := range bytes.Lines(raw) {
		if len(bytes.TrimSpace(line)) == 0 {
			continue
		}
		var event codexEvent
		if err := json.Unmarshal(line, &event); err != nil {
			return 0, 0, fmt.Errorf("cannot decode codex event: %w", err)
		}
		if event.Type != "turn.completed" || event.Usage == nil {
			continue
		}
		input += event.Usage.InputTokens
		output += event.Usage.OutputTokens
		turns++
	}
	if turns == 0 {
		return 0, 0, fmt.Errorf("codex events have no turn.completed usage: %w", domain.ErrBadOutput)
	}
	return input, output, nil
}

// answer puts flags before the prompt; the prompt stays the last argument.
func (c *CodexBackend) answer(ctx context.Context, job Job, flags, login []string) ([]byte, error) {
	output := filepath.Join(job.Dir, outDirName, outputFile)
	if err := os.Remove(output); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return nil, fmt.Errorf("cannot remove old output: %w", err)
	}

	args := []string{
		"exec", "--skip-git-repo-check", "--ephemeral",
		"-s", "read-only",
		"-C", c.cfg.AgentDir,
		"--output-schema", filepath.Join(job.Dir, schemaFile),
		"-o", output,
		"--json",
	}
	args = append(args, flags...)
	args = append(args, fmt.Sprintf("Use the $%s skill. Input: %s. Answer with the JSON object only.", job.Skill, filepath.Join(job.Dir, inputFile)))

	err := runProcess(ctx, processData{
		Bin:      c.cfg.CodexBin,
		Args:     args,
		Env:      login,
		RunAs:    c.cfg.RunAs,
		WorkDir:  c.cfg.AgentDir,
		JobDir:   job.Dir,
		Timeout:  c.cfg.Timeout,
	})
	if err != nil {
		return nil, err
	}

	raw, err := readAgentFile(ctx, output)
	if err != nil {
		return nil, fmt.Errorf("cannot read codex output: %w", err)
	}
	return raw, nil
}
