package adminapi

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/francescostumpo/legal-callegarin/internal/articles"
	"github.com/francescostumpo/legal-callegarin/internal/storage/memory"
	publicweb "github.com/francescostumpo/legal-callegarin/internal/web/public"
	"github.com/francescostumpo/legal-callegarin/internal/webassets"
)

type articleAPIClock struct{ now time.Time }

func (clock articleAPIClock) Now() time.Time { return clock.now }

type articleAPIIDs struct{}

func (articleAPIIDs) NewID() string { return "article-api-1" }

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
	payload := `{"slug":"prova-api","title":"Titolo valido API","summary":"Sommario sufficientemente lungo per la prova API","area":"diritti-reali","coverId":"","body":{"schemaVersion":1,"document":{"type":"doc","content":[{"type":"paragraph","content":[{"type":"text","text":"Contenuto sicuro"}]}]}}}`
	request := httptest.NewRequest(http.MethodPost, "https://studio.example.test/api/admin/articles", strings.NewReader(payload))
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
	if covers.Code != 200 || strings.Count(covers.Body.String(), `"id"`) != 12 {
		t.Fatalf("covers=%d %s", covers.Code, covers.Body.String())
	}
	preview := httptest.NewRecorder()
	handler.ServeHTTP(preview, httptest.NewRequest(http.MethodGet, "https://studio.example.test/admin/preview/articles/article-api-1", nil))
	if preview.Code != 200 || !strings.Contains(preview.Body.String(), "Anteprima salvata") || preview.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("preview=%d %#v %s", preview.Code, preview.Header(), preview.Body.String())
	}
}
