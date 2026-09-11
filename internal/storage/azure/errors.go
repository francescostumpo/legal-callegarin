package azure

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"strings"

	"github.com/Azure/azure-sdk-for-go/sdk/azcore"
)

var (
	ErrNotFound       = errors.New("azure storage object not found")
	ErrConflict       = errors.New("azure storage conflict")
	ErrPrecondition   = errors.New("azure storage precondition failed")
	ErrAuthentication = errors.New("azure storage authentication failed")
	ErrTransient      = errors.New("azure storage transient failure")
)

func classifyStorageError(err error) error {
	if err == nil || errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return err
	}
	var responseError *azcore.ResponseError
	if errors.As(err, &responseError) {
		var class error
		switch responseError.StatusCode {
		case http.StatusNotFound:
			class = ErrNotFound
		case http.StatusConflict:
			class = ErrConflict
		case http.StatusPreconditionFailed:
			class = ErrPrecondition
		case http.StatusUnauthorized, http.StatusForbidden:
			class = ErrAuthentication
		case http.StatusRequestTimeout, http.StatusTooManyRequests,
			http.StatusInternalServerError, http.StatusBadGateway,
			http.StatusServiceUnavailable, http.StatusGatewayTimeout:
			class = ErrTransient
		}
		if class != nil {
			return fmt.Errorf("%w: %v", class, err)
		}
	}
	var networkError net.Error
	if errors.As(err, &networkError) {
		return fmt.Errorf("%w: %v", ErrTransient, err)
	}
	return err
}

// The Tables SDK reports a failed transactional subrequest using the outer
// batch status (202). The only preserved subrequest status is in the parsed
// multipart error text, so classify it at this boundary before retry policy is
// applied. Exact HTTP status lines avoid matching application data.
func classifyTransactionError(err error) error {
	if err == nil {
		return nil
	}
	for _, status := range []struct {
		line  string
		class error
	}{
		{"HTTP/1.1 404 ", ErrNotFound},
		{"HTTP/1.1 409 ", ErrConflict},
		{"HTTP/1.1 412 ", ErrPrecondition},
		{"HTTP/1.1 401 ", ErrAuthentication},
		{"HTTP/1.1 403 ", ErrAuthentication},
		{"HTTP/1.1 408 ", ErrTransient},
		{"HTTP/1.1 429 ", ErrTransient},
		{"HTTP/1.1 500 ", ErrTransient},
		{"HTTP/1.1 502 ", ErrTransient},
		{"HTTP/1.1 503 ", ErrTransient},
		{"HTTP/1.1 504 ", ErrTransient},
	} {
		if strings.Contains(err.Error(), status.line) {
			return fmt.Errorf("%w: %v", status.class, err)
		}
	}
	return classifyStorageError(err)
}
