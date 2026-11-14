package llm

import (
	"fmt"
	"os"
	"os/user"
	"strconv"
	"syscall"
)

// RunAs is the user the agent programs run as.
type RunAs struct {
	Name       string
	Home       string
	Credential *syscall.Credential // nil: the bot's own user
}

// NewRunAs switches the agents to agentUser only when the bot runs as root (euid 0).
// The agents then cannot read the bot's environment, which holds the Slack tokens.
// Otherwise, as on the Mac, the agents run as the bot's own user.
func NewRunAs(agentUser string, euid int) (RunAs, error) {
	if agentUser == "" || euid != 0 {
		home, err := os.UserHomeDir()
		if err != nil {
			return RunAs{}, fmt.Errorf("cannot find the home folder: %w", err)
		}
		return RunAs{Name: currentUserName(), Home: home}, nil
	}

	u, err := user.Lookup(agentUser)
	if err != nil {
		return RunAs{}, fmt.Errorf("cannot find agent user %q: %w", agentUser, err)
	}
	uid, err := strconv.ParseUint(u.Uid, 10, 32)
	if err != nil {
		return RunAs{}, fmt.Errorf("cannot parse uid %q of agent user %q: %w", u.Uid, agentUser, err)
	}
	gid, err := strconv.ParseUint(u.Gid, 10, 32)
	if err != nil {
		return RunAs{}, fmt.Errorf("cannot parse gid %q of agent user %q: %w", u.Gid, agentUser, err)
	}
	return RunAs{
		Name:       u.Username,
		Home:       u.HomeDir,
		Credential: &syscall.Credential{Uid: uint32(uid), Gid: uint32(gid)},
	}, nil
}

// currentUserName is needed on macOS: claude finds its keychain login only when USER is set.
func currentUserName() string {
	if u, err := user.Current(); err == nil {
		return u.Username
	}
	return os.Getenv("USER")
}
