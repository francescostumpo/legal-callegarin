package public

import (
	"io/fs"
	"net/http"
	"net/http/httptest"
	"testing"
	"testing/fstest"
)

func TestFingerprintAssetReceivesImmutableCachingOnlyWhenHashed(t *testing.T) {
	t.Parallel()

	files := fstest.MapFS{
		"app-2cf24dba.css": {Data: []byte("hashed")},
		"site.css":         {Data: []byte("stable")},
	}
	handler := staticAssetHandler(fs.FS(files))

	for _, test := range []struct {
		path      string
		immutable bool
	}{
		{path: "/app-2cf24dba.css", immutable: true},
		{path: "/site.css", immutable: false},
	} {
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, test.path, nil))
		if response.Code != http.StatusOK {
			t.Fatalf("GET %s status = %d", test.path, response.Code)
		}
		got := response.Header().Get("Cache-Control")
		want := "public, max-age=31536000, immutable"
		if test.immutable && got != want {
			t.Fatalf("GET %s Cache-Control = %q, want %q", test.path, got, want)
		}
		if !test.immutable && got == want {
			t.Fatalf("GET %s incorrectly received immutable caching", test.path)
		}
	}
}
