package middleware

import (
	"bytes"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/felixge/httpsnoop"
)

const defaultAdminResponseLimit = int64(2 << 20)

var errAdminResponseTooLarge = errors.New("admin response exceeds buffer limit")

type responseMetrics interface {
	responseStatus() int
	responseOverflowed() bool
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

func (response *boundedAdminResponse) commit(destination http.ResponseWriter) {
	copyHeaders(destination.Header(), response.header)
	destination.WriteHeader(response.responseStatus())
	if response.body.Len() > 0 {
		_, _ = destination.Write(response.body.Bytes())
	}
}

func recoverPanics(next http.Handler, logger *slog.Logger, adminResponseLimit int64) http.Handler {
	return http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		if !adminPath(request.URL.Path) {
			defer func() {
				if recover() == nil {
					return
				}
				logger.Error("request panic recovered", "request_id", RequestIDFromContext(request.Context()))
				response.Header().Set("Cache-Control", "no-store")
				response.Header().Set("Content-Type", "text/plain; charset=utf-8")
				response.WriteHeader(http.StatusInternalServerError)
				_, _ = response.Write([]byte("Internal Server Error\n"))
			}()
			next.ServeHTTP(response, request)
			return
		}

		buffered := newBoundedAdminResponse(response.Header(), adminResponseLimit)
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
			writeAtomicAdminFailure(response, request.URL.Path)
		case buffered.responseOverflowed():
			logger.Error("admin response exceeded buffer", "request_id", RequestIDFromContext(request.Context()))
			writeAtomicAdminFailure(response, request.URL.Path)
		default:
			buffered.commit(response)
		}
	})
}

func accessLog(next http.Handler, logger *slog.Logger, now func() time.Time) http.Handler {
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
			outcome := "completed"
			if metrics.responseOverflowed() {
				status = http.StatusInternalServerError
				outcome = "response_too_large"
			}
			if panicValue != nil {
				status = http.StatusInternalServerError
				outcome = "recovered_panic"
			}
			logger.Info("request completed",
				"request_id", RequestIDFromContext(request.Context()),
				"method", request.Method,
				"path", request.URL.Path,
				"status", status,
				"outcome", outcome,
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
	mu     sync.Mutex
	status int
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

func applySecurityHeaders(header http.Header, path string) {
	header.Set("Content-Security-Policy", "default-src 'self'; base-uri 'none'; frame-ancestors 'none'; form-action 'self'; object-src 'none'")
	header.Set("Strict-Transport-Security", "max-age=31536000; includeSubDomains")
	header.Set("Referrer-Policy", "no-referrer")
	header.Set("X-Content-Type-Options", "nosniff")
	header.Set("X-Frame-Options", "DENY")
	header.Set("Permissions-Policy", "camera=(), geolocation=(), microphone=()")
	if adminPath(path) {
		header.Set("Cache-Control", "no-store")
		header.Set("X-Robots-Tag", "noindex, nofollow")
	}
}

func writeAtomicAdminFailure(response http.ResponseWriter, path string) {
	requestID := response.Header().Get("X-Request-ID")
	clear(response.Header())
	if requestID != "" {
		response.Header().Set("X-Request-ID", requestID)
	}
	applySecurityHeaders(response.Header(), path)
	response.Header().Set("Content-Type", "text/plain; charset=utf-8")
	response.WriteHeader(http.StatusInternalServerError)
	_, _ = response.Write([]byte("Internal Server Error\n"))
}

func copyHeaders(destination, source http.Header) {
	clear(destination)
	for name, values := range source {
		destination[name] = append([]string(nil), values...)
	}
}

func adminPath(path string) bool {
	return path == "/admin" || strings.HasPrefix(path, "/admin/") || path == "/api/admin" || strings.HasPrefix(path, "/api/admin/")
}
