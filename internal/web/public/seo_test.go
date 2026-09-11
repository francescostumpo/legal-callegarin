package public_test

import (
	"context"
	"fmt"
	"net/http"
	"regexp"
	"strings"
	"testing"

	"github.com/francescostumpo/legal-callegarin/internal/articles"
	publicweb "github.com/francescostumpo/legal-callegarin/internal/web/public"
	"github.com/francescostumpo/legal-callegarin/internal/webassets"
)

func TestSEORejectsUnsafePublicBaseURL(t *testing.T) {
	t.Parallel()

	for _, baseURL := range []string{"", "javascript:alert(1)", "ftp://studio.example.test", "https://user@studio.example.test", "https://studio.example.test/path", "https://studio.example.test?query=1"} {
		if _, err := publicweb.NewRenderer(webassets.Files, baseURL); err == nil {
			t.Errorf("NewRenderer(%q) error = nil, want unsafe base URL error", baseURL)
		}
	}
}

func TestSEOPublicPagesHaveOpenGraphAndStructuredData(t *testing.T) {
	t.Parallel()

	home := serveRequest(newPublicHandler(t), "/")
	body := home.Body.String()
	for _, fragment := range []string{
		`<meta property="og:title"`,
		`<meta property="og:description"`,
		`<meta property="og:type" content="website"`,
		`<meta property="og:url" content="https://studio.example.test/"`,
		`<script type="application/ld+json">`,
		`"@type":"LegalService"`,
		`"@type":"Person"`,
	} {
		if !strings.Contains(body, fragment) {
			t.Fatalf("home SEO lacks %q", fragment)
		}
	}
	if !regexp.MustCompile(`<meta property="og:image" content="https://studio\.example\.test/assets/covers/hero-architecture-landscape-[0-9a-f]{12}\.webp"`).MatchString(body) {
		t.Fatal("home SEO lacks content-addressed Open Graph image")
	}
}

func TestSEOArticleMetadataDatesDisclaimerAndAreaLink(t *testing.T) {
	t.Parallel()

	fixture := newPublicArticleFixture(t, 1)
	article := fixture.publish(t, fixture.create(t, "seo-articolo", "Titolo SEO articolo", "corpo seo"))
	response := serveRequest(newArticleHandler(t, fixture.service), "/sentenze-e-riflessioni/"+article.Slug)
	body := response.Body.String()
	for _, fragment := range []string{
		`<meta property="og:type" content="article"`,
		`"@type":"Article"`,
		`"headline":"Titolo SEO articolo"`,
		`Di Alessandro Callegarin`,
		`<time datetime="2026-09-11">11 settembre 2026</time>`,
		`finalità esclusivamente informative`,
		`href="/aree-di-attivita/obbligazioni-e-contratti"`,
	} {
		if !strings.Contains(body, fragment) {
			t.Fatalf("article SEO lacks %q", fragment)
		}
	}
}

func TestSEOPaginatedArticleIndexSelfReferencesValidCursor(t *testing.T) {
	t.Parallel()

	fixture := newPublicArticleFixture(t, 7)
	for index := range 7 {
		fixture.publish(t, fixture.create(t, fmt.Sprintf("seo-page-%d", index), fmt.Sprintf("Titolo SEO pagina %d", index), "corpo SEO pagina"))
		fixture.tick()
	}
	handler := newArticleHandler(t, fixture.service)
	first := serveRequest(handler, "/sentenze-e-riflessioni")
	match := regexp.MustCompile(`href="(/sentenze-e-riflessioni\?cursor=[^"]+)"`).FindStringSubmatch(first.Body.String())
	if len(match) != 2 {
		t.Fatalf("first page lacks next cursor link: %q", first.Body.String())
	}
	second := serveRequest(handler, match[1])
	wantURL := canonicalBaseURL + match[1]
	for _, fragment := range []string{
		`<link rel="canonical" href="` + wantURL + `"`,
		`<meta property="og:url" content="` + wantURL + `"`,
		`<title>Sentenze e riflessioni — pagina successiva`,
		`content="Approfondimenti su decisioni e temi di diritto. Pagina successiva della raccolta."`,
	} {
		if second.Code != http.StatusOK || !strings.Contains(second.Body.String(), fragment) {
			t.Fatalf("second-page SEO lacks %q: status %d, body %q", fragment, second.Code, second.Body.String())
		}
	}
}

func TestSEOSitemapRobotsAndRelatedArticleInvalidation(t *testing.T) {
	t.Parallel()

	components := newPublicArticleComponents(t, 2)
	reader := &articleReaderSlot{}
	renderer, handler := newArticleRendererAndHandler(t, reader)
	service := articles.NewService(components.repository, components.bodies, components.clock, components.ids, renderer)
	reader.delegate = service
	live, err := service.CreateDraft(context.Background(), publicArticleDraft("articolo-area", "Titolo collegato area", "corpo collegato"))
	if err != nil {
		t.Fatalf("CreateDraft(live) error = %v", err)
	}
	live, err = service.Publish(context.Background(), live.ID, live.ETag)
	if err != nil {
		t.Fatalf("Publish(live) error = %v", err)
	}
	draft, err := service.CreateDraft(context.Background(), publicArticleDraft("bozza-esclusa", "Titolo bozza esclusa", "corpo bozza"))
	if err != nil || draft.Status != articles.StatusDraft {
		t.Fatalf("CreateDraft(draft) = %#v, %v", draft, err)
	}

	for _, path := range []string{"/", "/aree-di-attivita/obbligazioni-e-contratti"} {
		response := serveRequest(handler, path)
		if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), live.Title) || strings.Contains(response.Body.String(), draft.Title) {
			t.Fatalf("related route %q status/body = %d, %q", path, response.Code, response.Body.String())
		}
	}
	sitemap := serveRequest(handler, "/sitemap.xml")
	if sitemap.Code != http.StatusOK || sitemap.Header().Get("Content-Type") != "application/xml; charset=utf-8" || !strings.Contains(sitemap.Body.String(), canonicalBaseURL+"/sentenze-e-riflessioni/articolo-area") || strings.Contains(sitemap.Body.String(), "bozza-esclusa") {
		t.Fatalf("sitemap status/headers/body = %d, %q, %q", sitemap.Code, sitemap.Header(), sitemap.Body.String())
	}
	robots := serveRequest(handler, "/robots.txt")
	lines := make(map[string]bool)
	for _, line := range strings.Split(strings.TrimSpace(robots.Body.String()), "\n") {
		lines[line] = true
	}
	for _, exactLine := range []string{
		"User-agent: *",
		"Disallow: /admin", "Disallow: /admin/",
		"Disallow: /admin/preview", "Disallow: /admin/preview/",
		"Disallow: /api/admin", "Disallow: /api/admin/",
		"Sitemap: " + canonicalBaseURL + "/sitemap.xml",
	} {
		if robots.Code != http.StatusOK || !lines[exactLine] {
			t.Fatalf("robots lacks exact line %q: status %d, body %q", exactLine, robots.Code, robots.Body.String())
		}
	}

	components.clock.now = components.clock.now.Add(timeMinute)
	updatedInput := publicArticleDraft("articolo-area", "Titolo collegato aggiornato", "corpo aggiornato")
	saved, err := service.SaveDraft(context.Background(), live.ID, updatedInput, live.ETag)
	if err != nil {
		t.Fatalf("SaveDraft() error = %v", err)
	}
	if _, err := service.Publish(context.Background(), saved.ID, saved.ETag); err != nil {
		t.Fatalf("replacement Publish() error = %v", err)
	}
	for _, path := range []string{"/", "/aree-di-attivita/obbligazioni-e-contratti"} {
		response := serveRequest(handler, path)
		if !strings.Contains(response.Body.String(), updatedInput.Title) || strings.Contains(response.Body.String(), live.Title) {
			t.Fatalf("invalidated related route %q body = %q", path, response.Body.String())
		}
	}

	current, err := service.GetPublished(context.Background(), live.Slug)
	if err != nil {
		t.Fatalf("GetPublished() error = %v", err)
	}
	withdrawn, err := service.Withdraw(context.Background(), current.Article.ID, current.Article.ETag)
	if err != nil || withdrawn.Status != articles.StatusWithdrawn {
		t.Fatalf("Withdraw() = %#v, %v", withdrawn, err)
	}
	afterWithdraw := serveRequest(handler, "/sitemap.xml")
	if strings.Contains(afterWithdraw.Body.String(), "articolo-area") {
		t.Fatalf("withdrawn article remains in sitemap: %q", afterWithdraw.Body.String())
	}
	nonPublic := serveRequest(handler, "/sentenze-e-riflessioni/articolo-area")
	if nonPublic.Code != http.StatusNotFound || nonPublic.Header().Get("X-Robots-Tag") != "noindex, nofollow" || strings.Contains(nonPublic.Body.String(), "corpo") {
		t.Fatalf("withdrawn response = status %d, robots %q, body %q", nonPublic.Code, nonPublic.Header().Get("X-Robots-Tag"), nonPublic.Body.String())
	}
	assertPublicErrorHeaders(t, nonPublic)
}
