package llm_test

import (
	"os"
	"os/user"
	"strconv"
	"syscall"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/sadensmol/blogger/internal/llm"
)

const (
	rootUID    = 0
	nonRootUID = 1000
)

func currentUser(t *testing.T) *user.User {
	t.Helper()

	u, err := user.Current()
	require.NoError(t, err)
	return u
}

func TestRunAsSwitchesToAgentUserWhenRoot(t *testing.T) {
	agent := currentUser(t)
	uid, err := strconv.ParseUint(agent.Uid, 10, 32)
	require.NoError(t, err)
	gid, err := strconv.ParseUint(agent.Gid, 10, 32)
	require.NoError(t, err)

	runAs, err := llm.NewRunAs(agent.Username, rootUID)

	require.NoError(t, err)
	require.Equal(t, llm.RunAs{
		Name:       agent.Username,
		Home:       agent.HomeDir,
		Credential: &syscall.Credential{Uid: uint32(uid), Gid: uint32(gid)},
	}, runAs)
}

func TestRunAsKeepsBotUser(t *testing.T) {
	home, err := os.UserHomeDir()
	require.NoError(t, err)
	tests := []struct {
		name, agentUser string
		euid            int
	}{
		{name: "root without agent user", euid: rootUID},
		{name: "not root with agent user", agentUser: currentUser(t).Username, euid: nonRootUID},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			runAs, err := llm.NewRunAs(tt.agentUser, tt.euid)

			require.NoError(t, err)
			require.Nil(t, runAs.Credential)
			require.Equal(t, home, runAs.Home)
			require.Equal(t, currentUser(t).Username, runAs.Name)
		})
	}
}

func TestRunAsUnknownAgentUserFails(t *testing.T) {
	_, err := llm.NewRunAs("blogger-no-such-user", rootUID)

	require.ErrorContains(t, err, "blogger-no-such-user")
}
