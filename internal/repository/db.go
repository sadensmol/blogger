package repository

import (
	"context"
	"database/sql"
	"fmt"
	"io/fs"
	"net/url"
	"time"

	"github.com/pressly/goose/v3"
	"github.com/rs/zerolog/log"
	_ "modernc.org/sqlite"

	"github.com/sadensmol/blogger/db"
)

func Open(ctx context.Context, path string) (*sql.DB, error) {
	pragmas := url.Values{"_pragma": {"journal_mode(WAL)", "busy_timeout(5000)", "foreign_keys(1)"}}
	dsn := (&url.URL{Scheme: "file", Opaque: url.PathEscape(path), RawQuery: pragmas.Encode()}).String()
	sqlDB, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("cannot open sqlite %s: %w", path, err)
	}
	if err := migrate(ctx, sqlDB); err != nil {
		if closeErr := sqlDB.Close(); closeErr != nil {
			log.Error().Ctx(ctx).Err(closeErr).Msgf("cannot close sqlite %s after failed migration", path)
		}
		return nil, err
	}
	return sqlDB, nil
}

func migrate(ctx context.Context, sqlDB *sql.DB) error {
	migrations, err := fs.Sub(db.Migrations, "migrations")
	if err != nil {
		return fmt.Errorf("cannot find embedded migrations: %w", err)
	}
	provider, err := goose.NewProvider(goose.DialectSQLite3, sqlDB, migrations)
	if err != nil {
		return fmt.Errorf("cannot create migration provider: %w", err)
	}
	if _, err := provider.Up(ctx); err != nil {
		return fmt.Errorf("cannot apply migrations: %w", err)
	}
	return nil
}

func formatTime(t time.Time) string {
	return t.UTC().Format(time.RFC3339)
}

func parseTime(s string) (time.Time, error) {
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		return time.Time{}, fmt.Errorf("cannot parse time %q: %w", s, err)
	}
	return t, nil
}

func parseOptionalTime(s sql.NullString) (time.Time, error) {
	if !s.Valid {
		return time.Time{}, nil
	}
	return parseTime(s.String)
}
