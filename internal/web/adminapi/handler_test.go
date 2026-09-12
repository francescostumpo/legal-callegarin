package adminapi

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/francescostumpo/legal-callegarin/internal/auth"
	webmiddleware "github.com/francescostumpo/legal-callegarin/internal/web/middleware"
)

type verifierStub struct {
	err      error
	username string
	password string
	calls    int
}

func (stub *verifierStub) Verify(username, password string) error {
	stub.calls++
	stub.username, stub.password = username, password
	return stub.err
}

type sessionsStub struct {
	raw       string
	rotateErr error
	logoutErr error
	prior     string
	loggedOut string
}

func (stub *sessionsStub) Rotate(_ context.Context, prior string) (string, auth.Session, error) {
	stub.prior = prior
	return stub.raw, auth.Session{Username: "admin"}, stub.rotateErr
}
func (stub *sessionsStub) Logout(_ context.Context, raw string) error {
	stub.loggedOut = raw
	return stub.logoutErr
}

func TestLoginGETIsServerRenderedNoStoreAndSetsNoCookie(t *testing.T) {
	handler, _, _, now := newTestHandler(t)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "https://studio.example.test/admin/login", nil))
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `<form method="post" action="/admin/login">`) || !strings.Contains(response.Body.String(), `name="started"`) {
		t.Fatalf("login response = %d %q", response.Code, response.Body.String())
	}
	if response.Header().Get("Cache-Control") != "no-store" || response.Header().Get("X-Robots-Tag") != "noindex, nofollow" || response.Header().Get("Set-Cookie") != "" {
		t.Fatalf("login headers = %#v at %v", response.Header(), now)
	}
}

func TestLoginPOSTUniformFailureAndSecureSuccessCookie(t *testing.T) {
	handler, verifier, sessions, _ := newTestHandler(t)
	token := loginToken(t, handler)

	verifier.err = auth.ErrInvalidCredentials
	bad := submitLogin(handler, token, "admin", "wrong")
	badBody := bad.Body.String()
	if bad.Code != http.StatusUnauthorized || !strings.Contains(badBody, "Credenziali non valide") {
		t.Fatalf("bad login = %d %q", bad.Code, badBody)
	}
	verifier.err = auth.ErrInvalidCredentials
	unknown := submitLogin(handler, token, "unknown", "wrong")
	if unknown.Code != bad.Code || unknown.Body.String() != badBody {
		t.Fatalf("login failure differs: bad=%d/%q unknown=%d/%q", bad.Code, badBody, unknown.Code, unknown.Body.String())
	}

	verifier.err = nil
	sessions.raw = "new-raw-session"
	success := submitLogin(handler, token, "admin", "correct")
	if success.Code != http.StatusSeeOther || success.Header().Get("Location") != "/admin" {
		t.Fatalf("success = %d location %q", success.Code, success.Header().Get("Location"))
	}
	cookies := success.Result().Cookies()
	if len(cookies) != 1 {
		t.Fatalf("cookies = %#v", cookies)
	}
	cookie := cookies[0]
	if cookie.Name != webmiddleware.SessionCookieName || cookie.Value != sessions.raw || cookie.Path != "/" || cookie.Domain != "" || !cookie.Secure || !cookie.HttpOnly || cookie.SameSite != http.SameSiteStrictMode || cookie.MaxAge != 8*60*60 {
		t.Fatalf("session cookie = %#v", cookie)
	}
}

func TestLoginPOSTRequiresFormContentTypeTokenAndExactOrigin(t *testing.T) {
	handler, verifier, _, _ := newTestHandler(t)
	token := loginToken(t, handler)
	for name, mutate := range map[string]func(*http.Request){
		"wrong content type": func(request *http.Request) { request.Header.Set("Content-Type", "application/json") },
		"missing token": func(request *http.Request) {
			body := "username=admin&password=password"
			request.Body = io.NopCloser(strings.NewReader(body))
			request.ContentLength = int64(len(body))
		},
		"bad origin": func(request *http.Request) { request.Header.Set("Origin", "https://evil.example") },
	} {
		t.Run(name, func(t *testing.T) {
			form := url.Values{"username": {"admin"}, "password": {"password"}, "started": {token}}
			request := httptest.NewRequest(http.MethodPost, "https://studio.example.test/admin/login", strings.NewReader(form.Encode()))
			request.RemoteAddr = "192.0.2.1:1234"
			request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
			request.Header.Set("Origin", "https://studio.example.test")
			mutate(request)
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, request)
			if response.Code != http.StatusBadRequest && response.Code != http.StatusForbidden && response.Code != http.StatusUnsupportedMediaType {
				t.Fatalf("status = %d", response.Code)
			}
		})
	}
	if verifier.calls != 0 {
		t.Fatalf("credential verifier called %d times for rejected requests", verifier.calls)
	}
}

func TestLoginPOSTUsesSharedPasswordLengthBoundary(t *testing.T) {
	handler, verifier, _, _ := newTestHandler(t)
	token := loginToken(t, handler)
	maximum := strings.Repeat("x", auth.MaxPasswordBytes)
	response := submitLoginFrom(handler, token, "admin", maximum, "192.0.2.1:1000")
	if response.Code != http.StatusSeeOther || verifier.calls != 1 {
		t.Fatalf("maximum password = status %d verifier calls %d", response.Code, verifier.calls)
	}
	response = submitLoginFrom(handler, token, "admin", maximum+"x", "192.0.3.1:1000")
	if response.Code != http.StatusBadRequest || verifier.calls != 1 || !strings.Contains(response.Body.String(), "Richiesta non valida") {
		t.Fatalf("oversized password = status %d calls %d body %q", response.Code, verifier.calls, response.Body.String())
	}
}

func TestSessionAPIEmitsCSRFAndLogoutRevokesToken(t *testing.T) {
	inner, _, sessions, now := newTestHandler(t)
	authenticator := &authenticatorStub{session: auth.Session{Username: "admin", ExpiresAt: now.Add(8 * time.Hour)}}
	key := []byte("0123456789abcdef0123456789abcdef")
	handler, err := webmiddleware.New(inner, webmiddleware.Options{Authenticator: authenticator, SessionKey: key, PublicBaseURL: "https://studio.example.test", Now: func() time.Time { return now }})
	if err != nil {
		t.Fatal(err)
	}

	getRequest := httptest.NewRequest(http.MethodGet, "https://studio.example.test/api/admin/session", nil)
	getRequest.AddCookie(&http.Cookie{Name: webmiddleware.SessionCookieName, Value: "raw-session"})
	getResponse := httptest.NewRecorder()
	handler.ServeHTTP(getResponse, getRequest)
	if getResponse.Code != http.StatusOK || strings.Contains(getResponse.Body.String(), "raw-session") || !strings.Contains(getResponse.Body.String(), webmiddleware.CSRFToken("raw-session", key)) || !strings.Contains(getResponse.Body.String(), `"username":"admin"`) {
		t.Fatalf("session GET = %d %q", getResponse.Code, getResponse.Body.String())
	}
	assertPrivateAdminResponse(t, getResponse)

	deleteRequest := httptest.NewRequest(http.MethodDelete, "https://studio.example.test/api/admin/session", nil)
	deleteRequest.AddCookie(&http.Cookie{Name: webmiddleware.SessionCookieName, Value: "raw-session"})
	deleteRequest.Header.Set("Origin", "https://studio.example.test")
	deleteRequest.Header.Set("X-CSRF-Token", webmiddleware.CSRFToken("raw-session", key))
	deleteResponse := httptest.NewRecorder()
	handler.ServeHTTP(deleteResponse, deleteRequest)
	if deleteResponse.Code != http.StatusNoContent || sessions.loggedOut != "raw-session" {
		t.Fatalf("logout = %d token %q", deleteResponse.Code, sessions.loggedOut)
	}
	assertPrivateAdminResponse(t, deleteResponse)
	if cookies := deleteResponse.Result().Cookies(); len(cookies) != 1 || cookies[0].MaxAge >= 0 {
		t.Fatalf("logout cookies = %#v", cookies)
	}

	sessions.logoutErr = errors.New("storage unavailable")
	failureRequest := httptest.NewRequest(http.MethodDelete, "https://studio.example.test/api/admin/session", nil)
	failureRequest.AddCookie(&http.Cookie{Name: webmiddleware.SessionCookieName, Value: "raw-session"})
	failureRequest.Header.Set("Origin", "https://studio.example.test")
	failureRequest.Header.Set("X-CSRF-Token", webmiddleware.CSRFToken("raw-session", key))
	failureResponse := httptest.NewRecorder()
	handler.ServeHTTP(failureResponse, failureRequest)
	if failureResponse.Code != http.StatusServiceUnavailable {
		t.Fatalf("logout failure status = %d", failureResponse.Code)
	}
	assertPrivateAdminResponse(t, failureResponse)

	assetRequest := httptest.NewRequest(http.MethodGet, "https://studio.example.test/admin/assets/index-test.js", nil)
	assetRequest.AddCookie(&http.Cookie{Name: webmiddleware.SessionCookieName, Value: "raw-session"})
	assetResponse := httptest.NewRecorder()
	handler.ServeHTTP(assetResponse, assetRequest)
	if assetResponse.Code != http.StatusOK {
		t.Fatalf("asset status = %d body=%q", assetResponse.Code, assetResponse.Body.String())
	}
	assertPrivateAdminResponse(t, assetResponse)
}

func TestDirectAdminAPIErrorIsPrivateAndNoindex(t *testing.T) {
	handler, _, _, _ := newTestHandler(t)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "https://studio.example.test/api/admin/session", nil))
	if response.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d", response.Code)
	}
	assertPrivateAdminResponse(t, response)
}

func TestLoginLimiterUsesSeparateBoundedAddressAndUsernameBuckets(t *testing.T) {
	now := time.Date(2026, 9, 11, 12, 0, 0, 0, time.UTC)
	limiter := newLoginLimiter(1, time.Hour, time.Hour, 2, "alice")
	if !limiter.allowAddress("address-a", now) || !limiter.allowUsername("alice", now) {
		t.Fatal("initial independent buckets rejected")
	}
	if limiter.allowAddress("address-a", now) || limiter.allowUsername("alice", now) {
		t.Fatal("exhausted buckets allowed")
	}
	if !limiter.allowAddress("address-b", now) || !limiter.allowUsername("bob", now) {
		t.Fatal("separate keys did not have independent capacity")
	}
	_ = limiter.allowAddress("address-c", now)
	_ = limiter.allowUsername("carol", now)
	if limiter.addressSize() > 2 || limiter.usernameSize() > 2 {
		t.Fatalf("bucket maps are unbounded: address=%d username=%d", limiter.addressSize(), limiter.usernameSize())
	}
	if normalizeUsername("  AdMiN  ") != normalizeUsername("admin") {
		t.Fatal("username rate-limit keys are not normalized")
	}
}

func TestLoginLimiterPinsConfiguredUsernameAgainstChurnAndRefills(t *testing.T) {
	now := time.Date(2026, 9, 11, 12, 0, 0, 0, time.UTC)
	verifier := &verifierStub{err: auth.ErrInvalidCredentials}
	sessions := &sessionsStub{}
	assets := fstest.MapFS{
		"admin/dist/index.html":           {Data: []byte("<!doctype html><title>Admin</title>")},
		"admin/dist/assets/index-test.js": {Data: []byte("console.log('stub')")},
	}
	key := []byte("0123456789abcdef0123456789abcdef")
	handler, err := New(Options{
		Credentials: verifier, ConfiguredUsername: "admin", Sessions: sessions, Assets: assets,
		SessionKey: key, PublicBaseURL: "https://studio.example.test",
		Now: func() time.Time { return now }, LoginCapacity: 1, MaxBuckets: 3,
	})
	if err != nil {
		t.Fatal(err)
	}
	token := loginToken(t, handler)
	if response := submitLoginFrom(handler, token, "admin", "wrong", "192.0.2.1:1000"); response.Code != http.StatusUnauthorized {
		t.Fatalf("initial target status = %d", response.Code)
	}

	// Once the address bucket is exhausted, fake usernames must not consume
	// username-map admission or evict the pinned configured username.
	for index := range 10 {
		now = now.Add(time.Second)
		response := submitLoginFrom(handler, token, "blocked-fake-"+strconv.Itoa(index), "wrong", "192.0.2.1:1000")
		if response.Code != http.StatusTooManyRequests {
			t.Fatalf("blocked-IP churn %d status = %d", index, response.Code)
		}
	}
	// Churn from fresh allowed address prefixes may fill the bounded username
	// map, but cannot evict/reset the configured username bucket.
	for index := range 10 {
		now = now.Add(time.Second)
		response := submitLoginFrom(handler, token, "allowed-fake-"+strconv.Itoa(index), "wrong", fmt.Sprintf("198.51.%d.1:1000", index))
		if response.Code != http.StatusUnauthorized && response.Code != http.StatusTooManyRequests {
			t.Fatalf("allowed-IP churn %d status = %d", index, response.Code)
		}
	}
	if response := submitLoginFrom(handler, token, "admin", "wrong", "203.0.113.1:1000"); response.Code != http.StatusTooManyRequests {
		t.Fatalf("target after churn status = %d, want 429", response.Code)
	}
	if verifier.calls > 3 {
		t.Fatalf("blocked/admission-denied churn reached verifier %d times", verifier.calls)
	}

	now = now.Add(3 * time.Minute)
	if response := submitLoginFrom(handler, token, "admin", "wrong", "203.0.114.1:1000"); response.Code != http.StatusUnauthorized {
		t.Fatalf("target after refill status = %d, want 401", response.Code)
	}
}

func TestUsernameLimiterFailsClosedAtCapacityWithoutEvictingPinnedBucket(t *testing.T) {
	now := time.Date(2026, 9, 11, 12, 0, 0, 0, time.UTC)
	limiter := newLoginLimiter(1, time.Hour, time.Hour, 2, "configured")
	if !limiter.allowUsername("configured", now) || limiter.allowUsername("configured", now) {
		t.Fatal("configured username bucket did not exhaust")
	}
	if !limiter.allowUsername("first-fake", now) {
		t.Fatal("first fake should use the one free username slot")
	}
	for index := range 20 {
		if limiter.allowUsername("churn-"+strconv.Itoa(index), now) {
			t.Fatalf("username admission %d succeeded after capacity", index)
		}
	}
	if limiter.usernameSize() != 2 || limiter.allowUsername("configured", now) {
		t.Fatalf("pinned bucket was reset/evicted: size=%d", limiter.usernameSize())
	}
	if !limiter.allowUsername("configured", now.Add(time.Hour)) {
		t.Fatal("pinned bucket did not recover after refill")
	}
}

type authenticatorStub struct{ session auth.Session }

func (stub *authenticatorStub) Validate(context.Context, string) (auth.Session, error) {
	return stub.session, nil
}

func newTestHandler(t *testing.T) (http.Handler, *verifierStub, *sessionsStub, time.Time) {
	t.Helper()
	now := time.Date(2026, 9, 11, 12, 0, 0, 0, time.UTC)
	verifier := &verifierStub{}
	sessions := &sessionsStub{}
	assets := fstest.MapFS{
		"admin/dist/index.html":           {Data: []byte("<!doctype html><title>Admin</title><div id=root></div>")},
		"admin/dist/assets/index-test.js": {Data: []byte("console.log('stub')")},
	}
	var assetFS fs.FS = assets
	handler, err := New(Options{
		Credentials:        verifier,
		ConfiguredUsername: "admin",
		Sessions:           sessions,
		Assets:             assetFS,
		SessionKey:         []byte("0123456789abcdef0123456789abcdef"),
		PublicBaseURL:      "https://studio.example.test",
		Now:                func() time.Time { return now },
		LoginCapacity:      100,
	})
	if err != nil {
		t.Fatal(err)
	}
	return handler, verifier, sessions, now
}

func loginToken(t *testing.T, handler http.Handler) string {
	t.Helper()
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "https://studio.example.test/admin/login", nil))
	match := regexp.MustCompile(`name="started" value="([^"]+)"`).FindStringSubmatch(response.Body.String())
	if len(match) != 2 {
		t.Fatalf("login token missing: %q", response.Body.String())
	}
	return match[1]
}

func submitLogin(handler http.Handler, token, username, password string) *httptest.ResponseRecorder {
	return submitLoginFrom(handler, token, username, password, "192.0.2.1:1234")
}

func submitLoginFrom(handler http.Handler, token, username, password, remoteAddr string) *httptest.ResponseRecorder {
	form := url.Values{"username": {username}, "password": {password}, "started": {token}}
	request := httptest.NewRequest(http.MethodPost, "https://studio.example.test/admin/login", strings.NewReader(form.Encode()))
	request.RemoteAddr = remoteAddr
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	request.Header.Set("Origin", "https://studio.example.test")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	return response
}

func assertPrivateAdminResponse(t *testing.T, response *httptest.ResponseRecorder) {
	t.Helper()
	if response.Header().Get("Cache-Control") != "no-store" || response.Header().Get("X-Robots-Tag") != "noindex, nofollow" {
		t.Fatalf("admin privacy headers = %#v", response.Header())
	}
}
