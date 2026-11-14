package main

import (
	"fmt"
	"os"
	_ "time/tzdata"
)

var version = "dev"

const usage = "usage: blogger version"

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
	default:
		fmt.Fprintln(os.Stderr, usage)
		return 2
	}
}
