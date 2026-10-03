package main

import (
	"context"
	"git-exporter/internal/cli"
	"os"
	"os/signal"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	if cli.New().ExecuteContext(ctx) != nil {
		os.Exit(1)
	}
}
