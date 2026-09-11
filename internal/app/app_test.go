package app

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/francescostumpo/legal-callegarin/internal/articles"
	"github.com/francescostumpo/legal-callegarin/internal/config"
	"github.com/francescostumpo/legal-callegarin/internal/storage/memory"
	publicweb "github.com/francescostumpo/legal-callegarin/internal/web/public"
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
	if got := response.Header().Get("Cache-Control"); got != "no-store" {
		t.Fatalf("Cache-Control = %q, want no-store", got)
	}
	if got := response.Header().Get("X-Robots-Tag"); got != "noindex, nofollow" {
		t.Fatalf("X-Robots-Tag = %q, want noindex, nofollow", got)
	}
}

func TestNewSharesPublicationInvalidationWithArticleService(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	clock := &appArticleClock{now: time.Date(2026, 9, 11, 10, 0, 0, 0, time.UTC)}
	events := publicweb.NewArticleEventSink()
	service := articles.NewService(
		memory.NewArticleMetadataRepository(),
		memory.NewArticleBodyStore(clock.Now),
		clock,
		&appArticleIDs{},
		events,
	)
	draft, err := service.CreateDraft(ctx, appArticleDraft("wired-cache", "Titolo prima versione", "corpo prima versione"))
	if err != nil {
		t.Fatalf("CreateDraft() error = %v", err)
	}
	handler, err := New(Options{
		Config:        config.Config{PublicBaseURL: "https://studio.example.test"},
		Assets:        webassets.Files,
		Articles:      service,
		ArticleEvents: events,
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	published, err := service.Publish(ctx, draft.ID, draft.ETag)
	if err != nil {
		t.Fatalf("Publish(first) error = %v", err)
	}
	first := httptest.NewRecorder()
	handler.ServeHTTP(first, httptest.NewRequest(http.MethodGet, "/sentenze-e-riflessioni/wired-cache", nil))
	if first.Code != http.StatusOK || !strings.Contains(first.Body.String(), "corpo prima versione") {
		t.Fatalf("first response = status %d, body %q", first.Code, first.Body.String())
	}

	clock.now = clock.now.Add(time.Minute)
	saved, err := service.SaveDraft(ctx, published.ID, appArticleDraft("wired-cache", "Titolo seconda versione", "corpo seconda versione"), published.ETag)
	if err != nil {
		t.Fatalf("SaveDraft() error = %v", err)
	}
	if _, err := service.Publish(ctx, saved.ID, saved.ETag); err != nil {
		t.Fatalf("Publish(second) error = %v", err)
	}
	updated := httptest.NewRecorder()
	handler.ServeHTTP(updated, httptest.NewRequest(http.MethodGet, "/sentenze-e-riflessioni/wired-cache", nil))
	if updated.Code != http.StatusOK || !strings.Contains(updated.Body.String(), "corpo seconda versione") || strings.Contains(updated.Body.String(), "corpo prima versione") {
		t.Fatalf("updated response = status %d, body %q", updated.Code, updated.Body.String())
	}
}

type failingArticleReader struct{}

type appArticleClock struct{ now time.Time }

func (clock *appArticleClock) Now() time.Time { return clock.now }

type appArticleIDs struct{ next int }

func (ids *appArticleIDs) NewID() string {
	ids.next++
	return fmt.Sprintf("app-article-%d", ids.next)
}

func appArticleDraft(slug, title, body string) articles.DraftInput {
	return articles.DraftInput{
		Slug: slug, Title: title, Summary: "Sommario sufficientemente descrittivo", Area: "obbligazioni-e-contratti", CoverID: "contracts-pen",
		Body: articles.Body{SchemaVersion: 1, Document: []byte(`{"type":"doc"}`), HTML: "<p>" + body + "</p>", PlainText: body},
	}
}

func (failingArticleReader) ListPublished(context.Context, articles.ListOptions) (articles.ArticlePage, error) {
	return articles.ArticlePage{}, errors.New("article storage unavailable")
}

func (failingArticleReader) GetPublished(context.Context, string) (articles.ArticleWithBody, error) {
	return articles.ArticleWithBody{}, errors.New("article storage unavailable")
}
