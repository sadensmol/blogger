package llm

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
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
