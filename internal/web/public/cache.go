package public

import (
	"container/list"
	"context"
	"sync"
	"time"
)

const (
	publicCacheMaxEntries = 256
	publicCacheMaxBytes   = 32 << 20
	publicCacheTTL        = 15 * time.Minute
)

type htmlCache struct {
	mu         sync.Mutex
	now        func() time.Time
	maxEntries int
	maxBytes   int
	ttl        time.Duration
	entries    map[string]*cacheEntry
	recency    *list.List
	flights    map[string]*cacheFlight
	totalBytes int
	generation uint64
}

type cacheEntry struct {
	key       string
	value     []byte
	expiresAt time.Time
	element   *list.Element
}

type cacheFlight struct {
	done       chan struct{}
	generation uint64
	value      []byte
	err        error
}

func newHTMLCache(now func() time.Time, maxEntries, maxBytes int, ttl time.Duration) *htmlCache {
	return &htmlCache{
		now: now, maxEntries: maxEntries, maxBytes: maxBytes, ttl: ttl,
		entries: make(map[string]*cacheEntry), recency: list.New(), flights: make(map[string]*cacheFlight),
	}
}

func (cache *htmlCache) GetOrFill(ctx context.Context, key string, fill func(context.Context) ([]byte, error)) ([]byte, error) {
	cache.mu.Lock()
	if entry, exists := cache.entries[key]; exists {
		if cache.now().Before(entry.expiresAt) {
			cache.recency.MoveToFront(entry.element)
			value := cloneBytes(entry.value)
			cache.mu.Unlock()
			return value, nil
		}
		cache.removeEntry(entry)
	}
	if flight, exists := cache.flights[key]; exists && flight.generation == cache.generation {
		cache.mu.Unlock()
		select {
		case <-flight.done:
			return cloneBytes(flight.value), flight.err
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	flight := &cacheFlight{done: make(chan struct{}), generation: cache.generation}
	cache.flights[key] = flight
	cache.mu.Unlock()

	value, err := fill(ctx)
	result := cloneBytes(value)

	cache.mu.Lock()
	if cache.flights[key] == flight {
		delete(cache.flights, key)
	}
	flight.value = cloneBytes(result)
	flight.err = err
	if err == nil && flight.generation == cache.generation && len(result) <= cache.maxBytes {
		cache.insert(key, result)
	}
	close(flight.done)
	cache.mu.Unlock()
	return result, err
}

func (cache *htmlCache) Invalidate(keys ...string) {
	cache.mu.Lock()
	defer cache.mu.Unlock()
	cache.generation++
	for _, key := range keys {
		if entry, exists := cache.entries[key]; exists {
			cache.removeEntry(entry)
		}
	}
}

func (cache *htmlCache) InvalidatePrefix(prefix string) {
	cache.mu.Lock()
	defer cache.mu.Unlock()
	cache.generation++
	for key, entry := range cache.entries {
		if len(key) >= len(prefix) && key[:len(prefix)] == prefix {
			cache.removeEntry(entry)
		}
	}
}

func (cache *htmlCache) insert(key string, value []byte) {
	if existing, exists := cache.entries[key]; exists {
		cache.removeEntry(existing)
	}
	stored := cloneBytes(value)
	entry := &cacheEntry{key: key, value: stored, expiresAt: cache.now().Add(cache.ttl)}
	entry.element = cache.recency.PushFront(entry)
	cache.entries[key] = entry
	cache.totalBytes += len(stored)
	for len(cache.entries) > cache.maxEntries || cache.totalBytes > cache.maxBytes {
		oldest := cache.recency.Back()
		if oldest == nil {
			break
		}
		cache.removeEntry(oldest.Value.(*cacheEntry))
	}
}

func (cache *htmlCache) removeEntry(entry *cacheEntry) {
	delete(cache.entries, entry.key)
	cache.recency.Remove(entry.element)
	cache.totalBytes -= len(entry.value)
}

func cloneBytes(value []byte) []byte {
	return append([]byte(nil), value...)
}
