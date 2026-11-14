package controllercli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"go.yaml.in/yaml/v3"

	"github.com/sadensmol/blogger/internal/domain"
	"github.com/sadensmol/blogger/internal/service"
)

const sourcesUsage = "usage: blogger sources add <url> | remove|pause|resume <id-or-url> | export | import [--apply] [file]"

type SourcesData struct {
	Svc  *service.Source
	Args []string
	In   io.Reader
	Out  io.Writer
}

type sourcesFile struct {
	Sources []sourceEntry `yaml:"sources"`
}

type sourceEntry struct {
	ID     int64  `yaml:"id,omitempty"`
	URL    string `yaml:"url,omitempty"`
	Feed   string `yaml:"feed,omitempty"`
	Title  string `yaml:"title,omitempty"`
	Status string `yaml:"status,omitempty"`
	Origin string `yaml:"origin,omitempty"`
	Up     int    `yaml:"up"`
	Down   int    `yaml:"down"`
}

type controllerSource domain.Source

func (c controllerSource) toEntry() sourceEntry {
	return sourceEntry{
		ID:     int64(c.ID),
		URL:    string(c.SiteURL),
		Feed:   string(c.FeedURL),
		Title:  c.Title,
		Status: string(c.Status),
		Origin: string(c.Origin),
		Up:     c.Votes.Up,
		Down:   c.Votes.Down,
	}
}

type controllerEntry sourceEntry

func (c controllerEntry) toSpec() (domain.SourceSpec, error) {
	if c.URL == "" && c.Feed == "" {
		return domain.SourceSpec{}, errors.New("entry has neither url nor feed")
	}
	spec := domain.SourceSpec{Title: c.Title, Status: domain.SourceActive}
	if c.URL != "" {
		pageURL, err := domain.ParseURL(c.URL)
		if err != nil {
			return domain.SourceSpec{}, err
		}
		spec.URL = pageURL
	}
	if c.Feed != "" {
		feedURL, err := domain.ParseURL(c.Feed)
		if err != nil {
			return domain.SourceSpec{}, err
		}
		spec.FeedURL = feedURL
	}
	if c.Status != "" {
		status, err := domain.ParseSourceStatus(c.Status)
		if err != nil {
			return domain.SourceSpec{}, err
		}
		spec.Status = status
	}
	return spec, nil
}

func Sources(ctx context.Context, d SourcesData) error {
	if len(d.Args) == 0 {
		return errors.New(sourcesUsage)
	}
	cmd, args := d.Args[0], d.Args[1:]
	switch cmd {
	case "add", "remove", "pause", "resume":
		if len(args) != 1 {
			return errors.New(sourcesUsage)
		}
		return runSourceAction(ctx, d, cmd, args[0])
	case "export":
		return exportSources(ctx, d)
	case "import":
		return importSources(ctx, d, args)
	default:
		return errors.New(sourcesUsage)
	}
}

func runSourceAction(ctx context.Context, d SourcesData, cmd, ref string) error {
	switch cmd {
	case "add":
		src, err := d.Svc.Add(ctx, ref)
		if err != nil {
			return err
		}
		_, err = fmt.Fprintf(d.Out, "Added #%d %s (%s).\n", src.ID, src.Title, src.FeedURL)
		return err
	case "remove":
		src, err := d.Svc.Remove(ctx, ref)
		if err != nil {
			return err
		}
		_, err = fmt.Fprintf(d.Out, "Removed #%d %s.\n", src.ID, src.Title)
		return err
	case "pause":
		src, err := d.Svc.Pause(ctx, ref)
		if err != nil {
			return err
		}
		_, err = fmt.Fprintf(d.Out, "Paused #%d %s.\n", src.ID, src.Title)
		return err
	default:
		src, err := d.Svc.Resume(ctx, ref)
		if err != nil {
			return err
		}
		_, err = fmt.Fprintf(d.Out, "Resumed #%d %s.\n", src.ID, src.Title)
		return err
	}
}

func exportSources(ctx context.Context, d SourcesData) error {
	sources, err := d.Svc.List(ctx)
	if err != nil {
		return err
	}
	file := sourcesFile{Sources: make([]sourceEntry, 0, len(sources))}
	for _, src := range sources {
		file.Sources = append(file.Sources, controllerSource(src).toEntry())
	}
	data, err := yaml.Marshal(file)
	if err != nil {
		return fmt.Errorf("cannot encode sources: %w", err)
	}
	if _, err := d.Out.Write(data); err != nil {
		return fmt.Errorf("cannot write sources: %w", err)
	}
	return nil
}

func importSources(ctx context.Context, d SourcesData, args []string) error {
	apply := false
	in := d.In
	for _, arg := range args {
		if arg == "--apply" {
			apply = true
			continue
		}
		f, err := os.Open(arg)
		if err != nil {
			return fmt.Errorf("cannot open %s: %w", arg, err)
		}
		defer f.Close()
		in = f
	}

	var file sourcesFile
	if err := yaml.NewDecoder(in).Decode(&file); err != nil && !errors.Is(err, io.EOF) {
		return fmt.Errorf("cannot read sources yaml: %w", err)
	}
	wanted := make([]domain.SourceSpec, 0, len(file.Sources))
	for i, entry := range file.Sources {
		spec, err := controllerEntry(entry).toSpec()
		if err != nil {
			return fmt.Errorf("cannot use sources entry %d: %w", i+1, err)
		}
		wanted = append(wanted, spec)
	}

	diff, err := d.Svc.Diff(ctx, wanted)
	if err != nil {
		return err
	}
	total := len(diff.Add) + len(diff.Remove) + len(diff.Change)
	if total == 0 {
		_, err := fmt.Fprintln(d.Out, "No changes.")
		return err
	}

	var out strings.Builder
	for _, src := range diff.Add {
		fmt.Fprintf(&out, "+ add     %s\n", src.FeedURL)
	}
	for _, src := range diff.Remove {
		fmt.Fprintf(&out, "- remove  #%d %s\n", src.ID, src.FeedURL)
	}
	for _, change := range diff.Change {
		fmt.Fprintf(&out, "~ change  #%d %s %s\n", change.Before.ID, change.Before.FeedURL, describeChange(change))
	}
	if !apply {
		fmt.Fprintf(&out, "%d changes. Run again with --apply to apply them.\n", total)
		_, err := io.WriteString(d.Out, out.String())
		return err
	}
	if err := d.Svc.Apply(ctx, diff); err != nil {
		return err
	}
	fmt.Fprintf(&out, "Applied %d changes.\n", total)
	_, err = io.WriteString(d.Out, out.String())
	return err
}

func describeChange(c domain.SourceChange) string {
	var parts []string
	if c.Before.Status != c.After.Status {
		parts = append(parts, fmt.Sprintf("status %s -> %s", c.Before.Status, c.After.Status))
	}
	if c.Before.Title != c.After.Title {
		parts = append(parts, fmt.Sprintf("title %q -> %q", c.Before.Title, c.After.Title))
	}
	if c.Before.SiteURL != c.After.SiteURL {
		parts = append(parts, fmt.Sprintf("url %s -> %s", c.Before.SiteURL, c.After.SiteURL))
	}
	return strings.Join(parts, ", ")
}
