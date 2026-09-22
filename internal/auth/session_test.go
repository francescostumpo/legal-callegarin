package auth

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"strings"
	"testing"
	"time"
)

func TestSessionValidate(t *testing.T) {
	t.Parallel()

	created := time.Date(2026, time.September, 11, 10, 0, 0, 0, time.UTC)
	digest := sha256.Sum256([]byte("raw-token"))
	valid := Session{
		TokenHash:         hex.EncodeToString(digest[:]),
		Username:          "admin",
		CredentialVersion: "credential-v1",
		CreatedAt:         created,
		ExpiresAt:         created.Add(8 * time.Hour),
	}
	if err := valid.Validate(); err != nil {
		t.Fatalf("valid Session.Validate() error = %v", err)
	}

	tests := []struct {
		name   string
		mutate func(*Session)
	}{
		{name: "missing token hash", mutate: func(session *Session) { session.TokenHash = "" }},
		{name: "raw token instead of hash", mutate: func(session *Session) { session.TokenHash = "raw-token" }},
		{name: "uppercase hash", mutate: func(session *Session) { session.TokenHash = strings.ToUpper(session.TokenHash) }},
		{name: "missing username", mutate: func(session *Session) { session.Username = "" }},
		{name: "missing credential version", mutate: func(session *Session) { session.CredentialVersion = "" }},
		{name: "missing created timestamp", mutate: func(session *Session) { session.CreatedAt = time.Time{} }},
		{name: "wrong absolute expiry", mutate: func(session *Session) { session.ExpiresAt = session.ExpiresAt.Add(time.Second) }},
		{name: "revoked before creation", mutate: func(session *Session) { revoked := session.CreatedAt.Add(-time.Second); session.RevokedAt = &revoked }},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			session := valid
			tt.mutate(&session)
			if err := session.Validate(); !errors.Is(err, ErrValidation) {
				t.Fatalf("Session.Validate() error = %v, want ErrValidation", err)
			}
		})
	}
}
