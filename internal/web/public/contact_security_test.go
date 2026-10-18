package public

import (
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestContactFormTokenValidationWindowsAndSignature(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 9, 11, 12, 0, 0, 0, time.UTC)
	signer, err := newContactSigner([]byte("0123456789abcdef0123456789abcdef"))
	if err != nil {
		t.Fatalf("newContactSigner() error = %v", err)
	}

	tests := []struct {
		name      string
		tokenTime time.Time
		checkTime time.Time
		mutate    func(string) string
		wantValid bool
	}{
		{name: "minimum age reached", tokenTime: now, checkTime: now.Add(contactMinimumCompletionTime), wantValid: true},
		{name: "too fast", tokenTime: now, checkTime: now.Add(contactMinimumCompletionTime - time.Millisecond)},
		{name: "maximum age reached", tokenTime: now, checkTime: now.Add(contactFormTokenMaxAge), wantValid: true},
		{name: "expired", tokenTime: now, checkTime: now.Add(contactFormTokenMaxAge + time.Second)},
		{name: "future within skew remains too fast", tokenTime: now.Add(contactTokenFutureSkew), checkTime: now},
		{name: "future beyond skew", tokenTime: now.Add(contactTokenFutureSkew + time.Second), checkTime: now},
		{name: "bad signature", tokenTime: now, checkTime: now.Add(contactMinimumCompletionTime), mutate: mutateLastTokenByte},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			token := signer.newFormToken(tt.tokenTime)
			if tt.mutate != nil {
				token = tt.mutate(token)
			}
			if got := signer.validFormToken(token, tt.checkTime); got != tt.wantValid {
				t.Fatalf("validFormToken() = %t, want %t", got, tt.wantValid)
			}
		})
	}

	if signer.validFormToken("malformed", now) {
		t.Fatal("validFormToken(malformed) = true")
	}
}

func TestContactSuccessTokenIsShortLivedAndDomainSeparated(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 9, 11, 12, 0, 0, 0, time.UTC)
	signer, err := newContactSigner([]byte("0123456789abcdef0123456789abcdef"))
	if err != nil {
		t.Fatalf("newContactSigner() error = %v", err)
	}
	success := signer.newSuccessToken(now)
	if !signer.validSuccessToken(success, now.Add(contactSuccessTokenMaxAge)) {
		t.Fatal("success token invalid at maximum age")
	}
	if signer.validSuccessToken(success, now.Add(contactSuccessTokenMaxAge+time.Second)) {
		t.Fatal("expired success token is valid")
	}
	if signer.validSuccessToken(signer.newFormToken(now), now) {
		t.Fatal("form token validates as success token")
	}
	if signer.validFormToken(success, now.Add(contactMinimumCompletionTime)) {
		t.Fatal("success token validates as form token")
	}
	if len(success) > 80 {
		t.Fatalf("success token length = %d, want <= 80", len(success))
	}
}

func TestContactSignerRejectsShortKey(t *testing.T) {
	t.Parallel()

	if _, err := newContactSigner([]byte("short")); err == nil {
		t.Fatal("newContactSigner(short key) error = nil")
	}
}

func TestContactClientKeyUsesForwardedHeadersOnlyForTrustedProxy(t *testing.T) {
	t.Parallel()

	signer, err := newContactSigner([]byte("0123456789abcdef0123456789abcdef"))
	if err != nil {
		t.Fatalf("newContactSigner() error = %v", err)
	}
	request := httptest.NewRequest("POST", "https://studio.example.test/contatti", nil)
	request.RemoteAddr = "192.0.2.44:1234"
	request.Header.Set("X-Forwarded-For", "203.0.113.20")

	direct, err := signer.clientKey(request, 0)
	if err != nil {
		t.Fatalf("clientKey(untrusted) error = %v", err)
	}
	forwarded, err := signer.clientKey(request, 1)
	if err != nil {
		t.Fatalf("clientKey(trusted) error = %v", err)
	}
	if direct == forwarded {
		t.Fatal("trusted Forwarded address did not change client key")
	}
	for _, raw := range []string{"192.0.2.44", "203.0.113.20"} {
		if strings.Contains(direct, raw) || strings.Contains(forwarded, raw) {
			t.Fatalf("client key contains raw address fragment %q", raw)
		}
	}

}

func TestContactClientKeyFailsClosedForMalformedTrustedForwarding(t *testing.T) {
	t.Parallel()

	signer, err := newContactSigner([]byte("0123456789abcdef0123456789abcdef"))
	if err != nil {
		t.Fatalf("newContactSigner() error = %v", err)
	}
	request := httptest.NewRequest("POST", "https://studio.example.test/contatti", nil)
	request.RemoteAddr = "192.0.2.44:1234"
	request.Header.Set("X-Forwarded-For", "not-an-ip")
	trusted, err := signer.clientKey(request, 1)
	if err != nil {
		t.Fatalf("clientKey(malformed trusted header) error = %v", err)
	}
	direct, err := signer.clientKey(request, 0)
	if err != nil {
		t.Fatalf("clientKey(untrusted malformed header) error = %v", err)
	}
	if trusted != direct {
		t.Fatalf("malformed header did not fall back to RemoteAddr: trusted=%q direct=%q", trusted, direct)
	}
}

func TestContactRateLimiterExhaustionRecoveryIdleExpiryAndBound(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 9, 11, 12, 0, 0, 0, time.UTC)
	limiter := newContactRateLimiter(2, time.Minute, 10*time.Minute, 3)
	if !limiter.allow("client-a", now) {
		t.Fatal("first bucket token denied")
	}
	if !limiter.allow("client-a", now) {
		t.Fatal("second bucket token denied")
	}
	if limiter.allow("client-a", now) {
		t.Fatal("exhausted bucket allowed request")
	}
	if !limiter.allow("client-a", now.Add(time.Minute)) {
		t.Fatal("refilled bucket denied request")
	}

	limiter.allow("client-b", now)
	limiter.allow("client-c", now)
	limiter.allow("client-d", now)
	if got := limiter.size(); got != 3 {
		t.Fatalf("limiter size = %d, want bounded to 3", got)
	}
	limiter.allow("client-e", now.Add(11*time.Minute))
	if got := limiter.size(); got != 1 {
		t.Fatalf("limiter size after idle expiry = %d, want 1", got)
	}
}

func mutateLastTokenByte(token string) string {
	last := token[len(token)-1]
	if last == 'A' {
		last = 'B'
	} else {
		last = 'A'
	}
	return token[:len(token)-1] + string(last)
}
