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
	Get(context.Context, string) (Contact, error)
	List(context.Context, ListOptions) (ContactPage, error)
	Dashboard(context.Context) (DashboardSummary, error)
	Open(context.Context, string, string) (Contact, error)
	Archive(context.Context, string, string) (Contact, error)
	Restore(context.Context, string, string) (Contact, error)
	ScheduleDeletion(context.Context, string, string) (Contact, error)
	CancelDeletion(context.Context, string, string) (Contact, error)
	PurgeDue(context.Context, time.Time) (int, error)
}

type DashboardSummary struct {
	New               int
	Read              int
	Archived          int
	DeletionScheduled int
	RetentionReview   int
	Purged            int
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

func (service *service) Get(ctx context.Context, id string) (Contact, error) {
	return service.repository.Get(ctx, id)
}

func (service *service) List(ctx context.Context, options ListOptions) (ContactPage, error) {
	if err := options.Validate(); err != nil {
		return ContactPage{}, err
	}
	return service.repository.List(ctx, options)
}

func (service *service) Dashboard(ctx context.Context) (DashboardSummary, error) {
	now := service.clock.Now()
	purged, err := service.PurgeDue(ctx, now)
	if err != nil {
		return DashboardSummary{}, err
	}
	summary := DashboardSummary{Purged: purged}
	err = service.scan(ctx, func(contact Contact) {
		switch contact.State {
		case StateNew:
			summary.New++
		case StateRead:
			summary.Read++
		case StateArchived:
			summary.Archived++
		}
		if contact.DeletionDueAt != nil {
			summary.DeletionScheduled++
		}
		if !contact.ReviewDueAt.After(now) {
			summary.RetentionReview++
		}
	})
	if err != nil {
		return DashboardSummary{}, err
	}
	return summary, nil
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
	if contact.DeletionDueAt != nil {
		return Contact{}, fmt.Errorf("%w: deletion is already scheduled", ErrInvalidTransition)
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
	if contact.DeletionDueAt == nil {
		return Contact{}, fmt.Errorf("%w: deletion is not scheduled", ErrInvalidTransition)
	}
	contact.DeletionDueAt = nil
	contact.UpdatedAt = service.clock.Now()
	return service.repository.Update(ctx, contact, expectedETag)
}

func (service *service) PurgeDue(ctx context.Context, cutoff time.Time) (int, error) {
	if cutoff.IsZero() {
		return 0, fmt.Errorf("%w: purge cutoff is required", ErrValidation)
	}
	candidates := make([]Contact, 0)
	if err := service.scan(ctx, func(contact Contact) {
		if contact.DeletionDueAt != nil && !contact.DeletionDueAt.After(cutoff) {
			candidates = append(candidates, contact)
		}
	}); err != nil {
		return 0, err
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

func (service *service) scan(ctx context.Context, visit func(Contact)) error {
	cursor := ""
	scanned := 0
	for {
		page, err := service.repository.List(ctx, ListOptions{Cursor: cursor, Limit: 100})
		if err != nil {
			return err
		}
		if scanned+len(page.Items) > MaxAdminContactScan {
			return fmt.Errorf("%w: contact scan exceeds %d entries", ErrValidation, MaxAdminContactScan)
		}
		for _, contact := range page.Items {
			visit(contact)
		}
		scanned += len(page.Items)
		if page.NextCursor == "" {
			return nil
		}
		if scanned >= MaxAdminContactScan || page.NextCursor == cursor {
			return fmt.Errorf("%w: contact scan exceeds its bounded cursor window", ErrValidation)
		}
		cursor = page.NextCursor
	}
}
