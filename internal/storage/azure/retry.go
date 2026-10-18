package azure

import (
	"context"
	"errors"
	"math/rand/v2"
	"time"
)

const (
	defaultAttempts  = 3
	defaultTimeout   = 5 * time.Second
	defaultBaseDelay = 50 * time.Millisecond
	defaultMaxDelay  = 500 * time.Millisecond
)

type operationExecutor struct {
	attempts  int
	timeout   time.Duration
	baseDelay time.Duration
	maxDelay  time.Duration
	jitter    func(time.Duration) time.Duration
	sleep     func(context.Context, time.Duration) error
}

func defaultExecutor() operationExecutor {
	return operationExecutor{
		attempts: defaultAttempts, timeout: defaultTimeout,
		baseDelay: defaultBaseDelay, maxDelay: defaultMaxDelay,
		jitter: func(delay time.Duration) time.Duration {
			return delay + time.Duration(rand.Int64N(max(1, int64(delay/2))))
		},
		sleep: sleepContext,
	}
}

func (executor operationExecutor) do(ctx context.Context, operation func(context.Context) error) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	attempts := executor.attempts
	if attempts <= 0 {
		attempts = defaultAttempts
	}
	timeout := executor.timeout
	if timeout <= 0 {
		timeout = defaultTimeout
	}
	baseDelay := executor.baseDelay
	if baseDelay <= 0 {
		baseDelay = defaultBaseDelay
	}
	maxDelay := executor.maxDelay
	if maxDelay <= 0 {
		maxDelay = defaultMaxDelay
	}
	jitter := executor.jitter
	if jitter == nil {
		jitter = func(delay time.Duration) time.Duration { return delay }
	}
	sleep := executor.sleep
	if sleep == nil {
		sleep = sleepContext
	}

	var err error
	for attempt := 0; attempt < attempts; attempt++ {
		attemptCtx, cancel := context.WithTimeout(ctx, timeout)
		err = operation(attemptCtx)
		cancel()
		if ctxErr := ctx.Err(); ctxErr != nil {
			return ctxErr
		}
		classified := classifyStorageError(err)
		if err == nil || !errors.Is(classified, ErrTransient) || attempt == attempts-1 {
			return classified
		}
		delay := baseDelay << attempt
		if delay > maxDelay {
			delay = maxDelay
		}
		if err := sleep(ctx, jitter(delay)); err != nil {
			return err
		}
	}
	return classifyStorageError(err)
}

func (executor operationExecutor) mutate(ctx context.Context, operation func(context.Context) error) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	timeout := executor.timeout
	if timeout <= 0 {
		timeout = defaultTimeout
	}
	attemptCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	err := operation(attemptCtx)
	if ctxErr := ctx.Err(); ctxErr != nil {
		return ctxErr
	}
	return classifyStorageError(err)
}

func sleepContext(ctx context.Context, delay time.Duration) error {
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}
