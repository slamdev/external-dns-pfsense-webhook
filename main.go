package main

import (
	"log/slog"
	"os"

	"github.com/slamdev/external-dns-pfsense-webhook/pkg"
	"github.com/slamdev/external-dns-pfsense-webhook/pkg/integration"

	_ "golang.org/x/mod/modfile" // transitive dependency that is not recognized by go mod tidy
)

func main() {
	app, err := pkg.NewApp()
	if err != nil {
		slog.Error("failed to create app", "err", err)
		os.Exit(1)
	}

	go func() {
		slog.Info("starting app")
		if err := app.Start(); err != nil {
			slog.Error("failed to start app", "err", err)
			os.Exit(1)
		}
	}()
	slog.Info("app is running")

	if err := integration.WaitForShutdown(app.Stop); err != nil {
		slog.Error("failed to stop app", "err", err)
		os.Exit(1)
	}

	slog.Info("app is stopped")
}
