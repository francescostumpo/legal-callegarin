package public_test

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/francescostumpo/legal-callegarin/internal/articles"
	"github.com/francescostumpo/legal-callegarin/internal/storage/memory"
	publicweb "github.com/francescostumpo/legal-callegarin/internal/web/public"
	"github.com/francescostumpo/legal-callegarin/internal/webassets"
)

func TestArticleRoutesExposeOnlyCurrentPublishedVersions(t *testing.T) {
	t.Parallel()

	fixture := newPublicArticleFixture(t, 5)
	live := fixture.create(t, "versione-pubblica", "Titolo pubblico", "corpo pubblicato")
	live = fixture.publish(t, live)
	replacement := publicArticleDraft("versione-pubblica", "Titolo bozza segreta", "corpo bozza segreto")
	if _, err := fixture.service.SaveDraft(context.Background(), live.ID, replacement, live.ETag); err != nil {
		t.Fatalf("SaveDraft(live replacement) error = %v", err)
	}
	draft := fixture.create(t, "solo-bozza", "Titolo solo bozza", "testo solo bozza segreto")
	withdrawn := fixture.publish(t, fixture.create(t, "ritirato", "Titolo ritirato", "testo ritirato segreto"))
	fixture.tick()
	if _, err := fixture.service.Withdraw(context.Background(), withdrawn.ID, withdrawn.ETag); err != nil {
		t.Fatalf("Withdraw() error = %v", err)
	}

	handler := newArticleHandler(t, fixture.service)
	index := serveRequest(handler, "/sentenze-e-riflessioni")
	if index.Code != http.StatusOK || !strings.Contains(index.Body.String(), "Titolo pubblico") {
		t.Fatalf("article index status/body = %d, %q", index.Code, index.Body.String())
	}
	for _, leaked := range []string{replacement.Title, draft.Title, withdrawn.Title, "segreto"} {
		if strings.Contains(index.Body.String(), leaked) {
			t.Fatalf("article index leaked %q", leaked)
		}
	}

	detail := serveRequest(handler, "/sentenze-e-riflessioni/versione-pubblica")
	if detail.Code != http.StatusOK || !strings.Contains(detail.Body.String(), "corpo pubblicato") || strings.Contains(detail.Body.String(), "corpo bozza segreto") {
		t.Fatalf("published detail status/body = %d, %q", detail.Code, detail.Body.String())
	}
	for _, path := range []string{"/sentenze-e-riflessioni/solo-bozza", "/sentenze-e-riflessioni/ritirato"} {
		response := serveRequest(handler, path)
		if response.Code != http.StatusNotFound || strings.Contains(response.Body.String(), "segreto") {
			t.Fatalf("non-public detail %q status/body = %d, %q", path, response.Code, response.Body.String())
		}
	}
}

func TestArticleIndexPaginatesDeterministically(t *testing.T) {
	t.Parallel()

	fixture := newPublicArticleFixture(t, 7)
	for index := range 7 {
		article := fixture.create(t, fmt.Sprintf("articolo-%d", index), fmt.Sprintf("Titolo articolo %d", index), fmt.Sprintf("corpo %d", index))
		fixture.publish(t, article)
		fixture.tick()
	}
	handler := newArticleHandler(t, fixture.service)
	first := serveRequest(handler, "/sentenze-e-riflessioni")
	if first.Code != http.StatusOK {
		t.Fatalf("first page status = %d", first.Code)
	}
	for index := 1; index < 7; index++ {
		if !strings.Contains(first.Body.String(), fmt.Sprintf("Titolo articolo %d", index)) {
			t.Fatalf("first page lacks article %d", index)
		}
	}
	if strings.Contains(first.Body.String(), "Titolo articolo 0") {
		t.Fatal("first page contains seventh article")
	}
	match := regexp.MustCompile(`href="(/sentenze-e-riflessioni\?cursor=[^"]+)"`).FindStringSubmatch(first.Body.String())
	if len(match) != 2 {
		t.Fatalf("first page lacks next cursor link: %q", first.Body.String())
	}
	second := serveRequest(handler, match[1])
	if second.Code != http.StatusOK || !strings.Contains(second.Body.String(), "Titolo articolo 0") {
		t.Fatalf("second page status/body = %d, %q", second.Code, second.Body.String())
	}
}

func TestHistoricalArticleSlugRedirectsWithoutLeakingBody(t *testing.T) {
	t.Parallel()

	fixture := newPublicArticleFixture(t, 1)
	article := fixture.publish(t, fixture.create(t, "slug-originale", "Titolo originale", "corpo originale"))
	fixture.tick()
	replacement := publicArticleDraft("slug-nuovo", "Titolo sostitutivo", "corpo sostitutivo")
	saved, err := fixture.service.SaveDraft(context.Background(), article.ID, replacement, article.ETag)
	if err != nil {
		t.Fatalf("SaveDraft() error = %v", err)
	}
	fixture.tick()
	fixture.publish(t, saved)

	handler := newArticleHandler(t, fixture.service)
	redirect := serveRequest(handler, "/sentenze-e-riflessioni/slug-originale")
	if redirect.Code != http.StatusPermanentRedirect || redirect.Header().Get("Location") != "/sentenze-e-riflessioni/slug-nuovo" || strings.Contains(redirect.Body.String(), "corpo") {
		t.Fatalf("historical redirect = status %d, location %q, body %q", redirect.Code, redirect.Header().Get("Location"), redirect.Body.String())
	}
	current := serveRequest(handler, "/sentenze-e-riflessioni/slug-nuovo")
	if current.Code != http.StatusOK || !strings.Contains(current.Body.String(), "corpo sostitutivo") {
		t.Fatalf("current article = status %d, body %q", current.Code, current.Body.String())
	}
}

func TestArticleMissingCoverUsesEditorialFallback(t *testing.T) {
	t.Parallel()

	fixture := newPublicArticleFixture(t, 1)
	input := publicArticleDraft("cover-mancante", "Titolo senza cover", "corpo senza cover")
	input.CoverID = "cover-non-presente"
	article, err := fixture.service.CreateDraft(context.Background(), input)
	if err != nil {
		t.Fatalf("CreateDraft() error = %v", err)
	}
	fixture.publish(t, article)
	response := serveRequest(newArticleHandler(t, fixture.service), "/sentenze-e-riflessioni/cover-mancante")
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), "article-notebook-landscape") {
		t.Fatalf("fallback detail = status %d, body %q", response.Code, response.Body.String())
	}
}

func TestPreviewUsesExactArticleTemplateWithoutPublicRoute(t *testing.T) {
	t.Parallel()

	fixture := newPublicArticleFixture(t, 1)
	article := fixture.publish(t, fixture.create(t, "preview-condivisa", "Titolo preview condivisa", "corpo condiviso"))
	live, err := fixture.service.GetPublished(context.Background(), article.Slug)
	if err != nil {
		t.Fatalf("GetPublished() error = %v", err)
	}
	renderer, err := publicweb.NewRenderer(webassets.Files, canonicalBaseURL, publicweb.WithArticleReader(fixture.service))
	if err != nil {
		t.Fatalf("NewRenderer() error = %v", err)
	}
	publicData, err := renderer.NewArticlePageData(live, false)
	if err != nil {
		t.Fatalf("NewArticlePageData(public) error = %v", err)
	}
	previewData, err := renderer.NewArticlePageData(live, true)
	if err != nil {
		t.Fatalf("NewArticlePageData(preview) error = %v", err)
	}
	var publicHTML, previewHTML bytes.Buffer
	if err := renderer.RenderArticle(&publicHTML, publicData); err != nil {
		t.Fatalf("RenderArticle(public) error = %v", err)
	}
	if err := renderer.RenderArticle(&previewHTML, previewData); err != nil {
		t.Fatalf("RenderArticle(preview) error = %v", err)
	}
	if articleFragment(publicHTML.String()) != articleFragment(previewHTML.String()) {
		t.Fatal("public and preview article bodies differ")
	}
	if strings.Contains(publicHTML.String(), "Anteprima") || !strings.Contains(previewHTML.String(), "Anteprima salvata") || !strings.Contains(previewHTML.String(), `content="noindex, nofollow"`) {
		t.Fatalf("preview chrome mismatch: public=%q preview=%q", publicHTML.String(), previewHTML.String())
	}
	previewRoute := serveRequest(newArticleHandler(t, fixture.service), "/admin/preview/articles/"+article.ID)
	if previewRoute.Code != http.StatusNotFound {
		t.Fatalf("preview route status = %d, want 404", previewRoute.Code)
	}
}

func newArticleHandler(t *testing.T, reader publicweb.ArticleReader) http.Handler {
	t.Helper()
	renderer, err := publicweb.NewRenderer(webassets.Files, canonicalBaseURL, publicweb.WithArticleReader(reader))
	if err != nil {
		t.Fatalf("NewRenderer() error = %v", err)
	}
	mux := http.NewServeMux()
	if err := publicweb.RegisterRoutes(mux, renderer, webassets.Files); err != nil {
		t.Fatalf("RegisterRoutes() error = %v", err)
	}
	return mux
}

func serveRequest(handler http.Handler, target string) *httptest.ResponseRecorder {
	request := httptest.NewRequest(http.MethodGet, target, nil)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	return response
}

func articleFragment(page string) string {
	start := strings.Index(page, `<article class="article-detail"`)
	end := strings.Index(page, `</article>`)
	if start < 0 || end < start {
		return ""
	}
	return page[start : end+len(`</article>`)]
}

type publicArticleFixture struct {
	service articles.ArticleService
	clock   *publicArticleClock
}

func newPublicArticleFixture(t *testing.T, count int) *publicArticleFixture {
	t.Helper()
	clock := &publicArticleClock{now: time.Date(2026, 9, 11, 12, 0, 0, 0, time.UTC)}
	repository := memory.NewArticleMetadataRepository()
	bodies := memory.NewArticleBodyStore(clock.Now)
	ids := &publicArticleIDs{values: make([]string, count)}
	for index := range count {
		ids.values[index] = fmt.Sprintf("article-%d", index)
	}
	return &publicArticleFixture{service: articles.NewService(repository, bodies, clock, ids), clock: clock}
}

func (fixture *publicArticleFixture) create(t *testing.T, slug, title, body string) articles.Article {
	t.Helper()
	article, err := fixture.service.CreateDraft(context.Background(), publicArticleDraft(slug, title, body))
	if err != nil {
		t.Fatalf("CreateDraft(%q) error = %v", slug, err)
	}
	return article
}

func (fixture *publicArticleFixture) publish(t *testing.T, article articles.Article) articles.Article {
	t.Helper()
	published, err := fixture.service.Publish(context.Background(), article.ID, article.ETag)
	if err != nil {
		t.Fatalf("Publish(%q) error = %v", article.Slug, err)
	}
	return published
}

func (fixture *publicArticleFixture) tick() { fixture.clock.now = fixture.clock.now.Add(time.Minute) }

func publicArticleDraft(slug, title, body string) articles.DraftInput {
	return articles.DraftInput{
		Slug: slug, Title: title, Summary: "Sommario pubblico sufficientemente descrittivo", Area: "obbligazioni-e-contratti", CoverID: "contracts-pen",
		Body: articles.Body{SchemaVersion: 1, Document: []byte(`{"type":"doc"}`), HTML: "<p>" + body + "</p>", PlainText: body},
	}
}

type publicArticleClock struct{ now time.Time }

func (clock *publicArticleClock) Now() time.Time { return clock.now }

type publicArticleIDs struct {
	values []string
	index  int
}

func (ids *publicArticleIDs) NewID() string {
	if ids.index >= len(ids.values) {
		panic(errors.New("public article fixture exhausted IDs"))
	}
	value := ids.values[ids.index]
	ids.index++
	return value
}
