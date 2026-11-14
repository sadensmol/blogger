package config_test

import (
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/sadensmol/blogger/internal/config"
)

func setSlackEnv(t *testing.T) {
	t.Helper()
	t.Setenv("SLACK_BOT_TOKEN", "bot")
	t.Setenv("SLACK_APP_TOKEN", "app")
	t.Setenv("SLACK_CHANNEL_ID", "C1")
	t.Setenv("SLACK_OWNER_USER_ID", "U1")
}

func TestLoadServeDefaults(t *testing.T) {
	setSlackEnv(t)

	cfg, err := config.LoadServe()

	require.NoError(t, err)
	require.Equal(t, 5, cfg.DigestSize)
	require.Equal(t, "claude,codex,codex-oss", cfg.Backends)
	require.Equal(t, 3*time.Hour, cfg.FetchEvery)
	require.Equal(t, 365, cfg.HistoryDays)
	require.Equal(t, 7*24*time.Hour, cfg.RepeatAfter)
	require.Equal(t, 3, cfg.RepeatMax)
	require.Equal(t, "10:00", cfg.DigestTime)
	require.Equal(t, "Asia/Tbilisi", cfg.TimeZone)
	require.Equal(t, "/data", cfg.DataDir)
}

func TestLoadServeWithoutBotTokenFails(t *testing.T) {
	setSlackEnv(t)
	require.NoError(t, os.Unsetenv("SLACK_BOT_TOKEN"))

	_, err := config.LoadServe()

	require.ErrorContains(t, err, "SLACK_BOT_TOKEN")
}

func TestLoadServeWithBadValuesFails(t *testing.T) {
	tests := []struct {
		name, key, value string
	}{
		{name: "zero job timeout", key: "BLOGGER_JOB_TIMEOUT", value: "0s"},
		{name: "zero digest size", key: "BLOGGER_DIGEST_SIZE", value: "0"},
		{name: "zero history days", key: "BLOGGER_HISTORY_DAYS", value: "0"},
		{name: "zero repeat after", key: "BLOGGER_REPEAT_AFTER", value: "0s"},
		{name: "zero repeat max", key: "BLOGGER_REPEAT_MAX", value: "0"},
		{name: "blank claude binary", key: "BLOGGER_CLAUDE_BIN", value: " "},
		{name: "blank codex binary", key: "BLOGGER_CODEX_BIN", value: " "},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			setSlackEnv(t)
			t.Setenv(tt.key, tt.value)

			_, err := config.LoadServe()

			require.ErrorContains(t, err, tt.key)
		})
	}
}
