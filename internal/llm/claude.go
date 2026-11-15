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

// claudeUsage is apart from claudeEnvelope: a usage field of an unexpected shape must not fail the answer.
type claudeUsage struct {
	Usage *struct {
		InputTokens              int64 `json:"input_tokens"`
		CacheCreationInputTokens int64 `json:"cache_creation_input_tokens"`
		CacheReadInputTokens     int64 `json:"cache_read_input_tokens"`
		OutputTokens             int64 `json:"output_tokens"`
	} `json:"usage"`
	ModelUsage map[string]struct {
		InputTokens              int64 `json:"inputTokens"`
		CacheCreationInputTokens int64 `json:"cacheCreationInputTokens"`
		CacheReadInputTokens     int64 `json:"cacheReadInputTokens"`
		OutputTokens             int64 `json:"outputTokens"`
	} `json:"modelUsage"`
}

func (c *ClaudeBackend) Usage(ctx context.Context, job Job) (int64, int64, error) {
	raw, err := readAgentFile(ctx, filepath.Join(job.Dir, stdoutFile))
	if err != nil {
		return 0, 0, fmt.Errorf("cannot read claude output: %w", err)
	}
	var report claudeUsage
	if err := json.Unmarshal(raw, &report); err != nil {
		return 0, 0, fmt.Errorf("cannot decode claude usage: %w", err)
	}

	if len(report.ModelUsage) > 0 {
		var input, output int64
		for _, model := range report.ModelUsage {
			input += model.InputTokens + model.CacheCreationInputTokens + model.CacheReadInputTokens
			output += model.OutputTokens
		}
		return input, output, nil
	}
	if usage := report.Usage; usage != nil {
		return usage.InputTokens + usage.CacheCreationInputTokens + usage.CacheReadInputTokens, usage.OutputTokens, nil
	}
	return 0, 0, fmt.Errorf("claude output has no usage: %w", domain.ErrBadOutput)
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

	// the native install updates itself; the pinned version is the one the tests saw
	env := []string{"DISABLE_AUTOUPDATER=1"}
	if c.cfg.ClaudeToken != "" {
		env = append(env, fmt.Sprintf("CLAUDE_CODE_OAUTH_TOKEN=%s", c.cfg.ClaudeToken))
	}

	err = runProcess(ctx, processData{
		Bin:      c.cfg.ClaudeBin,
		Args:     args,
		Env:      env,
		RunAs:    c.cfg.RunAs,
		WorkDir:  c.cfg.AgentDir,
		JobDir:   job.Dir,
		Timeout:  c.cfg.Timeout,
	})
	if err != nil {
		return nil, err
	}

	raw, err := readAgentFile(ctx, filepath.Join(job.Dir, stdoutFile))
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
