package azure

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
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
		err := responseErrorWithMessage(t, http.StatusAccepted, "multipart boundary\r\nContent-Type: application/http\r\nContent-Transfer-Encoding: binary\r\n\r\nHTTP/1.1 "+test.status+"\r\n")
		if got := classifyTransactionError(err); !errors.Is(got, test.want) {
			t.Fatalf("inner %s classified as %v, want %v", test.status, got, test.want)
		}
	}
}

func TestClassifyTransactionErrorRejectsUntrustedStatusText(t *testing.T) {
	secret := "customer-secret HTTP/1.1 409 Conflict"
	for _, err := range []error{
		errors.New("\r\n\r\nHTTP/1.1 409 Conflict\r\n"),
		responseErrorWithMessage(t, http.StatusBadRequest, "\r\n\r\nHTTP/1.1 409 Conflict\r\n"),
		responseErrorWithMessage(t, http.StatusAccepted, "application data: "+secret+"\r\n"),
		responseErrorWithMessage(t, http.StatusAccepted, "\n\nHTTP/1.1 409 Conflict\n"),
	} {
		classified := classifyTransactionError(err)
		if errors.Is(classified, ErrConflict) {
			t.Fatalf("classifyTransactionError(%T) trusted an unanchored status", err)
		}
		if classified != nil && classified.Error() != err.Error() && contains(classified.Error(), "customer-secret") {
			t.Fatal("classified error exposed application data")
		}
	}
}

func TestClassifyTransactionErrorDoesNotExposeMultipartBody(t *testing.T) {
	err := responseErrorWithMessage(t, http.StatusAccepted, "secret-value\r\nContent-Type: application/http\r\nContent-Transfer-Encoding: binary\r\n\r\nHTTP/1.1 409 Conflict\r\n")
	classified := classifyTransactionError(err)
	if !errors.Is(classified, ErrConflict) || contains(classified.Error(), "secret-value") {
		t.Fatalf("classified error = %q", classified)
	}
}

func responseErrorWithMessage(t *testing.T, status int, message string) error {
	t.Helper()
	encoded, err := json.Marshal(map[string]any{"statusCode": status, "errorMessage": message})
	if err != nil {
		t.Fatal(err)
	}
	var response azcore.ResponseError
	if err := json.Unmarshal(encoded, &response); err != nil {
		t.Fatal(err)
	}
	return &response
}

func contains(value, substring string) bool {
	return len(substring) <= len(value) && strings.Contains(value, substring)
}
