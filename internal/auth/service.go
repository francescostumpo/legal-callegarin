package auth

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"
)

var ErrUnauthenticated = errors.New("authentication required")

const (
	sessionLifetime       = 8 * time.Hour
	sessionTokenByteCount = 32
	maxTokenCollisions    = 3
	maxRevokeAttempts     = 2
)

type SessionService struct {
	repository        SessionRepository
	username          string
	credentialVersion string
	random            io.Reader
	now               func() time.Time
}

func NewSessionService(repository SessionRepository, username, credentialVersion string, random io.Reader, now func() time.Time) (*SessionService, error) {
	if repository == nil || random == nil || now == nil || strings.TrimSpace(username) == "" || strings.TrimSpace(credentialVersion) == "" {
		return nil, fmt.Errorf("%w: session service dependencies are required", ErrValidation)
	}
	return &SessionService{
		repository:        repository,
		username:          username,
		credentialVersion: credentialVersion,
		random:            random,
		now:               now,
	}, nil
}

// Create returns the only copy of the raw bearer token. Repositories receive
// only its SHA-256 digest.
func (service *SessionService) Create(ctx context.Context) (string, Session, error) {
	for range maxTokenCollisions {
		rawBytes := make([]byte, sessionTokenByteCount)
		if _, err := io.ReadFull(service.random, rawBytes); err != nil {
			return "", Session{}, fmt.Errorf("create session: obtain token: %w", err)
		}
		raw := base64.RawURLEncoding.EncodeToString(rawBytes)
		createdAt := service.now().UTC()
		session := Session{
			TokenHash:         hashSessionToken(raw),
			Username:          service.username,
			CredentialVersion: service.credentialVersion,
			CreatedAt:         createdAt,
			ExpiresAt:         createdAt.Add(sessionLifetime),
		}
		if err := service.repository.Create(ctx, session); err != nil {
			if errors.Is(err, ErrConflict) {
				continue
			}
			return "", Session{}, err
		}
		return raw, session, nil
	}
	return "", Session{}, ErrConflict
}

func (service *SessionService) Validate(ctx context.Context, raw string) (Session, error) {
	if !validRawSessionToken(raw) {
		return Session{}, ErrUnauthenticated
	}
	session, err := service.repository.Get(ctx, hashSessionToken(raw))
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			return Session{}, ErrUnauthenticated
		}
		return Session{}, err
	}
	now := service.now().UTC()
	if session.RevokedAt != nil || !now.Before(session.ExpiresAt) || session.Username != service.username || session.CredentialVersion != service.credentialVersion {
		return Session{}, ErrUnauthenticated
	}
	return session, nil
}

// Rotate revokes a currently valid prior session before creating its
// replacement. An ambiguous revoke is returned to the caller and no new token
// is created, preventing the service from knowingly leaving two valid tokens.
func (service *SessionService) Rotate(ctx context.Context, priorRaw string) (string, Session, error) {
	if priorRaw != "" {
		prior, err := service.Validate(ctx, priorRaw)
		switch {
		case err == nil:
			if err := service.revoke(ctx, prior); err != nil {
				return "", Session{}, err
			}
		case errors.Is(err, ErrUnauthenticated):
			// An invalid, expired, or stale cookie does not block a fresh login.
		default:
			return "", Session{}, err
		}
	}
	return service.Create(ctx)
}

// Logout is idempotent for absent, malformed, expired, and already-revoked
// sessions. Persistence errors are returned so handlers can fail closed.
func (service *SessionService) Logout(ctx context.Context, raw string) error {
	if !validRawSessionToken(raw) {
		return nil
	}
	session, err := service.repository.Get(ctx, hashSessionToken(raw))
	if errors.Is(err, ErrNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	if session.RevokedAt != nil || !service.now().UTC().Before(session.ExpiresAt) {
		return nil
	}
	return service.revoke(ctx, session)
}

func (service *SessionService) revoke(ctx context.Context, session Session) error {
	for range maxRevokeAttempts {
		err := service.repository.Revoke(ctx, session.TokenHash, session.ETag, service.now().UTC())
		if err == nil {
			return nil
		}
		if !errors.Is(err, ErrConflict) {
			return err
		}
		observed, getErr := service.repository.Get(ctx, session.TokenHash)
		if errors.Is(getErr, ErrNotFound) {
			return nil
		}
		if getErr != nil {
			return getErr
		}
		if observed.RevokedAt != nil {
			return nil
		}
		session = observed
	}
	return ErrConflict
}

func hashSessionToken(raw string) string {
	digest := sha256.Sum256([]byte(raw))
	return hex.EncodeToString(digest[:])
}

func validRawSessionToken(raw string) bool {
	if len(raw) != base64.RawURLEncoding.EncodedLen(sessionTokenByteCount) {
		return false
	}
	decoded, err := base64.RawURLEncoding.Strict().DecodeString(raw)
	return err == nil && len(decoded) == sessionTokenByteCount && base64.RawURLEncoding.EncodeToString(decoded) == raw
}
