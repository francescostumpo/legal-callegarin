package adminapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/francescostumpo/legal-callegarin/internal/articles"
	"github.com/francescostumpo/legal-callegarin/internal/auth"
	"github.com/francescostumpo/legal-callegarin/internal/storage/memory"
	webmiddleware "github.com/francescostumpo/legal-callegarin/internal/web/middleware"
	publicweb "github.com/francescostumpo/legal-callegarin/internal/web/public"
	"github.com/francescostumpo/legal-callegarin/internal/webassets"
)

type articleAPIClock struct{ now time.Time }

func (clock articleAPIClock) Now() time.Time { return clock.now }

type articleAPIIDs struct{}

func (articleAPIIDs) NewID() string { return "article-api-1" }

const validArticlePayload = `{"slug":"prova-api","title":"Titolo valido API","summary":"Sommario sufficientemente lungo per la prova API","area":"diritti-reali","coverId":"","body":{"schemaVersion":1,"document":{"type":"doc","content":[{"type":"paragraph","content":[{"type":"text","text":"Contenuto sicuro"}]}]}}}`

func TestArticleAPICreateDetailCoversPreviewAndPreconditions(t *testing.T) {
	clock := articleAPIClock{time.Date(2026, 9, 11, 10, 0, 0, 0, time.UTC)}
	service := articles.NewService(memory.NewArticleMetadataRepository(), memory.NewArticleBodyStore(clock.Now), clock, articleAPIIDs{})
	renderer, err := publicweb.NewRenderer(webassets.Files, "https://studio.example.test", publicweb.WithArticleReader(service))
	if err != nil {
		t.Fatal(err)
	}
	handler, err := New(Options{Credentials: &verifierStub{}, ConfiguredUsername: "admin", Sessions: &sessionsStub{}, Articles: service, Renderer: renderer, Assets: webassets.Files, SessionKey: []byte("0123456789abcdef0123456789abcdef"), PublicBaseURL: "https://studio.example.test"})
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodPost, "https://studio.example.test/api/admin/articles", strings.NewReader(validArticlePayload))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusCreated || response.Header().Get("Location") != "/api/admin/articles/article-api-1" || response.Header().Get("ETag") == "" || strings.Contains(response.Body.String(), "html") || strings.Contains(response.Body.String(), "plainText") {
		t.Fatalf("create=%d headers=%#v body=%s", response.Code, response.Header(), response.Body.String())
	}
	detail := httptest.NewRecorder()
	handler.ServeHTTP(detail, httptest.NewRequest(http.MethodGet, "https://studio.example.test/api/admin/articles/article-api-1", nil))
	if detail.Code != 200 || !strings.Contains(detail.Body.String(), "Contenuto sicuro") {
		t.Fatalf("detail=%d %s", detail.Code, detail.Body.String())
	}
	missing := httptest.NewRequest(http.MethodPost, "https://studio.example.test/api/admin/articles/article-api-1/publish", nil)
	missingResponse := httptest.NewRecorder()
	handler.ServeHTTP(missingResponse, missing)
	if missingResponse.Code != http.StatusPreconditionRequired {
		t.Fatalf("missing If-Match=%d", missingResponse.Code)
	}
	covers := httptest.NewRecorder()
	handler.ServeHTTP(covers, httptest.NewRequest(http.MethodGet, "https://studio.example.test/api/admin/covers", nil))
	var coverOptions []coverDTO
	if err := json.Unmarshal(covers.Body.Bytes(), &coverOptions); err != nil {
		t.Fatal(err)
	}
	wantCoverIDs := []string{"hero-architecture", "approach-library", "family-objects", "succession-seal", "contracts-pen", "debt-ledger", "damages-road", "property-key", "criminal-threshold", "tax-ledger", "article-notebook", "contact-entrance"}
	seenCoverIDs := make(map[string]bool, len(coverOptions))
	for index, cover := range coverOptions {
		if index >= len(wantCoverIDs) || cover.ID != wantCoverIDs[index] || seenCoverIDs[cover.ID] {
			t.Fatalf("cover[%d]=%q options=%#v", index, cover.ID, coverOptions)
		}
		seenCoverIDs[cover.ID] = true
	}
	if covers.Code != 200 || len(coverOptions) != len(wantCoverIDs) {
		t.Fatalf("covers=%d %s", covers.Code, covers.Body.String())
	}
	preview := httptest.NewRecorder()
	handler.ServeHTTP(preview, httptest.NewRequest(http.MethodGet, "https://studio.example.test/admin/preview/articles/article-api-1", nil))
	if preview.Code != 200 || !strings.Contains(preview.Body.String(), "Anteprima salvata") || preview.Header().Get("Cache-Control") != "no-store" || preview.Header().Get("X-Robots-Tag") != "noindex, nofollow" {
		t.Fatalf("preview=%d %#v %s", preview.Code, preview.Header(), preview.Body.String())
	}
}

func TestArticleAPIRouteAndStatusLifecycle(t *testing.T) {
	handler, _ := newArticleTestHandler(t)
	created := articleRequest(handler, http.MethodPost, "/api/admin/articles", validArticlePayload, "")
	if created.Code != http.StatusCreated {
		t.Fatalf("create = %d %s", created.Code, created.Body.String())
	}
	etag := created.Header().Get("ETag")
	for _, testCase := range []struct {
		method, target, body string
		want                 int
	}{
		{http.MethodGet, "/api/admin/articles?status=draft", "", http.StatusOK},
		{http.MethodGet, "/api/admin/articles?status=unknown", "", http.StatusBadRequest},
		{http.MethodGet, "/api/admin/articles?unexpected=1", "", http.StatusBadRequest},
		{http.MethodGet, "/api/admin/articles?cursor=not-a-cursor", "", http.StatusUnprocessableEntity},
		{http.MethodPut, "/api/admin/articles/article-api-1/draft", validArticlePayload, http.StatusOK},
	} {
		response := articleRequest(handler, testCase.method, testCase.target, testCase.body, etag)
		if response.Code != testCase.want {
			t.Fatalf("%s %s = %d, want %d: %s", testCase.method, testCase.target, response.Code, testCase.want, response.Body.String())
		}
		if next := response.Header().Get("ETag"); next != "" {
			etag = next
		}
	}
	for _, action := range []string{"publish", "withdraw"} {
		response := articleRequest(handler, http.MethodPost, "/api/admin/articles/article-api-1/"+action, "", etag)
		if response.Code != http.StatusOK || strings.Contains(response.Body.String(), `"body"`) {
			t.Fatalf("%s = %d %s", action, response.Code, response.Body.String())
		}
		etag = response.Header().Get("ETag")
	}
	withdrawn := articleRequest(handler, http.MethodGet, "/api/admin/articles?status=withdrawn", "", "")
	if withdrawn.Code != http.StatusOK || !strings.Contains(withdrawn.Body.String(), `"id":"article-api-1"`) || strings.Contains(withdrawn.Body.String(), `"body"`) || strings.Contains(withdrawn.Body.String(), "blobName") {
		t.Fatalf("withdrawn list = %d %s", withdrawn.Code, withdrawn.Body.String())
	}
	detail := articleRequest(handler, http.MethodGet, "/api/admin/articles/article-api-1", "", "")
	if detail.Code != http.StatusOK || strings.Contains(detail.Body.String(), `"html"`) || strings.Contains(detail.Body.String(), `"plainText"`) || strings.Contains(detail.Body.String(), "blobName") || strings.Contains(detail.Body.String(), "historicalSlugs") {
		t.Fatalf("detail leakage = %d %s", detail.Code, detail.Body.String())
	}
}

func TestArticleAPIRejectsInvalidContentFramingAndBounds(t *testing.T) {
	handler, _ := newArticleTestHandler(t)
	oversized := `{"padding":"` + strings.Repeat("x", articleBodyLimit) + `"}`
	for name, testCase := range map[string]struct {
		contentType string
		body        string
		want        int
	}{
		"missing content type":   {body: validArticlePayload, want: http.StatusUnsupportedMediaType},
		"wrong content type":     {contentType: "text/plain", body: validArticlePayload, want: http.StatusUnsupportedMediaType},
		"malformed JSON":         {contentType: "application/json", body: `{"slug":`, want: http.StatusBadRequest},
		"trailing JSON":          {contentType: "application/json", body: validArticlePayload + `{}`, want: http.StatusBadRequest},
		"unknown root field":     {contentType: "application/json", body: strings.Replace(validArticlePayload, `"slug":`, `"unknown":true,"slug":`, 1), want: http.StatusBadRequest},
		"unknown body field":     {contentType: "application/json", body: strings.Replace(validArticlePayload, `"schemaVersion":1`, `"schemaVersion":1,"html":"caller"`, 1), want: http.StatusBadRequest},
		"unknown document field": {contentType: "application/json", body: strings.Replace(validArticlePayload, `"type":"doc"`, `"type":"doc","onclick":"caller"`, 1), want: http.StatusUnprocessableEntity},
		"request body limit":     {contentType: "application/json", body: oversized, want: http.StatusRequestEntityTooLarge},
	} {
		t.Run(name, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodPost, "https://studio.example.test/api/admin/articles", strings.NewReader(testCase.body))
			if testCase.contentType != "" {
				request.Header.Set("Content-Type", testCase.contentType)
			}
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, request)
			if response.Code != testCase.want || response.Header().Get("Content-Type") != "application/json; charset=utf-8" || response.Header().Get("Cache-Control") != "no-store" {
				t.Fatalf("response = %d headers=%#v body=%s", response.Code, response.Header(), response.Body.String())
			}
		})
	}
}

func TestArticleAPIAuthCSRFAndErrorMapping(t *testing.T) {
	_, service := newArticleTestHandler(t)
	handler := newAuthenticatedArticleHandler(t, service)
	if response := serveAdminAPI(t, handler, http.MethodGet, "/api/admin/articles", "", false, false); response.Code != http.StatusUnauthorized {
		t.Fatalf("unauthenticated GET = %d", response.Code)
	}
	if response := serveAdminAPI(t, handler, http.MethodPost, "/api/admin/articles", "", true, false); response.Code != http.StatusForbidden {
		t.Fatalf("missing-CSRF POST = %d", response.Code)
	}
	if response := serveAdminAPI(t, handler, http.MethodPost, "/api/admin/articles", "", true, true); response.Code != http.StatusUnsupportedMediaType {
		t.Fatalf("authenticated CSRF POST = %d", response.Code)
	}

	stub := &transitionWithoutPreviewService{result: articles.Article{ID: "article-1", ETag: "etag-next"}}
	direct, err := New(Options{Credentials: &verifierStub{}, ConfiguredUsername: "admin", Sessions: &sessionsStub{}, Articles: stub, Assets: webassets.Files, SessionKey: []byte("0123456789abcdef0123456789abcdef"), PublicBaseURL: "https://studio.example.test"})
	if err != nil {
		t.Fatal(err)
	}
	for name, testCase := range map[string]struct {
		err  error
		want int
		code string
	}{
		"not found":          {articles.ErrNotFound, http.StatusNotFound, "article_not_found"},
		"validation":         {articles.ErrValidation, http.StatusUnprocessableEntity, "article_validation"},
		"body too large":     {articles.ErrBodyTooLarge, http.StatusUnprocessableEntity, "article_validation"},
		"slug taken":         {articles.ErrSlugTaken, http.StatusConflict, "slug_taken"},
		"conflict":           {articles.ErrConflict, http.StatusConflict, "article_conflict"},
		"invalid transition": {articles.ErrInvalidTransition, http.StatusConflict, "invalid_transition"},
		"commit unknown":     {articles.ErrCommitUnknown, http.StatusServiceUnavailable, "commit_unknown"},
		"private failure":    {errors.New("secret storage endpoint"), http.StatusServiceUnavailable, "articles_unavailable"},
	} {
		t.Run(name, func(t *testing.T) {
			stub.mutationErr = testCase.err
			response := articleRequest(direct, http.MethodPost, "/api/admin/articles/article-1/publish", "", formatETag("etag"))
			if response.Code != testCase.want || !strings.Contains(response.Body.String(), `"code":"`+testCase.code+`"`) || strings.Contains(response.Body.String(), "secret") {
				t.Fatalf("response = %d %s", response.Code, response.Body.String())
			}
		})
	}
}

type transitionWithoutPreviewService struct {
	articles.ArticleService
	result       articles.Article
	mutationErr  error
	previewCalls int
}

func (service *transitionWithoutPreviewService) Publish(context.Context, string, string) (articles.Article, error) {
	return service.result, service.mutationErr
}
func (service *transitionWithoutPreviewService) Withdraw(context.Context, string, string) (articles.Article, error) {
	return service.result, service.mutationErr
}
func (service *transitionWithoutPreviewService) GetPreview(context.Context, string) (articles.ArticleWithBody, error) {
	service.previewCalls++
	return articles.ArticleWithBody{}, errors.New("body storage unavailable after commit")
}

func TestArticleTransitionResponseDoesNotReadBodyAfterDurableMutation(t *testing.T) {
	result := articles.Article{ID: "article-1", Slug: "article-one", Title: "Titolo valido", Summary: "Sommario sufficientemente lungo", Area: "diritti-reali", Status: articles.StatusPublished, CreatedAt: time.Now(), UpdatedAt: time.Now(), ETag: "etag-after"}
	service := &transitionWithoutPreviewService{result: result}
	handler, err := New(Options{Credentials: &verifierStub{}, ConfiguredUsername: "admin", Sessions: &sessionsStub{}, Articles: service, Assets: webassets.Files, SessionKey: []byte("0123456789abcdef0123456789abcdef"), PublicBaseURL: "https://studio.example.test"})
	if err != nil {
		t.Fatal(err)
	}
	for _, action := range []string{"publish", "withdraw"} {
		request := httptest.NewRequest(http.MethodPost, "https://studio.example.test/api/admin/articles/article-1/"+action, nil)
		request.Header.Set("If-Match", formatETag("etag-before"))
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		if response.Code != http.StatusOK || response.Header().Get("ETag") != formatETag("etag-after") {
			t.Fatalf("%s response=%d headers=%#v body=%s", action, response.Code, response.Header(), response.Body.String())
		}
	}
	if service.previewCalls != 0 {
		t.Fatalf("post-commit preview reads=%d", service.previewCalls)
	}
}

func TestArticleDetailIncludesPublishedSnapshotAndUnpublishedChangeFact(t *testing.T) {
	clock := articleAPIClock{time.Date(2026, 9, 11, 10, 0, 0, 0, time.UTC)}
	service := articles.NewService(memory.NewArticleMetadataRepository(), memory.NewArticleBodyStore(clock.Now), clock, articleAPIIDs{})
	first := toDraft(articleInput{Slug: "first-slug", Title: "Titolo prima versione", Summary: "Sommario sufficientemente lungo prima versione", Area: "diritti-reali", Body: articleBodyInput{SchemaVersion: 1, Document: []byte(`{"type":"doc","content":[{"type":"paragraph","content":[{"type":"text","text":"versione uno"}]}]}`)}})
	created, err := service.CreateDraft(context.Background(), first)
	if err != nil {
		t.Fatal(err)
	}
	published, err := service.Publish(context.Background(), created.ID, created.ETag)
	if err != nil {
		t.Fatal(err)
	}
	second := first
	second.Slug = "second-slug"
	second.Title = "Titolo seconda versione"
	second.Body.Document = []byte(`{"type":"doc","content":[{"type":"paragraph","content":[{"type":"text","text":"versione due"}]}]}`)
	if _, err = service.SaveDraft(context.Background(), published.ID, second, published.ETag); err != nil {
		t.Fatal(err)
	}
	handler, err := New(Options{Credentials: &verifierStub{}, ConfiguredUsername: "admin", Sessions: &sessionsStub{}, Articles: service, Assets: webassets.Files, SessionKey: []byte("0123456789abcdef0123456789abcdef"), PublicBaseURL: "https://studio.example.test"})
	if err != nil {
		t.Fatal(err)
	}
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "https://studio.example.test/api/admin/articles/article-api-1", nil))
	body := response.Body.String()
	for _, fragment := range []string{`"published":{"slug":"first-slug"`, `"firstPublishedAt":`, `"lastPublishedAt":`, `"hasUnpublishedChanges":true`} {
		if !strings.Contains(body, fragment) {
			t.Fatalf("detail lacks %s: %s", fragment, body)
		}
	}
}

func newArticleTestHandler(t *testing.T) (http.Handler, articles.ArticleService) {
	t.Helper()
	clock := articleAPIClock{time.Date(2026, 9, 11, 10, 0, 0, 0, time.UTC)}
	service := articles.NewService(memory.NewArticleMetadataRepository(), memory.NewArticleBodyStore(clock.Now), clock, articleAPIIDs{})
	handler, err := New(Options{Credentials: &verifierStub{}, ConfiguredUsername: "admin", Sessions: &sessionsStub{}, Articles: service, Assets: webassets.Files, SessionKey: []byte("0123456789abcdef0123456789abcdef"), PublicBaseURL: "https://studio.example.test"})
	if err != nil {
		t.Fatal(err)
	}
	return handler, service
}

func newAuthenticatedArticleHandler(t *testing.T, service articles.ArticleService) http.Handler {
	t.Helper()
	key := []byte("0123456789abcdef0123456789abcdef")
	now := time.Date(2026, 9, 11, 10, 0, 0, 0, time.UTC)
	inner, err := New(Options{Credentials: &verifierStub{}, ConfiguredUsername: "admin", Sessions: &sessionsStub{}, Articles: service, Assets: webassets.Files, SessionKey: key, PublicBaseURL: "https://studio.example.test", Now: func() time.Time { return now }})
	if err != nil {
		t.Fatal(err)
	}
	handler, err := webmiddleware.New(inner, webmiddleware.Options{
		Authenticator: &authenticatorStub{session: auth.Session{Username: "admin", ExpiresAt: now.Add(8 * time.Hour)}},
		SessionKey:    key,
		PublicBaseURL: "https://studio.example.test",
		Now:           func() time.Time { return now },
	})
	if err != nil {
		t.Fatal(err)
	}
	return handler
}

func articleRequest(handler http.Handler, method, target, body, etag string) *httptest.ResponseRecorder {
	request := httptest.NewRequest(method, "https://studio.example.test"+target, strings.NewReader(body))
	if body != "" {
		request.Header.Set("Content-Type", "application/json")
	}
	if etag != "" {
		request.Header.Set("If-Match", etag)
	}
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	return response
}
