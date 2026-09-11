package contracttest

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/francescostumpo/legal-callegarin/internal/auth"
)

func SessionRepository(t *testing.T, factory func() auth.SessionRepository) {
	t.Helper()

	repository := factory()
	ctx := context.Background()
	created := time.Date(2026, 9, 11, 10, 0, 0, 0, time.UTC)
	first := sessionFixture("first-token", created)
	if err := repository.Create(ctx, first); err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	if err := repository.Create(ctx, first); !errors.Is(err, auth.ErrConflict) {
		t.Fatalf("duplicate Create() error = %v, want ErrConflict", err)
	}

	stored, err := repository.Get(ctx, first.TokenHash)
	if err != nil || stored.ETag == "" {
		t.Fatalf("Get() = %#v, %v", stored, err)
	}
	revokedAt := created.Add(time.Hour)
	if err := repository.Revoke(ctx, first.TokenHash, "stale", revokedAt); !errors.Is(err, auth.ErrConflict) {
		t.Fatalf("stale Revoke() error = %v, want ErrConflict", err)
	}
	if err := repository.Revoke(ctx, first.TokenHash, stored.ETag, revokedAt); err != nil {
		t.Fatalf("Revoke() error = %v", err)
	}
	revoked, err := repository.Get(ctx, first.TokenHash)
	if err != nil || revoked.RevokedAt == nil || !revoked.RevokedAt.Equal(revokedAt) || revoked.ETag == stored.ETag {
		t.Fatalf("Get(revoked) = %#v, %v", revoked, err)
	}

	second := sessionFixture("second-token", created.Add(2*time.Hour))
	if err := repository.Create(ctx, second); err != nil {
		t.Fatalf("Create(second) error = %v", err)
	}
	deleted, err := repository.DeleteExpired(ctx, first.ExpiresAt)
	if err != nil || deleted != 1 {
		t.Fatalf("DeleteExpired() = %d, %v", deleted, err)
	}
	if _, err := repository.Get(ctx, first.TokenHash); !errors.Is(err, auth.ErrNotFound) {
		t.Fatalf("Get(expired) error = %v, want ErrNotFound", err)
	}
	if _, err := repository.Get(ctx, second.TokenHash); err != nil {
		t.Fatalf("Get(unexpired) error = %v", err)
	}
}

func sessionFixture(tokenHash string, created time.Time) auth.Session {
	return auth.Session{
		TokenHash:         tokenHash,
		Username:          "admin",
		CredentialVersion: "credential-v1",
		CreatedAt:         created,
		ExpiresAt:         created.Add(8 * time.Hour),
	}
}
