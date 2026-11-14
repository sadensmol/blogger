package llm

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"time"

	"github.com/rs/zerolog/log"
)

const killDelay = 10 * time.Second

type processData struct {
	Bin      string
	Args     []string
	LoginEnv []string // the program's own login; HOME and PATH are always set
	Home     string
	WorkDir  string
	JobDir   string
	Timeout  time.Duration
}

// runProcess sends stdout to a file, not a pipe: a killed program's children keep
// a pipe open, and Wait would block until they exit.
func runProcess(ctx context.Context, d processData) error {
	stdout, err := os.Create(filepath.Join(d.JobDir, stdoutFile))
	if err != nil {
		return fmt.Errorf("cannot create stdout file: %w", err)
	}
	defer closeFile(ctx, stdout)

	stderr, err := os.OpenFile(filepath.Join(d.JobDir, stderrFile), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return fmt.Errorf("cannot open stderr file: %w", err)
	}
	defer closeFile(ctx, stderr)
	if _, err := fmt.Fprintf(stderr, "--- %s\n", d.Bin); err != nil {
		return fmt.Errorf("cannot write stderr file: %w", err)
	}

	runCtx, cancel := context.WithTimeout(ctx, d.Timeout)
	defer cancel()

	cmd := exec.CommandContext(runCtx, d.Bin, d.Args...)
	cmd.Dir = d.WorkDir
	cmd.Env = append([]string{fmt.Sprintf("HOME=%s", d.Home), fmt.Sprintf("PATH=%s", os.Getenv("PATH"))}, d.LoginEnv...)
	cmd.Stdout = stdout
	cmd.Stderr = stderr
	cmd.Cancel = func() error { return cmd.Process.Signal(syscall.SIGTERM) }
	cmd.WaitDelay = killDelay

	if err := cmd.Run(); err != nil {
		if errors.Is(runCtx.Err(), context.DeadlineExceeded) {
			return fmt.Errorf("%s ran longer than %s: %w", d.Bin, d.Timeout, err)
		}
		return fmt.Errorf("%s failed: %w", d.Bin, err)
	}
	return nil
}

func closeFile(ctx context.Context, f *os.File) {
	if err := f.Close(); err != nil {
		log.Ctx(ctx).Warn().Err(err).Msgf("cannot close %s", f.Name())
	}
}
