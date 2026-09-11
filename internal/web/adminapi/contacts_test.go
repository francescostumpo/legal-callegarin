package adminapi

import (
	"context"
	"encoding/json"
	"errors"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/francescostumpo/legal-callegarin/internal/auth"
	"github.com/francescostumpo/legal-callegarin/internal/contacts"
	"github.com/francescostumpo/legal-callegarin/internal/storage/memory"
	webmiddleware "github.com/francescostumpo/legal-callegarin/internal/web/middleware"
)

type adminContactServiceStub struct {
	contacts.ContactService
	dashboard    contacts.DashboardSummary
	dashboardErr error
	page         contacts.ContactPage
	listOptions  contacts.ListOptions
	listErr      error
	detail       contacts.Contact
	getErr       error
	mutation     contacts.Contact
	mutationErr  error
	action       string
	id           string
	etag         string
}

func (stub *adminContactServiceStub) Dashboard(context.Context) (contacts.DashboardSummary, error) {
	return stub.dashboard, stub.dashboardErr
}
func (stub *adminContactServiceStub) List(_ context.Context, options contacts.ListOptions) (contacts.ContactPage, error) {
	stub.listOptions = options
	return stub.page, stub.listErr
}
func (stub *adminContactServiceStub) Get(_ context.Context, id string) (contacts.Contact, error) {
	stub.id = id
	return stub.detail, stub.getErr
}
func (stub *adminContactServiceStub) mutate(action, id, etag string) (contacts.Contact, error) {
	stub.action, stub.id, stub.etag = action, id, etag
	return stub.mutation, stub.mutationErr
}
func (stub *adminContactServiceStub) Open(_ context.Context, id, etag string) (contacts.Contact, error) {
	return stub.mutate("read", id, etag)
}
func (stub *adminContactServiceStub) Archive(_ context.Context, id, etag string) (contacts.Contact, error) {
	return stub.mutate("archive", id, etag)
}
func (stub *adminContactServiceStub) Restore(_ context.Context, id, etag string) (contacts.Contact, error) {
	return stub.mutate("restore", id, etag)
}
func (stub *adminContactServiceStub) ScheduleDeletion(_ context.Context, id, etag string) (contacts.Contact, error) {
	return stub.mutate("schedule-deletion", id, etag)
}
func (stub *adminContactServiceStub) CancelDeletion(_ context.Context, id, etag string) (contacts.Contact, error) {
	return stub.mutate("cancel-deletion", id, etag)
}

func TestDashboardAPIUsesStableCompleteCounts(t *testing.T) {
	stub := &adminContactServiceStub{dashboard: contacts.DashboardSummary{
		New: 3, Read: 4, Archived: 5, DeletionScheduled: 2, RetentionReview: 1, Purged: 6,
	}}
	handler := newContactAPIHandler(t, stub)
	response := serveAdminAPI(t, handler, http.MethodGet, "/api/admin/dashboard", "", true, false)
	if response.Code != http.StatusOK || response.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("dashboard = %d headers=%#v body=%q", response.Code, response.Header(), response.Body.String())
	}
	var body map[string]int
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	want := map[string]int{"new": 3, "read": 4, "archived": 5, "deletionScheduled": 2, "retentionReview": 1, "purged": 6}
	if len(body) != len(want) {
		t.Fatalf("dashboard body = %#v", body)
	}
	for key, value := range want {
		if body[key] != value {
			t.Fatalf("dashboard[%q] = %d, want %d", key, body[key], value)
		}
	}

	stub.dashboardErr = errors.New("storage contains private detail")
	response = serveAdminAPI(t, handler, http.MethodGet, "/api/admin/dashboard", "", true, false)
	if response.Code != http.StatusServiceUnavailable || strings.Contains(response.Body.String(), "private") || strings.Contains(response.Body.String(), `"new"`) {
		t.Fatalf("failed dashboard = %d %q", response.Code, response.Body.String())
	}
}

func TestContactListAPIParsesBoundedFiltersAndUsesSummaryDTO(t *testing.T) {
	now := time.Date(2026, 9, 11, 12, 0, 0, 0, time.UTC)
	due := now.AddDate(0, 0, 30)
	stub := &adminContactServiceStub{page: contacts.ContactPage{
		Items: []contacts.Contact{{
			ID: "contact-1", Name: "Mario Rossi", Email: "mario@example.test", Phone: "+39 secret", Message: "private message",
			State: contacts.StateArchived, CreatedAt: now, UpdatedAt: now, ReviewDueAt: now.AddDate(2, 0, 0), DeletionDueAt: &due, ETag: "private-etag",
		}},
		NextCursor: "next-cursor",
	}}
	handler := newContactAPIHandler(t, stub)
	response := serveAdminAPI(t, handler, http.MethodGet, "/api/admin/contacts?q=MARIO&state=archived&cursor=cursor-1&retentionReview=true&deletionScheduled=true", "", true, false)
	if response.Code != http.StatusOK {
		t.Fatalf("list = %d %q", response.Code, response.Body.String())
	}
	if stub.listOptions.Query != "MARIO" || stub.listOptions.Cursor != "cursor-1" || stub.listOptions.State == nil || *stub.listOptions.State != contacts.StateArchived || !stub.listOptions.RetentionReview || !stub.listOptions.DeletionScheduled || stub.listOptions.Limit != 25 {
		t.Fatalf("list options = %#v", stub.listOptions)
	}
	body := response.Body.String()
	for _, expected := range []string{`"items"`, `"nextCursor":"next-cursor"`, `"id":"contact-1"`, `"name":"Mario Rossi"`, `"state":"archived"`, `"deletionDueAt"`} {
		if !strings.Contains(body, expected) {
			t.Fatalf("list body missing %s: %s", expected, body)
		}
	}
	for _, private := range []string{"private message", "+39 secret", "private-etag", "consentVersion"} {
		if strings.Contains(body, private) {
			t.Fatalf("list body exposes %q: %s", private, body)
		}
	}
}

func TestContactListAPIRejectsMalformedOrOversizedQueries(t *testing.T) {
	handler := newContactAPIHandler(t, &adminContactServiceStub{})
	tests := []string{
		"/api/admin/contacts?state=unknown",
		"/api/admin/contacts?retentionReview=yes",
		"/api/admin/contacts?deletionScheduled=1",
		"/api/admin/contacts?q=" + strings.Repeat("x", contacts.MaxAdminSearchRunes+1),
		"/api/admin/contacts?q=one&q=two",
	}
	for _, target := range tests {
		response := serveAdminAPI(t, handler, http.MethodGet, target, "", true, false)
		if response.Code != http.StatusBadRequest {
			t.Errorf("GET %s = %d, want 400; body=%q", target, response.Code, response.Body.String())
		}
	}
}

func TestContactDetailIsSideEffectFreeAndLifecycleRequiresCurrentETag(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 9, 11, 12, 0, 0, 0, time.UTC)
	clock := &apiContactClock{now: now}
	repository := memory.NewContactRepository(clock.Now)
	service := contacts.NewService(repository, clock, &apiContactIDs{values: []string{"contact-1"}})
	created, err := service.Submit(ctx, contacts.Submission{
		Name: "Mario Rossi", Email: "mario@example.test", Phone: "+39 000 000000",
		Message: "Messaggio sufficientemente lungo", ConsentVersion: "privacy-v1",
	})
	if err != nil {
		t.Fatal(err)
	}
	handler := newContactAPIHandler(t, service)

	detail := serveAdminAPI(t, handler, http.MethodGet, "/api/admin/contacts/contact-1", "", true, false)
	if detail.Code != http.StatusOK || detail.Header().Get("ETag") == "" || !strings.Contains(detail.Body.String(), `"message":"Messaggio sufficientemente lungo"`) {
		t.Fatalf("detail = %d etag=%q body=%q", detail.Code, detail.Header().Get("ETag"), detail.Body.String())
	}
	stored, err := repository.Get(ctx, created.ID)
	if err != nil || stored.State != contacts.StateNew || stored.ETag != created.ETag {
		t.Fatalf("detail GET mutated contact: %#v, %v", stored, err)
	}

	missing := serveAdminAPI(t, handler, http.MethodPost, "/api/admin/contacts/contact-1/read", "", true, true)
	if missing.Code != http.StatusPreconditionRequired {
		t.Fatalf("missing If-Match = %d %q", missing.Code, missing.Body.String())
	}
	malformed := serveAdminAPI(t, handler, http.MethodPost, "/api/admin/contacts/contact-1/read", "raw-etag", true, true)
	if malformed.Code != http.StatusBadRequest {
		t.Fatalf("malformed If-Match = %d %q", malformed.Code, malformed.Body.String())
	}

	currentETag := detail.Header().Get("ETag")
	read := serveAdminAPI(t, handler, http.MethodPost, "/api/admin/contacts/contact-1/read", currentETag, true, true)
	if read.Code != http.StatusOK || read.Header().Get("ETag") == "" || read.Header().Get("ETag") == currentETag || !strings.Contains(read.Body.String(), `"state":"read"`) {
		t.Fatalf("read = %d etag=%q body=%q", read.Code, read.Header().Get("ETag"), read.Body.String())
	}
	stale := serveAdminAPI(t, handler, http.MethodPost, "/api/admin/contacts/contact-1/archive", currentETag, true, true)
	if stale.Code != http.StatusConflict {
		t.Fatalf("stale archive = %d %q", stale.Code, stale.Body.String())
	}

	archived := serveAdminAPI(t, handler, http.MethodPost, "/api/admin/contacts/contact-1/archive", read.Header().Get("ETag"), true, true)
	if archived.Code != http.StatusOK || !strings.Contains(archived.Body.String(), `"state":"archived"`) {
		t.Fatalf("archive = %d %q", archived.Code, archived.Body.String())
	}
	restored := serveAdminAPI(t, handler, http.MethodPost, "/api/admin/contacts/contact-1/restore", archived.Header().Get("ETag"), true, true)
	if restored.Code != http.StatusOK || !strings.Contains(restored.Body.String(), `"state":"read"`) {
		t.Fatalf("restore = %d %q", restored.Code, restored.Body.String())
	}
	clock.now = now.Add(time.Hour)
	scheduled := serveAdminAPI(t, handler, http.MethodPost, "/api/admin/contacts/contact-1/schedule-deletion", restored.Header().Get("ETag"), true, true)
	if scheduled.Code != http.StatusOK || !strings.Contains(scheduled.Body.String(), `"deletionDueAt"`) || !strings.Contains(scheduled.Body.String(), `"state":"read"`) {
		t.Fatalf("schedule deletion = %d %q", scheduled.Code, scheduled.Body.String())
	}
	cancelled := serveAdminAPI(t, handler, http.MethodPost, "/api/admin/contacts/contact-1/cancel-deletion", scheduled.Header().Get("ETag"), true, true)
	if cancelled.Code != http.StatusOK || strings.Contains(cancelled.Body.String(), `"deletionDueAt"`) || !strings.Contains(cancelled.Body.String(), `"state":"read"`) {
		t.Fatalf("cancel deletion = %d %q", cancelled.Code, cancelled.Body.String())
	}
}

func TestContactAPIMutationAuthCSRFAndErrorMapping(t *testing.T) {
	stub := &adminContactServiceStub{mutation: contacts.Contact{ID: "contact-1", ETag: "etag-next"}}
	handler := newContactAPIHandler(t, stub)
	if response := serveAdminAPI(t, handler, http.MethodPost, "/api/admin/contacts/contact-1/read", `"ZXRhZw"`, false, false); response.Code != http.StatusUnauthorized {
		t.Fatalf("unauthenticated mutation = %d", response.Code)
	}
	if response := serveAdminAPI(t, handler, http.MethodPost, "/api/admin/contacts/contact-1/read", `"ZXRhZw"`, true, false); response.Code != http.StatusForbidden {
		t.Fatalf("missing CSRF = %d", response.Code)
	}

	for name, testCase := range map[string]struct {
		err  error
		want int
	}{
		"not found":          {err: contacts.ErrNotFound, want: http.StatusNotFound},
		"stale":              {err: contacts.ErrConflict, want: http.StatusConflict},
		"invalid transition": {err: contacts.ErrInvalidTransition, want: http.StatusConflict},
		"commit unknown":     {err: contacts.ErrCommitUnknown, want: http.StatusServiceUnavailable},
		"unavailable":        {err: errors.New("storage private detail"), want: http.StatusServiceUnavailable},
	} {
		t.Run(name, func(t *testing.T) {
			stub.mutationErr = testCase.err
			response := serveAdminAPI(t, handler, http.MethodPost, "/api/admin/contacts/contact-1/read", `"ZXRhZw"`, true, true)
			if response.Code != testCase.want || strings.Contains(response.Body.String(), "private detail") {
				t.Fatalf("mutation error = %d %q", response.Code, response.Body.String())
			}
		})
	}
}

func TestAdminNestedFallbackAndAPIMissesStayScoped(t *testing.T) {
	handler := newContactAPIHandler(t, &adminContactServiceStub{})
	spa := serveAdminAPI(t, handler, http.MethodGet, "/admin/contatti/contact-1", "", true, false)
	if spa.Code != http.StatusOK || !strings.Contains(spa.Body.String(), `<div id="root"></div>`) {
		t.Fatalf("SPA fallback = %d %q", spa.Code, spa.Body.String())
	}
	asset := serveAdminAPI(t, handler, http.MethodGet, "/admin/assets/missing.js", "", true, false)
	if asset.Code != http.StatusNotFound || strings.Contains(asset.Body.String(), `<div id="root"></div>`) {
		t.Fatalf("asset miss = %d %q", asset.Code, asset.Body.String())
	}
	api := serveAdminAPI(t, handler, http.MethodGet, "/api/admin/not-a-route", "", true, false)
	if api.Code != http.StatusNotFound || api.Header().Get("Content-Type") != "application/json; charset=utf-8" || !strings.Contains(api.Body.String(), `"error"`) {
		t.Fatalf("API miss = %d headers=%#v body=%q", api.Code, api.Header(), api.Body.String())
	}
}

func newContactAPIHandler(t *testing.T, service contacts.ContactService) http.Handler {
	t.Helper()
	now := time.Date(2026, 9, 11, 12, 0, 0, 0, time.UTC)
	assets := fstest.MapFS{
		"admin/dist/index.html":           {Data: []byte(`<!doctype html><div id="root"></div>`)},
		"admin/dist/assets/index-test.js": {Data: []byte("console.log('stub')")},
	}
	var assetFS fs.FS = assets
	key := []byte("0123456789abcdef0123456789abcdef")
	inner, err := New(Options{
		Credentials: &verifierStub{}, ConfiguredUsername: "admin", Sessions: &sessionsStub{}, Contacts: service,
		Assets: assetFS, SessionKey: key, PublicBaseURL: "https://studio.example.test", Now: func() time.Time { return now }, LoginCapacity: 100,
	})
	if err != nil {
		t.Fatal(err)
	}
	authenticator := &authenticatorStub{session: auth.Session{Username: "admin", ExpiresAt: now.Add(8 * time.Hour)}}
	handler, err := webmiddleware.New(inner, webmiddleware.Options{
		Authenticator: authenticator, SessionKey: key, PublicBaseURL: "https://studio.example.test", Now: func() time.Time { return now },
	})
	if err != nil {
		t.Fatal(err)
	}
	return handler
}

func serveAdminAPI(t *testing.T, handler http.Handler, method, target, ifMatch string, authenticated, csrf bool) *httptest.ResponseRecorder {
	t.Helper()
	request := httptest.NewRequest(method, "https://studio.example.test"+target, nil)
	if authenticated {
		request.AddCookie(&http.Cookie{Name: webmiddleware.SessionCookieName, Value: "raw-session"})
	}
	if ifMatch != "" {
		request.Header.Set("If-Match", ifMatch)
	}
	if csrf {
		request.Header.Set("Origin", "https://studio.example.test")
		request.Header.Set("X-CSRF-Token", webmiddleware.CSRFToken("raw-session", []byte("0123456789abcdef0123456789abcdef")))
	}
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	return response
}

type apiContactClock struct{ now time.Time }

func (clock *apiContactClock) Now() time.Time { return clock.now }

type apiContactIDs struct {
	values []string
	index  int
}

func (ids *apiContactIDs) NewID() string {
	value := ids.values[ids.index]
	ids.index++
	return value
}
