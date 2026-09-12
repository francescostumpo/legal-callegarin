package main

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"net/http"
	"strings"
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
	err := runConfigured(context.Background(), cfg, slog.Default(), runtime, func(_ context.Context, _ config.Config, _ http.Handler, _ *slog.Logger) error {
		listened = true
		return nil
	})
	if err == nil || listened {
		t.Fatalf("runConfigured() error = %v, listened = %t", err, listened)
	}
}

func TestNewHTTPServerUsesBoundedProductionTimeoutsAndRedactedErrorLog(t *testing.T) {
	var output bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&output, nil))
	server := newHTTPServer(config.Config{HTTPAddress: ":8080"}, http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}), logger)
	if server.ReadHeaderTimeout != 5*time.Second || server.ReadTimeout != 15*time.Second || server.WriteTimeout != 30*time.Second || server.IdleTimeout != 60*time.Second || server.MaxHeaderBytes != 32<<10 {
		t.Fatalf("server hardening = header %s read %s write %s idle %s max-header %d", server.ReadHeaderTimeout, server.ReadTimeout, server.WriteTimeout, server.IdleTimeout, server.MaxHeaderBytes)
	}
	server.ErrorLog.Print("GET /secret?email=persona@example.test from 192.0.2.1")
	line := output.String()
	if !strings.Contains(line, `"event":"http_server_error"`) {
		t.Fatalf("structured server error missing event: %s", line)
	}
	for _, secret := range []string{"/secret", "persona@example.test", "192.0.2.1"} {
		if strings.Contains(line, secret) {
			t.Fatalf("server error log leaked %q: %s", secret, line)
		}
	}
}

func TestServeServerShutsDownWithinBoundAfterContextCancellation(t *testing.T) {
	runtime := newFakeHTTPServer()
	ctx, cancel := context.WithCancel(context.Background())
	result := make(chan error, 1)
	go func() { result <- serveServer(ctx, runtime, slog.Default()) }()
	<-runtime.started
	cancel()
	if err := <-result; err != nil {
		t.Fatalf("serveServer() error = %v", err)
	}
	if runtime.shutdownDeadline.IsZero() {
		t.Fatal("shutdown context had no deadline")
	}
	remaining := time.Until(runtime.shutdownDeadline)
	if remaining <= 0 || remaining > shutdownTimeout {
		t.Fatalf("shutdown deadline remaining = %s", remaining)
	}
}

func TestServeServerReturnsListenerFailureWithoutWaitingForSignal(t *testing.T) {
	want := errors.New("listen failed")
	runtime := &fakeHTTPServer{started: make(chan struct{}), listenResult: make(chan error, 1)}
	runtime.listenResult <- want
	if err := serveServer(context.Background(), runtime, slog.Default()); !errors.Is(err, want) {
		t.Fatalf("serveServer() error = %v, want %v", err, want)
	}
}

type fakeHTTPServer struct {
	started          chan struct{}
	listenResult     chan error
	shutdownDeadline time.Time
}

func newFakeHTTPServer() *fakeHTTPServer {
	return &fakeHTTPServer{started: make(chan struct{}), listenResult: make(chan error, 1)}
}

func (server *fakeHTTPServer) ListenAndServe() error {
	close(server.started)
	return <-server.listenResult
}

func (server *fakeHTTPServer) Shutdown(ctx context.Context) error {
	server.shutdownDeadline, _ = ctx.Deadline()
	server.listenResult <- http.ErrServerClosed
	return nil
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
