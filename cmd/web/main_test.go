package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"testing"
	"time"

	"github.com/francescostumpo/legal-callegarin/internal/config"
	storagebundle "github.com/francescostumpo/legal-callegarin/internal/storage"
	azurestorage "github.com/francescostumpo/legal-callegarin/internal/storage/azure"
	memorystorage "github.com/francescostumpo/legal-callegarin/internal/storage/memory"
)

func TestAzureStartupDispatchesSchemaModeBeforeOpen(t *testing.T) {
	t.Parallel()

	for _, mode := range []string{config.ArticleStorageSchemaCompat, config.ArticleStorageSchemaMigrate, config.ArticleStorageSchemaRepair} {
		mode := mode
		t.Run(mode, func(t *testing.T) {
			t.Parallel()
			runtime := &recordingAzureRuntime{bundle: memorystorage.NewBundle(time.Now)}
			cfg := config.Config{
				Environment:              "production",
				StorageMode:              "azure",
				ArticleStorageSchemaMode: mode,
				AzureStorageAccountURL:   "https://example.blob.core.windows.net",
			}

			_, _, err := initializeStorage(context.Background(), cfg, time.Now, runtime)
			if err != nil {
				t.Fatal(err)
			}

			want := []string{"open:" + mode}
			switch mode {
			case config.ArticleStorageSchemaMigrate:
				want = []string{"migrate", "open:migrate"}
			case config.ArticleStorageSchemaRepair:
				want = []string{"repair", "open:repair"}
			}
			if !equalStrings(runtime.calls, want) {
				t.Fatalf("calls = %v, want %v", runtime.calls, want)
			}
		})
	}
}

func TestAzureDevelopmentEnsuresBeforeModeActionAndOpen(t *testing.T) {
	t.Parallel()

	runtime := &recordingAzureRuntime{bundle: memorystorage.NewBundle(time.Now)}
	cfg := config.Config{
		Environment:                  "development",
		StorageMode:                  "azure",
		ArticleStorageSchemaMode:     config.ArticleStorageSchemaMigrate,
		AzureStorageConnectionString: "UseDevelopmentStorage=true",
	}
	_, _, err := initializeStorage(context.Background(), cfg, time.Now, runtime)
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"ensure", "migrate", "open:migrate"}; !equalStrings(runtime.calls, want) {
		t.Fatalf("calls = %v, want %v", runtime.calls, want)
	}
}

func TestStartupFailureDoesNotReachListener(t *testing.T) {
	t.Parallel()

	runtime := &recordingAzureRuntime{migrationErr: errors.New("migration unavailable")}
	listened := false
	cfg := config.Config{
		Environment:              "production",
		HTTPAddress:              ":8080",
		PublicBaseURL:            "https://studio.example.test",
		StorageMode:              "azure",
		ArticleStorageSchemaMode: config.ArticleStorageSchemaMigrate,
		AzureStorageAccountURL:   "https://example.blob.core.windows.net",
	}
	err := runConfigured(context.Background(), cfg, slog.Default(), runtime, func(_ config.Config, _ http.Handler, _ *slog.Logger) error {
		listened = true
		return nil
	})
	if err == nil || listened {
		t.Fatalf("runConfigured() error = %v, listened = %t", err, listened)
	}
}

type recordingAzureRuntime struct {
	calls        []string
	bundle       *storagebundle.Bundle
	migrationErr error
}

func (runtime *recordingAzureRuntime) Ensure(context.Context, config.Config) error {
	runtime.calls = append(runtime.calls, "ensure")
	return nil
}

func (runtime *recordingAzureRuntime) Migrate(context.Context, config.Config) (azurestorage.ArticleMigrationResult, error) {
	runtime.calls = append(runtime.calls, "migrate")
	return azurestorage.ArticleMigrationResult{}, runtime.migrationErr
}

func (runtime *recordingAzureRuntime) Repair(context.Context, config.Config) (azurestorage.ArticleMigrationResult, error) {
	runtime.calls = append(runtime.calls, "repair")
	return azurestorage.ArticleMigrationResult{}, runtime.migrationErr
}

func (runtime *recordingAzureRuntime) Open(_ context.Context, cfg config.Config, _ func() time.Time) (*storagebundle.Bundle, error) {
	runtime.calls = append(runtime.calls, "open:"+cfg.ArticleStorageSchemaMode)
	return runtime.bundle, nil
}

func equalStrings(left, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}
