package middleware

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/francescostumpo/legal-callegarin/internal/auth"
	"github.com/francescostumpo/legal-callegarin/internal/web/clientinfo"
)

const (
	SessionCookieName = "__Host-callegarin_admin"
	SessionMaxAge     = int((8 * time.Hour) / time.Second)
	defaultBodyLimit  = int64(1 << 20)
)

type Authenticator interface {
	Validate(context.Context, string) (auth.Session, error)
}

type Options struct {
	Authenticator         Authenticator
	SessionKey            []byte
	PublicBaseURL         string
	Logger                *slog.Logger
	MaxBodyBytes          int64
	MaxAdminResponseBytes int64
	RequestID             func() string
	Now                   func() time.Time
	Environment           string
	TrustedProxyHops      int
	HandlerTimeout        time.Duration
	ErrorRenderer         func(http.ResponseWriter, *http.Request, int, string, string)
}

type Principal struct {
	Username string
	Token    string
	Session  auth.Session
}

type principalContextKey struct{}
type requestIDContextKey struct{}
type cspNonceContextKey struct{}

func New(next http.Handler, options Options) (http.Handler, error) {
	if next == nil {
		return nil, errors.New("middleware requires a handler")
	}
	if len(options.SessionKey) > 0 && len(options.SessionKey) < 32 {
		return nil, errors.New("middleware session key must contain at least 32 bytes")
	}
	if len(options.SessionKey) == 0 {
		options.SessionKey = make([]byte, 32)
		if _, err := rand.Read(options.SessionKey); err != nil {
			return nil, errors.New("generate middleware signing key")
		}
	}
	base, err := url.Parse(options.PublicBaseURL)
	if err != nil || !secureOrigin(base) || base.User != nil || base.Path != "" || base.RawQuery != "" || base.Fragment != "" {
		return nil, errors.New("middleware requires an HTTPS public base origin without path, query, or fragment")
	}
	if options.Logger == nil {
		options.Logger = slog.New(slog.NewTextHandler(io.Discard, nil))
	}
	if options.MaxBodyBytes <= 0 {
		options.MaxBodyBytes = defaultBodyLimit
	}
	if options.MaxAdminResponseBytes <= 0 {
		options.MaxAdminResponseBytes = defaultAdminResponseLimit
	}
	if options.RequestID == nil {
		options.RequestID = secureRequestID
	}
	if options.Now == nil {
		options.Now = time.Now
	}
	clientHashKey := make([]byte, 32)
	if _, err := rand.Read(clientHashKey); err != nil {
		return nil, errors.New("generate client correlation key")
	}
	expectedOrigin := base.Scheme + "://" + base.Host

	handler := next
	handler = csrf(handler, options.SessionKey)
	handler = origin(handler, expectedOrigin)
	handler = authenticate(handler, options.Authenticator, options.Now)
	handler = bodyLimit(handler, options.MaxBodyBytes)
	if options.HandlerTimeout > 0 {
		handler = timeout(handler, options.HandlerTimeout, options.ErrorRenderer)
	}
	if options.Environment == "production" {
		handler = canonicalRedirect(handler, base, options.TrustedProxyHops)
	}
	handler = accessLog(handler, options.Logger, options.Now, options.TrustedProxyHops, clientHashKey)
	handler = securityHeaders(handler, options.Environment == "production")
	handler = recoverPanics(handler, options.Logger, options.MaxAdminResponseBytes, options.ErrorRenderer)
	handler = requestID(handler, options.RequestID)
	return handler, nil
}

func secureOrigin(origin *url.URL) bool {
	if origin.Host == "" {
		return false
	}
	if origin.Scheme == "https" {
		return true
	}
	host := origin.Hostname()
	return origin.Scheme == "http" && (host == "localhost" || host == "127.0.0.1" || host == "::1")
}

func PrincipalFromContext(ctx context.Context) (Principal, bool) {
	principal, ok := ctx.Value(principalContextKey{}).(Principal)
	return principal, ok
}

func RequestIDFromContext(ctx context.Context) string {
	requestID, _ := ctx.Value(requestIDContextKey{}).(string)
	return requestID
}

func CSPNonceFromContext(ctx context.Context) string {
	nonce, _ := ctx.Value(cspNonceContextKey{}).(string)
	return nonce
}

func CSRFToken(raw string, key []byte) string {
	mac := hmac.New(sha256.New, key)
	_, _ = mac.Write([]byte("callegarin/admin-csrf/v1\x00"))
	_, _ = mac.Write([]byte(raw))
	return base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}

func SetSessionCookie(response http.ResponseWriter, raw string, now time.Time) {
	http.SetCookie(response, &http.Cookie{
		Name:     SessionCookieName,
		Value:    raw,
		Path:     "/",
		Expires:  now.UTC().Add(8 * time.Hour),
		MaxAge:   SessionMaxAge,
		Secure:   true,
		HttpOnly: true,
		SameSite: http.SameSiteStrictMode,
	})
}

func ClearSessionCookie(response http.ResponseWriter, now time.Time) {
	http.SetCookie(response, &http.Cookie{
		Name:     SessionCookieName,
		Value:    "",
		Path:     "/",
		Expires:  now.UTC().Add(-time.Hour),
		MaxAge:   -1,
		Secure:   true,
		HttpOnly: true,
		SameSite: http.SameSiteStrictMode,
	})
}

func requestID(next http.Handler, generate func() string) http.Handler {
	return http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		id := request.Header.Get("X-Request-ID")
		if !validRequestID(id) {
			id = generate()
		}
		if !validRequestID(id) {
			id = secureRequestID()
		}
		nonce := secureNonce()
		response.Header().Set("X-Request-ID", id)
		ctx := context.WithValue(request.Context(), requestIDContextKey{}, id)
		ctx = context.WithValue(ctx, cspNonceContextKey{}, nonce)
		next.ServeHTTP(response, request.WithContext(ctx))
	})
}

func validRequestID(value string) bool {
	if value == "" || len(value) > 64 {
		return false
	}
	for _, character := range value {
		if !(character >= 'a' && character <= 'z' || character >= 'A' && character <= 'Z' || character >= '0' && character <= '9' || character == '-' || character == '_' || character == '.') {
			return false
		}
	}
	return true
}

func securityHeaders(next http.Handler, production bool) http.Handler {
	return http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		applySecurityHeaders(response.Header(), request.URL.Path, production, CSPNonceFromContext(request.Context()))
		next.ServeHTTP(response, request)
	})
}

func bodyLimit(next http.Handler, limit int64) http.Handler {
	return http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		if request.Body != nil {
			request.Body = http.MaxBytesReader(response, request.Body, limit)
		}
		next.ServeHTTP(response, request)
	})
}

func authenticate(next http.Handler, authenticator Authenticator, now func() time.Time) http.Handler {
	return http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		if !protectedPath(request.URL.Path) {
			next.ServeHTTP(response, request)
			return
		}
		cookie, cookieErr := request.Cookie(SessionCookieName)
		if cookieErr == nil && authenticator != nil {
			session, err := authenticator.Validate(request.Context(), cookie.Value)
			if err == nil {
				principal := Principal{Username: session.Username, Token: cookie.Value, Session: session}
				next.ServeHTTP(response, request.WithContext(context.WithValue(request.Context(), principalContextKey{}, principal)))
				return
			}
			if !errors.Is(err, auth.ErrUnauthenticated) {
				WriteAPIError(response, request, http.StatusServiceUnavailable, "authentication_unavailable", "Autenticazione temporaneamente non disponibile.", nil)
				return
			}
			ClearSessionCookie(response, now())
		}
		response.Header().Set("Cache-Control", "no-store")
		if strings.HasPrefix(request.URL.Path, "/api/admin/") || request.URL.Path == "/api/admin" {
			WriteAPIError(response, request, http.StatusUnauthorized, "authentication_required", "Autenticazione richiesta.", nil)
			return
		}
		http.Redirect(response, request, "/admin/login", http.StatusSeeOther)
	})
}

func origin(next http.Handler, expected string) http.Handler {
	return http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		if unsafeAdminAPI(request) && request.Header.Get("Origin") != expected {
			WriteAPIError(response, request, http.StatusForbidden, "request_rejected", "Richiesta rifiutata.", nil)
			return
		}
		next.ServeHTTP(response, request)
	})
}

func csrf(next http.Handler, key []byte) http.Handler {
	return http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		if unsafeAdminAPI(request) {
			principal, ok := PrincipalFromContext(request.Context())
			expected := CSRFToken(principal.Token, key)
			provided := request.Header.Get("X-CSRF-Token")
			if !ok || subtle.ConstantTimeCompare([]byte(provided), []byte(expected)) != 1 {
				WriteAPIError(response, request, http.StatusForbidden, "request_rejected", "Richiesta rifiutata.", nil)
				return
			}
		}
		next.ServeHTTP(response, request)
	})
}

func protectedPath(path string) bool {
	if path == "/admin/login" {
		return false
	}
	return path == "/admin" || strings.HasPrefix(path, "/admin/") || path == "/api/admin" || strings.HasPrefix(path, "/api/admin/")
}

func unsafeAdminAPI(request *http.Request) bool {
	if request.Method == http.MethodGet || request.Method == http.MethodHead || request.Method == http.MethodOptions {
		return false
	}
	return request.URL.Path == "/api/admin" || strings.HasPrefix(request.URL.Path, "/api/admin/")
}

type APIError struct {
	Code      string            `json:"code"`
	Message   string            `json:"message"`
	RequestID string            `json:"requestId"`
	Fields    map[string]string `json:"fields"`
}

func WriteAPIError(response http.ResponseWriter, request *http.Request, status int, code, message string, fields map[string]string) {
	requestID := ""
	if request != nil {
		requestID = RequestIDFromContext(request.Context())
	}
	if requestID == "" {
		requestID = response.Header().Get("X-Request-ID")
	}
	writeAPIErrorResponse(response, status, code, message, requestID, fields)
}

func WriteAPIErrorResponse(response http.ResponseWriter, status int, code, message string, fields map[string]string) {
	writeAPIErrorResponse(response, status, code, message, response.Header().Get("X-Request-ID"), fields)
}

func writeAPIErrorResponse(response http.ResponseWriter, status int, code, message, requestID string, fields map[string]string) {
	if fields == nil {
		fields = map[string]string{}
	}
	response.Header().Set("Cache-Control", "no-store")
	response.Header().Set("Content-Type", "application/json; charset=utf-8")
	response.WriteHeader(status)
	_ = json.NewEncoder(response).Encode(struct {
		Error APIError `json:"error"`
	}{Error: APIError{Code: code, Message: message, RequestID: requestID, Fields: fields}})
}

func secureRequestID() string {
	buffer := make([]byte, 16)
	if _, err := rand.Read(buffer); err != nil {
		return "request-id-unavailable"
	}
	return base64.RawURLEncoding.EncodeToString(buffer)
}

func secureNonce() string {
	buffer := make([]byte, 18)
	if _, err := rand.Read(buffer); err != nil {
		return secureRequestID()
	}
	return base64.RawURLEncoding.EncodeToString(buffer)
}

func canonicalRedirect(next http.Handler, base *url.URL, trustedProxyHops int) http.Handler {
	return http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		if request.URL.Path == "/health/live" || request.URL.Path == "/health/ready" {
			next.ServeHTTP(response, request)
			return
		}
		identity, _ := clientinfo.Resolve(request, trustedProxyHops)
		scheme := identity.Scheme
		if scheme == "" {
			scheme = "http"
		}
		if !strings.EqualFold(request.Host, base.Host) || scheme != base.Scheme {
			target := base.Scheme + "://" + base.Host + request.URL.EscapedPath()
			if request.URL.RawQuery != "" {
				target += "?" + request.URL.RawQuery
			}
			http.Redirect(response, request, target, http.StatusPermanentRedirect)
			return
		}
		next.ServeHTTP(response, request)
	})
}
