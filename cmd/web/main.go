package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/francescostumpo/legal-callegarin/internal/app"
	"github.com/francescostumpo/legal-callegarin/internal/config"
	storagebundle "github.com/francescostumpo/legal-callegarin/internal/storage"
	azurestorage "github.com/francescostumpo/legal-callegarin/internal/storage/azure"
	memorystorage "github.com/francescostumpo/legal-callegarin/internal/storage/memory"
	"github.com/francescostumpo/legal-callegarin/internal/webassets"
)

const shutdownTimeout = 20 * time.Second

const maxBuildMetadataLength = 64

var (
	buildVersion = "development"
	buildCommit  = "unknown"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := run(ctx, logger); err != nil {
		logger.Error("web server stopped", "event", "server_stopped", "outcome", "failed")
		os.Exit(1)
	}
}

func run(ctx context.Context, logger *slog.Logger) error {
	getenv, err := prepareRuntimeEnvironment(os.Getenv)
	if err != nil {
		return err
	}
	cfg, err := config.Load(getenv)
	if err != nil {
		return err
	}
	return runConfigured(ctx, cfg, logger, productionAzureRuntime{}, serveRuntimeHTTP)
}

type azureRuntime interface {
	Ensure(context.Context, config.Config) error
	Migrate(context.Context, config.Config) (azurestorage.ArticleMigrationResult, error)
	Repair(context.Context, config.Config) (azurestorage.ArticleMigrationResult, error)
	Open(context.Context, config.Config, func() time.Time) (*storagebundle.Bundle, error)
}

type productionAzureRuntime struct{}

func (productionAzureRuntime) Ensure(ctx context.Context, cfg config.Config) error {
	return azurestorage.EnsureFromConnectionString(ctx, cfg.AzureStorageConnectionString, azurestorage.DefaultResourceNames())
}

func (productionAzureRuntime) Migrate(ctx context.Context, cfg config.Config) (azurestorage.ArticleMigrationResult, error) {
	if cfg.AzureStorageConnectionString != "" {
		return azurestorage.MigrateArticleRowsFromConnectionString(ctx, cfg.AzureStorageConnectionString, azurestorage.DefaultResourceNames())
	}
	return azurestorage.MigrateArticleRows(ctx, cfg.AzureStorageAccountURL, azurestorage.DefaultResourceNames())
}

func (productionAzureRuntime) Repair(ctx context.Context, cfg config.Config) (azurestorage.ArticleMigrationResult, error) {
	if cfg.AzureStorageConnectionString != "" {
		return azurestorage.RepairArticleRowsFromConnectionString(ctx, cfg.AzureStorageConnectionString, azurestorage.DefaultResourceNames())
	}
	return azurestorage.RepairArticleRows(ctx, cfg.AzureStorageAccountURL, azurestorage.DefaultResourceNames())
}

func (productionAzureRuntime) Open(ctx context.Context, cfg config.Config, now func() time.Time) (*storagebundle.Bundle, error) {
	mode := azurestorage.ArticleSchemaMode(cfg.ArticleStorageSchemaMode)
	if cfg.AzureStorageConnectionString != "" {
		return azurestorage.OpenFromConnectionStringWithArticleSchemaMode(ctx, cfg.AzureStorageConnectionString, azurestorage.DefaultResourceNames(), mode, now)
	}
	return azurestorage.OpenWithArticleSchemaMode(ctx, cfg.AzureStorageAccountURL, azurestorage.DefaultResourceNames(), mode, now)
}

func initializeStorage(ctx context.Context, cfg config.Config, now func() time.Time, runtime azureRuntime) (*storagebundle.Bundle, azurestorage.ArticleMigrationResult, error) {
	switch cfg.StorageMode {
	case "memory":
		return memorystorage.NewBundle(now), azurestorage.ArticleMigrationResult{}, nil
	case "azure":
		storageContext, cancel := context.WithTimeout(ctx, 2*time.Minute)
		defer cancel()
		if cfg.AzureStorageConnectionString != "" && cfg.Environment != "production" {
			if err := runtime.Ensure(storageContext, cfg); err != nil {
				return nil, azurestorage.ArticleMigrationResult{}, fmt.Errorf("provision development Azure storage: %w", err)
			}
		}
		var migration azurestorage.ArticleMigrationResult
		var err error
		switch cfg.ArticleStorageSchemaMode {
		case config.ArticleStorageSchemaCompat:
		case config.ArticleStorageSchemaMigrate:
			migration, err = runtime.Migrate(storageContext, cfg)
		case config.ArticleStorageSchemaRepair:
			migration, err = runtime.Repair(storageContext, cfg)
		default:
			return nil, migration, fmt.Errorf("initialize storage: unsupported article schema mode %q", cfg.ArticleStorageSchemaMode)
		}
		if err != nil {
			return nil, migration, fmt.Errorf("prepare article storage schema: %w", err)
		}
		bundle, err := runtime.Open(storageContext, cfg, now)
		if err != nil {
			return nil, migration, fmt.Errorf("initialize storage: %w", err)
		}
		return bundle, migration, nil
	default:
		return nil, azurestorage.ArticleMigrationResult{}, fmt.Errorf("initialize storage: unsupported mode %q", cfg.StorageMode)
	}
}

func runConfigured(ctx context.Context, cfg config.Config, logger *slog.Logger, runtime azureRuntime, serve func(context.Context, config.Config, http.Handler, *slog.Logger) error) error {
	bundle, _, err := initializeStorage(ctx, cfg, storageNow, runtime)
	if err != nil {
		return err
	}
	if cfg.StorageMode == "azure" && cfg.ArticleStorageSchemaMode != config.ArticleStorageSchemaCompat {
		logger.Info("article storage schema ready", "event", "article_schema", "outcome", "ready")
	}
	if err := seedRuntimeData(ctx, bundle); err != nil {
		return fmt.Errorf("initialize runtime data: %w", err)
	}

	handler, err := app.New(app.Options{Config: cfg, Assets: webassets.Files, Logger: logger, Storage: bundle})
	if err != nil {
		return fmt.Errorf("initialize application: %w", err)
	}
	return serve(ctx, cfg, handler, logger)
}

func newHTTPServer(cfg config.Config, handler http.Handler, logger *slog.Logger) *http.Server {
	return &http.Server{
		Addr:              cfg.HTTPAddress,
		Handler:           handler,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       60 * time.Second,
		MaxHeaderBytes:    32 << 10,
		ErrorLog:          log.New(redactedServerErrorWriter{logger: logger}, "", 0),
	}
}

func serveHTTP(ctx context.Context, cfg config.Config, handler http.Handler, logger *slog.Logger) error {
	server := newHTTPServer(cfg, handler, logger)
	logServerStarting(logger, buildVersion, buildCommit)
	return serveServer(ctx, server, logger)
}

func logServerStarting(logger *slog.Logger, version, commit string) {
	logger.Info("web server starting",
		"event", "server_starting",
		"version", safeBuildMetadata(version, "development"),
		"commit", safeBuildMetadata(commit, "unknown"),
	)
}

func safeBuildMetadata(value, fallback string) string {
	if value == "" || len(value) > maxBuildMetadataLength {
		return fallback
	}
	for _, character := range value {
		if (character >= 'a' && character <= 'z') ||
			(character >= 'A' && character <= 'Z') ||
			(character >= '0' && character <= '9') ||
			character == '.' || character == '_' || character == '+' || character == '-' {
			continue
		}
		return fallback
	}
	return value
}

type httpServerRuntime interface {
	ListenAndServe() error
	Shutdown(context.Context) error
}

func serveServer(ctx context.Context, server httpServerRuntime, logger *slog.Logger) error {
	listenResult := make(chan error, 1)
	go func() {
		listenResult <- server.ListenAndServe()
	}()

	select {
	case err := <-listenResult:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	case <-ctx.Done():
	}

	logger.Info("web server shutdown requested", "event", "server_shutdown")
	shutdownContext, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
	defer cancel()
	if err := server.Shutdown(shutdownContext); err != nil {
		return fmt.Errorf("shutdown HTTP server: %w", err)
	}
	select {
	case err := <-listenResult:
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			return err
		}
		return nil
	case <-shutdownContext.Done():
		return fmt.Errorf("wait for HTTP server shutdown: %w", shutdownContext.Err())
	}
}

type redactedServerErrorWriter struct {
	logger *slog.Logger
}

func (writer redactedServerErrorWriter) Write(value []byte) (int, error) {
	if writer.logger != nil {
		writer.logger.Error("HTTP server internal error", "event", "http_server_error")
	}
	return len(value), nil
}
