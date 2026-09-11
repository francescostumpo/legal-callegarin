package azure

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
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

func TestLegacyArticleRowCanBeUpdatedWithItsStoredETag(t *testing.T) {
	driver := newMemoryTableDriver()
	repository := newArticleMetadataRepository(driver)
	article := outcomeArticle("legacy-1", "legacy-one")
	legacy := legacyArticleEntity(t, article)
	etag, err := driver.Add(context.Background(), legacy)
	if err != nil {
		t.Fatal(err)
	}
	loaded, err := repository.Get(context.Background(), article.ID)
	if err != nil || loaded.ETag != etag {
		t.Fatalf("Get legacy=%#v %v", loaded, err)
	}
	computedRow, _ := articleRowKey(article.ID, article.CreatedAt)
	if _, err = driver.Get(context.Background(), articlesPartition, computedRow); err != ErrNotFound {
		t.Fatalf("read-only Get created current row: %v", err)
	}
	stored, err := driver.Get(context.Background(), articlesPartition, article.ID)
	if err != nil {
		t.Fatal(err)
	}
	var properties map[string]any
	if err = json.Unmarshal(stored.Value, &properties); err != nil {
		t.Fatal(err)
	}
	if _, mutated := properties["id"]; mutated {
		t.Fatal("read-only Get mutated legacy entity")
	}
	result, err := migrateLegacyArticleRows(context.Background(), driver)
	if err != nil || result.Migrated != 1 {
		t.Fatalf("migrate legacy rows = %#v, %v", result, err)
	}
	loaded, err = repository.Get(context.Background(), article.ID)
	if err != nil {
		t.Fatal(err)
	}
	loaded.Title = "Titolo legacy aggiornato"
	loaded.UpdatedAt = loaded.UpdatedAt.Add(time.Minute)
	updated, err := repository.Update(context.Background(), loaded, loaded.ETag)
	if err != nil || updated.Title != loaded.Title {
		t.Fatalf("Update legacy=%#v %v", updated, err)
	}
	if _, err = driver.Get(context.Background(), articlesPartition, article.ID); err != ErrNotFound {
		t.Fatalf("legacy row remains after migration: %v", err)
	}
	if _, err = driver.Get(context.Background(), articlesPartition, computedRow); err != nil {
		t.Fatalf("migrated row missing after update: %v", err)
	}
}

func TestMixedLegacyAndCurrentRowsPageInGlobalNewestFirstOrder(t *testing.T) {
	driver := newMemoryTableDriver()
	base := time.Date(2026, 9, 11, 10, 0, 0, 0, time.UTC)
	for index := range 36 {
		article := outcomeArticle(fmt.Sprintf("mixed-%03d", index), fmt.Sprintf("mixed-%03d", index))
		article.CreatedAt = base.Add(time.Duration(index) * time.Minute)
		article.UpdatedAt = article.CreatedAt
		article.DraftBody.SavedAt = article.CreatedAt
		var encoded []byte
		var err error
		if index%4 == 0 {
			encoded = legacyArticleEntity(t, article)
		} else {
			encoded, err = marshalArticleEntity(article)
			if err != nil {
				t.Fatal(err)
			}
		}
		if _, err = driver.Add(context.Background(), encoded); err != nil {
			t.Fatal(err)
		}
	}
	if result, err := migrateLegacyArticleRows(context.Background(), driver); err != nil || result.Migrated != 9 {
		t.Fatalf("migration = %#v, %v", result, err)
	}
	repository := newArticleMetadataRepository(driver)
	cursor := ""
	seen := map[string]bool{}
	var prior time.Time
	for {
		page, err := repository.List(context.Background(), articles.ListOptions{Limit: 7, Cursor: cursor})
		if err != nil {
			t.Fatal(err)
		}
		for _, item := range page.Items {
			if seen[item.ID] {
				t.Fatalf("duplicate %s", item.ID)
			}
			seen[item.ID] = true
			if !prior.IsZero() && !item.CreatedAt.Before(prior) {
				t.Fatalf("out of order %v after %v", item.CreatedAt, prior)
			}
			prior = item.CreatedAt
		}
		if page.NextCursor == "" {
			break
		}
		cursor = page.NextCursor
	}
	if len(seen) != 36 {
		t.Fatalf("seen=%d", len(seen))
	}
}

func TestStatusFilterFillsPageAcrossSparseStoragePages(t *testing.T) {
	driver := &articlePagingTableDriver{memoryTableDriver: newMemoryTableDriver()}
	base := time.Date(2026, 9, 11, 10, 0, 0, 0, time.UTC)
	for index := range 1251 {
		article := outcomeArticle(fmt.Sprintf("sparse-%03d", index), fmt.Sprintf("sparse-%03d", index))
		article.CreatedAt = base.Add(time.Duration(index) * time.Minute)
		article.UpdatedAt = article.CreatedAt
		article.DraftBody.SavedAt = article.CreatedAt
		if index%250 != 0 {
			publishedAt := article.CreatedAt
			article.Status = articles.StatusPublished
			article.PublishedBody = article.DraftBody
			article.Published = &articles.PublishedMetadata{Slug: article.Slug, Title: article.Title, Summary: article.Summary, Area: article.Area, CoverID: article.CoverID}
			article.FirstPublishedAt = &publishedAt
			article.LastPublishedAt = &publishedAt
		}
		encoded, err := marshalArticleEntity(article)
		if err != nil {
			t.Fatal(err)
		}
		if _, err = driver.Add(context.Background(), encoded); err != nil {
			t.Fatal(err)
		}
	}
	status := articles.StatusDraft
	page, err := newArticleMetadataRepository(driver).List(context.Background(), articles.ListOptions{Status: &status, Limit: 5})
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Items) != 5 || page.NextCursor == "" || driver.pageCalls != 1 {
		t.Fatalf("items=%d cursor=%q calls=%d", len(page.Items), page.NextCursor, driver.pageCalls)
	}
	if len(driver.filters) == 0 || !strings.Contains(driver.filters[0], "status eq 'draft'") {
		t.Fatalf("status was not pushed into Azure query: %v", driver.filters)
	}
}

func legacyArticleEntity(t *testing.T, article articles.Article) []byte {
	t.Helper()
	encoded, err := marshalArticleEntity(article)
	if err != nil {
		t.Fatal(err)
	}
	var value map[string]any
	if err = json.Unmarshal(encoded, &value); err != nil {
		t.Fatal(err)
	}
	value["RowKey"] = article.ID
	delete(value, "id")
	encoded, err = json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return encoded
}

func TestArticleRepositoryPagesBeyondOneThousandWithBoundedRequests(t *testing.T) {
	driver := &articlePagingTableDriver{memoryTableDriver: newMemoryTableDriver()}
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

// articlePagingTableDriver records the raw Table requests made by a logical
// sequence of repository pages.
type articlePagingTableDriver struct {
	*memoryTableDriver
	pageCalls int
	maxTop    int32
	filters   []string
}

func (driver *articlePagingTableDriver) ListPage(ctx context.Context, filter string, top int32, continuation *tableContinuation) ([]tableEntity, *tableContinuation, error) {
	driver.pageCalls++
	driver.filters = append(driver.filters, filter)
	if top > driver.maxTop {
		driver.maxTop = top
	}
	return driver.memoryTableDriver.ListPage(ctx, filter, top, continuation)
}

func TestLegacyArticleMigrationIsIdempotentAndResumesAfterInterruption(t *testing.T) {
	driver := newMemoryTableDriver()
	base := time.Date(2026, 9, 11, 10, 0, 0, 0, time.UTC)
	for index := range 3 {
		article := outcomeArticle(fmt.Sprintf("legacy-resume-%d", index), fmt.Sprintf("legacy-resume-%d", index))
		article.CreatedAt = base.Add(time.Duration(index) * time.Minute)
		article.UpdatedAt, article.DraftBody.SavedAt = article.CreatedAt, article.CreatedAt
		if _, err := driver.Add(context.Background(), legacyArticleEntity(t, article)); err != nil {
			t.Fatal(err)
		}
	}
	interrupted := &interruptingMigrationDriver{memoryTableDriver: driver, failBeforeAt: 2}
	if _, err := migrateLegacyArticleRows(context.Background(), interrupted); err == nil {
		t.Fatal("migration succeeded despite injected interruption")
	}
	result, err := migrateLegacyArticleRows(context.Background(), driver)
	if err != nil || result.Migrated != 2 {
		t.Fatalf("resumed migration = %#v, %v", result, err)
	}
	result, err = migrateLegacyArticleRows(context.Background(), driver)
	if err != nil || result.Migrated != 0 {
		t.Fatalf("idempotent migration = %#v, %v", result, err)
	}
	for index := range 3 {
		id := fmt.Sprintf("legacy-resume-%d", index)
		if _, err = driver.Get(context.Background(), articlesPartition, id); err != ErrNotFound {
			t.Fatalf("legacy row %q remains: %v", id, err)
		}
	}
}

func TestLegacyArticleMigrationReconcilesCommitUnknownAndDuplicateRows(t *testing.T) {
	driver := newMemoryTableDriver()
	article := outcomeArticle("legacy-unknown", "legacy-unknown")
	if _, err := driver.Add(context.Background(), legacyArticleEntity(t, article)); err != nil {
		t.Fatal(err)
	}
	unknown := &interruptingMigrationDriver{memoryTableDriver: driver, failAfterAt: 1}
	result, err := migrateLegacyArticleRows(context.Background(), unknown)
	if err != nil || result.Migrated != 1 {
		t.Fatalf("unknown commit migration = %#v, %v", result, err)
	}
	if _, err = driver.Get(context.Background(), articlesPartition, article.ID); err != ErrNotFound {
		t.Fatalf("legacy row remains after reconciled commit: %v", err)
	}

	duplicate := outcomeArticle("legacy-duplicate", "legacy-duplicate")
	if _, err = driver.Add(context.Background(), legacyArticleEntity(t, duplicate)); err != nil {
		t.Fatal(err)
	}
	encoded, err := marshalArticleEntity(duplicate)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = driver.Add(context.Background(), encoded); err != nil {
		t.Fatal(err)
	}
	result, err = migrateLegacyArticleRows(context.Background(), driver)
	if err != nil || result.Migrated != 1 {
		t.Fatalf("duplicate reconciliation = %#v, %v", result, err)
	}
	if _, err = driver.Get(context.Background(), articlesPartition, duplicate.ID); err != ErrNotFound {
		t.Fatalf("duplicate legacy row remains: %v", err)
	}
}

func TestLegacyArticleMigrationLeavesConflictingTargetUntouched(t *testing.T) {
	driver := newMemoryTableDriver()
	legacy := outcomeArticle("legacy-conflict", "legacy-conflict")
	if _, err := driver.Add(context.Background(), legacyArticleEntity(t, legacy)); err != nil {
		t.Fatal(err)
	}
	conflicting := legacy
	conflicting.Title = "Titolo concorrente"
	encoded, err := marshalArticleEntity(conflicting)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = driver.Add(context.Background(), encoded); err != nil {
		t.Fatal(err)
	}
	if _, err = migrateLegacyArticleRows(context.Background(), driver); err == nil {
		t.Fatal("migration accepted a conflicting target")
	}
	rowKey, _ := articleRowKey(legacy.ID, legacy.CreatedAt)
	if _, err = driver.Get(context.Background(), articlesPartition, legacy.ID); err != nil {
		t.Fatalf("legacy source was removed: %v", err)
	}
	stored, err := driver.Get(context.Background(), articlesPartition, rowKey)
	if err != nil {
		t.Fatalf("conflicting target was removed: %v", err)
	}
	got, err := unmarshalArticleEntity(stored.Value, stored.ETag)
	if err != nil || got.Title != conflicting.Title {
		t.Fatalf("conflicting target changed: %#v, %v", got, err)
	}
}

type interruptingMigrationDriver struct {
	*memoryTableDriver
	transactions int
	failBeforeAt int
	failAfterAt  int
}

func (driver *interruptingMigrationDriver) Transaction(ctx context.Context, actions []tableAction) error {
	driver.transactions++
	if driver.transactions == driver.failBeforeAt {
		return context.DeadlineExceeded
	}
	err := driver.memoryTableDriver.Transaction(ctx, actions)
	if err == nil && driver.transactions == driver.failAfterAt {
		return context.DeadlineExceeded
	}
	return err
}

func TestArticleKeysetCursorRemainsStableAcrossBoundaryInsertions(t *testing.T) {
	driver := newMemoryTableDriver()
	repository := newArticleMetadataRepository(driver)
	base := time.Date(2026, 9, 11, 10, 0, 0, 0, time.UTC)
	add := func(id string, createdAt time.Time) {
		t.Helper()
		article := outcomeArticle(id, id)
		article.CreatedAt, article.UpdatedAt, article.DraftBody.SavedAt = createdAt, createdAt, createdAt
		encoded, err := marshalArticleEntity(article)
		if err != nil {
			t.Fatal(err)
		}
		if _, err = driver.Add(context.Background(), encoded); err != nil {
			t.Fatal(err)
		}
	}
	for index := range 20 {
		add(fmt.Sprintf("boundary-%02d", index), base.Add(time.Duration(index)*time.Minute))
	}
	first, err := repository.List(context.Background(), articles.ListOptions{Limit: 5})
	if err != nil || first.NextCursor == "" {
		t.Fatalf("first page = %#v, %v", first, err)
	}
	add("inserted-newer", base.Add(2*time.Hour))
	add("inserted-older", base.Add(-time.Hour))
	seen := map[string]bool{}
	for _, item := range first.Items {
		seen[item.ID] = true
	}
	cursor := first.NextCursor
	for cursor != "" {
		page, listErr := repository.List(context.Background(), articles.ListOptions{Limit: 5, Cursor: cursor})
		if listErr != nil {
			t.Fatal(listErr)
		}
		for _, item := range page.Items {
			if seen[item.ID] {
				t.Fatalf("duplicate %s across cursor boundary", item.ID)
			}
			seen[item.ID] = true
		}
		cursor = page.NextCursor
	}
	if len(seen) != 21 || seen["inserted-newer"] || !seen["inserted-older"] {
		t.Fatalf("seen=%d newer=%v older=%v", len(seen), seen["inserted-newer"], seen["inserted-older"])
	}
}
