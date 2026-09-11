package azure

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/francescostumpo/legal-callegarin/internal/articles"
	"github.com/francescostumpo/legal-callegarin/internal/storage/contracttest"
)

func TestArticleBodyStoreContract(t *testing.T) {
	now := time.Date(2026, 9, 11, 12, 0, 0, 0, time.UTC)
	var sequence int
	contracttest.ArticleBodyStore(t, func() articles.BodyStore {
		return newArticleBodyStore(newMemoryBlobDriver(), func() time.Time { return now.Add(time.Duration(sequence) * time.Second) }, func() string { sequence++; return fmt.Sprintf("version-%d", sequence) })
	})
}

func TestArticleBodyStoreRejectsAnExistingVersion(t *testing.T) {
	store := newArticleBodyStore(newMemoryBlobDriver(), time.Now, func() string { return "fixed-version" })
	body := articles.Body{SchemaVersion: 1, Document: []byte(`{"type":"doc"}`), HTML: "<p>body</p>", PlainText: "body"}
	first, err := store.Put(context.Background(), "article-1", body)
	if err != nil || first.BlobName != "articles/article-1/fixed-version.json" {
		t.Fatalf("first Put() = %#v, %v", first, err)
	}
	if _, err := store.Put(context.Background(), "article-1", body); !errors.Is(err, articles.ErrConflict) {
		t.Fatalf("second Put() error = %v, want ErrConflict", err)
	}
}
