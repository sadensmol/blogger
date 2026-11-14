package service

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"time"

	"github.com/rs/zerolog/log"

	"github.com/sadensmol/blogger/internal/domain"
	"github.com/sadensmol/blogger/internal/repository"
)

type Profile struct {
	path string
	repo *repository.ProfileVersion
	now  func() time.Time
}

func NewProfile(path string, repo *repository.ProfileVersion, now func() time.Time) *Profile {
	return &Profile{path: path, repo: repo, now: now}
}

func (p *Profile) Current(ctx context.Context) (domain.Profile, error) {
	file, err := p.readFile()
	if err == nil {
		return file, nil
	}
	if !errors.Is(err, fs.ErrNotExist) {
		return "", err
	}
	latest, err := p.repo.FindLatest(ctx)
	if err != nil {
		return "", fmt.Errorf("cannot restore interests file: %w", err)
	}
	if err := p.writeFile(ctx, latest); err != nil {
		return "", err
	}
	return latest, nil
}

func (p *Profile) Save(ctx context.Context, next domain.Profile, change domain.ProfileChange) error {
	current, err := p.Current(ctx)
	if err != nil && !errors.Is(err, domain.ErrNotFound) {
		return err
	}
	next, err = next.WithNotesFrom(current)
	if err != nil {
		return fmt.Errorf("cannot keep user notes: %w", err)
	}
	return p.store(ctx, next, change)
}

func (p *Profile) SetNotes(ctx context.Context, notes string) error {
	current, err := p.Current(ctx)
	if err != nil && !errors.Is(err, domain.ErrNotFound) {
		return err
	}
	return p.store(ctx, current.WithNotes(notes), domain.ProfileNotes)
}

func (p *Profile) LastLearnedAt(ctx context.Context) (time.Time, error) {
	at, err := p.repo.LastCreatedAtOfInitOrUpdate(ctx)
	if err != nil {
		return time.Time{}, fmt.Errorf("cannot find last learning time: %w", err)
	}
	return at, nil
}

func (p *Profile) store(ctx context.Context, profile domain.Profile, change domain.ProfileChange) error {
	if err := p.writeFile(ctx, profile); err != nil {
		return err
	}
	if err := p.repo.Create(ctx, profile, change, p.now()); err != nil {
		return fmt.Errorf("cannot store %s profile version: %w", change, err)
	}
	return nil
}

func (p *Profile) readFile() (domain.Profile, error) {
	data, err := os.ReadFile(p.path)
	if err != nil {
		return "", fmt.Errorf("cannot read interests file %s: %w", p.path, err)
	}
	return domain.Profile(data), nil
}

func (p *Profile) writeFile(ctx context.Context, profile domain.Profile) (err error) {
	tmp, err := os.CreateTemp(filepath.Dir(p.path), "interests-*.tmp")
	if err != nil {
		return fmt.Errorf("cannot create temp file for %s: %w", p.path, err)
	}
	defer func() {
		if err == nil {
			return
		}
		if closeErr := tmp.Close(); closeErr != nil && !errors.Is(closeErr, os.ErrClosed) {
			log.Ctx(ctx).Error().Err(closeErr).Msgf("cannot close temp file %s", tmp.Name())
		}
		if removeErr := os.Remove(tmp.Name()); removeErr != nil && !errors.Is(removeErr, fs.ErrNotExist) {
			log.Ctx(ctx).Error().Err(removeErr).Msgf("cannot remove temp file %s", tmp.Name())
		}
	}()
	if _, err := tmp.WriteString(string(profile)); err != nil {
		return fmt.Errorf("cannot write %s: %w", tmp.Name(), err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("cannot close %s: %w", tmp.Name(), err)
	}
	if err := os.Chmod(tmp.Name(), 0o644); err != nil {
		return fmt.Errorf("cannot set mode of %s: %w", tmp.Name(), err)
	}
	if err := os.Rename(tmp.Name(), p.path); err != nil {
		return fmt.Errorf("cannot replace %s: %w", p.path, err)
	}
	return nil
}
