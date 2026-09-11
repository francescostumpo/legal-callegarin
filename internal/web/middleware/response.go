package middleware

import (
	"bufio"
	"bytes"
	"errors"
	"io"
	"log/slog"
	"net"
	"net/http"
	"strings"
	"time"
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
			captured := &captureResponseWriter{ResponseWriter: response}
			response = captured
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

// captureResponseWriter records status without buffering public responses. It
// exposes Unwrap for ResponseController and forwards the optional interfaces
// used by the standard HTTP stack when the underlying writer supports them.
type captureResponseWriter struct {
	http.ResponseWriter
	status int
}

func (writer *captureResponseWriter) Unwrap() http.ResponseWriter { return writer.ResponseWriter }

func (writer *captureResponseWriter) WriteHeader(status int) {
	if writer.status != 0 {
		return
	}
	writer.status = status
	writer.ResponseWriter.WriteHeader(status)
}

func (writer *captureResponseWriter) Write(value []byte) (int, error) {
	if writer.status == 0 {
		writer.WriteHeader(http.StatusOK)
	}
	return writer.ResponseWriter.Write(value)
}

func (writer *captureResponseWriter) responseStatus() int {
	if writer.status == 0 {
		return http.StatusOK
	}
	return writer.status
}

func (*captureResponseWriter) responseOverflowed() bool { return false }

func (writer *captureResponseWriter) Flush() {
	if writer.status == 0 {
		writer.WriteHeader(http.StatusOK)
	}
	_ = http.NewResponseController(writer.ResponseWriter).Flush()
}

func (writer *captureResponseWriter) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	return http.NewResponseController(writer.ResponseWriter).Hijack()
}

func (writer *captureResponseWriter) Push(target string, options *http.PushOptions) error {
	current := writer.ResponseWriter
	for {
		if pusher, ok := current.(http.Pusher); ok {
			return pusher.Push(target, options)
		}
		unwrapper, ok := current.(interface{ Unwrap() http.ResponseWriter })
		if !ok {
			return http.ErrNotSupported
		}
		current = unwrapper.Unwrap()
	}
}

func (writer *captureResponseWriter) ReadFrom(reader io.Reader) (int64, error) {
	if writer.status == 0 {
		writer.WriteHeader(http.StatusOK)
	}
	if readerFrom, ok := writer.ResponseWriter.(io.ReaderFrom); ok {
		return readerFrom.ReadFrom(reader)
	}
	return io.Copy(writer.ResponseWriter, reader)
}

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
