package main

import (
	"context"
	"fmt"
	"os"
	_ "time/tzdata"

	"github.com/rs/zerolog"

	"github.com/sadensmol/blogger/internal/app"
	"github.com/sadensmol/blogger/internal/config"
)

var version = "dev"

const usage = "usage: blogger version | sources ... | profile ..."

func main() {
	os.Exit(run(os.Args[1:]))
}

func run(args []string) int {
	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, usage)
		return 2
	}
	switch args[0] {
	case "version":
		fmt.Printf("blogger %s\n", version)
		return 0
	case "sources", "profile":
		cfg, err := config.LoadStore()
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 1
		}
		logger := zerolog.New(os.Stderr).With().Timestamp().Logger()
		cli := app.SourcesCLI
		if args[0] == "profile" {
			cli = app.ProfileCLI
		}
		if err := cli(logger.WithContext(context.Background()), cfg, args[1:]); err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 1
		}
		return 0
	default:
		fmt.Fprintln(os.Stderr, usage)
		return 2
	}
}
