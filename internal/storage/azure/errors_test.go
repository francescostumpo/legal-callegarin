package azure

import (
	"context"
	"errors"
	"net/http"
	"testing"

	"github.com/Azure/azure-sdk-for-go/sdk/azcore"
)

func TestClassifyStorageError(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want error
	}{
		{"not found", &azcore.ResponseError{StatusCode: http.StatusNotFound}, ErrNotFound},
		{"conflict", &azcore.ResponseError{StatusCode: http.StatusConflict}, ErrConflict},
		{"precondition", &azcore.ResponseError{StatusCode: http.StatusPreconditionFailed}, ErrPrecondition},
		{"unauthorized", &azcore.ResponseError{StatusCode: http.StatusUnauthorized}, ErrAuthentication},
		{"forbidden", &azcore.ResponseError{StatusCode: http.StatusForbidden}, ErrAuthentication},
		{"throttled", &azcore.ResponseError{StatusCode: http.StatusTooManyRequests}, ErrTransient},
		{"server failure", &azcore.ResponseError{StatusCode: http.StatusServiceUnavailable}, ErrTransient},
		{"deadline", context.DeadlineExceeded, context.DeadlineExceeded},
		{"cancel", context.Canceled, context.Canceled},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := classifyStorageError(test.err); !errors.Is(got, test.want) {
				t.Fatalf("classifyStorageError(%v) = %v, want %v", test.err, got, test.want)
			}
		})
	}
}

func TestClassifyTransactionErrorReadsInnerMultipartStatus(t *testing.T) {
	for _, test := range []struct {
		status string
		want   error
	}{
		{"409 Conflict", ErrConflict},
		{"412 Precondition Failed", ErrPrecondition},
		{"404 Not Found", ErrNotFound},
		{"403 Forbidden", ErrAuthentication},
		{"503 Service Unavailable", ErrTransient},
	} {
		err := errors.New("RESPONSE 202: 202 Accepted\r\nHTTP/1.1 " + test.status + "\r\n")
		if got := classifyTransactionError(err); !errors.Is(got, test.want) {
			t.Fatalf("inner %s classified as %v, want %v", test.status, got, test.want)
		}
	}
}
