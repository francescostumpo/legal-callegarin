package memory_test

import (
	"testing"
	"time"

	"github.com/francescostumpo/legal-callegarin/internal/articles"
	"github.com/francescostumpo/legal-callegarin/internal/auth"
	"github.com/francescostumpo/legal-callegarin/internal/contacts"
	"github.com/francescostumpo/legal-callegarin/internal/storage/contracttest"
	"github.com/francescostumpo/legal-callegarin/internal/storage/memory"
)

func TestArticleMetadataRepositoryContract(t *testing.T) {
	contracttest.ArticleMetadataRepository(t, func() articles.MetadataRepository {
		return memory.NewArticleMetadataRepository()
	})
}

func TestArticleBodyStoreContract(t *testing.T) {
	fixed := time.Date(2026, time.September, 11, 10, 0, 0, 0, time.UTC)
	contracttest.ArticleBodyStore(t, func() articles.BodyStore {
		return memory.NewArticleBodyStore(func() time.Time { return fixed })
	})
}

func TestContactRepositoryContract(t *testing.T) {
	fixed := time.Date(2026, time.September, 11, 10, 0, 0, 0, time.UTC)
	contracttest.ContactRepository(t, func() contacts.Repository {
		return memory.NewContactRepository(func() time.Time { return fixed })
	})
}

func TestSessionRepositoryContract(t *testing.T) {
	contracttest.SessionRepository(t, func() auth.SessionRepository {
		return memory.NewSessionRepository()
	})
}
