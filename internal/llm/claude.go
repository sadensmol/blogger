package llm

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/sadensmol/blogger/internal/domain"
)

// ClaudeBackend is not named Claude: that name is the Backend value.
type ClaudeBackend struct {
	cfg Config
}

func NewClaude(cfg Config) *ClaudeBackend {
	return &ClaudeBackend{cfg: cfg}
}

func (c *ClaudeBackend) Name() Backend {
	return Claude
}

func (c *ClaudeBackend) Supports(Job) bool {
	return true
}

type claudeEnvelope struct {
	IsError          bool            `json:"is_error"`
	Subtype          string          `json:"subtype"`
	StructuredOutput json.RawMessage `json:"structured_output"`
}

func (c *ClaudeBackend) Answer(ctx context.Context, job Job) ([]byte, error) {
	schema, err := os.ReadFile(filepath.Join(job.Dir, schemaFile))
	if err != nil {
		return nil, fmt.Errorf("cannot read schema: %w", err)
	}

	args := []string{
		"-p", fmt.Sprintf("/%s Input: %s. Answer with the JSON object only.", job.Skill, filepath.Join(job.Dir, inputFile)),
		"--output-format", "json",
		"--json-schema", string(schema),
		"--allowedTools", "Read", "Skill",
	}
	if job.NeedsWeb {
		args = append(args, "WebSearch", "WebFetch")
	}
	args = append(args, "--add-dir", job.Dir, "--no-session-persistence")

	var login []string
	if c.cfg.ClaudeToken != "" {
		login = append(login, fmt.Sprintf("CLAUDE_CODE_OAUTH_TOKEN=%s", c.cfg.ClaudeToken))
	}

	err = runProcess(ctx, processData{
		Bin:      c.cfg.ClaudeBin,
		Args:     args,
		LoginEnv: login,
		Home:     c.cfg.Home,
		WorkDir:  c.cfg.AgentDir,
		JobDir:   job.Dir,
		Timeout:  c.cfg.Timeout,
	})
	if err != nil {
		return nil, err
	}

	raw, err := os.ReadFile(filepath.Join(job.Dir, stdoutFile))
	if err != nil {
		return nil, fmt.Errorf("cannot read claude output: %w", err)
	}
	var envelope claudeEnvelope
	if err := json.Unmarshal(raw, &envelope); err != nil {
		return nil, fmt.Errorf("cannot decode claude envelope: %w", err)
	}
	if envelope.IsError {
		return nil, fmt.Errorf("claude reported an error, subtype %q: %w", envelope.Subtype, domain.ErrBadOutput)
	}
	if len(envelope.StructuredOutput) == 0 || string(envelope.StructuredOutput) == "null" {
		return nil, fmt.Errorf("claude envelope has no structured_output: %w", domain.ErrBadOutput)
	}
	return envelope.StructuredOutput, nil
}
