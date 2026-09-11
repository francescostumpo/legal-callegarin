package contacts_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/francescostumpo/legal-callegarin/internal/contacts"
	"github.com/francescostumpo/legal-callegarin/internal/storage/memory"
)

func TestContactServiceLifecyclePreservesReviewDue(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	clock := &contactClock{now: time.Date(2024, 2, 29, 10, 0, 0, 0, time.UTC)}
	repository := memory.NewContactRepository(clock.Now)
	service := contacts.NewService(repository, clock, &contactIDs{values: []string{"contact-1"}})

	created, err := service.Submit(ctx, validSubmission())
	if err != nil {
		t.Fatalf("Submit() error = %v", err)
	}
	wantReview := clock.now.AddDate(2, 0, 0)
	if created.State != contacts.StateNew || !created.ReviewDueAt.Equal(wantReview) || !created.PrivacyAcceptedAt.Equal(clock.now) {
		t.Fatalf("Submit() = %#v", created)
	}

	clock.now = clock.now.Add(time.Hour)
	opened, err := service.Open(ctx, created.ID, created.ETag)
	if err != nil || opened.State != contacts.StateRead || opened.ReadAt == nil || !opened.ReadAt.Equal(clock.now) || !opened.ReviewDueAt.Equal(wantReview) {
		t.Fatalf("Open() = %#v, %v", opened, err)
	}
	readAt := *opened.ReadAt

	clock.now = clock.now.Add(time.Hour)
	archived, err := service.Archive(ctx, opened.ID, opened.ETag)
	if err != nil || archived.State != contacts.StateArchived || archived.ArchivedAt == nil || !archived.ArchivedAt.Equal(clock.now) || !archived.ReviewDueAt.Equal(wantReview) {
		t.Fatalf("Archive() = %#v, %v", archived, err)
	}

	clock.now = clock.now.Add(time.Hour)
	restored, err := service.Restore(ctx, archived.ID, archived.ETag)
	if err != nil || restored.State != contacts.StateRead || restored.ArchivedAt != nil || restored.ReadAt == nil || !restored.ReadAt.Equal(readAt) || !restored.ReviewDueAt.Equal(wantReview) {
		t.Fatalf("Restore() = %#v, %v", restored, err)
	}
}

func TestContactServiceDeletionIsExplicitAndPreservesState(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	clock := &contactClock{now: time.Date(2024, 1, 1, 10, 0, 0, 0, time.UTC)}
	repository := memory.NewContactRepository(clock.Now)
	service := contacts.NewService(repository, clock, &contactIDs{values: []string{"scheduled", "old-unscheduled"}})

	scheduled, err := service.Submit(ctx, validSubmission())
	if err != nil {
		t.Fatalf("Submit(scheduled) error = %v", err)
	}
	clock.now = clock.now.Add(time.Hour)
	scheduled, err = service.ScheduleDeletion(ctx, scheduled.ID, scheduled.ETag)
	if err != nil || scheduled.State != contacts.StateNew || scheduled.DeletionDueAt == nil || !scheduled.DeletionDueAt.Equal(clock.now.AddDate(0, 0, 30)) {
		t.Fatalf("ScheduleDeletion() = %#v, %v", scheduled, err)
	}
	clock.now = clock.now.Add(time.Hour)
	cancelled, err := service.CancelDeletion(ctx, scheduled.ID, scheduled.ETag)
	if err != nil || cancelled.State != contacts.StateNew || cancelled.DeletionDueAt != nil {
		t.Fatalf("CancelDeletion() = %#v, %v", cancelled, err)
	}
	scheduled, err = service.ScheduleDeletion(ctx, cancelled.ID, cancelled.ETag)
	if err != nil {
		t.Fatalf("second ScheduleDeletion() error = %v", err)
	}

	clock.now = time.Date(2022, 1, 1, 10, 0, 0, 0, time.UTC)
	old, err := service.Submit(ctx, validSubmission())
	if err != nil {
		t.Fatalf("Submit(old) error = %v", err)
	}
	clock.now = scheduled.DeletionDueAt.Add(time.Second)
	deleted, err := service.PurgeDue(ctx, clock.now)
	if err != nil || deleted != 1 {
		t.Fatalf("PurgeDue() = %d, %v", deleted, err)
	}
	if _, err := repository.Get(ctx, scheduled.ID); !errors.Is(err, contacts.ErrNotFound) {
		t.Fatalf("scheduled contact still exists: %v", err)
	}
	if _, err := repository.Get(ctx, old.ID); err != nil {
		t.Fatalf("old unscheduled contact was automatically deleted: %v", err)
	}
}

func TestContactServiceRejectsInvalidTransitions(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	clock := &contactClock{now: time.Date(2026, 9, 11, 10, 0, 0, 0, time.UTC)}
	repository := memory.NewContactRepository(clock.Now)
	service := contacts.NewService(repository, clock, &contactIDs{values: []string{"contact-1"}})
	created, err := service.Submit(ctx, validSubmission())
	if err != nil {
		t.Fatalf("Submit() error = %v", err)
	}
	if _, err := service.Archive(ctx, created.ID, created.ETag); !errors.Is(err, contacts.ErrInvalidTransition) {
		t.Fatalf("Archive(new) error = %v, want ErrInvalidTransition", err)
	}
	if _, err := service.Restore(ctx, created.ID, created.ETag); !errors.Is(err, contacts.ErrInvalidTransition) {
		t.Fatalf("Restore(new) error = %v, want ErrInvalidTransition", err)
	}
}

func validSubmission() contacts.Submission {
	return contacts.Submission{
		Name:           " Mario Rossi ",
		Email:          " mario@example.test ",
		Phone:          " +39 000 000000 ",
		Message:        " Messaggio sufficientemente lungo ",
		ConsentVersion: " privacy-v1 ",
	}
}

type contactClock struct{ now time.Time }

func (clock *contactClock) Now() time.Time { return clock.now }

type contactIDs struct {
	values []string
	index  int
}

func (ids *contactIDs) NewID() string {
	value := ids.values[ids.index]
	ids.index++
	return value
}
