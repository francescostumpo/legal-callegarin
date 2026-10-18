package azure

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"testing"
	"time"

	"github.com/francescostumpo/legal-callegarin/internal/auth"
	"github.com/francescostumpo/legal-callegarin/internal/storage/contracttest"
)

func TestSessionRepositoryContract(t *testing.T) {
	contracttest.SessionRepository(t, func() auth.SessionRepository {
		return newSessionRepository(newMemoryTableDriver())
	})
}

type pagingTableDriver struct {
	*memoryTableDriver
	pageCalls int
	maxTop    int32
}

func (driver *pagingTableDriver) ListPage(ctx context.Context, filter string, top int32, continuation *tableContinuation) ([]tableEntity, *tableContinuation, error) {
	driver.pageCalls++
	if top > driver.maxTop {
		driver.maxTop = top
	}
	return driver.memoryTableDriver.ListPage(ctx, filter, top, continuation)
}

func TestSessionCleanupPagesAcrossMoreThanOneThousandSparseExpirations(t *testing.T) {
	driver := &pagingTableDriver{memoryTableDriver: newMemoryTableDriver()}
	repository := newSessionRepository(driver)
	now := time.Date(2026, 9, 11, 12, 0, 0, 0, time.UTC)
	expired := 0
	for index := 0; index < 1105; index++ {
		digest := sha256.Sum256([]byte(fmt.Sprintf("session-%04d", index)))
		created := now
		if index%11 == 0 {
			created = now.Add(-9 * time.Hour)
			expired++
		}
		session := auth.Session{TokenHash: hex.EncodeToString(digest[:]), Username: "admin", CredentialVersion: "v1", CreatedAt: created, ExpiresAt: created.Add(8 * time.Hour)}
		if err := repository.Create(context.Background(), session); err != nil {
			t.Fatalf("Create(%d): %v", index, err)
		}
	}

	deleted, err := repository.DeleteExpired(context.Background(), now)
	if err != nil || deleted != expired {
		t.Fatalf("DeleteExpired() = %d, %v; want %d, nil", deleted, err, expired)
	}
	if driver.pageCalls < 12 || driver.maxTop > sessionCleanupPageSize {
		t.Fatalf("paging calls=%d maxTop=%d", driver.pageCalls, driver.maxTop)
	}
	remaining, err := driver.List(context.Background(), "PartitionKey eq 'sessions'", 2000)
	if err != nil || len(remaining) != 1105-expired {
		t.Fatalf("remaining sessions = %d, %v", len(remaining), err)
	}
}
