package contacts

import (
	"context"
	"fmt"
	"strings"
	"time"
)

type Submission struct {
	Name           string
	Email          string
	Phone          string
	Message        string
	ConsentVersion string
}

type Clock interface {
	Now() time.Time
}

type IDGenerator interface {
	NewID() string
}

type ContactService interface {
	Submit(context.Context, Submission) (Contact, error)
	Open(context.Context, string, string) (Contact, error)
	Archive(context.Context, string, string) (Contact, error)
	Restore(context.Context, string, string) (Contact, error)
	ScheduleDeletion(context.Context, string, string) (Contact, error)
	CancelDeletion(context.Context, string, string) (Contact, error)
	PurgeDue(context.Context, time.Time) (int, error)
}

type service struct {
	repository Repository
	clock      Clock
	ids        IDGenerator
}

func NewService(repository Repository, clock Clock, ids IDGenerator) ContactService {
	return &service{repository: repository, clock: clock, ids: ids}
}

func (service *service) Submit(ctx context.Context, submission Submission) (Contact, error) {
	now := service.clock.Now()
	contact := Contact{
		ID:                service.ids.NewID(),
		Name:              strings.TrimSpace(submission.Name),
		Email:             strings.TrimSpace(submission.Email),
		Phone:             strings.TrimSpace(submission.Phone),
		Message:           strings.TrimSpace(submission.Message),
		ConsentVersion:    strings.TrimSpace(submission.ConsentVersion),
		PrivacyAcceptedAt: now,
		State:             StateNew,
		CreatedAt:         now,
		UpdatedAt:         now,
		ReviewDueAt:       now.AddDate(2, 0, 0),
	}
	if err := contact.Validate(); err != nil {
		return Contact{}, err
	}
	return service.repository.Create(ctx, contact)
}

func (service *service) Open(ctx context.Context, id, expectedETag string) (Contact, error) {
	contact, err := service.repository.Get(ctx, id)
	if err != nil {
		return Contact{}, err
	}
	if contact.State != StateNew {
		return Contact{}, fmt.Errorf("%w: only a new contact can be opened", ErrInvalidTransition)
	}
	now := service.clock.Now()
	contact.State = StateRead
	contact.ReadAt = &now
	contact.UpdatedAt = now
	return service.repository.Update(ctx, contact, expectedETag)
}

func (service *service) Archive(ctx context.Context, id, expectedETag string) (Contact, error) {
	contact, err := service.repository.Get(ctx, id)
	if err != nil {
		return Contact{}, err
	}
	if contact.State != StateRead {
		return Contact{}, fmt.Errorf("%w: only a read contact can be archived", ErrInvalidTransition)
	}
	now := service.clock.Now()
	contact.State = StateArchived
	contact.ArchivedAt = &now
	contact.UpdatedAt = now
	return service.repository.Update(ctx, contact, expectedETag)
}

func (service *service) Restore(ctx context.Context, id, expectedETag string) (Contact, error) {
	contact, err := service.repository.Get(ctx, id)
	if err != nil {
		return Contact{}, err
	}
	if contact.State != StateArchived {
		return Contact{}, fmt.Errorf("%w: only an archived contact can be restored", ErrInvalidTransition)
	}
	contact.State = StateRead
	contact.ArchivedAt = nil
	contact.UpdatedAt = service.clock.Now()
	return service.repository.Update(ctx, contact, expectedETag)
}

func (service *service) ScheduleDeletion(ctx context.Context, id, expectedETag string) (Contact, error) {
	contact, err := service.repository.Get(ctx, id)
	if err != nil {
		return Contact{}, err
	}
	now := service.clock.Now()
	due := now.AddDate(0, 0, 30)
	contact.DeletionDueAt = &due
	contact.UpdatedAt = now
	return service.repository.Update(ctx, contact, expectedETag)
}

func (service *service) CancelDeletion(ctx context.Context, id, expectedETag string) (Contact, error) {
	contact, err := service.repository.Get(ctx, id)
	if err != nil {
		return Contact{}, err
	}
	contact.DeletionDueAt = nil
	contact.UpdatedAt = service.clock.Now()
	return service.repository.Update(ctx, contact, expectedETag)
}

func (service *service) PurgeDue(ctx context.Context, cutoff time.Time) (int, error) {
	if cutoff.IsZero() {
		return 0, fmt.Errorf("%w: purge cutoff is required", ErrValidation)
	}
	var candidates []Contact
	cursor := ""
	for {
		page, err := service.repository.List(ctx, ListOptions{Cursor: cursor, Limit: 100})
		if err != nil {
			return 0, err
		}
		for _, contact := range page.Items {
			if contact.DeletionDueAt != nil && !contact.DeletionDueAt.After(cutoff) {
				candidates = append(candidates, contact)
			}
		}
		if page.NextCursor == "" {
			break
		}
		cursor = page.NextCursor
	}

	deleted := 0
	for _, contact := range candidates {
		if err := service.repository.Delete(ctx, contact.ID, contact.ETag); err != nil {
			return deleted, err
		}
		deleted++
	}
	return deleted, nil
}
