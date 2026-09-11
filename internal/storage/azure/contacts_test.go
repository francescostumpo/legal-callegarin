package azure

import (
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
