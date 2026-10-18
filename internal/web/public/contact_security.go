package public

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"net/http"
	"net/netip"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/francescostumpo/legal-callegarin/internal/web/clientinfo"
)

const (
	contactMinimumCompletionTime = 3 * time.Second
	contactFormTokenMaxAge       = 2 * time.Hour
	contactSuccessTokenMaxAge    = 5 * time.Minute
	contactTokenFutureSkew       = 5 * time.Second
	contactSigningKeyMinLength   = 32
)

var rawURL = base64.RawURLEncoding

type contactSigner struct {
	formKey    []byte
	successKey []byte
	addressKey []byte
}

func newContactSigner(master []byte) (*contactSigner, error) {
	if len(master) < contactSigningKeyMinLength {
		return nil, fmt.Errorf("contact signing key must contain at least %d bytes", contactSigningKeyMinLength)
	}
	return &contactSigner{
		formKey:    deriveContactKey(master, "public-contact-form-timestamp-v1"),
		successKey: deriveContactKey(master, "public-contact-success-v1"),
		addressKey: deriveContactKey(master, "public-contact-client-address-v1"),
	}, nil
}

func deriveContactKey(master []byte, label string) []byte {
	mac := hmac.New(sha256.New, master)
	_, _ = mac.Write([]byte(label))
	return mac.Sum(nil)
}

func (signer *contactSigner) newFormToken(now time.Time) string {
	return newContactTimedToken(signer.formKey, now)
}

func (signer *contactSigner) validFormToken(token string, now time.Time) bool {
	issued, valid := validateContactTimedToken(signer.formKey, token)
	if !valid {
		return false
	}
	age := now.Sub(issued)
	if age < -contactTokenFutureSkew {
		return false
	}
	return age >= contactMinimumCompletionTime && age <= contactFormTokenMaxAge
}

func (signer *contactSigner) newSuccessToken(now time.Time) string {
	return newContactTimedToken(signer.successKey, now)
}

func (signer *contactSigner) validSuccessToken(token string, now time.Time) bool {
	issued, valid := validateContactTimedToken(signer.successKey, token)
	if !valid {
		return false
	}
	age := now.Sub(issued)
	return age >= -contactTokenFutureSkew && age <= contactSuccessTokenMaxAge
}

func newContactTimedToken(key []byte, now time.Time) string {
	payload := strconv.FormatInt(now.Unix(), 10)
	mac := hmac.New(sha256.New, key)
	_, _ = mac.Write([]byte(payload))
	return payload + "." + rawURL.EncodeToString(mac.Sum(nil))
}

func validateContactTimedToken(key []byte, token string) (time.Time, bool) {
	payload, encodedSignature, found := strings.Cut(token, ".")
	if !found || payload == "" || encodedSignature == "" || strings.Contains(encodedSignature, ".") {
		return time.Time{}, false
	}
	signature, err := rawURL.DecodeString(encodedSignature)
	if err != nil {
		return time.Time{}, false
	}
	mac := hmac.New(sha256.New, key)
	_, _ = mac.Write([]byte(payload))
	if !hmac.Equal(signature, mac.Sum(nil)) {
		return time.Time{}, false
	}
	seconds, err := strconv.ParseInt(payload, 10, 64)
	if err != nil {
		return time.Time{}, false
	}
	return time.Unix(seconds, 0).UTC(), true
}

func (signer *contactSigner) clientKey(request *http.Request, trustedProxyHops int) (string, error) {
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
	mac := hmac.New(sha256.New, signer.addressKey)
	_, _ = mac.Write(masked.AsSlice())
	return rawURL.EncodeToString(mac.Sum(nil)[:16]), nil
}

type contactBucket struct {
	tokens     float64
	lastRefill time.Time
	lastSeen   time.Time
}

type contactRateLimiter struct {
	mu             sync.Mutex
	buckets        map[string]*contactBucket
	capacity       float64
	refillInterval time.Duration
	idleExpiry     time.Duration
	maxEntries     int
}

func newContactRateLimiter(capacity int, refillInterval, idleExpiry time.Duration, maxEntries int) *contactRateLimiter {
	return &contactRateLimiter{
		buckets:        make(map[string]*contactBucket),
		capacity:       float64(capacity),
		refillInterval: refillInterval,
		idleExpiry:     idleExpiry,
		maxEntries:     maxEntries,
	}
}

func (limiter *contactRateLimiter) allow(key string, now time.Time) bool {
	limiter.mu.Lock()
	defer limiter.mu.Unlock()

	for candidate, bucket := range limiter.buckets {
		if now.Sub(bucket.lastSeen) >= limiter.idleExpiry {
			delete(limiter.buckets, candidate)
		}
	}
	bucket, exists := limiter.buckets[key]
	if !exists {
		if limiter.maxEntries <= 0 || limiter.capacity <= 0 || limiter.refillInterval <= 0 {
			return false
		}
		if len(limiter.buckets) >= limiter.maxEntries {
			limiter.evictOldest()
		}
		bucket = &contactBucket{tokens: limiter.capacity, lastRefill: now, lastSeen: now}
		limiter.buckets[key] = bucket
	}
	if elapsed := now.Sub(bucket.lastRefill); elapsed > 0 {
		bucket.tokens = min(limiter.capacity, bucket.tokens+float64(elapsed)/float64(limiter.refillInterval))
		bucket.lastRefill = now
	}
	bucket.lastSeen = now
	if bucket.tokens < 1 {
		return false
	}
	bucket.tokens--
	return true
}

func (limiter *contactRateLimiter) evictOldest() {
	var oldestKey string
	var oldestTime time.Time
	for key, bucket := range limiter.buckets {
		if oldestKey == "" || bucket.lastSeen.Before(oldestTime) {
			oldestKey = key
			oldestTime = bucket.lastSeen
		}
	}
	delete(limiter.buckets, oldestKey)
}

func (limiter *contactRateLimiter) size() int {
	limiter.mu.Lock()
	defer limiter.mu.Unlock()
	return len(limiter.buckets)
}
