package azure

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"regexp"
	"strconv"

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
			return fmt.Errorf("%w: Azure Storage returned HTTP %d", class, responseError.StatusCode)
		}
		return fmt.Errorf("azure storage returned HTTP %d", responseError.StatusCode)
	}
	var networkError net.Error
	if errors.As(err, &networkError) {
		return fmt.Errorf("%w: network request failed", ErrTransient)
	}
	return err
}

// The Tables SDK reports a failed transactional subrequest using the outer
// batch status (202). The only preserved subrequest status is in the parsed
// multipart error text, so classify it at this boundary before retry policy is
// applied. Exact HTTP status lines avoid matching application data.
var transactionStatusLine = regexp.MustCompile(`\r\nContent-Type: application/http\r\nContent-Transfer-Encoding: binary\r\n\r\nHTTP/1\.1 ([0-9]{3})(?: [^\r\n]*)?\r\n`)

func classifyTransactionError(err error) error {
	if err == nil {
		return nil
	}
	var responseError *azcore.ResponseError
	if !errors.As(err, &responseError) || responseError.StatusCode != http.StatusAccepted {
		return classifyStorageError(err)
	}
	match := transactionStatusLine.FindStringSubmatch(err.Error())
	if len(match) != 2 {
		return classifyStorageError(err)
	}
	status, parseErr := strconv.Atoi(match[1])
	if parseErr != nil {
		return classifyStorageError(err)
	}
	var class error
	switch status {
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
		return fmt.Errorf("%w: table transaction subrequest returned HTTP %d", class, status)
	}
	return classifyStorageError(err)
}
