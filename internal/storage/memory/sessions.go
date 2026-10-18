package memory

import (
	"context"
	"encoding/base64"
	"strconv"
	"sync"
	"time"

	"github.com/francescostumpo/legal-callegarin/internal/auth"
)

type SessionRepository struct {
	mu       sync.RWMutex
	sessions map[string]auth.Session
	nextETag uint64
}

func NewSessionRepository() *SessionRepository {
	return &SessionRepository{sessions: make(map[string]auth.Session)}
}

func (repository *SessionRepository) Create(ctx context.Context, session auth.Session) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := session.Validate(); err != nil {
		return err
	}
	repository.mu.Lock()
	defer repository.mu.Unlock()
	if _, exists := repository.sessions[session.TokenHash]; exists {
		return auth.ErrConflict
	}
	session.ETag = repository.newETag()
	repository.sessions[session.TokenHash] = cloneSession(session)
	return nil
}

func (repository *SessionRepository) Get(ctx context.Context, tokenHash string) (auth.Session, error) {
	if err := ctx.Err(); err != nil {
		return auth.Session{}, err
	}
	repository.mu.RLock()
	defer repository.mu.RUnlock()
	session, exists := repository.sessions[tokenHash]
	if !exists {
		return auth.Session{}, auth.ErrNotFound
	}
	return cloneSession(session), nil
}

func (repository *SessionRepository) Revoke(ctx context.Context, tokenHash, expectedETag string, revokedAt time.Time) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	repository.mu.Lock()
	defer repository.mu.Unlock()
	session, exists := repository.sessions[tokenHash]
	if !exists {
		return auth.ErrNotFound
	}
	if session.ETag != expectedETag {
		return auth.ErrConflict
	}
	session.RevokedAt = &revokedAt
	if err := session.Validate(); err != nil {
		return err
	}
	session.ETag = repository.newETag()
	repository.sessions[tokenHash] = cloneSession(session)
	return nil
}

func (repository *SessionRepository) DeleteExpired(ctx context.Context, cutoff time.Time) (int, error) {
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	repository.mu.Lock()
	defer repository.mu.Unlock()
	deleted := 0
	for tokenHash, session := range repository.sessions {
		if !session.ExpiresAt.After(cutoff) {
			delete(repository.sessions, tokenHash)
			deleted++
		}
	}
	return deleted, nil
}

func (repository *SessionRepository) newETag() string {
	repository.nextETag++
	return base64.RawURLEncoding.EncodeToString([]byte(strconv.FormatUint(repository.nextETag, 10)))
}

func cloneSession(session auth.Session) auth.Session {
	session.RevokedAt = cloneTime(session.RevokedAt)
	return session
}
