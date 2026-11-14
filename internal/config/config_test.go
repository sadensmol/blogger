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
