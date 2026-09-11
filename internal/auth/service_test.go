package auth

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"testing"
	"time"
)

type sessionRepositoryStub struct {
	sessions  map[string]Session
	createErr error
	revokeErr error
	created   []Session
	revoked   []string
}

func newSessionRepositoryStub() *sessionRepositoryStub {
	return &sessionRepositoryStub{sessions: make(map[string]Session)}
}

func (repository *sessionRepositoryStub) Create(_ context.Context, session Session) error {
	if repository.createErr != nil {
		return repository.createErr
	}
	if _, exists := repository.sessions[session.TokenHash]; exists {
		return ErrConflict
	}
	session.ETag = "created-etag"
	repository.sessions[session.TokenHash] = session
	repository.created = append(repository.created, session)
	return nil
}

func (repository *sessionRepositoryStub) Get(_ context.Context, tokenHash string) (Session, error) {
	session, ok := repository.sessions[tokenHash]
	if !ok {
		return Session{}, ErrNotFound
	}
	return session, nil
}

func (repository *sessionRepositoryStub) Revoke(_ context.Context, tokenHash, expectedETag string, at time.Time) error {
	if repository.revokeErr != nil {
		return repository.revokeErr
	}
	session, ok := repository.sessions[tokenHash]
	if !ok {
		return ErrNotFound
	}
	if session.ETag != expectedETag {
		return ErrConflict
	}
	session.RevokedAt = &at
	session.ETag = "revoked-etag"
	repository.sessions[tokenHash] = session
	repository.revoked = append(repository.revoked, tokenHash)
	return nil
}

func (*sessionRepositoryStub) DeleteExpired(context.Context, time.Time) (int, error) { return 0, nil }

func TestSessionServiceCreatesAndValidatesOpaqueSession(t *testing.T) {
	now := time.Date(2026, time.September, 11, 12, 0, 0, 0, time.UTC)
	repository := newSessionRepositoryStub()
	service, err := NewSessionService(repository, "admin", "credential-v1", bytes.NewReader(bytes.Repeat([]byte{0x31}, 32)), func() time.Time { return now })
	if err != nil {
		t.Fatal(err)
	}

	raw, created, err := service.Create(context.Background())
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if len(raw) != 43 {
		t.Fatalf("raw token length = %d, want 43", len(raw))
	}
	digest := sha256.Sum256([]byte(raw))
	if created.TokenHash != hex.EncodeToString(digest[:]) {
		t.Fatalf("stored token hash = %q, does not match raw token", created.TokenHash)
	}
	if created.TokenHash == raw {
		t.Fatal("raw token was persisted")
	}
	if created.Username != "admin" || created.CredentialVersion != "credential-v1" || !created.ExpiresAt.Equal(now.Add(8*time.Hour)) {
		t.Fatalf("unexpected session: %+v", created)
	}
	validated, err := service.Validate(context.Background(), raw)
	if err != nil || validated.TokenHash != created.TokenHash {
		t.Fatalf("Validate = (%+v, %v)", validated, err)
	}
}

func TestSessionServiceRejectsInvalidExpiredRevokedAndStaleCredentialSessions(t *testing.T) {
	now := time.Date(2026, time.September, 11, 12, 0, 0, 0, time.UTC)
	repository := newSessionRepositoryStub()
	service, err := NewSessionService(repository, "admin", "credential-v2", bytes.NewReader(nil), func() time.Time { return now })
	if err != nil {
		t.Fatal(err)
	}

	for name, testCase := range map[string]struct {
		raw     string
		session Session
	}{
		"expired": {
			raw: tokenForByte(0x10),
		},
		"revoked": {
			raw: tokenForByte(0x11),
		},
		"stale credentials": {
			raw: tokenForByte(0x12),
		},
	} {
		t.Run(name, func(t *testing.T) {
			created := now.Add(-time.Hour)
			version := "credential-v2"
			var revoked *time.Time
			if name == "expired" {
				created = now.Add(-8 * time.Hour)
			}
			if name == "revoked" {
				revoked = ptrTime(now.Add(-time.Minute))
			}
			if name == "stale credentials" {
				version = "credential-v1"
			}
			testCase.session = makeStoredSession(testCase.raw, created, revoked, version)
			repository.sessions[testCase.session.TokenHash] = testCase.session
			if _, err := service.Validate(context.Background(), testCase.raw); !errors.Is(err, ErrUnauthenticated) {
				t.Fatalf("Validate error = %v, want ErrUnauthenticated", err)
			}
		})
	}
	for _, raw := range []string{"", "not base64", "c2hvcnQ"} {
		if _, err := service.Validate(context.Background(), raw); !errors.Is(err, ErrUnauthenticated) {
			t.Fatalf("Validate(%q) error = %v, want ErrUnauthenticated", raw, err)
		}
	}
}

func TestSessionServiceRotateRevokesValidPriorSessionBeforeCreating(t *testing.T) {
	now := time.Date(2026, time.September, 11, 12, 0, 0, 0, time.UTC)
	oldRaw := tokenForByte(0x21)
	repository := newSessionRepositoryStub()
	old := makeStoredSession(oldRaw, now.Add(-time.Hour), nil, "credential-v1")
	old.ETag = "old-etag"
	repository.sessions[old.TokenHash] = old
	service, _ := NewSessionService(repository, "admin", "credential-v1", bytes.NewReader(bytes.Repeat([]byte{0x32}, 32)), func() time.Time { return now })

	newRaw, _, err := service.Rotate(context.Background(), oldRaw)
	if err != nil {
		t.Fatalf("Rotate: %v", err)
	}
	if newRaw == oldRaw || len(repository.revoked) != 1 || len(repository.created) != 1 {
		t.Fatalf("rotation did not revoke then create: new=%q revoked=%v created=%d", newRaw, repository.revoked, len(repository.created))
	}
}

func TestSessionServiceRotateDoesNotCreateAfterAmbiguousRevocation(t *testing.T) {
	now := time.Date(2026, time.September, 11, 12, 0, 0, 0, time.UTC)
	oldRaw := tokenForByte(0x21)
	repository := newSessionRepositoryStub()
	old := makeStoredSession(oldRaw, now.Add(-time.Hour), nil, "credential-v1")
	old.ETag = "old-etag"
	repository.sessions[old.TokenHash] = old
	repository.revokeErr = ErrCommitUnknown
	service, _ := NewSessionService(repository, "admin", "credential-v1", bytes.NewReader(bytes.Repeat([]byte{0x32}, 32)), func() time.Time { return now })

	if _, _, err := service.Rotate(context.Background(), oldRaw); !errors.Is(err, ErrCommitUnknown) {
		t.Fatalf("Rotate error = %v, want ErrCommitUnknown", err)
	}
	if len(repository.created) != 0 {
		t.Fatal("new session created despite ambiguous prior revocation")
	}
}

func TestSessionServiceLogoutIsIdempotent(t *testing.T) {
	now := time.Date(2026, time.September, 11, 12, 0, 0, 0, time.UTC)
	raw := tokenForByte(0x45)
	repository := newSessionRepositoryStub()
	session := makeStoredSession(raw, now.Add(-time.Hour), nil, "credential-v1")
	session.ETag = "etag"
	repository.sessions[session.TokenHash] = session
	service, _ := NewSessionService(repository, "admin", "credential-v1", bytes.NewReader(nil), func() time.Time { return now })

	if err := service.Logout(context.Background(), raw); err != nil {
		t.Fatalf("first Logout: %v", err)
	}
	if err := service.Logout(context.Background(), raw); err != nil {
		t.Fatalf("second Logout: %v", err)
	}
	if err := service.Logout(context.Background(), "malformed"); err != nil {
		t.Fatalf("malformed Logout: %v", err)
	}
	if len(repository.revoked) != 1 {
		t.Fatalf("revocations = %d, want 1", len(repository.revoked))
	}
}

func TestSessionServiceFailsClosedWhenRandomnessFails(t *testing.T) {
	service, err := NewSessionService(newSessionRepositoryStub(), "admin", "credential-v1", &failingReader{}, time.Now)
	if err != nil {
		t.Fatal(err)
	}
	if raw, _, err := service.Create(context.Background()); err == nil || raw != "" {
		t.Fatalf("Create = raw %q error %v", raw, err)
	}
}

func TestSessionServiceRetriesBoundedTokenCollisions(t *testing.T) {
	repository := newSessionRepositoryStub()
	service, _ := NewSessionService(repository, "admin", "credential-v1", bytes.NewReader(bytes.Repeat([]byte{0x62}, 4*32)), time.Now)
	raw, _, err := service.Create(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := service.Create(context.Background()); !errors.Is(err, ErrConflict) {
		t.Fatalf("second Create error = %v, want ErrConflict after bounded collisions", err)
	}
	if len(repository.created) != 1 || raw == "" {
		t.Fatalf("created sessions = %d raw=%q", len(repository.created), raw)
	}
}

func makeStoredSession(raw string, created time.Time, revoked *time.Time, version string) Session {
	digest := sha256.Sum256([]byte(raw))
	return Session{
		TokenHash:         hex.EncodeToString(digest[:]),
		Username:          "admin",
		CredentialVersion: version,
		CreatedAt:         created,
		ExpiresAt:         created.Add(8 * time.Hour),
		RevokedAt:         revoked,
	}
}

func tokenForByte(value byte) string {
	service, _ := NewSessionService(newSessionRepositoryStub(), "admin", "v", bytes.NewReader(bytes.Repeat([]byte{value}, 32)), time.Now)
	raw, _, _ := service.Create(context.Background())
	return raw
}

func ptrTime(value time.Time) *time.Time { return &value }

type failingReader struct{}

func (*failingReader) Read([]byte) (int, error) { return 0, io.ErrUnexpectedEOF }
