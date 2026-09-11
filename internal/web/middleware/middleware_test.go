package middleware

import (
	"context"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/francescostumpo/legal-callegarin/internal/auth"
)

type authenticatorStub struct {
	session auth.Session
	err     error
	raw     string
}

func (stub *authenticatorStub) Validate(_ context.Context, raw string) (auth.Session, error) {
	stub.raw = raw
	return stub.session, stub.err
}

func TestProtectedRoutesRequireSessionButSafeRequestSkipsOriginAndCSRF(t *testing.T) {
	authenticator := &authenticatorStub{session: auth.Session{Username: "admin"}, err: auth.ErrUnauthenticated}
	handler, err := New(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		principal, ok := PrincipalFromContext(request.Context())
		if !ok || principal.Username != "admin" {
			t.Fatal("authenticated principal missing")
		}
		response.WriteHeader(http.StatusNoContent)
	}), Options{Authenticator: authenticator, SessionKey: []byte("0123456789abcdef0123456789abcdef"), PublicBaseURL: "https://studio.example.test"})
	if err != nil {
		t.Fatal(err)
	}

	unauthorized := httptest.NewRecorder()
	handler.ServeHTTP(unauthorized, httptest.NewRequest(http.MethodGet, "https://studio.example.test/api/admin/session", nil))
	if unauthorized.Code != http.StatusUnauthorized || unauthorized.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("unauthorized response = %d cache %q", unauthorized.Code, unauthorized.Header().Get("Cache-Control"))
	}

	authenticator.err = nil
	request := httptest.NewRequest(http.MethodGet, "https://studio.example.test/api/admin/session", nil)
	request.AddCookie(&http.Cookie{Name: SessionCookieName, Value: "raw-token"})
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusNoContent || authenticator.raw != "raw-token" {
		t.Fatalf("authenticated response = %d raw=%q", response.Code, authenticator.raw)
	}
}

func TestUnsafeAdminAPIRequiresExactOriginAndSessionCSRF(t *testing.T) {
	key := []byte("0123456789abcdef0123456789abcdef")
	authenticator := &authenticatorStub{session: auth.Session{Username: "admin"}}
	handler, err := New(http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
		response.WriteHeader(http.StatusNoContent)
	}), Options{Authenticator: authenticator, SessionKey: key, PublicBaseURL: "https://studio.example.test"})
	if err != nil {
		t.Fatal(err)
	}

	for name, testCase := range map[string]struct {
		origin string
		csrf   string
		want   int
	}{
		"missing origin": {csrf: CSRFToken("raw-token", key), want: http.StatusForbidden},
		"wrong origin":   {origin: "https://evil.example", csrf: CSRFToken("raw-token", key), want: http.StatusForbidden},
		"missing csrf":   {origin: "https://studio.example.test", want: http.StatusForbidden},
		"bad csrf":       {origin: "https://studio.example.test", csrf: "bad", want: http.StatusForbidden},
		"valid":          {origin: "https://studio.example.test", csrf: CSRFToken("raw-token", key), want: http.StatusNoContent},
	} {
		t.Run(name, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodDelete, "https://studio.example.test/api/admin/session", nil)
			request.AddCookie(&http.Cookie{Name: SessionCookieName, Value: "raw-token"})
			request.Header.Set("Origin", testCase.origin)
			request.Header.Set("X-CSRF-Token", testCase.csrf)
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, request)
			if response.Code != testCase.want {
				t.Fatalf("status = %d, want %d", response.Code, testCase.want)
			}
		})
	}
}

func TestMiddlewareAddsSecurityHeadersRequestIDAndRecoversWithoutLeaking(t *testing.T) {
	handler, err := New(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		panic("secret-panic-value")
	}), Options{
		SessionKey:    []byte("0123456789abcdef0123456789abcdef"),
		PublicBaseURL: "https://studio.example.test",
		Logger:        slog.New(slog.NewTextHandler(&strings.Builder{}, nil)),
		RequestID:     func() string { return "request-123" },
	})
	if err != nil {
		t.Fatal(err)
	}
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "https://studio.example.test/health/live", nil))
	if response.Code != http.StatusInternalServerError || strings.Contains(response.Body.String(), "secret") {
		t.Fatalf("recovery response = %d %q", response.Code, response.Body.String())
	}
	for name := range map[string]bool{"Content-Security-Policy": true, "Strict-Transport-Security": true, "X-Content-Type-Options": true, "Referrer-Policy": true} {
		if response.Header().Get(name) == "" {
			t.Errorf("missing %s", name)
		}
	}
	if response.Header().Get("X-Request-ID") != "request-123" || response.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("request/recovery headers = %#v", response.Header())
	}
}

func TestMiddlewareCapsRequestBodiesBeforeHandler(t *testing.T) {
	handler, err := New(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		buffer := make([]byte, 32)
		_, readErr := request.Body.Read(buffer)
		if readErr == nil {
			t.Fatal("oversized body was not capped")
		}
		response.WriteHeader(http.StatusNoContent)
	}), Options{SessionKey: []byte("0123456789abcdef0123456789abcdef"), PublicBaseURL: "https://studio.example.test", MaxBodyBytes: 4})
	if err != nil {
		t.Fatal(err)
	}
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodPost, "https://studio.example.test/unprotected", strings.NewReader("oversized")))
	if response.Code != http.StatusNoContent {
		t.Fatalf("status = %d", response.Code)
	}
}

func TestExpiredSessionClearsExactHostCookie(t *testing.T) {
	authenticator := &authenticatorStub{err: auth.ErrUnauthenticated}
	handler, err := New(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}), Options{Authenticator: authenticator, SessionKey: []byte("0123456789abcdef0123456789abcdef"), PublicBaseURL: "https://studio.example.test", Now: time.Now})
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodGet, "https://studio.example.test/admin", nil)
	request.AddCookie(&http.Cookie{Name: SessionCookieName, Value: "expired"})
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusSeeOther {
		t.Fatalf("status = %d", response.Code)
	}
	cookies := response.Result().Cookies()
	if len(cookies) != 1 || cookies[0].Name != SessionCookieName || cookies[0].Path != "/" || !cookies[0].Secure || !cookies[0].HttpOnly || cookies[0].SameSite != http.SameSiteStrictMode || cookies[0].Domain != "" || cookies[0].MaxAge >= 0 {
		t.Fatalf("cleared cookie = %#v", cookies)
	}
}

func TestNewRejectsInvalidConfiguration(t *testing.T) {
	for name, options := range map[string]Options{
		"short key": {SessionKey: []byte("short"), PublicBaseURL: "https://studio.example.test"},
		"http base": {SessionKey: []byte("0123456789abcdef0123456789abcdef"), PublicBaseURL: "http://studio.example.test"},
		"base path": {SessionKey: []byte("0123456789abcdef0123456789abcdef"), PublicBaseURL: "https://studio.example.test/path"},
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := New(http.NotFoundHandler(), options); err == nil {
				t.Fatal("New error = nil")
			}
		})
	}
}
