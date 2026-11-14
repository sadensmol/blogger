package llm_test

import (
	"context"
	"os"
	"path/filepath"
	"syscall"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/sadensmol/blogger/internal/llm"
)

const agentFileContent = `{"picks":[]}`

// otherGroup is a group of the test user other than its primary one: a non-root user may chown to it,
// so a test can see which paths changed hands.
func otherGroup(t *testing.T) uint32 {
	t.Helper()

	groups, err := os.Getgroups()
	require.NoError(t, err)
	for _, g := range groups {
		if g != os.Getgid() {
			return uint32(g)
		}
	}
	t.Skip("the test user has no second group")
	return 0
}

func groupOf(t *testing.T, path string) uint32 {
	t.Helper()

	info, err := os.Lstat(path)
	require.NoError(t, err)
	return info.Sys().(*syscall.Stat_t).Gid
}

func TestReadAgentFileReadsRegularFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "output.json")
	require.NoError(t, os.WriteFile(path, []byte(agentFileContent), 0o600))

	raw, err := llm.ReadAgentFile(context.Background(), path)

	require.NoError(t, err)
	require.Equal(t, agentFileContent, string(raw))
}

func TestReadAgentFileRefusesLinks(t *testing.T) {
	dir := t.TempDir()
	secret := filepath.Join(dir, "root-only.db")
	require.NoError(t, os.WriteFile(secret, []byte(agentFileContent), 0o600))
	symlink := filepath.Join(dir, "symlink.json")
	require.NoError(t, os.Symlink(secret, symlink))
	hardlink := filepath.Join(dir, "hardlink.json")
	require.NoError(t, os.Link(secret, hardlink))
	fifo := filepath.Join(dir, "fifo.json")
	require.NoError(t, syscall.Mkfifo(fifo, 0o600))

	for _, path := range []string{symlink, hardlink, fifo} {
		t.Run(filepath.Base(path), func(t *testing.T) {
			_, err := llm.ReadAgentFile(context.Background(), path)

			require.ErrorContains(t, err, path)
		})
	}
}

func TestHandOverRefusesSymlink(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "root-only")
	require.NoError(t, os.Mkdir(target, 0o700))
	link := filepath.Join(dir, "out")
	require.NoError(t, os.Symlink(target, link))
	before := groupOf(t, target)

	err := llm.HandOver(link, &syscall.Credential{Uid: uint32(os.Getuid()), Gid: otherGroup(t)})

	require.ErrorContains(t, err, link)
	require.Equal(t, before, groupOf(t, target))
}

func TestNewJobHandsOverOnlyOutFolder(t *testing.T) {
	group := otherGroup(t)
	jobsDir := t.TempDir()
	before := groupOf(t, jobsDir)
	require.NotEqual(t, before, group)
	runner := llm.NewRunner(nil, jobsDir, llm.RunAs{Credential: &syscall.Credential{Uid: uint32(os.Getuid()), Gid: group}})

	job, err := runner.NewJob(llm.SkillPickArticles)

	require.NoError(t, err)
	require.Equal(t, group, groupOf(t, filepath.Join(job.Dir, llm.OutDirName)))
	for _, path := range []string{jobsDir, job.Dir, filepath.Join(job.Dir, "input.json"), filepath.Join(job.Dir, "schema.json")} {
		require.Equal(t, before, groupOf(t, path), path)
	}
}
