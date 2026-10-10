package main

import (
	"context"
	"git-exporter/internal/cli"
	"os"
	"os/signal"
)

// main runs the CLI with interrupt cancellation and exits with a failure status on error.
func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	command := cli.New()
	if command.ExecuteContext(ctx) != nil {
		os.Exit(1)
	}
}
