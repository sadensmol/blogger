package repository

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/sadensmol/blogger/internal/domain"
)

type ProfileVersion struct {
	db *sql.DB
}

func NewProfileVersion(db *sql.DB) *ProfileVersion {
	return &ProfileVersion{db: db}
}

func (r *ProfileVersion) Create(ctx context.Context, p domain.Profile, change domain.ProfileChange, now time.Time) error {
	_, err := r.db.ExecContext(ctx, `
INSERT INTO profile_version (content, change, created_at, updated_at)
VALUES (?, ?, ?, ?)`,
		string(p), change, formatTime(now), formatTime(now))
	if err != nil {
		return fmt.Errorf("cannot create %s profile version: %w", change, err)
	}
	return nil
}

func (r *ProfileVersion) FindLatest(ctx context.Context) (domain.Profile, error) {
	var content string
	err := r.db.QueryRowContext(ctx, `SELECT content FROM profile_version ORDER BY id DESC LIMIT 1`).Scan(&content)
	if errors.Is(err, sql.ErrNoRows) {
		return "", fmt.Errorf("profile version: %w", domain.ErrNotFound)
	}
	if err != nil {
		return "", fmt.Errorf("cannot find latest profile version: %w", err)
	}
	return domain.Profile(content), nil
}

func (r *ProfileVersion) LastCreatedAtOfInitOrUpdate(ctx context.Context) (time.Time, error) {
	var createdAt sql.NullString
	err := r.db.QueryRowContext(ctx,
		`SELECT MAX(created_at) FROM profile_version WHERE change IN (?, ?)`, domain.ProfileInit, domain.ProfileUpdate,
	).Scan(&createdAt)
	if err != nil {
		return time.Time{}, fmt.Errorf("cannot find last learned profile version: %w", err)
	}
	return parseOptionalTime(createdAt)
}
