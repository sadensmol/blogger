package tests

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/sadensmol/blogger/internal/llm"
)

// FakeAgents drives internal/llm/testdata/fake-*.sh through files in Home.
type FakeAgents struct {
	Home      string
	ClaudeBin string
	CodexBin  string

	t *testing.T
}

func NewFakeAgents(t *testing.T) *FakeAgents {
	t.Helper()

	claudeBin, err := filepath.Abs("../internal/llm/testdata/fake-claude.sh")
	require.NoError(t, err)
	codexBin, err := filepath.Abs("../internal/llm/testdata/fake-codex.sh")
	require.NoError(t, err)
	return &FakeAgents{Home: t.TempDir(), ClaudeBin: claudeBin, CodexBin: codexBin, t: t}
}

// program names the fake script that serves the backend: codex-oss runs the codex binary.
func program(backend llm.Backend) string {
	switch backend {
	case llm.Claude:
		return "claude"
	case llm.Codex, llm.CodexOSS:
		return "codex"
	}
	panic(fmt.Sprintf("unknown backend %q", backend))
}

func (f *FakeAgents) Answer(backend llm.Backend, skill llm.Skill, obj any) {
	f.t.Helper()

	if program(backend) == "codex" {
		out, err := json.Marshal(obj)
		require.NoError(f.t, err)
		f.write(fmt.Sprintf("codex.%s.output", skill), out)
		return
	}

	raw, err := os.ReadFile("../internal/llm/testdata/claude-envelope.json")
	require.NoError(f.t, err)
	var envelope map[string]any
	require.NoError(f.t, json.Unmarshal(raw, &envelope))
	envelope["structured_output"] = obj
	out, err := json.Marshal(envelope)
	require.NoError(f.t, err)
	f.write(fmt.Sprintf("claude.%s.stdout", skill), out)
}

func (f *FakeAgents) Fail(backend llm.Backend) {
	f.write(fmt.Sprintf("%s.exit", program(backend)), []byte("1"))
}

func (f *FakeAgents) Sleep(backend llm.Backend, seconds int) {
	f.write(fmt.Sprintf("%s.sleep", program(backend)), fmt.Appendf(nil, "%d", seconds))
}

// Args returns the arguments of each run of the backend's program, oldest first.
// The json schema argument spans lines, so a run starts at a line with the program's first argument.
func (f *FakeAgents) Args(backend llm.Backend) []string {
	start := "exec "
	if program(backend) == "claude" {
		start = "-p "
	}
	var runs []string
	for line := range strings.Lines(string(f.read(fmt.Sprintf("%s.args", program(backend))))) {
		if strings.HasPrefix(line, start) || len(runs) == 0 {
			runs = append(runs, line)
			continue
		}
		runs[len(runs)-1] = fmt.Sprintf("%s%s", runs[len(runs)-1], line)
	}
	for i := range runs {
		runs[i] = strings.TrimSuffix(runs[i], "\n")
	}
	return runs
}

func (f *FakeAgents) Input(backend llm.Backend, skill llm.Skill) []byte {
	return f.read(fmt.Sprintf("%s.%s.input", program(backend), skill))
}

func (f *FakeAgents) Overlapped() bool {
	return len(f.read("claude.overlap")) > 0 || len(f.read("codex.overlap")) > 0
}

func (f *FakeAgents) write(name string, content []byte) {
	f.t.Helper()

	require.NoError(f.t, os.WriteFile(filepath.Join(f.Home, name), content, 0o600))
}

// read returns nothing for a file the program never wrote.
func (f *FakeAgents) read(name string) []byte {
	f.t.Helper()

	raw, err := os.ReadFile(filepath.Join(f.Home, name))
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	require.NoError(f.t, err)
	return raw
}
