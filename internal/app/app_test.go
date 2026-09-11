package app

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"testing/fstest"

	"github.com/francescostumpo/legal-callegarin/internal/articles"
	"github.com/francescostumpo/legal-callegarin/internal/config"
	"github.com/francescostumpo/legal-callegarin/internal/webassets"
)

func TestHandler(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		path       string
		wantStatus int
		wantType   string
	}{
		{
			name:       "liveness",
			path:       "/health/live",
			wantStatus: http.StatusOK,
			wantType:   "text/plain; charset=utf-8",
		},
		{
			name:       "public home",
			path:       "/",
			wantStatus: http.StatusOK,
			wantType:   "text/html; charset=utf-8",
		},
	}
	handler, err := New(Options{
		Config: config.Config{PublicBaseURL: "https://studio.example.test"},
		Assets: webassets.Files,
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			request := httptest.NewRequest(http.MethodGet, tt.path, nil)
			response := httptest.NewRecorder()

			handler.ServeHTTP(response, request)

			if response.Code != tt.wantStatus {
				t.Fatalf("status = %d, want %d", response.Code, tt.wantStatus)
			}
			if got := response.Header().Get("Content-Type"); got != tt.wantType {
				t.Fatalf("Content-Type = %q, want %q", got, tt.wantType)
			}
		})
	}
}

func TestNewRejectsInvalidPublicAssets(t *testing.T) {
	t.Parallel()

	_, err := New(Options{Config: config.Config{PublicBaseURL: "https://studio.example.test"}, Assets: fstest.MapFS{}})
	if err == nil {
		t.Fatal("New() error = nil, want template initialization error")
	}
}

func TestNewInjectsPublicArticleReader(t *testing.T) {
	t.Parallel()

	handler, err := New(Options{
		Config:   config.Config{PublicBaseURL: "https://studio.example.test"},
		Assets:   webassets.Files,
		Articles: failingArticleReader{},
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	request := httptest.NewRequest(http.MethodGet, "/sentenze-e-riflessioni", nil)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)

	if response.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusServiceUnavailable)
	}
}

type failingArticleReader struct{}

func (failingArticleReader) ListPublished(context.Context, articles.ListOptions) (articles.ArticlePage, error) {
	return articles.ArticlePage{}, errors.New("article storage unavailable")
}

func (failingArticleReader) GetPublished(context.Context, string) (articles.ArticleWithBody, error) {
	return articles.ArticleWithBody{}, errors.New("article storage unavailable")
}
