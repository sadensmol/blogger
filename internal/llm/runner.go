package llm

import (
	"bytes"
	"context"
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/rs/zerolog/log"
)

const (
	inputFile  = "input.json"
	schemaFile = "schema.json"
	outputFile = "output.json"
	stdoutFile = "stdout.log"
	stderrFile = "stderr.log"
)

var ErrAllBackendsFailed = errors.New("all backends failed")

//go:embed schema/*.json
var schemas embed.FS

type Runner struct {
	backends []IBackend
	jobsDir  string
	runAs    RunAs
}

// NewRunner takes runAs to give the job folders to the agent user.
func NewRunner(backends []IBackend, jobsDir string, runAs RunAs) *Runner {
	return &Runner{backends: backends, jobsDir: jobsDir, runAs: runAs}
}

type jobRequest struct {
	skill    Skill
	needsWeb bool
	input    any
}

// run keeps the job folder after a failure: its input and stderr.log are the evidence.
func (r *Runner) run(ctx context.Context, req jobRequest, accept func(answer []byte) error) (Backend, error) {
	job, err := r.newJob(req)
	if err != nil {
		return "", err
	}

	for _, backend := range r.backends {
		if err := ctx.Err(); err != nil {
			return "", fmt.Errorf("%s job stopped: %w", job.Skill, err)
		}
		if !backend.Supports(job) {
			continue
		}
		answer, err := backend.Answer(ctx, job)
		if err != nil {
			log.Ctx(ctx).Warn().Err(err).Msgf("%s job in %s failed on %s", job.Skill, job.Dir, backend.Name())
			continue
		}
		if err := accept(answer); err != nil {
			log.Ctx(ctx).Warn().Err(err).Msgf("%s job in %s got a bad answer from %s", job.Skill, job.Dir, backend.Name())
			continue
		}
		if err := os.RemoveAll(job.Dir); err != nil {
			log.Ctx(ctx).Warn().Err(err).Msgf("cannot remove job folder %s", job.Dir)
		}
		return backend.Name(), nil
	}
	if err := ctx.Err(); err != nil {
		return "", fmt.Errorf("%s job stopped: %w", job.Skill, err)
	}
	return "", fmt.Errorf("%s: %w", job.Skill, ErrAllBackendsFailed)
}

func (r *Runner) newJob(req jobRequest) (Job, error) {
	schema, err := schemas.ReadFile(fmt.Sprintf("schema/%s.json", req.skill))
	if err != nil {
		return Job{}, fmt.Errorf("cannot read %s schema: %w", req.skill, err)
	}
	input, err := json.Marshal(req.input)
	if err != nil {
		return Job{}, fmt.Errorf("cannot encode %s input: %w", req.skill, err)
	}

	dir := filepath.Join(r.jobsDir, fmt.Sprintf("%d-%s", time.Now().UnixNano(), req.skill))
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return Job{}, fmt.Errorf("cannot create job folder: %w", err)
	}
	if err := os.WriteFile(filepath.Join(dir, inputFile), input, 0o600); err != nil {
		return Job{}, fmt.Errorf("cannot write job input: %w", err)
	}
	if err := os.WriteFile(filepath.Join(dir, schemaFile), schema, 0o600); err != nil {
		return Job{}, fmt.Errorf("cannot write job schema: %w", err)
	}
	// the agent reads the input and schema and writes its output into the job folder
	if cred := r.runAs.Credential; cred != nil {
		for _, path := range []string{r.jobsDir, dir, filepath.Join(dir, inputFile), filepath.Join(dir, schemaFile)} {
			if err := os.Chown(path, int(cred.Uid), int(cred.Gid)); err != nil {
				return Job{}, fmt.Errorf("cannot give %s to the agent user: %w", path, err)
			}
		}
	}
	return Job{Skill: req.skill, Dir: dir, NeedsWeb: req.needsWeb}, nil
}

func decodeAnswer(answer []byte, v any) error {
	decoder := json.NewDecoder(bytes.NewReader(answer))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(v); err != nil {
		return fmt.Errorf("cannot decode answer: %w", err)
	}
	return nil
}
