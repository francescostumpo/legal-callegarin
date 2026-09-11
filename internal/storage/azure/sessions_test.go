package azure

import (
	"testing"

	"github.com/francescostumpo/legal-callegarin/internal/auth"
	"github.com/francescostumpo/legal-callegarin/internal/storage/contracttest"
)

func TestSessionRepositoryContract(t *testing.T) {
	contracttest.SessionRepository(t, func() auth.SessionRepository {
		return newSessionRepository(newMemoryTableDriver())
	})
}
