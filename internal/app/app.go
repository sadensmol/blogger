package app

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/rs/zerolog/log"

	"github.com/sadensmol/blogger/internal/config"
	controllercli "github.com/sadensmol/blogger/internal/controller/cli"
	"github.com/sadensmol/blogger/internal/feed"
	"github.com/sadensmol/blogger/internal/repository"
	"github.com/sadensmol/blogger/internal/service"
)

const (
	dbFileName  = "blogger.db"
	feedTimeout = 20 * time.Second
)

func SourcesCLI(ctx context.Context, cfg config.Store, args []string) error {
	db, err := repository.Open(ctx, filepath.Join(cfg.DataDir, dbFileName))
	if err != nil {
		return err
	}
	defer func() {
		if err := db.Close(); err != nil {
			log.Ctx(ctx).Error().Err(err).Msgf("cannot close database in %s", cfg.DataDir)
		}
	}()

	svc := service.NewSource(repository.NewSource(db), feed.NewClient(feedTimeout), time.Now)
	if err := controllercli.Sources(ctx, controllercli.SourcesData{Svc: svc, Args: args, In: os.Stdin, Out: os.Stdout}); err != nil {
		return fmt.Errorf("sources: %w", err)
	}
	return nil
}
