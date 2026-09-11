package azure

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/francescostumpo/legal-callegarin/internal/articles"
	"github.com/francescostumpo/legal-callegarin/internal/storage/contracttest"
)

func TestArticleMetadataRepositoryContract(t *testing.T) {
	contracttest.ArticleMetadataRepository(t, func() articles.MetadataRepository {
		return newArticleMetadataRepository(newMemoryTableDriver())
	})
}

func TestArticleRepositoryPagesBeyondOneThousandWithBoundedRequests(t *testing.T) {
	driver := &pagingTableDriver{memoryTableDriver: newMemoryTableDriver()}
	base := time.Date(2026, 9, 11, 10, 0, 0, 0, time.UTC)
	for index := range 1205 {
		article := outcomeArticle(fmt.Sprintf("article-%04d", index), fmt.Sprintf("article-%04d", index))
		article.CreatedAt = base.Add(time.Duration(index) * time.Second)
		article.UpdatedAt = article.CreatedAt
		article.DraftBody.SavedAt = article.CreatedAt
		encoded, err := marshalArticleEntity(article)
		if err != nil {
			t.Fatal(err)
		}
		if _, err = driver.Add(context.Background(), encoded); err != nil {
			t.Fatal(err)
		}
	}
	repository := newArticleMetadataRepository(driver)
	cursor := ""
	seen := 0
	for {
		page, err := repository.List(context.Background(), articles.ListOptions{Cursor: cursor, Limit: 100})
		if err != nil {
			t.Fatal(err)
		}
		seen += len(page.Items)
		if page.NextCursor == "" {
			break
		}
		if page.NextCursor == cursor {
			t.Fatal("cursor did not advance")
		}
		cursor = page.NextCursor
	}
	if seen != 1205 || driver.pageCalls != 13 || driver.maxTop != 100 {
		t.Fatalf("seen=%d calls=%d maxTop=%d", seen, driver.pageCalls, driver.maxTop)
	}
}
