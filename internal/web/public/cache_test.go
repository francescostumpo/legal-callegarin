package public

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestCacheCopiesValuesAndExpiresWithInjectedClock(t *testing.T) {
	t.Parallel()

	clock := &cacheClock{now: time.Date(2026, 9, 11, 12, 0, 0, 0, time.UTC)}
	cache := newHTMLCache(clock.Now, 2, 64, 15*time.Minute)
	var fills int
	source := []byte("first")
	first, err := cache.GetOrFill(context.Background(), "detail:first", func(context.Context) ([]byte, error) {
		fills++
		return source, nil
	})
	if err != nil {
		t.Fatalf("GetOrFill(first) error = %v", err)
	}
	first[0] = 'x'
	source[1] = 'x'
	cached, err := cache.GetOrFill(context.Background(), "detail:first", func(context.Context) ([]byte, error) {
		fills++
		return []byte("wrong"), nil
	})
	if err != nil || string(cached) != "first" || fills != 1 {
		t.Fatalf("cached value = %q, fills = %d, error = %v", cached, fills, err)
	}

	clock.now = clock.now.Add(15*time.Minute + time.Nanosecond)
	refreshed, err := cache.GetOrFill(context.Background(), "detail:first", func(context.Context) ([]byte, error) {
		fills++
		return []byte("second"), nil
	})
	if err != nil || string(refreshed) != "second" || fills != 2 {
		t.Fatalf("refreshed value = %q, fills = %d, error = %v", refreshed, fills, err)
	}
}

func TestCacheEnforcesLRUEntryAndByteLimits(t *testing.T) {
	t.Parallel()

	cache := newHTMLCache(time.Now, 2, 5, time.Hour)
	fillCacheValue(t, cache, "a", "aa")
	fillCacheValue(t, cache, "b", "bb")
	touched, err := cache.GetOrFill(context.Background(), "a", func(context.Context) ([]byte, error) {
		return []byte("unused"), nil
	})
	if err != nil || string(touched) != "aa" {
		t.Fatalf("touch cached a = %q, %v", touched, err)
	}
	fillCacheValue(t, cache, "c", "cc")

	var bFills int
	value, err := cache.GetOrFill(context.Background(), "b", func(context.Context) ([]byte, error) {
		bFills++
		return []byte("B"), nil
	})
	if err != nil || string(value) != "B" || bFills != 1 {
		t.Fatalf("LRU-evicted b = %q, fills = %d, error = %v", value, bFills, err)
	}

	fillCacheValue(t, cache, "large-a", "123")
	fillCacheValue(t, cache, "large-b", "456")
	var firstFills int
	value, err = cache.GetOrFill(context.Background(), "large-a", func(context.Context) ([]byte, error) {
		firstFills++
		return []byte("new"), nil
	})
	if err != nil || string(value) != "new" || firstFills != 1 {
		t.Fatalf("byte-evicted value = %q, fills = %d, error = %v", value, firstFills, err)
	}

	oversized := newHTMLCache(time.Now, 2, 2, time.Hour)
	var oversizedFills int
	for range 2 {
		value, err = oversized.GetOrFill(context.Background(), "oversized", func(context.Context) ([]byte, error) {
			oversizedFills++
			return []byte("123"), nil
		})
		if err != nil || string(value) != "123" {
			t.Fatalf("oversized value = %q, error = %v", value, err)
		}
	}
	if oversizedFills != 2 {
		t.Fatalf("oversized fill count = %d, want 2", oversizedFills)
	}
}

func TestCacheSuppressesDuplicateConcurrentFills(t *testing.T) {
	t.Parallel()

	cache := newHTMLCache(time.Now, 256, 32<<20, 15*time.Minute)
	started := make(chan struct{})
	release := make(chan struct{})
	var fills atomic.Int32
	fill := func(context.Context) ([]byte, error) {
		if fills.Add(1) == 1 {
			close(started)
		}
		<-release
		return []byte("shared"), nil
	}

	const callers = 24
	var wait sync.WaitGroup
	wait.Add(callers)
	errorsFound := make(chan error, callers)
	for range callers {
		go func() {
			defer wait.Done()
			value, err := cache.GetOrFill(context.Background(), "home", fill)
			if err != nil {
				errorsFound <- err
				return
			}
			if string(value) != "shared" {
				errorsFound <- errors.New("unexpected shared value")
			}
		}()
	}
	<-started
	close(release)
	wait.Wait()
	close(errorsFound)
	for err := range errorsFound {
		t.Error(err)
	}
	if fills.Load() != 1 {
		t.Fatalf("fill count = %d, want 1", fills.Load())
	}
}

func TestCacheDoesNotStoreErrorsAndInvalidatesNamespaces(t *testing.T) {
	t.Parallel()

	cache := newHTMLCache(time.Now, 10, 1024, time.Hour)
	wantError := errors.New("storage unavailable")
	if _, err := cache.GetOrFill(context.Background(), "detail:error", func(context.Context) ([]byte, error) {
		return nil, wantError
	}); !errors.Is(err, wantError) {
		t.Fatalf("GetOrFill(error) = %v, want storage error", err)
	}
	fillCacheValue(t, cache, "detail:one", "one")
	fillCacheValue(t, cache, "index:first", "index")
	fillCacheValue(t, cache, "home", "home")
	cache.Invalidate("home")
	cache.InvalidatePrefix("detail:")

	for _, key := range []string{"home", "detail:one"} {
		var fills int
		_, err := cache.GetOrFill(context.Background(), key, func(context.Context) ([]byte, error) {
			fills++
			return []byte("refilled"), nil
		})
		if err != nil || fills != 1 {
			t.Fatalf("invalidated key %q fills = %d, error = %v", key, fills, err)
		}
	}
	var indexFills int
	value, err := cache.GetOrFill(context.Background(), "index:first", func(context.Context) ([]byte, error) {
		indexFills++
		return []byte("wrong"), nil
	})
	if err != nil || string(value) != "index" || indexFills != 0 {
		t.Fatalf("unrelated key value = %q, fills = %d, error = %v", value, indexFills, err)
	}
}

func TestCacheInvalidationStartsANewFillInsteadOfJoiningAStaleFlight(t *testing.T) {
	t.Parallel()

	cache := newHTMLCache(time.Now, 10, 1024, time.Hour)
	oldStarted := make(chan struct{})
	releaseOld := make(chan struct{})
	oldDone := make(chan string, 1)
	go func() {
		value, _ := cache.GetOrFill(context.Background(), "detail:one", func(context.Context) ([]byte, error) {
			close(oldStarted)
			<-releaseOld
			return []byte("old"), nil
		})
		oldDone <- string(value)
	}()
	<-oldStarted

	cache.Invalidate("detail:one")
	newStarted := make(chan struct{})
	newDone := make(chan string, 1)
	go func() {
		value, _ := cache.GetOrFill(context.Background(), "detail:one", func(context.Context) ([]byte, error) {
			close(newStarted)
			return []byte("new"), nil
		})
		newDone <- string(value)
	}()

	select {
	case <-newStarted:
	case <-time.After(time.Second):
		close(releaseOld)
		<-oldDone
		<-newDone
		t.Fatal("post-invalidation request joined the stale in-flight fill")
	}
	if value := <-newDone; value != "new" {
		t.Fatalf("post-invalidation value = %q, want new", value)
	}
	close(releaseOld)
	if value := <-oldDone; value != "old" {
		t.Fatalf("pre-invalidation value = %q, want old", value)
	}
	value, err := cache.GetOrFill(context.Background(), "detail:one", func(context.Context) ([]byte, error) {
		return []byte("unexpected"), nil
	})
	if err != nil || string(value) != "new" {
		t.Fatalf("cached post-invalidation value = %q, error = %v", value, err)
	}
}

func fillCacheValue(t *testing.T, cache *htmlCache, key, value string) {
	t.Helper()
	got, err := cache.GetOrFill(context.Background(), key, func(context.Context) ([]byte, error) {
		return []byte(value), nil
	})
	if err != nil || string(got) != value {
		t.Fatalf("fill %q = %q, %v", key, got, err)
	}
}

type cacheClock struct{ now time.Time }

func (clock *cacheClock) Now() time.Time { return clock.now }
