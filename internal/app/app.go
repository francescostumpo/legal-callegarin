package app

import (
	"fmt"
	"io/fs"
	"log/slog"
	"net/http"

	"github.com/francescostumpo/legal-callegarin/internal/config"
	publicweb "github.com/francescostumpo/legal-callegarin/internal/web/public"
)

type Options struct {
	Config   config.Config
	Assets   fs.FS
	Logger   *slog.Logger
	Articles publicweb.ArticleReader
}

func New(options Options) (http.Handler, error) {
	rendererOptions := make([]publicweb.RendererOption, 0, 1)
	if options.Articles != nil {
		rendererOptions = append(rendererOptions, publicweb.WithArticleReader(options.Articles))
	}
	renderer, err := publicweb.NewRenderer(options.Assets, options.Config.PublicBaseURL, rendererOptions...)
	if err != nil {
		return nil, fmt.Errorf("initialize public renderer: %w", err)
	}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /health/live", func(response http.ResponseWriter, _ *http.Request) {
		response.Header().Set("Content-Type", "text/plain; charset=utf-8")
		response.WriteHeader(http.StatusOK)
	})
	if err := publicweb.RegisterRoutes(mux, renderer, options.Assets); err != nil {
		return nil, fmt.Errorf("register public routes: %w", err)
	}

	return mux, nil
}
