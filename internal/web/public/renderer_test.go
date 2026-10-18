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

func TestApprovedPublicCopyAndContactsRenderWithoutForbiddenClaims(t *testing.T) {
	t.Parallel()

	handler := newPublicHandler(t)
	tests := []struct {
		path     string
		status   int
		required []string
	}{
		{
			path:   "/",
			status: http.StatusOK,
			required: []string{
				"Assistenza legale chiara e rigorosa, vicina alle persone e alle loro esigenze.",
				"Lo Studio Legale Alessandro Callegarin offre consulenza e assistenza a Gallarate e nel territorio della provincia di Varese, con un approccio fondato sull’ascolto, sulla chiarezza e sulla valutazione concreta di ogni situazione.",
				"Comprendere il problema, chiarire le possibilità, costruire una tutela concreta.",
			},
		},
		{
			path:   "/profilo",
			status: http.StatusOK,
			required: []string{
				"Alessandro Callegarin si è laureato in Giurisprudenza presso l’Università degli Studi di Milano nel 2018.",
				"Svolge l’attività di avvocato a Gallarate dal 2022.",
			},
		},
		{
			path:   "/approccio",
			status: http.StatusOK,
			required: []string{
				"Ogni questione richiede attenzione, metodo e una valutazione costruita sulle reali esigenze della persona.",
				"L’obiettivo è offrire indicazioni comprensibili, illustrare con trasparenza le possibili strade e individuare la tutela più appropriata per il caso concreto.",
				"Le informazioni condivise con lo Studio sono trattate con riservatezza e con attenzione alla loro pertinenza rispetto alla richiesta.",
				"Ogni comunicazione viene gestita nel rispetto degli obblighi professionali e della normativa applicabile.",
			},
		},
		{
			path:   "/aree-di-attivita",
			status: http.StatusOK,
			required: []string{
				"Lo Studio assiste privati, famiglie e realtà del territorio in materia di diritto civile, penale e tributario. L’attività comprende, in particolare, separazioni e divorzi, tutela delle persone e dei minori, successioni e donazioni, contratti e locazioni, recupero crediti, risarcimento dei danni, diritti reali, procedimenti penali e contenzioso tributario.",
			},
		},
		{
			path:     "/contatti",
			status:   http.StatusOK,
			required: []string{"L’invio di una richiesta non costituisce conferimento di incarico."},
		},
		{path: "/aree-di-attivita/famiglia-e-persone", status: http.StatusOK},
		{path: "/aree-di-attivita/successioni-e-donazioni", status: http.StatusOK},
		{path: "/aree-di-attivita/obbligazioni-e-contratti", status: http.StatusOK},
		{path: "/aree-di-attivita/recupero-crediti", status: http.StatusOK},
		{path: "/aree-di-attivita/risarcimento-danni", status: http.StatusOK},
		{path: "/aree-di-attivita/diritti-reali", status: http.StatusOK},
		{path: "/aree-di-attivita/diritto-penale", status: http.StatusOK},
		{path: "/aree-di-attivita/diritto-tributario", status: http.StatusOK},
		{path: "/sentenze-e-riflessioni", status: http.StatusOK},
		{
			path:   "/privacy-cookie-policy",
			status: http.StatusOK,
			required: []string{
				"Titolare del trattamento", "Avv. Alessandro Callegarin",
				"Dati trattati e finalità", "articolo 6, paragrafo 1, lettera b)",
				"articolo 6, paragrafo 1, lettera f)", "Conferimento dei dati",
				"Destinatari e trasferimenti", "Italy North", "Conservazione",
				"24 mesi", "30 giorni", "Diritti dell’interessato",
				"Garante per la protezione dei dati personali",
				"Nessun processo decisionale automatizzato",
				"Cookie e strumenti di tracciamento", "otto ore",
			},
		},
		{
			path:     "/pagina-inesistente",
			status:   http.StatusNotFound,
			required: []string{"La pagina non è disponibile"},
		},
	}
	forbidden := []string{
		"DATO DA " + "CONFERMARE", "DA VALIDARE CON IL " + "PROFESSIONISTA",
		"103/110", "volontariato", "avvocato associato",
		"partita IVA", "codice fiscale", "numero di iscrizione",
	}
	contactRequired := []string{
		"Avv. Alessandro Callegarin — Gallarate (VA)",
		`href="tel:+390331792529"`,
		"0331 792529",
		`href="mailto:callegarinale@gmail.com"`,
		`href="mailto:alessandro.callegarin@busto.pecavvocati.it"`,
		"Via Borghi 8, Gallarate (VA)",
		"Dal lunedì al venerdì, 09:00–12:30 e 15:00–19:00",
	}

	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, tt.path, nil))
			if response.Code != tt.status {
				t.Fatalf("GET %s status = %d, want %d", tt.path, response.Code, tt.status)
			}
			body := response.Body.String()
			normalizedBody := strings.Join(strings.Fields(body), " ")
			for _, required := range append(tt.required, contactRequired...) {
				if !strings.Contains(normalizedBody, required) {
					t.Errorf("GET %s body lacks %q", tt.path, required)
				}
			}
			for _, claim := range forbidden {
				if strings.Contains(strings.ToLower(body), strings.ToLower(claim)) {
					t.Errorf("GET %s body contains forbidden claim %q", tt.path, claim)
				}
			}
		})
	}
}

func TestStudioAddressRemainsPlainTextWithoutMapLinks(t *testing.T) {
	t.Parallel()

	handler := newPublicHandler(t)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/", nil))
	if response.Code != http.StatusOK {
		t.Fatalf("GET / status = %d, want 200", response.Code)
	}
	body := response.Body.String()
	address := "Via Borghi 8, Gallarate (VA)"
	addressCount := 0
	for searchFrom := 0; searchFrom < len(body); {
		relativeIndex := strings.Index(body[searchFrom:], address)
		if relativeIndex < 0 {
			break
		}
		addressIndex := searchFrom + relativeIndex
		prefix := body[:addressIndex]
		openLinks := regexp.MustCompile(`(?i)<a(?:\s|>)`).FindAllStringIndex(prefix, -1)
		closedLinks := regexp.MustCompile(`(?i)</a\s*>`).FindAllStringIndex(prefix, -1)
		if len(openLinks) != len(closedLinks) {
			t.Fatalf("studio address occurrence %d is rendered inside a link", addressCount+1)
		}
		addressCount++
		searchFrom = addressIndex + len(address)
	}
	if addressCount == 0 {
		t.Fatalf("GET / body lacks address %q", address)
	}
	mapURL := regexp.MustCompile(`(?i)(?:href|src)="[^"]*(?:maps?|mappa|openstreetmap)[^"]*"`)
	if match := mapURL.FindString(body); match != "" {
		t.Fatalf("GET / body contains map URL %q", match)
	}
}

func TestDesignedPublicErrorsCoverRecoverableStatusesWithoutInternalDetails(t *testing.T) {
	renderer, err := publicweb.NewRenderer(webassets.Files, canonicalBaseURL)
	if err != nil {
		t.Fatal(err)
	}
	for _, status := range []int{404, 409, 422, 429, 500, 503} {
		response := httptest.NewRecorder()
		response.Header().Set("X-Request-ID", "safe-reference")
		renderer.WriteError(response, httptest.NewRequest(http.MethodGet, "/failure", nil), status, "internal-secret-code", "internal secret message")
		body := response.Body.String()
		if response.Code != status || response.Header().Get("Cache-Control") != "no-store" || response.Header().Get("X-Robots-Tag") != "noindex, nofollow" {
			t.Fatalf("status %d error response = status %d headers %#v", status, response.Code, response.Header())
		}
		for _, required := range []string{"Studio Legale Alessandro Callegarin", "/contatti", `href="tel:+390331792529"`, "safe-reference"} {
			if !strings.Contains(body, required) {
				t.Errorf("status %d body lacks %q", status, required)
			}
		}
		for _, forbidden := range []string{"internal-secret-code", "internal secret message"} {
			if strings.Contains(body, forbidden) {
				t.Errorf("status %d leaked %q", status, forbidden)
			}
		}
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
	for _, unapproved := range []string{"Lombardia", "via ", "Viale ", "Piazza "} {
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
