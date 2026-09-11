package azure

import (
	"testing"

	"github.com/francescostumpo/legal-callegarin/internal/articles"
	"github.com/francescostumpo/legal-callegarin/internal/storage/contracttest"
)

func TestArticleMetadataRepositoryContract(t *testing.T) {
	contracttest.ArticleMetadataRepository(t, func() articles.MetadataRepository {
		return newArticleMetadataRepository(newMemoryTableDriver())
	})
}
