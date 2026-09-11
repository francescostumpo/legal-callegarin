package azure

import (
	"context"
	"errors"
	"net/http"
	"testing"
	"time"

	"github.com/Azure/azure-sdk-for-go/sdk/azcore"
)

func TestExecutorRetriesOnlyTransientErrors(t *testing.T) {
	var attempts int
	var delays []time.Duration
	executor := operationExecutor{
		attempts:  3,
		baseDelay: 10 * time.Millisecond,
		maxDelay:  15 * time.Millisecond,
		jitter:    func(delay time.Duration) time.Duration { return delay + time.Millisecond },
		sleep:     func(_ context.Context, delay time.Duration) error { delays = append(delays, delay); return nil },
	}
	err := executor.do(context.Background(), func(context.Context) error {
		attempts++
		return &azcore.ResponseError{StatusCode: http.StatusServiceUnavailable}
	})
	if !errors.Is(err, ErrTransient) || attempts != 3 {
		t.Fatalf("do() error = %v, attempts = %d", err, attempts)
	}
	if len(delays) != 2 || delays[0] != 11*time.Millisecond || delays[1] != 16*time.Millisecond {
		t.Fatalf("delays = %v", delays)
	}

	for _, status := range []int{http.StatusBadRequest, http.StatusUnauthorized, http.StatusForbidden, http.StatusNotFound, http.StatusConflict, http.StatusPreconditionFailed} {
		attempts = 0
		err := executor.do(context.Background(), func(context.Context) error {
			attempts++
			return &azcore.ResponseError{StatusCode: status}
		})
		if err == nil || attempts != 1 {
			t.Fatalf("status %d: error = %v, attempts = %d", status, err, attempts)
		}
	}
}

func TestExecutorAppliesPerAttemptDeadlineAndCancellation(t *testing.T) {
	executor := operationExecutor{attempts: 3, timeout: time.Millisecond}
	var attempts int
	err := executor.do(context.Background(), func(ctx context.Context) error {
		attempts++
		<-ctx.Done()
		return ctx.Err()
	})
	if !errors.Is(err, context.DeadlineExceeded) || attempts != 1 {
		t.Fatalf("do() error = %v, attempts = %d", err, attempts)
	}

	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	attempts = 0
	err = executor.do(canceled, func(context.Context) error { attempts++; return nil })
	if !errors.Is(err, context.Canceled) || attempts != 0 {
		t.Fatalf("canceled do() error = %v, attempts = %d", err, attempts)
	}
}
