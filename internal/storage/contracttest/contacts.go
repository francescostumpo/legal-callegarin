package contracttest

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"testing"
	"time"

	"github.com/francescostumpo/legal-callegarin/internal/contacts"
)

func ContactRepository(t *testing.T, factory func() contacts.Repository) {
	t.Helper()

	t.Run("CRUD optimistic concurrency and defensive copies", func(t *testing.T) {
		repository := factory()
		ctx := context.Background()
		created, err := repository.Create(ctx, contactFixture("contact-1", "Mario Rossi", time.Date(2026, 9, 11, 10, 0, 0, 0, time.UTC)))
		if err != nil {
			t.Fatalf("Create() error = %v", err)
		}
		if created.ETag == "" {
			t.Fatal("Create() ETag is empty")
		}

		updatedInput := created
		readAt := created.CreatedAt.Add(time.Minute)
		updatedInput.State = contacts.StateRead
		updatedInput.ReadAt = &readAt
		updatedInput.UpdatedAt = readAt
		updated, err := repository.Update(ctx, updatedInput, created.ETag)
		if err != nil || updated.ETag == created.ETag {
			t.Fatalf("Update() = %#v, %v", updated, err)
		}
		if _, err := repository.Update(ctx, updatedInput, created.ETag); !errors.Is(err, contacts.ErrConflict) {
			t.Fatalf("stale Update() error = %v, want ErrConflict", err)
		}

		*updated.ReadAt = updated.ReadAt.Add(time.Hour)
		stored, err := repository.Get(ctx, created.ID)
		if err != nil {
			t.Fatalf("Get() error = %v", err)
		}
		if stored.ReadAt.Equal(*updated.ReadAt) {
			t.Fatal("repository exposed stored timestamp pointer")
		}
		if err := repository.Delete(ctx, created.ID, created.ETag); !errors.Is(err, contacts.ErrConflict) {
			t.Fatalf("stale Delete() error = %v, want ErrConflict", err)
		}
		if err := repository.Delete(ctx, created.ID, stored.ETag); err != nil {
			t.Fatalf("Delete() error = %v", err)
		}
		if _, err := repository.Get(ctx, created.ID); !errors.Is(err, contacts.ErrNotFound) {
			t.Fatalf("Get(deleted) error = %v, want ErrNotFound", err)
		}
	})

	t.Run("reverse chronology search filters pagination and cursor validation", func(t *testing.T) {
		repository := factory()
		ctx := context.Background()
		base := time.Date(2026, 9, 11, 10, 0, 0, 0, time.UTC)
		fixtures := []contacts.Contact{
			contactFixture("older", "Mario Rossi", base),
			contactFixture("middle", "Lucia Bianchi", base.Add(time.Minute)),
			contactFixture("newer", "ALBERTO Verdi", base.Add(2*time.Minute)),
		}
		for _, fixture := range fixtures {
			if _, err := repository.Create(ctx, fixture); err != nil {
				t.Fatalf("Create(%q) error = %v", fixture.ID, err)
			}
		}

		first, err := repository.List(ctx, contacts.ListOptions{Limit: 2})
		if err != nil || len(first.Items) != 2 || first.Items[0].ID != "newer" || first.Items[1].ID != "middle" || first.NextCursor == "" {
			t.Fatalf("first List() = %#v, %v", first, err)
		}
		second, err := repository.List(ctx, contacts.ListOptions{Limit: 2, Cursor: first.NextCursor})
		if err != nil || len(second.Items) != 1 || second.Items[0].ID != "older" {
			t.Fatalf("second List() = %#v, %v", second, err)
		}
		searched, err := repository.List(ctx, contacts.ListOptions{Query: "alberto"})
		if err != nil || len(searched.Items) != 1 || searched.Items[0].ID != "newer" {
			t.Fatalf("searched List() = %#v, %v", searched, err)
		}
		if _, err := repository.List(ctx, contacts.ListOptions{Cursor: "not-a-cursor"}); !errors.Is(err, contacts.ErrValidation) {
			t.Fatalf("invalid cursor error = %v, want ErrValidation", err)
		}
		state := contacts.StateNew
		filtered, err := repository.List(ctx, contacts.ListOptions{State: &state, Limit: 101})
		if err != nil || len(filtered.Items) != 3 {
			t.Fatalf("bounded filtered List() = %#v, %v", filtered, err)
		}
	})

	t.Run("retention review is a filter and never deletes", func(t *testing.T) {
		repository := factory()
		ctx := context.Background()
		due := contactFixture("due", "Due Contact", time.Date(2024, 9, 10, 10, 0, 0, 0, time.UTC))
		future := contactFixture("future", "Future Contact", time.Date(2025, 9, 12, 10, 0, 0, 0, time.UTC))
		for _, fixture := range []contacts.Contact{due, future} {
			if _, err := repository.Create(ctx, fixture); err != nil {
				t.Fatalf("Create(%q) error = %v", fixture.ID, err)
			}
		}
		page, err := repository.List(ctx, contacts.ListOptions{RetentionReview: true})
		if err != nil || len(page.Items) != 1 || page.Items[0].ID != "due" {
			t.Fatalf("retention List() = %#v, %v", page, err)
		}
		if _, err := repository.Get(ctx, due.ID); err != nil {
			t.Fatalf("retention List() deleted due contact: %v", err)
		}
	})

	t.Run("search is case insensitive and bounded to 100 results", func(t *testing.T) {
		repository := factory()
		ctx := context.Background()
		base := time.Date(2026, 9, 11, 10, 0, 0, 0, time.UTC)
		for index := range 105 {
			id := "search-" + strconv.Itoa(index)
			fixture := contactFixture(id, "Search Person "+strconv.Itoa(index), base.Add(time.Duration(index)*time.Second))
			if _, err := repository.Create(ctx, fixture); err != nil {
				t.Fatalf("Create(%q) error = %v", id, err)
			}
		}
		page, err := repository.List(ctx, contacts.ListOptions{Query: "SEARCH PERSON", Limit: 101})
		if err != nil || len(page.Items) != 100 || page.NextCursor == "" {
			t.Fatalf("bounded search List() len = %d, cursor = %q, error = %v", len(page.Items), page.NextCursor, err)
		}
	})

	contactPaginationLimits(t, factory)
}

func contactPaginationLimits(t *testing.T, factory func() contacts.Repository) {
	t.Helper()

	newPopulatedRepository := func(t *testing.T) contacts.Repository {
		t.Helper()
		repository := factory()
		ctx := context.Background()
		base := time.Date(2026, 9, 11, 10, 0, 0, 0, time.UTC)
		for index := range 105 {
			id := fmt.Sprintf("limit-%03d", index)
			fixture := contactFixture(id, "Limit Person "+strconv.Itoa(index), base.Add(time.Duration(index)*time.Second))
			if _, err := repository.Create(ctx, fixture); err != nil {
				t.Fatalf("Create(%q) error = %v", id, err)
			}
		}
		return repository
	}

	t.Run("pagination limit zero defaults to 25", func(t *testing.T) {
		repository := newPopulatedRepository(t)
		ctx := context.Background()
		page, err := repository.List(ctx, contacts.ListOptions{Limit: 0})
		if err != nil {
			t.Fatalf("List(Limit: 0) error = %v", err)
		}
		if len(page.Items) != 25 || page.Items[0].ID != "limit-104" || page.Items[24].ID != "limit-080" || page.NextCursor == "" {
			t.Fatalf("List(Limit: 0) = %#v", page)
		}
		next, err := repository.List(ctx, contacts.ListOptions{Limit: 0, Cursor: page.NextCursor})
		if err != nil || len(next.Items) != 25 || next.Items[0].ID != "limit-079" {
			t.Fatalf("List(default limit, next cursor) = %#v, %v", next, err)
		}
	})

	t.Run("pagination limit above 100 is capped", func(t *testing.T) {
		repository := newPopulatedRepository(t)
		ctx := context.Background()
		page, err := repository.List(ctx, contacts.ListOptions{Limit: 101})
		if err != nil {
			t.Fatalf("List(Limit: 101) error = %v", err)
		}
		if len(page.Items) != 100 || page.Items[0].ID != "limit-104" || page.Items[99].ID != "limit-005" || page.NextCursor == "" {
			t.Fatalf("List(Limit: 101) = %#v", page)
		}
		next, err := repository.List(ctx, contacts.ListOptions{Limit: 101, Cursor: page.NextCursor})
		if err != nil || len(next.Items) != 5 || next.Items[0].ID != "limit-004" || next.Items[4].ID != "limit-000" || next.NextCursor != "" {
			t.Fatalf("List(capped limit, next cursor) = %#v, %v", next, err)
		}
	})
}

func contactFixture(id, name string, created time.Time) contacts.Contact {
	return contacts.Contact{
		ID:                id,
		Name:              name,
		Email:             id + "@example.test",
		Phone:             "+39 000 000000",
		Message:           "Messaggio sufficientemente lungo",
		ConsentVersion:    "privacy-v1",
		PrivacyAcceptedAt: created,
		State:             contacts.StateNew,
		CreatedAt:         created,
		UpdatedAt:         created,
		ReviewDueAt:       created.AddDate(2, 0, 0),
	}
}
