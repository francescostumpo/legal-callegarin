package public

import (
	"html/template"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/francescostumpo/legal-callegarin/internal/webassets"
)

func TestErrorResponseForRenderingFailureIsNoStoreAndNoIndex(t *testing.T) {
	t.Parallel()

	renderer, err := NewRenderer(webassets.Files, "https://studio.example.test")
	if err != nil {
		t.Fatalf("NewRenderer() error = %v", err)
	}
	renderer.home = template.Must(template.New("broken").Parse(`{{define "base"}}{{.MissingField}}{{end}}`))
	handler := http.NewServeMux()
	if err := RegisterRoutes(handler, renderer, webassets.Files); err != nil {
		t.Fatalf("RegisterRoutes() error = %v", err)
	}
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/", nil))

	if response.Code != http.StatusServiceUnavailable {
		t.Fatalf("rendering failure status = %d, want 503", response.Code)
	}
	if got := response.Header().Get("Cache-Control"); got != "no-store" {
		t.Fatalf("Cache-Control = %q, want no-store", got)
	}
	if got := response.Header().Get("X-Robots-Tag"); got != "noindex, nofollow" {
		t.Fatalf("X-Robots-Tag = %q, want noindex, nofollow", got)
	}
}
