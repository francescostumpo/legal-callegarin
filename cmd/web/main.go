package main

import (
	"context"
	"errors"
	"fmt"
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

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	if err := run(logger); err != nil {
		logger.Error("web server stopped", "error", err)
		os.Exit(1)
	}
}

func run(logger *slog.Logger) error {
	cfg, err := config.Load(os.Getenv)
	if err != nil {
		return err
	}
	return runConfigured(context.Background(), cfg, logger, productionAzureRuntime{}, serveHTTP)
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

func runConfigured(ctx context.Context, cfg config.Config, logger *slog.Logger, runtime azureRuntime, serve func(config.Config, http.Handler, *slog.Logger) error) error {
	bundle, migration, err := initializeStorage(ctx, cfg, time.Now, runtime)
	if err != nil {
		return err
	}
	if cfg.StorageMode == "azure" && cfg.ArticleStorageSchemaMode != config.ArticleStorageSchemaCompat {
		logger.Info("article storage schema ready", "mode", cfg.ArticleStorageSchemaMode, "migrated", migration.Migrated, "scanned", migration.Scanned)
	}

	handler, err := app.New(app.Options{Config: cfg, Assets: webassets.Files, Logger: logger, Storage: bundle})
	if err != nil {
		return fmt.Errorf("initialize application: %w", err)
	}
	return serve(cfg, handler, logger)
}

func serveHTTP(cfg config.Config, handler http.Handler, logger *slog.Logger) error {
	server := &http.Server{
		Addr:              cfg.HTTPAddress,
		Handler:           handler,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       60 * time.Second,
	}

	signalContext, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	shutdownResult := make(chan error, 1)
	go func() {
		<-signalContext.Done()
		shutdownContext, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
		defer cancel()
		shutdownResult <- server.Shutdown(shutdownContext)
	}()

	logger.Info("web server starting", "address", cfg.HTTPAddress)
	if err := server.ListenAndServe(); !errors.Is(err, http.ErrServerClosed) {
		return err
	}

	return <-shutdownResult
}
