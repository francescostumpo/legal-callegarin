package app

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/francescostumpo/legal-callegarin/internal/articles"
	"github.com/francescostumpo/legal-callegarin/internal/auth"
	"github.com/francescostumpo/legal-callegarin/internal/config"
	"github.com/francescostumpo/legal-callegarin/internal/contacts"
	storagebundle "github.com/francescostumpo/legal-callegarin/internal/storage"
	"github.com/francescostumpo/legal-callegarin/internal/storage/memory"
	publicweb "github.com/francescostumpo/legal-callegarin/internal/web/public"
	"github.com/francescostumpo/legal-callegarin/internal/webassets"
)

func TestNewComposesProtectedAdminRoutes(t *testing.T) {
	t.Parallel()
	phc, err := auth.HashPassword([]byte("correct-password"), bytes.NewReader(bytes.Repeat([]byte{0x41}, 16)))
	if err != nil {
		t.Fatal(err)
	}
	handler, err := New(Options{
		Config: config.Config{
			Environment:       "test",
			StorageMode:       "memory",
			PublicBaseURL:     "https://studio.example.test",
			AdminUsername:     "admin",
			AdminPasswordHash: phc,
			SessionKey:        []byte("0123456789abcdef0123456789abcdef"),
		},
		Assets: webassets.Files,
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	protected := httptest.NewRecorder()
	handler.ServeHTTP(protected, httptest.NewRequest(http.MethodGet, "https://studio.example.test/admin", nil))
	if protected.Code != http.StatusSeeOther || protected.Header().Get("Location") != "/admin/login" || protected.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("protected admin = %d location=%q cache=%q", protected.Code, protected.Header().Get("Location"), protected.Header().Get("Cache-Control"))
	}
	login := httptest.NewRecorder()
	handler.ServeHTTP(login, httptest.NewRequest(http.MethodGet, "https://studio.example.test/admin/login", nil))
	if login.Code != http.StatusOK || login.Header().Get("Content-Security-Policy") == "" {
		t.Fatalf("admin login = %d headers=%#v", login.Code, login.Header())
	}
}

func TestNewWiresAuthenticatedContactAPIAndNestedAdminFallback(t *testing.T) {
	password := "correct-password"
	phc, err := auth.HashPassword([]byte(password), bytes.NewReader(bytes.Repeat([]byte{0x61}, 16)))
	if err != nil {
		t.Fatal(err)
	}
	handler, err := New(Options{
		Config: config.Config{
			Environment: "test", StorageMode: "memory", PublicBaseURL: "https://studio.example.test",
			AdminUsername: "admin", AdminPasswordHash: phc, SessionKey: []byte("0123456789abcdef0123456789abcdef"),
		},
		Assets: webassets.Files,
	})
	if err != nil {
		t.Fatal(err)
	}
	loginPage := httptest.NewRecorder()
	handler.ServeHTTP(loginPage, httptest.NewRequest(http.MethodGet, "https://studio.example.test/admin/login", nil))
	token := regexp.MustCompile(`name="started" value="([^"]+)"`).FindStringSubmatch(loginPage.Body.String())
	if len(token) != 2 {
		t.Fatalf("login token missing: %q", loginPage.Body.String())
	}
	form := url.Values{"username": {"admin"}, "password": {password}, "started": {token[1]}}
	loginRequest := httptest.NewRequest(http.MethodPost, "https://studio.example.test/admin/login", strings.NewReader(form.Encode()))
	loginRequest.RemoteAddr = "192.0.2.1:1234"
	loginRequest.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	loginRequest.Header.Set("Origin", "https://studio.example.test")
	login := httptest.NewRecorder()
	handler.ServeHTTP(login, loginRequest)
	if login.Code != http.StatusSeeOther || len(login.Result().Cookies()) != 1 {
		t.Fatalf("login = %d cookies=%#v body=%q", login.Code, login.Result().Cookies(), login.Body.String())
	}
	cookie := login.Result().Cookies()[0]

	for target, wantType := range map[string]string{
		"/api/admin/contacts":       "application/json; charset=utf-8",
		"/admin/contatti/contact-1": "text/html; charset=utf-8",
	} {
		request := httptest.NewRequest(http.MethodGet, "https://studio.example.test"+target, nil)
		request.AddCookie(cookie)
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		if response.Code != http.StatusOK || response.Header().Get("Content-Type") != wantType {
			t.Fatalf("GET %s = %d type=%q body=%q", target, response.Code, response.Header().Get("Content-Type"), response.Body.String())
		}
	}
}

func TestNewRejectsPartialOrMalformedAdminCredentials(t *testing.T) {
	t.Parallel()
	for _, cfg := range []config.Config{
		{PublicBaseURL: "https://studio.example.test", AdminUsername: "admin"},
		{PublicBaseURL: "https://studio.example.test", AdminUsername: "admin", AdminPasswordHash: "malformed", SessionKey: []byte("0123456789abcdef0123456789abcdef")},
	} {
		if _, err := New(Options{Config: cfg, Assets: webassets.Files}); err == nil {
			t.Fatalf("New(%#v) error = nil", cfg)
		}
	}
}

type readinessProbe struct {
	err         error
	hasDeadline bool
}

func (probe *readinessProbe) Ready(ctx context.Context) error {
	_, probe.hasDeadline = ctx.Deadline()
	return probe.err
}

func TestHealthReadinessIsDependencyAwareAndLivenessIsProcessOnly(t *testing.T) {
	t.Parallel()
	probe := &readinessProbe{err: errors.New("dependency unavailable")}
	handler, err := New(Options{
		Config:  config.Config{PublicBaseURL: "https://studio.example.test"},
		Assets:  webassets.Files,
		Storage: &storagebundle.Bundle{Readiness: probe},
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	ready := httptest.NewRecorder()
	handler.ServeHTTP(ready, httptest.NewRequest(http.MethodGet, "/health/ready", nil))
	if ready.Code != http.StatusServiceUnavailable || !probe.hasDeadline {
		t.Fatalf("readiness = status %d, deadline %t", ready.Code, probe.hasDeadline)
	}
	live := httptest.NewRecorder()
	handler.ServeHTTP(live, httptest.NewRequest(http.MethodGet, "/health/live", nil))
	if live.Code != http.StatusOK {
		t.Fatalf("liveness status = %d", live.Code)
	}
	probe.err = nil
	ready = httptest.NewRecorder()
	handler.ServeHTTP(ready, httptest.NewRequest(http.MethodGet, "/health/ready", nil))
	if ready.Code != http.StatusOK {
		t.Fatalf("healthy readiness status = %d", ready.Code)
	}
}

func TestNewRejectsAzureModeWithoutCoherentStorageBundle(t *testing.T) {
	t.Parallel()
	for _, bundle := range []*storagebundle.Bundle{nil, {Readiness: &readinessProbe{}}} {
		_, err := New(Options{Config: config.Config{StorageMode: "azure", PublicBaseURL: "https://studio.example.test"}, Assets: webassets.Files, Storage: bundle})
		if err == nil {
			t.Fatalf("New(Storage: %#v) error = nil, want Azure storage bundle error", bundle)
		}
	}
}

func TestHandler(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		path       string
		wantStatus int
		wantType   string
	}{
		{
			name:       "liveness",
			path:       "/health/live",
			wantStatus: http.StatusOK,
			wantType:   "text/plain; charset=utf-8",
		},
		{
			name:       "public home",
			path:       "/",
			wantStatus: http.StatusOK,
			wantType:   "text/html; charset=utf-8",
		},
	}
	handler, err := New(Options{
		Config: config.Config{PublicBaseURL: "https://studio.example.test"},
		Assets: webassets.Files,
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			request := httptest.NewRequest(http.MethodGet, tt.path, nil)
			response := httptest.NewRecorder()

			handler.ServeHTTP(response, request)

			if response.Code != tt.wantStatus {
				t.Fatalf("status = %d, want %d", response.Code, tt.wantStatus)
			}
			if got := response.Header().Get("Content-Type"); got != tt.wantType {
				t.Fatalf("Content-Type = %q, want %q", got, tt.wantType)
			}
		})
	}
}

func TestNewComposesDevelopmentContactFormWithInjectedClockAndSigningKey(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 9, 11, 12, 0, 0, 0, time.UTC)
	handler, err := New(Options{
		Config: config.Config{
			Environment:   "development",
			PublicBaseURL: "https://studio.example.test",
			StorageMode:   "memory",
			SessionKey:    []byte("configuration-key-that-must-not-win"),
		},
		Assets:            webassets.Files,
		SessionSigningKey: []byte("0123456789abcdef0123456789abcdef"),
		RateClock:         func() time.Time { return now },
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	get := httptest.NewRecorder()
	handler.ServeHTTP(get, httptest.NewRequest(http.MethodGet, "https://studio.example.test/contatti", nil))
	tokenMatch := regexp.MustCompile(`name="started" value="([^"]+)"`).FindStringSubmatch(get.Body.String())
	if len(tokenMatch) != 2 {
		t.Fatalf("contact GET lacks signed timestamp: %q", get.Body.String())
	}
	now = now.Add(3 * time.Second)
	form := url.Values{
		"name": {"Mario Rossi"}, "email": {"mario@example.test"}, "phone": {""},
		"message": {"Messaggio sufficientemente lungo"}, "privacy": {"accepted"},
		"website": {""}, "started": {tokenMatch[1]},
	}
	request := httptest.NewRequest(http.MethodPost, "https://studio.example.test/contatti", strings.NewReader(form.Encode()))
	request.RemoteAddr = "192.0.2.2:4000"
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	request.Header.Set("Origin", "https://studio.example.test")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusSeeOther {
		t.Fatalf("contact POST status = %d, want 303; body=%q", response.Code, response.Body.String())
	}
	if response.Header().Get("Set-Cookie") != "" {
		t.Fatalf("contact POST Set-Cookie = %q", response.Header().Get("Set-Cookie"))
	}
}

func TestNewInjectsContactService(t *testing.T) {
	t.Parallel()

	service := &appContactService{}
	now := time.Date(2026, 9, 11, 12, 0, 10, 0, time.UTC)
	handler, err := New(Options{
		Config:            config.Config{PublicBaseURL: "https://studio.example.test"},
		Assets:            webassets.Files,
		ContactService:    service,
		SessionSigningKey: []byte("0123456789abcdef0123456789abcdef"),
		RateClock:         func() time.Time { return now },
		TrustedProxy:      true,
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	get := httptest.NewRecorder()
	handler.ServeHTTP(get, httptest.NewRequest(http.MethodGet, "https://studio.example.test/contatti", nil))
	token := regexp.MustCompile(`name="started" value="([^"]+)"`).FindStringSubmatch(get.Body.String())[1]
	now = now.Add(3 * time.Second)
	form := url.Values{
		"name": {"Mario Rossi"}, "email": {"mario@example.test"}, "phone": {""},
		"message": {"Messaggio sufficientemente lungo"}, "privacy": {"accepted"},
		"website": {""}, "started": {token},
	}
	request := httptest.NewRequest(http.MethodPost, "https://studio.example.test/contatti", strings.NewReader(form.Encode()))
	request.RemoteAddr = "192.0.2.2:4000"
	request.Header.Set("X-Forwarded-For", "203.0.113.5")
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	request.Header.Set("Origin", "https://studio.example.test")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusSeeOther || service.calls != 1 || service.submission.ConsentVersion == "" {
		t.Fatalf("contact injection = status %d calls %d submission %#v", response.Code, service.calls, service.submission)
	}
}

type appContactService struct {
	contacts.ContactService
	calls      int
	submission contacts.Submission
}

func (service *appContactService) Submit(_ context.Context, submission contacts.Submission) (contacts.Contact, error) {
	service.calls++
	service.submission = submission
	return contacts.Contact{}, nil
}

func TestNewRejectsInvalidPublicAssets(t *testing.T) {
	t.Parallel()

	_, err := New(Options{Config: config.Config{PublicBaseURL: "https://studio.example.test"}, Assets: fstest.MapFS{}})
	if err == nil {
		t.Fatal("New() error = nil, want template initialization error")
	}
}

func TestNewInjectsPublicArticleReader(t *testing.T) {
	t.Parallel()

	handler, err := New(Options{
		Config:   config.Config{PublicBaseURL: "https://studio.example.test"},
		Assets:   webassets.Files,
		Articles: failingArticleReader{},
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	request := httptest.NewRequest(http.MethodGet, "/sentenze-e-riflessioni", nil)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)

	if response.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusServiceUnavailable)
	}
	if got := response.Header().Get("Cache-Control"); got != "no-store" {
		t.Fatalf("Cache-Control = %q, want no-store", got)
	}
	if got := response.Header().Get("X-Robots-Tag"); got != "noindex, nofollow" {
		t.Fatalf("X-Robots-Tag = %q, want noindex, nofollow", got)
	}
}

func TestNewSharesPublicationInvalidationWithArticleService(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	clock := &appArticleClock{now: time.Date(2026, 9, 11, 10, 0, 0, 0, time.UTC)}
	events := publicweb.NewArticleEventSink()
	service := articles.NewService(
		memory.NewArticleMetadataRepository(),
		memory.NewArticleBodyStore(clock.Now),
		clock,
		&appArticleIDs{},
		events,
	)
	draft, err := service.CreateDraft(ctx, appArticleDraft("wired-cache", "Titolo prima versione", "corpo prima versione"))
	if err != nil {
		t.Fatalf("CreateDraft() error = %v", err)
	}
	handler, err := New(Options{
		Config:        config.Config{PublicBaseURL: "https://studio.example.test"},
		Assets:        webassets.Files,
		Articles:      service,
		ArticleEvents: events,
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	published, err := service.Publish(ctx, draft.ID, draft.ETag)
	if err != nil {
		t.Fatalf("Publish(first) error = %v", err)
	}
	first := httptest.NewRecorder()
	handler.ServeHTTP(first, httptest.NewRequest(http.MethodGet, "/sentenze-e-riflessioni/wired-cache", nil))
	if first.Code != http.StatusOK || !strings.Contains(first.Body.String(), "corpo prima versione") {
		t.Fatalf("first response = status %d, body %q", first.Code, first.Body.String())
	}

	clock.now = clock.now.Add(time.Minute)
	saved, err := service.SaveDraft(ctx, published.ID, appArticleDraft("wired-cache", "Titolo seconda versione", "corpo seconda versione"), published.ETag)
	if err != nil {
		t.Fatalf("SaveDraft() error = %v", err)
	}
	if _, err := service.Publish(ctx, saved.ID, saved.ETag); err != nil {
		t.Fatalf("Publish(second) error = %v", err)
	}
	updated := httptest.NewRecorder()
	handler.ServeHTTP(updated, httptest.NewRequest(http.MethodGet, "/sentenze-e-riflessioni/wired-cache", nil))
	if updated.Code != http.StatusOK || !strings.Contains(updated.Body.String(), "corpo seconda versione") || strings.Contains(updated.Body.String(), "corpo prima versione") {
		t.Fatalf("updated response = status %d, body %q", updated.Code, updated.Body.String())
	}
}

type failingArticleReader struct{}

type appArticleClock struct{ now time.Time }

func (clock *appArticleClock) Now() time.Time { return clock.now }

type appArticleIDs struct{ next int }

func (ids *appArticleIDs) NewID() string {
	ids.next++
	return fmt.Sprintf("app-article-%d", ids.next)
}

func appArticleDraft(slug, title, body string) articles.DraftInput {
	return articles.DraftInput{
		Slug: slug, Title: title, Summary: "Sommario sufficientemente descrittivo", Area: "obbligazioni-e-contratti", CoverID: "contracts-pen",
		Body: articles.Body{SchemaVersion: 1, Document: []byte(`{"type":"doc","content":[{"type":"paragraph","content":[{"type":"text","text":"` + body + `"}]}]}`)},
	}
}

func (failingArticleReader) ListPublished(context.Context, articles.ListOptions) (articles.ArticlePage, error) {
	return articles.ArticlePage{}, errors.New("article storage unavailable")
}

func (failingArticleReader) GetPublished(context.Context, string) (articles.ArticleWithBody, error) {
	return articles.ArticleWithBody{}, errors.New("article storage unavailable")
}
