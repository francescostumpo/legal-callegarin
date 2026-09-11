package azure

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/francescostumpo/legal-callegarin/internal/auth"
)

const maxSessionScan = 1000

type SessionRepository struct{ table tableDriver }

func newSessionRepository(table tableDriver) *SessionRepository {
	return &SessionRepository{table: table}
}
func (repository *SessionRepository) Create(ctx context.Context, session auth.Session) error {
	encoded, err := marshalSessionEntity(session)
	if err != nil {
		return err
	}
	_, err = repository.table.Add(ctx, encoded)
	return mapSessionError(err)
}
func (repository *SessionRepository) Get(ctx context.Context, tokenHash string) (auth.Session, error) {
	entity, err := repository.table.Get(ctx, sessionsPartition, tokenHash)
	if err != nil {
		return auth.Session{}, mapSessionError(err)
	}
	return unmarshalSessionEntity(entity.Value, entity.ETag)
}
func (repository *SessionRepository) Revoke(ctx context.Context, tokenHash, expectedETag string, revokedAt time.Time) error {
	session, err := repository.Get(ctx, tokenHash)
	if err != nil {
		return err
	}
	if session.ETag != expectedETag {
		return auth.ErrConflict
	}
	session.RevokedAt = &revokedAt
	if err := session.Validate(); err != nil {
		return err
	}
	encoded, err := marshalSessionEntity(session)
	if err != nil {
		return err
	}
	_, err = repository.table.Update(ctx, encoded, expectedETag)
	return mapSessionError(err)
}
func (repository *SessionRepository) DeleteExpired(ctx context.Context, cutoff time.Time) (int, error) {
	entities, err := repository.table.List(ctx, "PartitionKey eq 'sessions'", maxSessionScan+1)
	if err != nil {
		return 0, mapSessionError(err)
	}
	if len(entities) > maxSessionScan {
		return 0, fmt.Errorf("%w: active session set exceeds %d", auth.ErrValidation, maxSessionScan)
	}
	deleted := 0
	for _, entity := range entities {
		var header entityHeader
		if err := decodeHeader(entity.Value, &header); err != nil {
			return deleted, err
		}
		if header.EntityType != sessionEntityType {
			continue
		}
		session, err := unmarshalSessionEntity(entity.Value, entity.ETag)
		if err != nil {
			return deleted, err
		}
		if session.ExpiresAt.After(cutoff) {
			continue
		}
		if err := repository.table.Delete(ctx, sessionsPartition, session.TokenHash, session.ETag); err != nil {
			return deleted, mapSessionError(err)
		}
		deleted++
	}
	return deleted, nil
}
func mapSessionError(err error) error {
	switch {
	case err == nil:
		return nil
	case errors.Is(err, ErrNotFound):
		return auth.ErrNotFound
	case errors.Is(err, ErrConflict), errors.Is(err, ErrPrecondition):
		return auth.ErrConflict
	default:
		return err
	}
}
