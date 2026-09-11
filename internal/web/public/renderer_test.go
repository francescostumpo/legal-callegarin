package public_test

import (
	"io/fs"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"
	"testing/fstest"

	publicweb "github.com/francescostumpo/legal-callegarin/internal/web/public"
	"github.com/francescostumpo/legal-callegarin/internal/webassets"
)

const canonicalBaseURL = "https://studio.example.test"

func TestStaticRoutesRenderSemanticHTMLWithoutCookies(t *testing.T) {
	t.Parallel()

	routes := []struct {
		path  string
		title string
	}{
		{path: "/", title: "Studio Legale Alessandro Callegarin"},
		{path: "/profilo", title: "Profilo"},
		{path: "/approccio", title: "Approccio"},
		{path: "/aree-di-attivita", title: "Aree di attività"},
		{path: "/aree-di-attivita/famiglia-e-persone", title: "Famiglia e persone"},
		{path: "/aree-di-attivita/successioni-e-donazioni", title: "Successioni e donazioni"},
		{path: "/aree-di-attivita/obbligazioni-e-contratti", title: "Obbligazioni e contratti"},
		{path: "/aree-di-attivita/recupero-crediti", title: "Recupero crediti"},
		{path: "/aree-di-attivita/risarcimento-danni", title: "Risarcimento danni"},
		{path: "/aree-di-attivita/diritti-reali", title: "Diritti reali"},
		{path: "/aree-di-attivita/diritto-penale", title: "Diritto penale"},
		{path: "/aree-di-attivita/diritto-tributario", title: "Diritto tributario"},
		{path: "/sentenze-e-riflessioni", title: "Sentenze e riflessioni"},
		{path: "/contatti", title: "Contatti"},
		{path: "/privacy-cookie-policy", title: "Privacy e cookie policy"},
	}
	handler := newPublicHandler(t)
	for _, route := range routes {
		t.Run(route.path, func(t *testing.T) {
			t.Parallel()

			request := httptest.NewRequest(http.MethodGet, route.path, nil)
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, request)

			if response.Code != http.StatusOK {
				t.Fatalf("GET %s status = %d, want 200", route.path, response.Code)
			}
			if response.Header().Get("Set-Cookie") != "" {
				t.Fatalf("GET %s set a cookie: %q", route.path, response.Header().Get("Set-Cookie"))
			}
			if got := response.Header().Get("Content-Type"); got != "text/html; charset=utf-8" {
				t.Fatalf("GET %s Content-Type = %q", route.path, got)
			}
			body := response.Body.String()
			assertSemanticPage(t, body, route.path, route.title)
			assertNoThirdPartyRuntimeURL(t, body)
		})
	}
}

func TestHomeFollowsApprovedSectionOrderAndLocality(t *testing.T) {
	t.Parallel()

	response := httptest.NewRecorder()
	newPublicHandler(t).ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/", nil))
	body := response.Body.String()
	sections := []string{"positioning", "introduction", "practice-areas", "approach", "articles", "statement", "contact"}
	lastIndex := -1
	for _, section := range sections {
		index := strings.Index(body, `data-home-section="`+section+`"`)
		if index < 0 {
			t.Fatalf("home lacks section %q", section)
		}
		if index <= lastIndex {
			t.Fatalf("home section %q is out of approved order", section)
		}
		lastIndex = index
	}
	if strings.Count(body, "Gallarate e provincia di Varese") != 1 {
		t.Fatalf("approved locality occurrence count = %d, want 1", strings.Count(body, "Gallarate e provincia di Varese"))
	}
	for _, unapproved := range []string{"Lombardia", "Milano", "via ", "Viale ", "Piazza "} {
		if strings.Contains(body, unapproved) {
			t.Fatalf("home contains unapproved locality detail %q", unapproved)
		}
	}
}

func TestNavigationIsProgressivelyEnhancedAndMarksCurrentPage(t *testing.T) {
	t.Parallel()

	response := httptest.NewRecorder()
	newPublicHandler(t).ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/approccio", nil))
	body := response.Body.String()
	for _, fragment := range []string{
		`<a class="skip-link" href="#main-content">`,
		`<details class="mobile-navigation" data-navigation>`,
		`<summary`,
		`data-navigation-close`,
	} {
		if !strings.Contains(body, fragment) {
			t.Fatalf("navigation HTML lacks %q", fragment)
		}
	}
	if !regexp.MustCompile(`src=['"]/assets/nav-[0-9a-f]{12}\.js['"]`).MatchString(body) {
		t.Fatal("navigation HTML lacks content-addressed script URL")
	}
	if !regexp.MustCompile(`href="/approccio"\s+aria-current="page"`).MatchString(body) {
		t.Fatal("navigation does not mark the current page")
	}
	if strings.Index(body, `href="#main-content"`) > strings.Index(body, `href="/"`) {
		t.Fatal("skip link is not the first page link")
	}
}

func TestNavigationEnhancementIsServedFirstPartyAndMarksJavaScriptBeforeSetup(t *testing.T) {
	t.Parallel()

	handler := newPublicHandler(t)
	pageResponse := httptest.NewRecorder()
	handler.ServeHTTP(pageResponse, httptest.NewRequest(http.MethodGet, "/", nil))
	if strings.Contains(pageResponse.Body.String(), `class="js-enabled"`) {
		t.Fatal("server HTML must not claim JavaScript enhancement")
	}

	scriptMatch := regexp.MustCompile(`src=['"](/assets/nav-[0-9a-f]{12}\.js)['"]`).FindStringSubmatch(pageResponse.Body.String())
	if len(scriptMatch) != 2 {
		t.Fatalf("page lacks content-addressed navigation script: %q", pageResponse.Body.String())
	}
	scriptResponse := httptest.NewRecorder()
	handler.ServeHTTP(scriptResponse, httptest.NewRequest(http.MethodGet, scriptMatch[1], nil))
	if scriptResponse.Code != http.StatusOK {
		t.Fatalf("GET %s status = %d, want 200", scriptMatch[1], scriptResponse.Code)
	}
	marker := `document.documentElement.classList.add("js-enabled")`
	if !strings.HasPrefix(strings.TrimSpace(scriptResponse.Body.String()), marker) {
		t.Fatal("first-party navigation script does not set its enhancement marker before setup")
	}
}

func TestRendererFailsWhenTemplatesCannotBeParsed(t *testing.T) {
	t.Parallel()

	if _, err := publicweb.NewRenderer(brokenTemplateFS(), canonicalBaseURL); err == nil {
		t.Fatal("NewRenderer() error = nil, want template parse error")
	}
}

func newPublicHandler(t *testing.T) http.Handler {
	t.Helper()

	renderer, err := publicweb.NewRenderer(webassets.Files, canonicalBaseURL)
	if err != nil {
		t.Fatalf("NewRenderer() error = %v", err)
	}
	mux := http.NewServeMux()
	publicweb.RegisterRoutes(mux, renderer, webassets.Files)
	return mux
}

func assertSemanticPage(t *testing.T, body, path, title string) {
	t.Helper()

	for _, fragment := range []string{
		"<!doctype html>",
		"<title>" + title,
		`<link rel="canonical" href="` + canonicalBaseURL + path + `"`,
		"<header",
		"<nav",
		`<main id="main-content"`,
		"<footer",
	} {
		if !strings.Contains(body, fragment) {
			t.Fatalf("GET %s lacks %q", path, fragment)
		}
	}
	if count := strings.Count(body, "<h1"); count != 1 {
		t.Fatalf("GET %s h1 count = %d, want 1", path, count)
	}
}

var runtimeURL = regexp.MustCompile(`(?:href|src)="(https?://[^"]+)"`)

func assertNoThirdPartyRuntimeURL(t *testing.T, body string) {
	t.Helper()

	for _, match := range runtimeURL.FindAllStringSubmatch(body, -1) {
		if !strings.HasPrefix(match[1], canonicalBaseURL) {
			t.Fatalf("third-party runtime URL found: %q", match[1])
		}
	}
}

func brokenTemplateFS() fs.FS {
	return fstest.MapFS{
		"templates/layouts/base.html": {Data: []byte("{{define")},
	}
}
