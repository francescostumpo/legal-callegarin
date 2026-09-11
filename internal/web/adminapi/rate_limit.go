package adminapi

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"net"
	"net/http"
	"net/netip"
	"strings"
	"sync"
	"time"
)

type loginBucket struct {
	tokens     float64
	lastRefill time.Time
	lastSeen   time.Time
}

type bucketSet struct {
	buckets        map[string]*loginBucket
	capacity       float64
	refillInterval time.Duration
	idleExpiry     time.Duration
	maxEntries     int
}

type loginLimiter struct {
	mu        sync.Mutex
	addresses bucketSet
	usernames bucketSet
}

func newLoginLimiter(capacity int, refillInterval, idleExpiry time.Duration, maxEntries int) *loginLimiter {
	newSet := func() bucketSet {
		return bucketSet{buckets: make(map[string]*loginBucket), capacity: float64(capacity), refillInterval: refillInterval, idleExpiry: idleExpiry, maxEntries: maxEntries}
	}
	return &loginLimiter{addresses: newSet(), usernames: newSet()}
}

func (limiter *loginLimiter) allowAddress(key string, now time.Time) bool {
	limiter.mu.Lock()
	defer limiter.mu.Unlock()
	return limiter.addresses.allow(key, now)
}

func (limiter *loginLimiter) allowUsername(key string, now time.Time) bool {
	limiter.mu.Lock()
	defer limiter.mu.Unlock()
	return limiter.usernames.allow(key, now)
}

func (limiter *loginLimiter) addressSize() int {
	limiter.mu.Lock()
	defer limiter.mu.Unlock()
	return len(limiter.addresses.buckets)
}

func (limiter *loginLimiter) usernameSize() int {
	limiter.mu.Lock()
	defer limiter.mu.Unlock()
	return len(limiter.usernames.buckets)
}

func (set *bucketSet) allow(key string, now time.Time) bool {
	for candidate, bucket := range set.buckets {
		if now.Sub(bucket.lastSeen) >= set.idleExpiry {
			delete(set.buckets, candidate)
		}
	}
	bucket := set.buckets[key]
	if bucket == nil {
		if set.capacity <= 0 || set.refillInterval <= 0 || set.maxEntries <= 0 {
			return false
		}
		if len(set.buckets) >= set.maxEntries {
			set.evictOldest()
		}
		bucket = &loginBucket{tokens: set.capacity, lastRefill: now, lastSeen: now}
		set.buckets[key] = bucket
	}
	if elapsed := now.Sub(bucket.lastRefill); elapsed > 0 {
		bucket.tokens = min(set.capacity, bucket.tokens+float64(elapsed)/float64(set.refillInterval))
		bucket.lastRefill = now
	}
	bucket.lastSeen = now
	if bucket.tokens < 1 {
		return false
	}
	bucket.tokens--
	return true
}

func (set *bucketSet) evictOldest() {
	var oldest string
	var seen time.Time
	for key, bucket := range set.buckets {
		if oldest == "" || bucket.lastSeen.Before(seen) {
			oldest, seen = key, bucket.lastSeen
		}
	}
	delete(set.buckets, oldest)
}

func loginAddressKey(request *http.Request, trustedProxy bool, key []byte) (string, error) {
	raw := request.RemoteAddr
	if trustedProxy {
		if forwarded := request.Header.Get("X-Forwarded-For"); forwarded != "" {
			raw, _, _ = strings.Cut(forwarded, ",")
			raw = strings.TrimSpace(raw)
		}
	}
	if host, _, err := net.SplitHostPort(raw); err == nil {
		raw = host
	}
	address, err := netip.ParseAddr(strings.Trim(raw, "[]"))
	if err != nil {
		return "", errors.New("invalid client address")
	}
	address = address.Unmap()
	prefixBits := 56
	if address.Is4() {
		prefixBits = 24
	}
	masked := netip.PrefixFrom(address, prefixBits).Masked().Addr()
	mac := hmac.New(sha256.New, key)
	_, _ = mac.Write([]byte("callegarin/admin-login-address/v1\x00"))
	_, _ = mac.Write(masked.AsSlice())
	return base64.RawURLEncoding.EncodeToString(mac.Sum(nil)[:16]), nil
}
