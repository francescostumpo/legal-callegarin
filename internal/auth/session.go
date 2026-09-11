package auth

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"
)

var (
	ErrNotFound   = errors.New("session not found")
	ErrConflict   = errors.New("session conflict")
	ErrValidation = errors.New("session validation failed")
)

type Session struct {
	TokenHash         string
	Username          string
	CredentialVersion string
	CreatedAt         time.Time
	ExpiresAt         time.Time
	RevokedAt         *time.Time
	ETag              string
}

type SessionRepository interface {
	Create(context.Context, Session) error
	Get(context.Context, string) (Session, error)
	Revoke(context.Context, string, string, time.Time) error
	DeleteExpired(context.Context, time.Time) (int, error)
}

func (session Session) Validate() error {
	if !validSessionTokenHash(session.TokenHash) || strings.TrimSpace(session.Username) == "" || strings.TrimSpace(session.CredentialVersion) == "" {
		return fmt.Errorf("%w: token hash, username, and credential version are required", ErrValidation)
	}
	if session.CreatedAt.IsZero() || !session.ExpiresAt.Equal(session.CreatedAt.Add(8*time.Hour)) {
		return fmt.Errorf("%w: session expiry must be exactly 8 hours after creation", ErrValidation)
	}
	if session.RevokedAt != nil && session.RevokedAt.Before(session.CreatedAt) {
		return fmt.Errorf("%w: revocation cannot precede creation", ErrValidation)
	}
	return nil
}

func validSessionTokenHash(value string) bool {
	if len(value) != sha256.Size*2 || value != strings.ToLower(value) {
		return false
	}
	decoded, err := hex.DecodeString(value)
	return err == nil && len(decoded) == sha256.Size
}
