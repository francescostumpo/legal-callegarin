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

	handler, err := app.New(app.Options{Config: cfg, Assets: webassets.Files, Logger: logger})
	if err != nil {
		return fmt.Errorf("initialize application: %w", err)
	}

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
