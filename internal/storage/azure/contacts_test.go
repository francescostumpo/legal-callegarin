package azure

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/francescostumpo/legal-callegarin/internal/contacts"
	"github.com/francescostumpo/legal-callegarin/internal/storage/contracttest"
)

func TestContactRepositoryContract(t *testing.T) {
	contracttest.ContactRepository(t, func() contacts.Repository {
		return newContactRepository(newMemoryTableDriver(), func() time.Time {
			return time.Date(2026, 9, 11, 10, 0, 0, 0, time.UTC)
		})
	})
}

func TestContactRepositoryUsesBoundedStorageContinuations(t *testing.T) {
	driver := &pagingTableDriver{memoryTableDriver: newMemoryTableDriver()}
	repository := newContactRepository(driver, time.Now)
	ctx := context.Background()
	base := time.Date(2026, 9, 11, 10, 0, 0, 0, time.UTC)
	for index := range 205 {
		contact := outcomeContact(fmt.Sprintf("paged-%03d", index))
		contact.CreatedAt = base.Add(time.Duration(index) * time.Second)
		contact.UpdatedAt = contact.CreatedAt
		contact.PrivacyAcceptedAt = contact.CreatedAt
		contact.ReviewDueAt = contact.CreatedAt.AddDate(2, 0, 0)
		if _, err := repository.Create(ctx, contact); err != nil {
			t.Fatalf("Create(%d) error = %v", index, err)
		}
	}

	count := 0
	cursor := ""
	for {
		page, err := repository.List(ctx, contacts.ListOptions{Cursor: cursor, Limit: 100})
		if err != nil {
			t.Fatalf("List() error = %v", err)
		}
		count += len(page.Items)
		if page.NextCursor == "" {
			break
		}
		cursor = page.NextCursor
	}
	if count != 205 || driver.pageCalls != 3 || driver.maxTop != 100 {
		t.Fatalf("count=%d pageCalls=%d maxTop=%d", count, driver.pageCalls, driver.maxTop)
	}
}
