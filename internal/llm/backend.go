package llm

import (
	"context"
	"fmt"
	"time"
)

type Backend string

const (
	Claude   Backend = "claude"
	Codex    Backend = "codex"
	CodexOSS Backend = "codex-oss"
)

type Skill string

const (
	SkillPickArticles  Skill = "pick-articles"
	SkillUpdateProfile Skill = "update-profile"
	SkillInitProfile   Skill = "init-profile"
	SkillDiscoverBlogs Skill = "discover-blogs"
)

type Job struct {
	Skill    Skill
	Dir      string // the job folder: input.json, schema.json, output.json, stdout.log, stderr.log
	NeedsWeb bool
}

type IBackend interface {
	Name() Backend
	Supports(job Job) bool // false: the runner skips the backend without a try
	Answer(ctx context.Context, job Job) ([]byte, error)
}

type Config struct {
	Order                 []Backend
	ClaudeBin, CodexBin   string
	ClaudeToken           string
	LocalModel, OllamaURL string
	AgentDir              string
	RunAs                 RunAs
	Timeout               time.Duration
}

func NewBackends(cfg Config) ([]IBackend, error) {
	backends := make([]IBackend, 0, len(cfg.Order))
	for _, name := range cfg.Order {
		switch name {
		case Claude:
			backends = append(backends, NewClaude(cfg))
		case Codex:
			backends = append(backends, NewCodex(cfg))
		case CodexOSS:
			backends = append(backends, NewCodexOSS(cfg))
		default:
			return nil, fmt.Errorf("unknown backend %q", name)
		}
	}
	return backends, nil
}
