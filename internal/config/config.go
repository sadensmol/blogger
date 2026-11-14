package config

import (
	"fmt"
	"time"

	"github.com/caarlos0/env/v11"
)

type Store struct {
	DataDir string `env:"BLOGGER_DATA_DIR" envDefault:"/data"`
}

type Serve struct {
	Store
	SlackBotToken  string        `env:"SLACK_BOT_TOKEN,required"`
	SlackAppToken  string        `env:"SLACK_APP_TOKEN,required"`
	SlackChannelID string        `env:"SLACK_CHANNEL_ID,required"`
	SlackOwnerID   string        `env:"SLACK_OWNER_USER_ID,required"`
	DigestTime     string        `env:"BLOGGER_DIGEST_TIME" envDefault:"10:00"`
	TimeZone       string        `env:"BLOGGER_TIME_ZONE" envDefault:"Asia/Tbilisi"`
	DigestSize     int           `env:"BLOGGER_DIGEST_SIZE" envDefault:"5"`
	Backends       string        `env:"BLOGGER_BACKENDS" envDefault:"claude,codex,codex-oss"`
	FetchEvery     time.Duration `env:"BLOGGER_FETCH_EVERY" envDefault:"3h"`
	JobTimeout     time.Duration `env:"BLOGGER_JOB_TIMEOUT" envDefault:"15m"`
	AgentDir       string        `env:"BLOGGER_AGENT_DIR" envDefault:"/app/agent"`
	ClaudeBin      string        `env:"BLOGGER_CLAUDE_BIN" envDefault:"claude"`
	CodexBin       string        `env:"BLOGGER_CODEX_BIN" envDefault:"codex"`
	ClaudeToken    string        `env:"CLAUDE_CODE_OAUTH_TOKEN"`
	LocalModel     string        `env:"BLOGGER_LOCAL_MODEL"`
	OllamaURL      string        `env:"BLOGGER_OLLAMA_URL" envDefault:"http://ollama:11434/v1"`
	LogLevel       string        `env:"BLOGGER_LOG_LEVEL" envDefault:"info"`
}

func LoadStore() (Store, error) {
	var cfg Store
	if err := env.Parse(&cfg); err != nil {
		return Store{}, fmt.Errorf("cannot load store config: %w", err)
	}
	return cfg, nil
}

func LoadServe() (Serve, error) {
	var cfg Serve
	if err := env.Parse(&cfg); err != nil {
		return Serve{}, fmt.Errorf("cannot load serve config: %w", err)
	}
	return cfg, nil
}
