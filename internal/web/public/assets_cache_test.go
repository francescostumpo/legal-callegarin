package public

import (
	"crypto/sha256"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path"
	"regexp"
	"strings"
	"testing"

	"github.com/francescostumpo/legal-callegarin/internal/webassets"
)

func TestAssetURLsEmittedForRealEmbeddedFilesAreContentHashedAndImmutable(t *testing.T) {
	t.Parallel()

	renderer, err := NewRenderer(webassets.Files, "https://studio.example.test")
	if err != nil {
		t.Fatalf("NewRenderer() error = %v", err)
	}
	handler := http.NewServeMux()
	if err := RegisterRoutes(handler, renderer, webassets.Files); err != nil {
		t.Fatalf("RegisterRoutes() error = %v", err)
	}
	home := httptest.NewRecorder()
	handler.ServeHTTP(home, httptest.NewRequest(http.MethodGet, "/", nil))
	if home.Code != http.StatusOK {
		t.Fatalf("GET / status = %d", home.Code)
	}

	cssURL := firstAssetURL(t, home.Body.String(), `href=['"](/assets/site-[0-9a-f]{12}\.css)['"]`)
	jsURL := firstAssetURL(t, home.Body.String(), `src=['"](/assets/nav-[0-9a-f]{12}\.js)['"]`)
	coverURL := firstAssetURL(t, home.Body.String(), `src="(/assets/covers/hero-architecture-landscape-[0-9a-f]{12}\.webp)"`)
	css := assertContentHashedImmutableAsset(t, handler, cssURL)
	assertContentHashedImmutableAsset(t, handler, jsURL)
	assertContentHashedImmutableAsset(t, handler, coverURL)

	fontURL := firstAssetURL(t, string(css), `url\("(/assets/fonts/fraunces-latin-variable-[0-9a-f]{12}\.woff2)"\)`)
	assertContentHashedImmutableAsset(t, handler, fontURL)
}

func firstAssetURL(t *testing.T, content, pattern string) string {
	t.Helper()
	match := regexp.MustCompile(pattern).FindStringSubmatch(content)
	if len(match) != 2 {
		t.Fatalf("content lacks asset matching %q: %q", pattern, content)
	}
	return match[1]
}

func assertContentHashedImmutableAsset(t *testing.T, handler http.Handler, assetURL string) []byte {
	t.Helper()
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, assetURL, nil))
	if response.Code != http.StatusOK {
		t.Fatalf("GET %s status = %d", assetURL, response.Code)
	}
	if got := response.Header().Get("Cache-Control"); got != "public, max-age=31536000, immutable" {
		t.Fatalf("GET %s Cache-Control = %q", assetURL, got)
	}
	filename := path.Base(assetURL)
	match := regexp.MustCompile(`-([0-9a-f]{12})\.[^.]+$`).FindStringSubmatch(filename)
	if len(match) != 2 {
		t.Fatalf("asset URL %q lacks a 12-character content hash", assetURL)
	}
	digest := sha256.Sum256(response.Body.Bytes())
	if got := fmt.Sprintf("%x", digest[:6]); !strings.EqualFold(got, match[1]) {
		t.Fatalf("GET %s hash = %q, want content hash %q", assetURL, match[1], got)
	}
	return response.Body.Bytes()
}
