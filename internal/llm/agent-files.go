package llm

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"syscall"
)

// handOver gives a fresh job path to the agent user. It refuses anything but a plain folder or file,
// and Lchown never follows a link, so root never gives away a file outside the job.
func handOver(path string, cred *syscall.Credential) error {
	info, err := os.Lstat(path)
	if err != nil {
		return fmt.Errorf("cannot check %s: %w", path, err)
	}
	if !info.IsDir() && !info.Mode().IsRegular() {
		return fmt.Errorf("cannot give %s to the agent user: it is a %s", path, info.Mode().Type())
	}
	if err := os.Lchown(path, int(cred.Uid), int(cred.Gid)); err != nil {
		return fmt.Errorf("cannot give %s to the agent user: %w", path, err)
	}
	return nil
}

// readAgentFile reads a file that an agent wrote. It refuses a symlink, a hard link and anything
// but a regular file, so a planted link cannot make the root bot read a root-only file.
// O_NONBLOCK keeps a planted FIFO from blocking the open.
func readAgentFile(ctx context.Context, path string) ([]byte, error) {
	f, err := os.OpenFile(path, os.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0)
	if errors.Is(err, syscall.ELOOP) {
		return nil, fmt.Errorf("cannot read %s: it is a symlink", path)
	}
	if err != nil {
		return nil, fmt.Errorf("cannot open %s: %w", path, err)
	}
	defer closeFile(ctx, f)

	info, err := f.Stat()
	if err != nil {
		return nil, fmt.Errorf("cannot check %s: %w", path, err)
	}
	if !info.Mode().IsRegular() {
		return nil, fmt.Errorf("cannot read %s: it is a %s", path, info.Mode().Type())
	}
	if stat, ok := info.Sys().(*syscall.Stat_t); !ok || stat.Nlink != 1 {
		return nil, fmt.Errorf("cannot read %s: it has more than one link", path)
	}
	raw, err := io.ReadAll(f)
	if err != nil {
		return nil, fmt.Errorf("cannot read %s: %w", path, err)
	}
	return raw, nil
}
