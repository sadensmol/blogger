package controllercli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/rs/zerolog/log"

	"github.com/sadensmol/blogger/internal/domain"
	"github.com/sadensmol/blogger/internal/service"
)

const (
	profileUsage     = "usage: blogger profile show | set-notes <file|->"
	noInterestsYet   = "No interests file yet."
	notesSaved       = "Notes saved."
	notesCutSection = "the notes cannot have a line that starts with \"## \"; it would cut the notes section"
)

type ProfileData struct {
	Svc  *service.Profile
	Args []string
	In   io.Reader
	Out  io.Writer
}

func Profile(ctx context.Context, d ProfileData) error {
	if len(d.Args) == 0 {
		return errors.New(profileUsage)
	}
	cmd, args := d.Args[0], d.Args[1:]
	switch {
	case cmd == "show" && len(args) == 0:
		return showProfile(ctx, d)
	case cmd == "set-notes" && len(args) == 1:
		return setNotes(ctx, d, args[0])
	default:
		return errors.New(profileUsage)
	}
}

func showProfile(ctx context.Context, d ProfileData) error {
	profile, err := d.Svc.Current(ctx)
	if errors.Is(err, domain.ErrNotFound) {
		return printLine(d.Out, noInterestsYet)
	}
	if err != nil {
		return err
	}
	if _, err := io.WriteString(d.Out, string(profile)); err != nil {
		return fmt.Errorf("cannot write interests: %w", err)
	}
	return nil
}

func setNotes(ctx context.Context, d ProfileData, source string) error {
	var notes string
	var err error
	if source == "-" {
		notes, err = readAll(d.In)
	} else {
		notes, err = readFile(ctx, source)
	}
	if err != nil {
		return err
	}
	for _, line := range strings.Split(notes, "\n") {
		if strings.HasPrefix(line, "## ") {
			return errors.New(notesCutSection)
		}
	}
	if err := d.Svc.SetNotes(ctx, notes); err != nil {
		return err
	}
	return printLine(d.Out, notesSaved)
}

func readAll(in io.Reader) (string, error) {
	data, err := io.ReadAll(in)
	if err != nil {
		return "", fmt.Errorf("cannot read notes: %w", err)
	}
	return string(data), nil
}

func readFile(ctx context.Context, path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", fmt.Errorf("cannot open %s: %w", path, err)
	}
	defer func() {
		if err := f.Close(); err != nil {
			log.Ctx(ctx).Error().Err(err).Msgf("cannot close %s", path)
		}
	}()
	return readAll(f)
}
