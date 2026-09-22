package adminapi

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"net/http"
	"net/netip"
	"sync"
	"time"

	"github.com/francescostumpo/legal-callegarin/internal/web/clientinfo"
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
	pinnedKey      string
	failClosedFull bool
}

type loginLimiter struct {
	mu        sync.Mutex
	addresses bucketSet
	usernames bucketSet
}

func newLoginLimiter(capacity int, refillInterval, idleExpiry time.Duration, maxEntries int, configuredUsernameKey string) *loginLimiter {
	newSet := func(pinnedKey string, failClosedFull bool) bucketSet {
		set := bucketSet{
			buckets: make(map[string]*loginBucket), capacity: float64(capacity),
			refillInterval: refillInterval, idleExpiry: idleExpiry, maxEntries: maxEntries,
			pinnedKey: pinnedKey, failClosedFull: failClosedFull,
		}
		if pinnedKey != "" && maxEntries > 0 {
			set.buckets[pinnedKey] = &loginBucket{tokens: float64(capacity)}
		}
		return set
	}
	return &loginLimiter{addresses: newSet("", false), usernames: newSet(configuredUsernameKey, true)}
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
		if candidate != set.pinnedKey && now.Sub(bucket.lastSeen) >= set.idleExpiry {
			delete(set.buckets, candidate)
		}
	}
	if set.failClosedFull && len(set.buckets) >= set.maxEntries {
		return false
	}
	bucket := set.buckets[key]
	if bucket == nil {
		if set.capacity <= 0 || set.refillInterval <= 0 || set.maxEntries <= 0 {
			return false
		}
		if len(set.buckets) >= set.maxEntries {
			if set.failClosedFull || !set.evictOldest() {
				return false
			}
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

func (set *bucketSet) evictOldest() bool {
	var oldest string
	var seen time.Time
	for key, bucket := range set.buckets {
		if key == set.pinnedKey {
			continue
		}
		if oldest == "" || bucket.lastSeen.Before(seen) {
			oldest, seen = key, bucket.lastSeen
		}
	}
	if oldest == "" {
		return false
	}
	delete(set.buckets, oldest)
	return true
}

func loginAddressKey(request *http.Request, trustedProxyHops int, key []byte) (string, error) {
	identity, err := clientinfo.Resolve(request, trustedProxyHops)
	if err != nil {
		return "", err
	}
	address := identity.Address
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
