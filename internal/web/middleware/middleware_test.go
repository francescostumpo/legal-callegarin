package middleware

import (
	"bufio"
	"context"
	"io"
	"log/slog"
	"net"
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
	for name, writeBeforePanic := range map[string]bool{"before write": false, "after apparent success": true} {
		t.Run(name, func(t *testing.T) {
			var logs strings.Builder
			handler, err := New(http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
				if writeBeforePanic {
					response.WriteHeader(http.StatusOK)
					_, _ = response.Write([]byte("partial-success-secret"))
				}
				panic("secret-panic-value")
			}), Options{
				SessionKey:    []byte("0123456789abcdef0123456789abcdef"),
				PublicBaseURL: "https://studio.example.test",
				Logger:        slog.New(slog.NewTextHandler(&logs, nil)),
				RequestID:     func() string { return "request-123" },
			})
			if err != nil {
				t.Fatal(err)
			}
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "https://studio.example.test/admin/login", nil))
			if response.Code != http.StatusInternalServerError || response.Body.String() != "Internal Server Error\n" {
				t.Fatalf("recovery response = %d %q", response.Code, response.Body.String())
			}
			for header := range map[string]bool{"Content-Security-Policy": true, "Strict-Transport-Security": true, "X-Content-Type-Options": true, "Referrer-Policy": true} {
				if response.Header().Get(header) == "" {
					t.Errorf("missing %s", header)
				}
			}
			if response.Header().Get("X-Request-ID") != "request-123" || response.Header().Get("Cache-Control") != "no-store" || response.Header().Get("X-Robots-Tag") != "noindex, nofollow" {
				t.Fatalf("request/recovery headers = %#v", response.Header())
			}
			if logText := logs.String(); !strings.Contains(logText, "request completed") || !strings.Contains(logText, "status=500") || !strings.Contains(logText, "outcome=recovered_panic") || strings.Contains(logText, "secret") {
				t.Fatalf("panic completion log = %q", logText)
			}
		})
	}
}

func TestAdminResponseBufferFailsClosedAtBoundWithoutPartialOutput(t *testing.T) {
	var logs strings.Builder
	handler, err := New(http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
		_, _ = response.Write([]byte("response-too-large"))
	}), Options{
		SessionKey: []byte("0123456789abcdef0123456789abcdef"), PublicBaseURL: "https://studio.example.test",
		Logger: slog.New(slog.NewTextHandler(&logs, nil)), MaxAdminResponseBytes: 8,
	})
	if err != nil {
		t.Fatal(err)
	}
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "https://studio.example.test/admin/login", nil))
	if response.Code != http.StatusInternalServerError || response.Body.String() != "Internal Server Error\n" {
		t.Fatalf("overflow response = %d %q", response.Code, response.Body.String())
	}
	if !strings.Contains(logs.String(), "status=500") || !strings.Contains(logs.String(), "outcome=response_too_large") {
		t.Fatalf("overflow log = %q", logs.String())
	}
}

func TestPublicResponseIsNotBufferedAndRecorderPreservesSemantics(t *testing.T) {
	underlying := newInterfaceResponseWriter()
	handler, err := New(http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
		response.WriteHeader(http.StatusCreated)
		response.WriteHeader(http.StatusTeapot)
		_, _ = response.Write([]byte("visible"))
		if underlying.body.String() != "visible" {
			t.Fatal("public response was buffered")
		}
		response.(http.Flusher).Flush()
		if _, _, err := response.(http.Hijacker).Hijack(); err != nil {
			t.Fatalf("Hijack: %v", err)
		}
		if err := response.(http.Pusher).Push("/asset", nil); err != nil {
			t.Fatalf("Push: %v", err)
		}
		if _, err := response.(io.ReaderFrom).ReadFrom(&onlyReader{data: []byte("-read-from")}); err != nil {
			t.Fatalf("ReadFrom: %v", err)
		}
	}), Options{SessionKey: []byte("0123456789abcdef0123456789abcdef"), PublicBaseURL: "https://studio.example.test"})
	if err != nil {
		t.Fatal(err)
	}
	handler.ServeHTTP(underlying, httptest.NewRequest(http.MethodGet, "https://studio.example.test/health/live", nil))
	if underlying.status != http.StatusCreated || underlying.body.String() != "visible-read-from" || !underlying.flushed || !underlying.hijacked || !underlying.pushed || !underlying.readFrom {
		t.Fatalf("recorder semantics = status=%d body=%q flush=%t hijack=%t push=%t readFrom=%t", underlying.status, underlying.body.String(), underlying.flushed, underlying.hijacked, underlying.pushed, underlying.readFrom)
	}
}

func TestResponseRecordersHonorImplicitStatusRepeatedHeaderAndAdminNoStreaming(t *testing.T) {
	underlying := newInterfaceResponseWriter()
	recorder := &captureResponseWriter{ResponseWriter: underlying}
	if _, err := recorder.Write([]byte("implicit")); err != nil {
		t.Fatal(err)
	}
	recorder.WriteHeader(http.StatusCreated)
	if recorder.responseStatus() != http.StatusOK || underlying.status != http.StatusOK {
		t.Fatalf("implicit status = recorder %d underlying %d", recorder.responseStatus(), underlying.status)
	}

	underlying = newInterfaceResponseWriter()
	recorder = &captureResponseWriter{ResponseWriter: underlying}
	recorder.WriteHeader(http.StatusCreated)
	recorder.WriteHeader(http.StatusTeapot)
	if recorder.responseStatus() != http.StatusCreated || underlying.status != http.StatusCreated {
		t.Fatalf("repeated WriteHeader changed status: recorder %d underlying %d", recorder.responseStatus(), underlying.status)
	}

	buffered := newBoundedAdminResponse(make(http.Header), 128)
	if _, ok := any(buffered).(http.Flusher); ok {
		t.Fatal("atomic admin response unexpectedly exposes streaming")
	}
	buffered.WriteHeader(http.StatusAccepted)
	buffered.WriteHeader(http.StatusTeapot)
	if buffered.responseStatus() != http.StatusAccepted {
		t.Fatalf("admin repeated WriteHeader status = %d", buffered.responseStatus())
	}
}

func TestAllAdminMiddlewareResponsesArePrivateAndNoindex(t *testing.T) {
	authenticator := &authenticatorStub{session: auth.Session{Username: "admin"}}
	handler, err := New(http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
		response.WriteHeader(http.StatusNoContent)
	}), Options{Authenticator: authenticator, SessionKey: []byte("0123456789abcdef0123456789abcdef"), PublicBaseURL: "https://studio.example.test"})
	if err != nil {
		t.Fatal(err)
	}
	for name, request := range map[string]*http.Request{
		"auth error": httptest.NewRequest(http.MethodGet, "https://studio.example.test/api/admin/session", nil),
		"origin error": func() *http.Request {
			r := httptest.NewRequest(http.MethodDelete, "https://studio.example.test/api/admin/session", nil)
			r.AddCookie(&http.Cookie{Name: SessionCookieName, Value: "raw"})
			return r
		}(),
		"asset success": func() *http.Request {
			r := httptest.NewRequest(http.MethodGet, "https://studio.example.test/admin/assets/app.js", nil)
			r.AddCookie(&http.Cookie{Name: SessionCookieName, Value: "raw"})
			return r
		}(),
	} {
		t.Run(name, func(t *testing.T) {
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, request)
			if response.Header().Get("Cache-Control") != "no-store" || response.Header().Get("X-Robots-Tag") != "noindex, nofollow" {
				t.Fatalf("admin privacy headers = %#v", response.Header())
			}
		})
	}
}

type interfaceResponseWriter struct {
	header                              http.Header
	body                                strings.Builder
	status                              int
	flushed, hijacked, pushed, readFrom bool
}

func newInterfaceResponseWriter() *interfaceResponseWriter {
	return &interfaceResponseWriter{header: make(http.Header)}
}
func (writer *interfaceResponseWriter) Header() http.Header { return writer.header }
func (writer *interfaceResponseWriter) WriteHeader(status int) {
	if writer.status == 0 {
		writer.status = status
	}
}
func (writer *interfaceResponseWriter) Write(value []byte) (int, error) {
	if writer.status == 0 {
		writer.status = http.StatusOK
	}
	return writer.body.Write(value)
}
func (writer *interfaceResponseWriter) Flush() { writer.flushed = true }
func (writer *interfaceResponseWriter) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	writer.hijacked = true
	left, right := net.Pipe()
	_ = right.Close()
	return left, bufio.NewReadWriter(bufio.NewReader(left), bufio.NewWriter(left)), nil
}
func (writer *interfaceResponseWriter) Push(string, *http.PushOptions) error {
	writer.pushed = true
	return nil
}
func (writer *interfaceResponseWriter) ReadFrom(reader io.Reader) (int64, error) {
	writer.readFrom = true
	value, err := io.ReadAll(reader)
	if err != nil {
		return 0, err
	}
	written, err := writer.Write(value)
	return int64(written), err
}

type onlyReader struct{ data []byte }

func (reader *onlyReader) Read(value []byte) (int, error) {
	if len(reader.data) == 0 {
		return 0, io.EOF
	}
	count := copy(value, reader.data)
	reader.data = reader.data[count:]
	return count, nil
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
