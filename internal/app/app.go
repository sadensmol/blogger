package app

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
	"github.com/slack-go/slack"
	"github.com/slack-go/slack/socketmode"

	"github.com/sadensmol/blogger/internal/config"
	controllercli "github.com/sadensmol/blogger/internal/controller/cli"
	controllerslack "github.com/sadensmol/blogger/internal/controller/slack"
	"github.com/sadensmol/blogger/internal/feed"
	"github.com/sadensmol/blogger/internal/llm"
	"github.com/sadensmol/blogger/internal/repository"
	"github.com/sadensmol/blogger/internal/scheduler"
	"github.com/sadensmol/blogger/internal/service"
	"github.com/sadensmol/blogger/internal/slackpost"
	"github.com/sadensmol/blogger/internal/usecase"
)

const (
	dbFileName        = "blogger.db"
	interestsFileName = "interests.md"
	jobsDirName       = "jobs"
	jobQueueSize      = 3
	slackTimeout      = 30 * time.Second
	workerStopWait    = 20 * time.Second
	feedTimeout = 20 * time.Second
	feedHostDelay     = 500 * time.Millisecond
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

func ProfileCLI(ctx context.Context, cfg config.Store, args []string) error {
	db, err := repository.Open(ctx, filepath.Join(cfg.DataDir, dbFileName))
	if err != nil {
		return err
	}
	defer func() {
		if err := db.Close(); err != nil {
			log.Ctx(ctx).Error().Err(err).Msgf("cannot close database in %s", cfg.DataDir)
		}
	}()

	svc := service.NewProfile(filepath.Join(cfg.DataDir, interestsFileName), repository.NewProfileVersion(db), time.Now)
	if err := controllercli.Profile(ctx, controllercli.ProfileData{Svc: svc, Args: args, In: os.Stdin, Out: os.Stdout}); err != nil {
		return fmt.Errorf("profile: %w", err)
	}
	return nil
}

func Serve(ctx context.Context, cfg config.Serve, version string) error {
	ctx, stop := signal.NotifyContext(ctx, os.Interrupt, syscall.SIGTERM)
	defer stop()

	level, err := zerolog.ParseLevel(cfg.LogLevel)
	if err != nil {
		return fmt.Errorf("cannot use BLOGGER_LOG_LEVEL %q: %w", cfg.LogLevel, err)
	}
	location, err := time.LoadLocation(cfg.TimeZone)
	if err != nil {
		return fmt.Errorf("cannot use BLOGGER_TIME_ZONE %q: %w", cfg.TimeZone, err)
	}
	digestAt, err := scheduler.ParseDailyTime(cfg.DigestTime)
	if err != nil {
		return fmt.Errorf("cannot use BLOGGER_DIGEST_TIME: %w", err)
	}
	zerolog.SetGlobalLevel(level)
	log.Ctx(ctx).Info().Msgf("blogger %s starting", version)
	runAs, err := llm.NewRunAs(cfg.AgentUser, os.Geteuid())
	if err != nil {
		return fmt.Errorf("cannot use BLOGGER_AGENT_USER: %w", err)
	}
	log.Ctx(ctx).Info().Msgf("agents run as %s with home %s", runAs.Name, runAs.Home)

	db, err := repository.Open(ctx, filepath.Join(cfg.DataDir, dbFileName))
	if err != nil {
		return err
	}
	defer func() {
		if err := db.Close(); err != nil {
			log.Ctx(ctx).Error().Err(err).Msgf("cannot close database in %s", cfg.DataDir)
		}
	}()

	runs := service.NewRuns(repository.NewRun(db), time.Now)
	interrupted, err := runs.FailInterrupted(ctx)
	if err != nil {
		return err
	}
	log.Ctx(ctx).Info().Msgf("marked %d interrupted runs as failed", interrupted)

	feeds := feed.NewClient(feedTimeout, feed.WithHostDelay(feedHostDelay))
	sources := service.NewSource(repository.NewSource(db), feeds, time.Now)
	posts := service.NewPosts(repository.NewPost(db), time.Now)
	votes := service.NewVotes(service.VotesData{
		Reactions: repository.NewReaction(db),
		Posts:     posts,
		Sources:   sources,
		Now:       time.Now,
	})
	articles := service.NewArticles(service.ArticlesData{
		Sources: sources,
		Repo:    repository.NewArticle(db),
		Feeds:   feeds,
		Runs:    runs,
		History: time.Duration(cfg.HistoryDays) * 24 * time.Hour,
		Now:     time.Now,
	})

	var order []llm.Backend
	for _, name := range strings.Split(cfg.Backends, ",") {
		order = append(order, llm.Backend(strings.TrimSpace(name)))
	}
	backends, err := llm.NewBackends(llm.Config{
		Order:       order,
		ClaudeBin:   cfg.ClaudeBin,
		CodexBin:    cfg.CodexBin,
		ClaudeToken: cfg.ClaudeToken,
		LocalModel:  cfg.LocalModel,
		OllamaURL:   cfg.OllamaURL,
		AgentDir:    cfg.AgentDir,
		RunAs:       runAs,
		Timeout:     cfg.JobTimeout,
	})
	if err != nil {
		return fmt.Errorf("cannot use BLOGGER_BACKENDS: %w", err)
	}
	jobsDir := cfg.JobsDir
	if jobsDir == "" {
		jobsDir = filepath.Join(cfg.DataDir, jobsDirName)
	}
	runner := llm.NewRunner(backends, jobsDir, runAs)

	slackHTTP := &http.Client{Timeout: slackTimeout}
	api := slack.New(cfg.SlackBotToken, slack.OptionAppLevelToken(cfg.SlackAppToken), slack.OptionHTTPClient(slackHTTP))
	poster := slackpost.NewPoster(api, slackHTTP, cfg.SlackChannelID)
	jobs := usecase.NewJobs(usecase.JobsData{
		Digest: usecase.NewDigest(usecase.DigestData{
			Articles: articles,
			Sources:  sources,
			Votes:    votes,
			Profile:  service.NewProfile(filepath.Join(cfg.DataDir, interestsFileName), repository.NewProfileVersion(db), time.Now),
			Posts:    posts,
			Runs:     runs,
			LLM:      runner,
			Poster:   poster,
			Size:     cfg.DigestSize,
			Location: location,
			Now:      time.Now,
		}),
		Discovery: usecase.NewDiscovery(usecase.DiscoveryData{
			Sources: sources,
			Feeds:   feeds,
			Runs:    runs,
			LLM:     runner,
		}),
		Size: jobQueueSize,
	})

	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	workerDone := make(chan struct{})
	go func() {
		defer close(workerDone)
		jobs.Run(ctx)
	}()
	schedulerDone := make(chan struct{})
	go func() {
		defer close(schedulerDone)
		scheduler.New(scheduler.Data{
			Jobs:       jobs,
			Articles:   articles,
			Runs:       runs,
			At:         digestAt,
			Location:   location,
			FetchEvery: cfg.FetchEvery,
			Now:        time.Now,
		}).Run(ctx)
	}()
	log.Ctx(ctx).Info().Msgf("scheduler started: digest %s %s", cfg.DigestTime, location)

	handler := controllerslack.NewHandler(controllerslack.HandlerData{
		Jobs:       jobs,
		Sources:    sources,
		Votes:      votes,
		Poster:     poster,
		ChannelID:  cfg.SlackChannelID,
		OwnerID:    cfg.SlackOwnerID,
		Background: ctx,
	})
	listenErr := controllerslack.Listen(ctx, socketmode.New(api), handler)

	cancel()
	<-schedulerDone
	handler.Wait()
	select {
	case <-workerDone:
	case <-time.After(workerStopWait):
		log.Ctx(ctx).Warn().Msgf("job worker still busy after %s: closing the database under it", workerStopWait)
	}
	log.Ctx(ctx).Info().Msg("stopped")
	return listenErr
}
