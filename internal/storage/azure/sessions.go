package azure

import (
	"context"
	"errors"
	"time"

	"github.com/francescostumpo/legal-callegarin/internal/auth"
)

const sessionCleanupPageSize = 100

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
	if mutationOutcomeMayBeUnknown(err) {
		observed, getErr := repository.Get(ctx, session.TokenHash)
		switch {
		case getErr == nil && sameSessionState(observed, session):
			return nil
		case errors.Is(getErr, auth.ErrNotFound):
			return mapSessionError(err)
		default:
			return unknownCommitForContext(ctx, auth.ErrCommitUnknown, err)
		}
	}
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
	prior := session
	session.RevokedAt = &revokedAt
	if err := session.Validate(); err != nil {
		return err
	}
	encoded, err := marshalSessionEntity(session)
	if err != nil {
		return err
	}
	_, err = repository.table.Update(ctx, encoded, expectedETag)
	if mutationOutcomeMayBeUnknown(err) {
		observed, getErr := repository.Get(ctx, tokenHash)
		switch {
		case getErr == nil && sameSessionState(observed, session):
			return nil
		case getErr == nil && sameSessionState(observed, prior):
			return mapSessionError(err)
		default:
			return unknownCommitForContext(ctx, auth.ErrCommitUnknown, err)
		}
	}
	return mapSessionError(err)
}
func (repository *SessionRepository) DeleteExpired(ctx context.Context, cutoff time.Time) (int, error) {
	deleted := 0
	var continuation *tableContinuation
	for {
		entities, next, err := repository.table.ListPage(ctx, "PartitionKey eq 'sessions'", sessionCleanupPageSize, continuation)
		if err != nil {
			return deleted, mapSessionError(err)
		}
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
			err = repository.table.Delete(ctx, sessionsPartition, session.TokenHash, session.ETag)
			if mutationOutcomeMayBeUnknown(err) {
				observed, getErr := repository.Get(ctx, session.TokenHash)
				switch {
				case errors.Is(getErr, auth.ErrNotFound):
					err = nil
				case getErr == nil && sameSessionState(observed, session):
					err = mapSessionError(err)
				default:
					err = unknownCommitForContext(ctx, auth.ErrCommitUnknown, err)
				}
			}
			if err != nil {
				return deleted, mapSessionError(err)
			}
			deleted++
		}
		if next == nil {
			return deleted, nil
		}
		if continuation != nil && *continuation == *next {
			return deleted, unknownCommit(auth.ErrCommitUnknown, errors.New("session cleanup continuation did not advance"))
		}
		continuation = next
	}
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
