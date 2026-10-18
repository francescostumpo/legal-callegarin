//go:build !e2e

package main

import (
	"context"
	"log/slog"
	"net/http"
	"time"

	"github.com/francescostumpo/legal-callegarin/internal/config"
	storagebundle "github.com/francescostumpo/legal-callegarin/internal/storage"
)

func prepareRuntimeEnvironment(getenv func(string) string) (func(string) string, error) {
	return getenv, nil
}

func storageNow() time.Time { return time.Now() }

func seedRuntimeData(context.Context, *storagebundle.Bundle) error { return nil }

func serveRuntimeHTTP(ctx context.Context, cfg config.Config, handler http.Handler, logger *slog.Logger) error {
	return serveHTTP(ctx, cfg, handler, logger)
}
