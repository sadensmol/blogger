package llm

import (
	"context"
	"fmt"
)

type CodexOSSBackend struct {
	codex     *CodexBackend
	model     string
	ollamaURL string
}

func NewCodexOSS(cfg Config) *CodexOSSBackend {
	return &CodexOSSBackend{codex: NewCodex(cfg), model: cfg.LocalModel, ollamaURL: cfg.OllamaURL}
}

func (c *CodexOSSBackend) Name() Backend {
	return CodexOSS
}

func (c *CodexOSSBackend) Supports(job Job) bool {
	return c.model != "" && !job.NeedsWeb
}

func (c *CodexOSSBackend) Answer(ctx context.Context, job Job) ([]byte, error) {
	flags := []string{"--oss", "--local-provider", "ollama", "-m", c.model}
	login := append(c.codex.login(), fmt.Sprintf("CODEX_OSS_BASE_URL=%s", c.ollamaURL))
	return c.codex.answer(ctx, job, flags, login)
}

func (c *CodexOSSBackend) Usage(ctx context.Context, job Job) (int64, int64, error) {
	return c.codex.Usage(ctx, job)
}
