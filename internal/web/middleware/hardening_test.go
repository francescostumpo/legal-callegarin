package middleware

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/francescostumpo/legal-callegarin/internal/auth"
)

func TestRequestIDAcceptsOnlyBoundedSafeValues(t *testing.T) {
	handler, err := New(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		response.Header().Set("Seen-Request-ID", RequestIDFromContext(request.Context()))
	}), Options{
		SessionKey:    []byte("0123456789abcdef0123456789abcdef"),
		PublicBaseURL: "https://studio.example.test",
		RequestID:     func() string { return "generated-safe-id" },
	})
	if err != nil {
		t.Fatal(err)
	}
	for name, testCase := range map[string]struct {
		provided string
		want     string
	}{
		"valid propagated": {provided: "edge-Request_123.abc", want: "edge-Request_123.abc"},
		"malformed":        {provided: "bad request id\nsecret", want: "generated-safe-id"},
		"unbounded":        {provided: strings.Repeat("a", 65), want: "generated-safe-id"},
	} {
		t.Run(name, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodGet, "https://studio.example.test/health/live", nil)
			request.Header.Set("X-Request-ID", testCase.provided)
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, request)
			if got := response.Header().Get("X-Request-ID"); got != testCase.want {
				t.Fatalf("X-Request-ID = %q, want %q", got, testCase.want)
			}
			if got := response.Header().Get("Seen-Request-ID"); got != testCase.want {
				t.Fatalf("context request ID = %q, want %q", got, testCase.want)
			}
		})
	}
}

func TestProductionCanonicalRedirectAndTrustedSchemePolicy(t *testing.T) {
	called := 0
	newHandler := func(hops int) http.Handler {
		handler, err := New(http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
			called++
			response.WriteHeader(http.StatusNoContent)
		}), Options{
			SessionKey:       []byte("0123456789abcdef0123456789abcdef"),
			PublicBaseURL:    "https://studio.example.test",
			Environment:      "production",
			TrustedProxyHops: hops,
			RequestID:        func() string { return "request-redirect" },
		})
		if err != nil {
			t.Fatal(err)
		}
		return handler
	}

	spoofed := httptest.NewRequest(http.MethodGet, "http://www.example.test/percorso?q=valore", nil)
	spoofed.RemoteAddr = "192.0.2.10:1234"
	spoofed.Header.Set("X-Forwarded-Proto", "https")
	response := httptest.NewRecorder()
	newHandler(0).ServeHTTP(response, spoofed)
	if response.Code != http.StatusPermanentRedirect || response.Header().Get("Location") != "https://studio.example.test/percorso?q=valore" || response.Header().Get("X-Request-ID") == "" || response.Header().Get("Strict-Transport-Security") == "" || called != 0 {
		t.Fatalf("untrusted redirect = status %d headers %#v called=%d", response.Code, response.Header(), called)
	}

	trusted := httptest.NewRequest(http.MethodGet, "http://studio.example.test/percorso", nil)
	trusted.RemoteAddr = "10.0.0.4:1234"
	trusted.Header.Set("X-Forwarded-For", "198.51.100.8")
	trusted.Header.Set("X-Forwarded-Proto", "https")
	response = httptest.NewRecorder()
	newHandler(1).ServeHTTP(response, trusted)
	if response.Code != http.StatusNoContent || called != 1 {
		t.Fatalf("trusted request = status %d called=%d location=%q", response.Code, called, response.Header().Get("Location"))
	}

	health := httptest.NewRequest(http.MethodGet, "http://platform.internal/health/ready?probe=1", nil)
	health.RemoteAddr = "10.0.0.4:1234"
	response = httptest.NewRecorder()
	newHandler(1).ServeHTTP(response, health)
	if response.Code != http.StatusNoContent || called != 2 || response.Header().Get("Location") != "" {
		t.Fatalf("health exemption = status %d called=%d headers=%#v", response.Code, called, response.Header())
	}
}

func TestProductionCSPNonceAndPreviewFramingArePerResponse(t *testing.T) {
	handler, err := New(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		response.Header().Set("Seen-Nonce", CSPNonceFromContext(request.Context()))
	}), Options{
		SessionKey:       []byte("0123456789abcdef0123456789abcdef"),
		PublicBaseURL:    "https://studio.example.test",
		Environment:      "production",
		TrustedProxyHops: 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	nonces := map[string]bool{}
	for _, path := range []string{"/", "/"} {
		request := httptest.NewRequest(http.MethodGet, "https://studio.example.test"+path, nil)
		request.RemoteAddr = "10.0.0.4:1234"
		request.Header.Set("X-Forwarded-For", "198.51.100.8")
		request.Header.Set("X-Forwarded-Proto", "https")
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		nonce := response.Header().Get("Seen-Nonce")
		if nonce == "" || !strings.Contains(response.Header().Get("Content-Security-Policy"), "'nonce-"+nonce+"'") {
			t.Fatalf("nonce response headers = %#v", response.Header())
		}
		nonces[nonce] = true
	}
	if len(nonces) != 2 {
		t.Fatalf("nonces were reused: %#v", nonces)
	}

	request := httptest.NewRequest(http.MethodGet, "https://studio.example.test/admin/preview/articles/article-1", nil)
	request.RemoteAddr = "10.0.0.4:1234"
	request.Header.Set("X-Forwarded-For", "198.51.100.8")
	request.Header.Set("X-Forwarded-Proto", "https")
	request.AddCookie(&http.Cookie{Name: SessionCookieName, Value: "raw"})
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Header().Get("X-Frame-Options") != "SAMEORIGIN" || !strings.Contains(response.Header().Get("Content-Security-Policy"), "frame-ancestors 'self'") {
		t.Fatalf("preview framing = %#v", response.Header())
	}
}

func TestHandlerTimeoutReturnsAtomicNestedAPIError(t *testing.T) {
	authenticator := &authenticatorStub{session: auth.Session{Username: "admin"}}
	handler, err := New(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		response.WriteHeader(http.StatusOK)
		_, _ = response.Write([]byte("partial-secret"))
		select {
		case <-request.Context().Done():
		case <-time.After(50 * time.Millisecond):
		}
	}), Options{
		Authenticator:  authenticator,
		SessionKey:     []byte("0123456789abcdef0123456789abcdef"),
		PublicBaseURL:  "https://studio.example.test",
		HandlerTimeout: 10 * time.Millisecond,
		RequestID:      func() string { return "request-timeout" },
	})
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodGet, "https://studio.example.test/api/admin/session", nil)
	request.AddCookie(&http.Cookie{Name: SessionCookieName, Value: "raw"})
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusServiceUnavailable || strings.Contains(response.Body.String(), "partial-secret") || !strings.Contains(response.Body.String(), `"code":"request_timeout"`) || !strings.Contains(response.Body.String(), `"requestId":"request-timeout"`) {
		t.Fatalf("timeout response = status %d body %q", response.Code, response.Body.String())
	}
}

func TestDefaultSecurityHeadersAreExplicitAndDoNotEmitDevelopmentHSTS(t *testing.T) {
	handler, err := New(http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {}), Options{
		SessionKey:    []byte("0123456789abcdef0123456789abcdef"),
		PublicBaseURL: "https://studio.example.test",
		RequestID:     func() string { return "request-id" },
	})
	if err != nil {
		t.Fatal(err)
	}
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "https://studio.example.test/", nil))
	if got := response.Header().Get("Strict-Transport-Security"); got != "" {
		t.Fatalf("development HSTS = %q", got)
	}
	csp := response.Header().Get("Content-Security-Policy")
	for _, directive := range []string{"default-src 'self'", "base-uri 'none'", "object-src 'none'", "form-action 'self'", "frame-ancestors 'none'", "script-src 'self'", "style-src 'self'", "font-src 'self'", "img-src 'self'", "connect-src 'self'"} {
		if !strings.Contains(csp, directive) {
			t.Errorf("CSP %q lacks %q", csp, directive)
		}
	}
	if strings.Contains(csp, "unsafe-inline") {
		t.Fatalf("CSP contains unsafe-inline: %q", csp)
	}
	if response.Header().Get("X-Content-Type-Options") != "nosniff" || response.Header().Get("Referrer-Policy") != "no-referrer" || response.Header().Get("Permissions-Policy") == "" || response.Header().Get("X-Frame-Options") != "DENY" {
		t.Fatalf("security headers = %#v", response.Header())
	}
}

func TestAPIPanicUsesNestedSafeErrorWithRequestID(t *testing.T) {
	authenticator := &authenticatorStub{session: auth.Session{Username: "admin"}}
	handler, err := New(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		panic("internal-secret")
	}), Options{
		Authenticator: authenticator,
		SessionKey:    []byte("0123456789abcdef0123456789abcdef"),
		PublicBaseURL: "https://studio.example.test",
		RequestID:     func() string { return "request-panic" },
	})
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodGet, "https://studio.example.test/api/admin/session", nil)
	request.AddCookie(&http.Cookie{Name: SessionCookieName, Value: "raw"})
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	var payload struct {
		Error struct {
			Code      string            `json:"code"`
			Message   string            `json:"message"`
			RequestID string            `json:"requestId"`
			Fields    map[string]string `json:"fields"`
		} `json:"error"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode response %q: %v", response.Body.String(), err)
	}
	if response.Code != http.StatusInternalServerError || payload.Error.Code != "internal_error" || payload.Error.RequestID != "request-panic" || payload.Error.Fields == nil || strings.Contains(response.Body.String(), "secret") {
		t.Fatalf("panic response = status %d payload %#v", response.Code, payload)
	}
}

func TestPublicHTMLPanicReplacesPartialResponseAtomically(t *testing.T) {
	handler, err := New(http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
		response.WriteHeader(http.StatusOK)
		_, _ = response.Write([]byte("partial-private-content"))
		panic("internal-secret")
	}), Options{
		PublicBaseURL: "https://studio.example.test",
		RequestID:     func() string { return "request-public-panic" },
		ErrorRenderer: func(response http.ResponseWriter, _ *http.Request, status int, _, _ string) {
			response.WriteHeader(status)
			_, _ = response.Write([]byte("designed-safe-error"))
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "https://studio.example.test/boom", nil))
	if response.Code != http.StatusInternalServerError || response.Body.String() != "designed-safe-error" {
		t.Fatalf("panic response = status %d body %q", response.Code, response.Body.String())
	}
}

func TestAccessLogUsesAllowlistedRouteAndHashedClientOnly(t *testing.T) {
	var output bytes.Buffer
	handler, err := New(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		request.Pattern = "GET /sentenze-e-riflessioni/{slug}"
		response.WriteHeader(http.StatusNotFound)
	}), Options{
		SessionKey:    []byte("0123456789abcdef0123456789abcdef"),
		PublicBaseURL: "https://studio.example.test",
		Logger:        slog.New(slog.NewJSONHandler(&output, nil)),
		RequestID:     func() string { return "request-log" },
	})
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodGet, "https://studio.example.test/sentenze-e-riflessioni/private-slug?email=secret@example.test", nil)
	request.RemoteAddr = "192.0.2.44:4321"
	request.Header.Set("X-Forwarded-For", "203.0.113.99")
	request.Header.Set("Cookie", "secret-cookie")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	logLine := output.String()
	for _, required := range []string{`"request_id":"request-log"`, `"route":"GET /sentenze-e-riflessioni/{slug}"`, `"client_hash":"`} {
		if !strings.Contains(logLine, required) {
			t.Errorf("log lacks %q: %s", required, logLine)
		}
	}
	for _, forbidden := range []string{"private-slug", "secret@example.test", "192.0.2.44", "203.0.113.99", "secret-cookie", `"path"`, `"url"`, `"host"`} {
		if strings.Contains(logLine, forbidden) {
			t.Errorf("log contains %q: %s", forbidden, logLine)
		}
	}
}
