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
	sessions         map[string]Session
	createErr        error
	createErrors     []error
	revokeErr        error
	deleteExpiredErr error
	created          []Session
	createAttempts   []Session
	revoked          []string
	deleteExpiredAt  []time.Time
	events           *[]string
}

func newSessionRepositoryStub() *sessionRepositoryStub {
	return &sessionRepositoryStub{sessions: make(map[string]Session)}
}

func (repository *sessionRepositoryStub) Create(_ context.Context, session Session) error {
	repository.createAttempts = append(repository.createAttempts, session)
	repository.record("create")
	if len(repository.createErrors) > 0 {
		err := repository.createErrors[0]
		repository.createErrors = repository.createErrors[1:]
		if err != nil {
			return err
		}
	}
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
	repository.record("revoke")
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

func (repository *sessionRepositoryStub) DeleteExpired(_ context.Context, before time.Time) (int, error) {
	repository.deleteExpiredAt = append(repository.deleteExpiredAt, before)
	repository.record("cleanup")
	return 0, repository.deleteExpiredErr
}

func (repository *sessionRepositoryStub) record(event string) {
	if repository.events != nil {
		*repository.events = append(*repository.events, event)
	}
}

func TestSessionServiceCreatesAndValidatesOpaqueSession(t *testing.T) {
	clockValue := time.Date(2026, time.September, 11, 14, 0, 0, 0, time.FixedZone("UTC+2", 2*60*60))
	now := clockValue.UTC()
	clockCalls := 0
	events := make([]string, 0, 3)
	repository := newSessionRepositoryStub()
	repository.events = &events
	random := &recordingReader{
		reader: bytes.NewReader(bytes.Repeat([]byte{0x31}, 32)),
		events: &events,
	}
	service, err := NewSessionService(repository, "admin", "credential-v1", random, func() time.Time {
		clockCalls++
		return clockValue
	})
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
	if clockCalls != 1 {
		t.Fatalf("clock calls = %d, want 1", clockCalls)
	}
	if len(repository.deleteExpiredAt) != 1 || !repository.deleteExpiredAt[0].Equal(now) || repository.deleteExpiredAt[0].Location() != time.UTC {
		t.Fatalf("DeleteExpired calls = %v, want one call at UTC %v", repository.deleteExpiredAt, now)
	}
	if !created.CreatedAt.Equal(now) || created.CreatedAt.Location() != time.UTC {
		t.Fatalf("CreatedAt = %v, want captured UTC %v", created.CreatedAt, now)
	}
	if want := []string{"cleanup", "random", "create"}; !equalStrings(events, want) {
		t.Fatalf("events = %v, want %v", events, want)
	}
	validated, err := service.Validate(context.Background(), raw)
	if err != nil || validated.TokenHash != created.TokenHash {
		t.Fatalf("Validate = (%+v, %v)", validated, err)
	}
}

func TestSessionServiceCreateFailsClosedWhenCleanupFails(t *testing.T) {
	now := time.Date(2026, time.September, 11, 12, 0, 0, 0, time.UTC)
	cleanupErr := errors.New("cleanup failed")
	events := make([]string, 0, 1)
	repository := newSessionRepositoryStub()
	repository.deleteExpiredErr = cleanupErr
	repository.events = &events
	random := &recordingReader{
		reader: bytes.NewReader(bytes.Repeat([]byte{0x31}, 32)),
		events: &events,
	}
	service, err := NewSessionService(repository, "admin", "credential-v1", random, func() time.Time { return now })
	if err != nil {
		t.Fatal(err)
	}

	raw, session, err := service.Create(context.Background())
	if err != cleanupErr || !errors.Is(err, cleanupErr) {
		t.Fatalf("Create error = %v, want cleanup error identity", err)
	}
	if raw != "" || session != (Session{}) {
		t.Fatalf("Create = (%q, %+v), want empty values", raw, session)
	}
	if random.reads != 0 || len(repository.createAttempts) != 0 {
		t.Fatalf("random reads = %d, create attempts = %d, want zero", random.reads, len(repository.createAttempts))
	}
	if want := []string{"cleanup"}; !equalStrings(events, want) {
		t.Fatalf("events = %v, want %v", events, want)
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
	events := make([]string, 0, 4)
	repository.events = &events
	old := makeStoredSession(oldRaw, now.Add(-time.Hour), nil, "credential-v1")
	old.ETag = "old-etag"
	repository.sessions[old.TokenHash] = old
	service, _ := NewSessionService(repository, "admin", "credential-v1", &recordingReader{
		reader: bytes.NewReader(bytes.Repeat([]byte{0x32}, 32)),
		events: &events,
	}, func() time.Time { return now })

	newRaw, _, err := service.Rotate(context.Background(), oldRaw)
	if err != nil {
		t.Fatalf("Rotate: %v", err)
	}
	if newRaw == oldRaw || len(repository.revoked) != 1 || len(repository.created) != 1 {
		t.Fatalf("rotation did not revoke then create: new=%q revoked=%v created=%d", newRaw, repository.revoked, len(repository.created))
	}
	if want := []string{"revoke", "cleanup", "random", "create"}; !equalStrings(events, want) {
		t.Fatalf("events = %v, want %v", events, want)
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
	if len(repository.deleteExpiredAt) != 0 {
		t.Fatal("cleanup attempted despite ambiguous prior revocation")
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

func TestSessionServiceCollisionRetryUsesSingleCleanupAndCapturedTime(t *testing.T) {
	firstNow := time.Date(2026, time.September, 11, 12, 0, 0, 0, time.UTC)
	clockCalls := 0
	events := make([]string, 0, 5)
	repository := newSessionRepositoryStub()
	repository.createErrors = []error{ErrConflict}
	repository.events = &events
	random := &recordingReader{
		reader: bytes.NewReader(append(bytes.Repeat([]byte{0x62}, 32), bytes.Repeat([]byte{0x63}, 32)...)),
		events: &events,
	}
	service, _ := NewSessionService(repository, "admin", "credential-v1", random, func() time.Time {
		value := firstNow.Add(time.Duration(clockCalls) * time.Hour)
		clockCalls++
		return value
	})
	raw, _, err := service.Create(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if raw == "" || len(repository.createAttempts) != 2 || len(repository.created) != 1 {
		t.Fatalf("Create = raw %q attempts %d created %d", raw, len(repository.createAttempts), len(repository.created))
	}
	if clockCalls != 1 || len(repository.deleteExpiredAt) != 1 {
		t.Fatalf("clock calls = %d, cleanup calls = %d, want 1 each", clockCalls, len(repository.deleteExpiredAt))
	}
	for index, attempt := range repository.createAttempts {
		if !attempt.CreatedAt.Equal(firstNow) || !attempt.ExpiresAt.Equal(firstNow.Add(8*time.Hour)) {
			t.Fatalf("attempt %d timestamps = (%v, %v), want (%v, %v)", index, attempt.CreatedAt, attempt.ExpiresAt, firstNow, firstNow.Add(8*time.Hour))
		}
	}
	if want := []string{"cleanup", "random", "create", "random", "create"}; !equalStrings(events, want) {
		t.Fatalf("events = %v, want %v", events, want)
	}
}

func TestSessionServiceStopsAfterMaximumTokenCollisions(t *testing.T) {
	repository := newSessionRepositoryStub()
	random := bytes.NewReader(bytes.Repeat([]byte{0x62}, (maxTokenCollisions+1)*sessionTokenByteCount))
	service, _ := NewSessionService(repository, "admin", "credential-v1", random, time.Now)

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
	wantAttempts := 1 + maxTokenCollisions
	if len(repository.createAttempts) != wantAttempts || random.Len() != 0 {
		t.Fatalf("create attempts = %d, unread random bytes = %d, want %d attempts and no extra read", len(repository.createAttempts), random.Len(), wantAttempts)
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

type recordingReader struct {
	reader io.Reader
	events *[]string
	reads  int
}

func (reader *recordingReader) Read(buffer []byte) (int, error) {
	reader.reads++
	if reader.events != nil {
		*reader.events = append(*reader.events, "random")
	}
	return reader.reader.Read(buffer)
}

func equalStrings(left, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}
