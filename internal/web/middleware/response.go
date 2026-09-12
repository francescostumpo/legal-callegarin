package middleware

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/felixge/httpsnoop"
	"github.com/francescostumpo/legal-callegarin/internal/web/clientinfo"
)

const defaultAdminResponseLimit = int64(2 << 20)

var errAdminResponseTooLarge = errors.New("admin response exceeds buffer limit")

type responseMetrics interface {
	responseStatus() int
	responseOverflowed() bool
	responseOutcome() string
}

// boundedAdminResponse deliberately does not implement streaming, hijacking,
// pushing, or ReaderFrom. Admin HTML/API responses are buffered so a panic or
// overflow can be replaced atomically before a success reaches the client.
type boundedAdminResponse struct {
	header   http.Header
	body     bytes.Buffer
	status   int
	maxBytes int64
	overflow bool
	outcome  string
}

func newBoundedAdminResponse(initial http.Header, maxBytes int64) *boundedAdminResponse {
	return &boundedAdminResponse{header: initial.Clone(), maxBytes: maxBytes}
}

func (response *boundedAdminResponse) Header() http.Header { return response.header }

func (response *boundedAdminResponse) WriteHeader(status int) {
	if response.status == 0 {
		response.status = status
	}
}

func (response *boundedAdminResponse) Write(value []byte) (int, error) {
	if response.status == 0 {
		response.status = http.StatusOK
	}
	if response.overflow || int64(response.body.Len()+len(value)) > response.maxBytes {
		response.overflow = true
		return 0, errAdminResponseTooLarge
	}
	return response.body.Write(value)
}

func (response *boundedAdminResponse) responseStatus() int {
	if response.status == 0 {
		return http.StatusOK
	}
	return response.status
}

func (response *boundedAdminResponse) responseOverflowed() bool { return response.overflow }

func (response *boundedAdminResponse) responseOutcome() string { return response.outcome }

func (response *boundedAdminResponse) setResponseOutcome(outcome string) {
	response.outcome = outcome
}

func (response *boundedAdminResponse) commit(destination http.ResponseWriter) {
	markResponseOutcome(destination, response.outcome)
	copyHeaders(destination.Header(), response.header)
	destination.WriteHeader(response.responseStatus())
	if response.body.Len() > 0 {
		_, _ = destination.Write(response.body.Bytes())
	}
}

func recoverPanics(next http.Handler, logger *slog.Logger, adminResponseLimit int64, renderError func(http.ResponseWriter, *http.Request, int, string, string)) http.Handler {
	return http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		if !atomicResponsePath(request.URL.Path) {
			defer func() {
				if recover() == nil {
					return
				}
				logger.Error("request panic recovered", "request_id", RequestIDFromContext(request.Context()))
				writeErrorResponse(response, request, http.StatusInternalServerError, "internal_error", "Si è verificato un errore. Riprova più tardi.", renderError)
			}()
			next.ServeHTTP(response, request)
			return
		}

		safeHeaders := response.Header().Clone()
		buffered := newBoundedAdminResponse(safeHeaders, adminResponseLimit)
		panicked := false
		func() {
			defer func() {
				if recover() != nil {
					panicked = true
				}
			}()
			next.ServeHTTP(buffered, request)
		}()
		switch {
		case panicked:
			logger.Error("request panic recovered", "request_id", RequestIDFromContext(request.Context()))
			writeAtomicFailure(response, request, safeFailureHeaders(buffered.header), http.StatusInternalServerError, "internal_error", "Si è verificato un errore. Riprova più tardi.", renderError)
		case buffered.responseOverflowed():
			logger.Error("response exceeded buffer", "request_id", RequestIDFromContext(request.Context()))
			writeAtomicFailure(response, request, safeFailureHeaders(buffered.header), http.StatusInternalServerError, "response_too_large", "Risposta temporaneamente non disponibile.", renderError)
		default:
			buffered.commit(response)
		}
	})
}

func atomicResponsePath(path string) bool {
	if path == "/health/live" || path == "/health/ready" || path == "/sitemap.xml" || path == "/robots.txt" || strings.HasPrefix(path, "/assets/") {
		return false
	}
	return true
}

func accessLog(next http.Handler, logger *slog.Logger, now func() time.Time, trustedProxyHops int, clientHashKey []byte) http.Handler {
	return http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		started := now()
		metrics, ok := response.(responseMetrics)
		if !ok {
			var captured *responseCapture
			response, captured = newCaptureResponseWriter(response)
			metrics = captured
		}
		defer func() {
			panicValue := recover()
			status := metrics.responseStatus()
			outcome := metrics.responseOutcome()
			if outcome == "" {
				outcome = "completed"
			}
			if metrics.responseOverflowed() {
				status = http.StatusInternalServerError
				outcome = "response_too_large"
			}
			if panicValue != nil {
				status = http.StatusInternalServerError
				outcome = "recovered_panic"
			}
			route := request.Pattern
			if route == "" {
				route = "unmatched"
			}
			logger.Info("request completed",
				"request_id", RequestIDFromContext(request.Context()),
				"method", request.Method,
				"route", route,
				"status", status,
				"outcome", outcome,
				"client_hash", clientHash(request, trustedProxyHops, clientHashKey),
				"duration_ms", now().Sub(started).Milliseconds(),
			)
			if panicValue != nil {
				panic(panicValue)
			}
		}()
		next.ServeHTTP(response, request)
	})
}

// responseCapture records public response status without buffering. httpsnoop
// preserves the underlying writer's exact optional-interface set and exposes
// Unwrap for ResponseController.
type responseCapture struct {
	mu      sync.Mutex
	status  int
	outcome string
}

func newCaptureResponseWriter(response http.ResponseWriter) (http.ResponseWriter, *responseCapture) {
	capture := &responseCapture{}
	wrapped := httpsnoop.Wrap(response, httpsnoop.Hooks{
		WriteHeader: func(next httpsnoop.WriteHeaderFunc) httpsnoop.WriteHeaderFunc {
			return func(status int) {
				if capture.recordStatus(status) {
					next(status)
				}
			}
		},
		Write: func(next httpsnoop.WriteFunc) httpsnoop.WriteFunc {
			return func(value []byte) (int, error) {
				capture.recordStatus(http.StatusOK)
				return next(value)
			}
		},
		Flush: func(next httpsnoop.FlushFunc) httpsnoop.FlushFunc {
			return func() {
				capture.recordStatus(http.StatusOK)
				next()
			}
		},
		ReadFrom: func(next httpsnoop.ReadFromFunc) httpsnoop.ReadFromFunc {
			return func(reader io.Reader) (int64, error) {
				capture.recordStatus(http.StatusOK)
				return next(reader)
			}
		},
	})
	return wrapped, capture
}

func (capture *responseCapture) recordStatus(status int) bool {
	capture.mu.Lock()
	defer capture.mu.Unlock()
	if capture.status != 0 {
		return false
	}
	capture.status = status
	return true
}

func (capture *responseCapture) responseStatus() int {
	capture.mu.Lock()
	defer capture.mu.Unlock()
	if capture.status == 0 {
		return http.StatusOK
	}
	return capture.status
}

func (*responseCapture) responseOverflowed() bool { return false }

func (capture *responseCapture) responseOutcome() string {
	capture.mu.Lock()
	defer capture.mu.Unlock()
	return capture.outcome
}

func (capture *responseCapture) setResponseOutcome(outcome string) {
	capture.mu.Lock()
	defer capture.mu.Unlock()
	capture.outcome = outcome
}

func applySecurityHeaders(header http.Header, path string, production bool, nonce string) {
	csp := "default-src 'self'; base-uri 'none'; object-src 'none'; form-action 'self'; script-src 'self'"
	if nonce != "" {
		csp += " 'nonce-" + nonce + "'"
	}
	csp += "; style-src 'self'; font-src 'self'; img-src 'self'; connect-src 'self'; frame-ancestors "
	if exactArticlePreviewPath(path) {
		header.Set("Content-Security-Policy", csp+"'self'")
		header.Set("X-Frame-Options", "SAMEORIGIN")
	} else {
		header.Set("Content-Security-Policy", csp+"'none'")
		header.Set("X-Frame-Options", "DENY")
	}
	if production {
		header.Set("Strict-Transport-Security", "max-age=31536000; includeSubDomains")
	} else {
		header.Del("Strict-Transport-Security")
	}
	header.Set("Referrer-Policy", "no-referrer")
	header.Set("X-Content-Type-Options", "nosniff")
	header.Set("Permissions-Policy", "camera=(), geolocation=(), microphone=()")
	if adminPath(path) {
		header.Set("Cache-Control", "no-store")
		header.Set("X-Robots-Tag", "noindex, nofollow")
	}
}

func exactArticlePreviewPath(path string) bool {
	const prefix = "/admin/preview/articles/"
	if !strings.HasPrefix(path, prefix) {
		return false
	}
	id := strings.TrimPrefix(path, prefix)
	if id == "" {
		return false
	}
	for _, character := range id {
		if !(character >= 'a' && character <= 'z' || character >= 'A' && character <= 'Z' || character >= '0' && character <= '9' || character == '-' || character == '_') {
			return false
		}
	}
	return true
}

func writeAtomicFailure(response http.ResponseWriter, request *http.Request, safeHeaders http.Header, status int, code, message string, renderError func(http.ResponseWriter, *http.Request, int, string, string)) {
	clear(response.Header())
	copyHeaders(response.Header(), safeHeaders)
	writeErrorResponse(response, request, status, code, message, renderError)
}

func copyHeaders(destination, source http.Header) {
	clear(destination)
	for name, values := range source {
		destination[name] = append([]string(nil), values...)
	}
}

func safeFailureHeaders(source http.Header) http.Header {
	safe := make(http.Header)
	for _, name := range []string{
		"Cache-Control",
		"Content-Security-Policy",
		"Permissions-Policy",
		"Referrer-Policy",
		"Strict-Transport-Security",
		"X-Content-Type-Options",
		"X-Frame-Options",
		"X-Request-ID",
		"X-Robots-Tag",
	} {
		if values := source.Values(name); len(values) != 0 {
			safe[http.CanonicalHeaderKey(name)] = append([]string(nil), values...)
		}
	}
	return safe
}

func adminPath(path string) bool {
	return path == "/admin" || strings.HasPrefix(path, "/admin/") || path == "/api/admin" || strings.HasPrefix(path, "/api/admin/")
}

func apiPath(path string) bool {
	return path == "/api/admin" || strings.HasPrefix(path, "/api/admin/")
}

func writeErrorResponse(response http.ResponseWriter, request *http.Request, status int, code, message string, renderError func(http.ResponseWriter, *http.Request, int, string, string)) {
	markResponseOutcome(response, code)
	if apiPath(request.URL.Path) {
		WriteAPIError(response, request, status, code, message, nil)
		return
	}
	if renderError != nil {
		renderError(response, request, status, code, message)
		return
	}
	response.Header().Set("Cache-Control", "no-store")
	response.Header().Set("Content-Type", "text/html; charset=utf-8")
	response.WriteHeader(status)
	_, _ = io.WriteString(response, "<!doctype html><html lang=\"it\"><title>Errore</title><main><h1>Richiesta non disponibile</h1></main></html>")
}

func markResponseOutcome(response http.ResponseWriter, outcome string) {
	if outcome == "" {
		return
	}
	for response != nil {
		if marker, ok := response.(interface{ setResponseOutcome(string) }); ok {
			marker.setResponseOutcome(outcome)
			return
		}
		unwrapper, ok := response.(interface{ Unwrap() http.ResponseWriter })
		if !ok {
			return
		}
		response = unwrapper.Unwrap()
	}
}

func clientHash(request *http.Request, trustedProxyHops int, key []byte) string {
	identity, err := clientinfo.Resolve(request, trustedProxyHops)
	if err != nil {
		return "unknown"
	}
	mac := hmac.New(sha256.New, key)
	_, _ = mac.Write([]byte("callegarin/http-client/v1\x00"))
	_, _ = mac.Write(identity.Address.AsSlice())
	return base64.RawURLEncoding.EncodeToString(mac.Sum(nil)[:9])
}

type timeoutResult struct {
	panicked bool
}

func timeout(next http.Handler, duration time.Duration, responseLimit int64, logger *slog.Logger, renderError func(http.ResponseWriter, *http.Request, int, string, string)) http.Handler {
	return http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		ctx, cancel := context.WithTimeout(request.Context(), duration)
		defer cancel()
		safeHeaders := response.Header().Clone()
		buffered := newBoundedAdminResponse(safeHeaders, responseLimit)
		done := make(chan timeoutResult, 1)
		go func() {
			result := timeoutResult{}
			defer func() {
				if recover() != nil {
					result.panicked = true
				}
				done <- result
			}()
			next.ServeHTTP(buffered, request.WithContext(ctx))
		}()
		select {
		case result := <-done:
			if result.panicked {
				logger.Error("request panic recovered", "request_id", RequestIDFromContext(request.Context()), "outcome", "internal_error")
				markResponseOutcome(response, "internal_error")
				writeAtomicFailure(response, request, safeHeaders, http.StatusInternalServerError, "internal_error", "Si è verificato un errore. Riprova più tardi.", renderError)
				return
			}
			if buffered.responseOverflowed() {
				logger.Error("response exceeded buffer", "request_id", RequestIDFromContext(request.Context()), "outcome", "response_too_large")
				markResponseOutcome(response, "response_too_large")
				writeErrorResponse(response, request, http.StatusInternalServerError, "response_too_large", "Risposta temporaneamente non disponibile.", renderError)
				return
			}
			buffered.commit(response)
		case <-ctx.Done():
			writeErrorResponse(response, request, http.StatusServiceUnavailable, "request_timeout", "Il servizio sta impiegando troppo tempo. Riprova.", renderError)
		}
	})
}
