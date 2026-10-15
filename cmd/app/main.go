// Command app serves a greeting over HTTP.
package main

import (
	"cmp"
	"context"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/ElAntagonista/app-go/internal/server"
)

const defaultPort = "8080"

// version is set at build time: -ldflags "-X main.version=v1.2.3".
var version = "dev"

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))

	if err := run(logger); err != nil {
		logger.Error("exiting", "error", err)
		os.Exit(1)
	}
}

func run(logger *slog.Logger) error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	port := cmp.Or(os.Getenv("PORT"), defaultPort)
	logger.Info("starting", "version", version, "port", port)

	return server.New(":"+port, logger).Run(ctx)
}
