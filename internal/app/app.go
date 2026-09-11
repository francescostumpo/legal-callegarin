package app

import (
	"io/fs"
	"log/slog"
	"net/http"

	"github.com/francescostumpo/legal-callegarin/internal/config"
)

type Options struct {
	Config config.Config
	Assets fs.FS
	Logger *slog.Logger
}

func New(_ Options) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /health/live", func(response http.ResponseWriter, _ *http.Request) {
		response.Header().Set("Content-Type", "text/plain; charset=utf-8")
		response.WriteHeader(http.StatusOK)
	})
	mux.HandleFunc("/", func(response http.ResponseWriter, _ *http.Request) {
		response.Header().Set("Content-Type", "text/plain; charset=utf-8")
		http.Error(response, "application not initialized", http.StatusServiceUnavailable)
	})

	return mux
}
