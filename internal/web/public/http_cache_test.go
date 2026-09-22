package public_test

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/francescostumpo/legal-callegarin/internal/articles"
	"github.com/francescostumpo/legal-callegarin/internal/storage/memory"
	publicweb "github.com/francescostumpo/legal-callegarin/internal/web/public"
	"github.com/francescostumpo/legal-callegarin/internal/webassets"
)

func TestCachedArticleSurvivesStorageOutageAndSupportsConditionalGET(t *testing.T) {
	t.Parallel()

	fixture := newPublicArticleFixture(t, 1)
	article := fixture.publish(t, fixture.create(t, "articolo-cached", "Titolo articolo cached", "corpo cached"))
	reader := &switchableArticleReader{delegate: fixture.service}
	handler := newArticleHandler(t, reader)

	first := serveRequest(handler, "/sentenze-e-riflessioni/"+article.Slug)
	etag := first.Header().Get("ETag")
	if first.Code != http.StatusOK || etag == "" || first.Header().Get("Cache-Control") != "public, max-age=0, must-revalidate" {
		t.Fatalf("first response = status %d, ETag %q, Cache-Control %q", first.Code, etag, first.Header().Get("Cache-Control"))
	}
	reader.fail.Store(true)
	cached := serveRequest(handler, "/sentenze-e-riflessioni/"+article.Slug)
	if cached.Code != http.StatusOK || !stringsContains(cached.Body.String(), "corpo cached") {
		t.Fatalf("cached outage response = status %d, body %q", cached.Code, cached.Body.String())
	}

	request := httptest.NewRequest(http.MethodGet, "/sentenze-e-riflessioni/"+article.Slug, nil)
	request.Header.Set("If-None-Match", etag)
	conditional := httptest.NewRecorder()
	handler.ServeHTTP(conditional, request)
	if conditional.Code != http.StatusNotModified || conditional.Body.Len() != 0 || conditional.Header().Get("ETag") != etag {
		t.Fatalf("conditional response = status %d, body %q, ETag %q", conditional.Code, conditional.Body.String(), conditional.Header().Get("ETag"))
	}

	miss := serveRequest(handler, "/sentenze-e-riflessioni/non-cached")
	if miss.Code != http.StatusServiceUnavailable {
		t.Fatalf("uncached outage status = %d, want 503", miss.Code)
	}
	assertPublicErrorHeaders(t, miss)
	sitemap := serveRequest(handler, "/sitemap.xml")
	if sitemap.Code != http.StatusServiceUnavailable {
		t.Fatalf("uncached sitemap outage status = %d, want 503", sitemap.Code)
	}
	assertPublicErrorHeaders(t, sitemap)
}

func TestArticlePublicationEventInvalidatesPublicHTML(t *testing.T) {
	t.Parallel()

	fixture := newPublicArticleComponents(t, 1)
	reader := &articleReaderSlot{}
	renderer, handler := newArticleRendererAndHandler(t, reader)
	service := articles.NewService(fixture.repository, fixture.bodies, fixture.clock, fixture.ids, renderer)
	reader.delegate = service
	article, err := service.CreateDraft(context.Background(), publicArticleDraft("invalidate-me", "Titolo prima pubblicazione", "corpo prima pubblicazione"))
	if err != nil {
		t.Fatalf("CreateDraft() error = %v", err)
	}
	article, err = service.Publish(context.Background(), article.ID, article.ETag)
	if err != nil {
		t.Fatalf("Publish() error = %v", err)
	}
	first := serveRequest(handler, "/sentenze-e-riflessioni/invalidate-me")
	if first.Code != http.StatusOK || !stringsContains(first.Body.String(), "corpo prima pubblicazione") {
		t.Fatalf("first detail = status %d, body %q", first.Code, first.Body.String())
	}

	fixture.clock.now = fixture.clock.now.Add(timeMinute)
	replacement := publicArticleDraft("invalidate-me", "Titolo seconda pubblicazione", "corpo seconda pubblicazione")
	saved, err := service.SaveDraft(context.Background(), article.ID, replacement, article.ETag)
	if err != nil {
		t.Fatalf("SaveDraft() error = %v", err)
	}
	draftOnly := serveRequest(handler, "/sentenze-e-riflessioni/invalidate-me")
	if !stringsContains(draftOnly.Body.String(), "corpo prima pubblicazione") || stringsContains(draftOnly.Body.String(), "seconda") {
		t.Fatalf("draft save evicted live cache: %q", draftOnly.Body.String())
	}
	fixture.clock.now = fixture.clock.now.Add(timeMinute)
	if _, err := service.Publish(context.Background(), saved.ID, saved.ETag); err != nil {
		t.Fatalf("replacement Publish() error = %v", err)
	}
	updated := serveRequest(handler, "/sentenze-e-riflessioni/invalidate-me")
	if updated.Code != http.StatusOK || !stringsContains(updated.Body.String(), "corpo seconda pubblicazione") {
		t.Fatalf("updated detail = status %d, body %q", updated.Code, updated.Body.String())
	}
}

type switchableArticleReader struct {
	delegate articles.ArticleService
	fail     atomic.Bool
}

func (reader *switchableArticleReader) ListPublished(ctx context.Context, options articles.ListOptions) (articles.ArticlePage, error) {
	if reader.fail.Load() {
		return articles.ArticlePage{}, errors.New("storage unavailable")
	}
	return reader.delegate.ListPublished(ctx, options)
}

func (reader *switchableArticleReader) GetPublished(ctx context.Context, slug string) (articles.ArticleWithBody, error) {
	if reader.fail.Load() {
		return articles.ArticleWithBody{}, errors.New("storage unavailable")
	}
	return reader.delegate.GetPublished(ctx, slug)
}

type articleReaderSlot struct{ delegate articles.ArticleService }

func (reader *articleReaderSlot) ListPublished(ctx context.Context, options articles.ListOptions) (articles.ArticlePage, error) {
	return reader.delegate.ListPublished(ctx, options)
}

func (reader *articleReaderSlot) GetPublished(ctx context.Context, slug string) (articles.ArticleWithBody, error) {
	return reader.delegate.GetPublished(ctx, slug)
}

func stringsContains(value, fragment string) bool { return strings.Contains(value, fragment) }

const timeMinute = time.Minute

type publicArticleComponents struct {
	repository *memory.ArticleMetadataRepository
	bodies     *memory.ArticleBodyStore
	clock      *publicArticleClock
	ids        *publicArticleIDs
}

func newPublicArticleComponents(t *testing.T, count int) *publicArticleComponents {
	t.Helper()
	clock := &publicArticleClock{now: time.Date(2026, 9, 11, 12, 0, 0, 0, time.UTC)}
	ids := &publicArticleIDs{values: make([]string, count)}
	for index := range count {
		ids.values[index] = fmt.Sprintf("article-%d", index)
	}
	return &publicArticleComponents{
		repository: memory.NewArticleMetadataRepository(),
		bodies:     memory.NewArticleBodyStore(clock.Now),
		clock:      clock, ids: ids,
	}
}

func newArticleRendererAndHandler(t *testing.T, reader publicweb.ArticleReader) (*publicweb.Renderer, http.Handler) {
	t.Helper()
	renderer, err := publicweb.NewRenderer(webassets.Files, canonicalBaseURL, publicweb.WithArticleReader(reader))
	if err != nil {
		t.Fatalf("NewRenderer() error = %v", err)
	}
	mux := http.NewServeMux()
	if err := publicweb.RegisterRoutes(mux, renderer, webassets.Files); err != nil {
		t.Fatalf("RegisterRoutes() error = %v", err)
	}
	return renderer, mux
}
