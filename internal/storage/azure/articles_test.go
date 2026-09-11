package azure

import (
	"context"
	"encoding/json"
	"errors"
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
	repository := newArticleMetadataRepositoryWithMode(driver, ArticleSchemaCompat)
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
	loaded.Title = "Titolo legacy aggiornato"
	loaded.UpdatedAt = loaded.UpdatedAt.Add(time.Minute)
	updated, err := repository.Update(context.Background(), loaded, loaded.ETag)
	if err != nil || updated.Title != loaded.Title {
		t.Fatalf("Update legacy=%#v %v", updated, err)
	}
	if _, err = driver.Get(context.Background(), articlesPartition, article.ID); err != ErrNotFound {
		t.Fatalf("legacy row remains after compat update: %v", err)
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
	repository := newArticleMetadataRepositoryWithMode(driver, ArticleSchemaCompat)
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

func TestCompatCreateAlwaysWritesCurrentRow(t *testing.T) {
	t.Parallel()

	driver := newMemoryTableDriver()
	repository := newArticleMetadataRepositoryWithMode(driver, ArticleSchemaCompat)
	article := outcomeArticle("compat-create", "compat-create")
	created, err := repository.Create(context.Background(), article)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := driver.Get(context.Background(), articlesPartition, article.ID); err != ErrNotFound {
		t.Fatalf("legacy row unexpectedly written: %v", err)
	}
	rowKey, _ := articleRowKey(article.ID, article.CreatedAt)
	if entity, err := driver.Get(context.Background(), articlesPartition, rowKey); err != nil || entity.ETag != created.ETag {
		t.Fatalf("current row = %#v, %v", entity, err)
	}
}

func TestCurrentModeDoesNotReadLegacyRows(t *testing.T) {
	t.Parallel()

	driver := newMemoryTableDriver()
	article := outcomeArticle("legacy-hidden", "legacy-hidden")
	if _, err := driver.Add(context.Background(), legacyArticleEntity(t, article)); err != nil {
		t.Fatal(err)
	}
	repository := newArticleMetadataRepositoryWithMode(driver, ArticleSchemaMigrate)
	if _, err := repository.Get(context.Background(), article.ID); err != articles.ErrNotFound {
		t.Fatalf("Get legacy in current mode error = %v", err)
	}
	page, err := repository.List(context.Background(), articles.ListOptions{Limit: 10})
	if err != nil || len(page.Items) != 0 {
		t.Fatalf("List current mode = %#v, %v", page, err)
	}
}

func TestArticleListRejectsOversizedCursorBeforeDecoding(t *testing.T) {
	t.Parallel()

	for _, mode := range []ArticleSchemaMode{ArticleSchemaCompat, ArticleSchemaMigrate} {
		repository := newArticleMetadataRepositoryWithMode(newMemoryTableDriver(), mode)
		if _, err := repository.List(context.Background(), articles.ListOptions{Limit: 10, Cursor: strings.Repeat("A", maxArticlePageCursorLength+1)}); !errors.Is(err, articles.ErrValidation) {
			t.Fatalf("mode %q List error = %v", mode, err)
		}
	}
}

func TestCompatLegacyUpdateReconcilesCommittedUnknownOutcome(t *testing.T) {
	t.Parallel()

	base := newMemoryTableDriver()
	article := outcomeArticle("compat-unknown", "compat-unknown")
	etag, err := base.Add(context.Background(), legacyArticleEntity(t, article))
	if err != nil {
		t.Fatal(err)
	}
	oldSlug, err := marshalSlugEntity(slugRecord{Slug: article.Slug, ArticleID: article.ID})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := base.Add(context.Background(), oldSlug); err != nil {
		t.Fatal(err)
	}
	driver := &interruptingMigrationDriver{memoryTableDriver: base, failAfterAt: 1}
	repository := newArticleMetadataRepositoryWithMode(driver, ArticleSchemaCompat)
	article.ETag = etag
	article.Slug = "compat-unknown-updated"
	article.Title = "Titolo aggiornato dopo esito incerto"
	article.UpdatedAt = article.UpdatedAt.Add(time.Minute)
	updated, err := repository.Update(context.Background(), article, etag)
	if err != nil || updated.Title != article.Title {
		t.Fatalf("Update = %#v, %v", updated, err)
	}
	if _, err := base.Get(context.Background(), articlesPartition, article.ID); err != ErrNotFound {
		t.Fatalf("legacy row remains: %v", err)
	}
	if _, err := base.Get(context.Background(), articlesPartition, slugRowKey("compat-unknown")); err != ErrNotFound {
		t.Fatalf("old slug remains: %v", err)
	}
	if _, err := base.Get(context.Background(), articlesPartition, slugRowKey(article.Slug)); err != nil {
		t.Fatalf("new slug missing: %v", err)
	}
}

func TestCompatLegacyUpdateLeavesSourceOnUncommittedUnknownOutcome(t *testing.T) {
	t.Parallel()

	base := newMemoryTableDriver()
	article := outcomeArticle("compat-uncommitted", "compat-uncommitted")
	etag, err := base.Add(context.Background(), legacyArticleEntity(t, article))
	if err != nil {
		t.Fatal(err)
	}
	driver := &interruptingMigrationDriver{memoryTableDriver: base, failBeforeAt: 1}
	repository := newArticleMetadataRepositoryWithMode(driver, ArticleSchemaCompat)
	article.Title = "Titolo non confermato"
	article.UpdatedAt = article.UpdatedAt.Add(time.Minute)
	if _, err := repository.Update(context.Background(), article, etag); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Update error = %v", err)
	}
	legacy, err := base.Get(context.Background(), articlesPartition, article.ID)
	if err != nil || legacy.ETag != etag {
		t.Fatalf("legacy source = %#v, %v", legacy, err)
	}
	rowKey, _ := articleRowKey(article.ID, article.CreatedAt)
	if _, err := base.Get(context.Background(), articlesPartition, rowKey); err != ErrNotFound {
		t.Fatalf("current row exists after uncommitted transaction: %v", err)
	}
}

func TestCompatMixedPagingBeyondOneThousandUsesBoundedRawPages(t *testing.T) {
	t.Parallel()

	driver := &staticPagedArticleDriver{memoryTableDriver: newMemoryTableDriver()}
	base := time.Date(2026, 9, 11, 10, 0, 0, 0, time.UTC)
	for index := range 1205 {
		article := outcomeArticle(fmt.Sprintf("compat-large-%04d", index), fmt.Sprintf("compat-large-%04d", index))
		article.CreatedAt = base.Add(time.Duration(index) * time.Second)
		article.UpdatedAt, article.DraftBody.SavedAt = article.CreatedAt, article.CreatedAt
		encoded, err := marshalArticleEntity(article)
		if index%9 == 0 {
			encoded = legacyArticleEntity(t, article)
		}
		if err != nil {
			t.Fatal(err)
		}
		driver.entities = append(driver.entities, tableEntity{Value: encoded, ETag: fmt.Sprintf("etag-%d", index)})
	}
	repository := newArticleMetadataRepositoryWithMode(driver, ArticleSchemaCompat)
	page, err := repository.List(context.Background(), articles.ListOptions{Limit: 100})
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Items) != 100 || page.NextCursor == "" || page.Items[0].ID != "compat-large-1204" || page.Items[99].ID != "compat-large-1105" || driver.maxTop != articleRawPageSize {
		t.Fatalf("page=%d first=%q last=%q cursor=%t maxTop=%d", len(page.Items), page.Items[0].ID, page.Items[99].ID, page.NextCursor != "", driver.maxTop)
	}
}

type staticPagedArticleDriver struct {
	*memoryTableDriver
	entities []tableEntity
	maxTop   int32
}

func (driver *staticPagedArticleDriver) ListPage(_ context.Context, _ string, top int32, continuation *tableContinuation) ([]tableEntity, *tableContinuation, error) {
	if top > driver.maxTop {
		driver.maxTop = top
	}
	start := 0
	if continuation != nil {
		if _, err := fmt.Sscanf(continuation.RowKey, "%d", &start); err != nil {
			return nil, nil, err
		}
	}
	end := min(start+int(top), len(driver.entities))
	page := append([]tableEntity(nil), driver.entities[start:end]...)
	if end == len(driver.entities) {
		return page, nil, nil
	}
	return page, &tableContinuation{PartitionKey: articlesPartition, RowKey: fmt.Sprintf("%d", end)}, nil
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
	if _, err := driver.Get(context.Background(), articlesPartition, articleMigrationMarkerRowKey); err != ErrNotFound {
		t.Fatalf("failed migration persisted completion marker: %v", err)
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

func TestCompletedArticleMigrationUsesOneMarkerReadAndScansNothing(t *testing.T) {
	driver := newMemoryTableDriver()
	base := time.Date(2026, 9, 11, 10, 0, 0, 0, time.UTC)
	for index := range 205 {
		article := outcomeArticle(fmt.Sprintf("cold-start-%03d", index), fmt.Sprintf("cold-start-%03d", index))
		article.CreatedAt = base.Add(time.Duration(index) * time.Minute)
		article.UpdatedAt, article.DraftBody.SavedAt = article.CreatedAt, article.CreatedAt
		encoded := legacyArticleEntity(t, article)
		if index%2 != 0 {
			var err error
			encoded, err = marshalArticleEntity(article)
			if err != nil {
				t.Fatal(err)
			}
		}
		if _, err := driver.Add(context.Background(), encoded); err != nil {
			t.Fatal(err)
		}
	}
	first := &migrationCountingDriver{tableDriver: driver}
	result, err := migrateLegacyArticleRows(context.Background(), first)
	if err != nil || result.Migrated != 103 || result.Scanned == 0 || first.listPages < 3 || first.adds != 1 {
		t.Fatalf("initial migration = %#v calls=%#v, %v", result, first, err)
	}
	second := &migrationCountingDriver{tableDriver: driver}
	result, err = migrateLegacyArticleRows(context.Background(), second)
	if err != nil || result.Migrated != 0 || result.Scanned != 0 {
		t.Fatalf("completed migration = %#v, %v", result, err)
	}
	if second.gets != 1 || second.listPages != 0 || second.adds != 0 {
		t.Fatalf("completed startup calls: gets=%d pages=%d adds=%d", second.gets, second.listPages, second.adds)
	}
}

func TestArticleRepairIgnoresCompletedMarkerAndConvergesLateLegacyRows(t *testing.T) {
	t.Parallel()

	driver := newMemoryTableDriver()
	if _, err := migrateLegacyArticleRows(context.Background(), driver); err != nil {
		t.Fatal(err)
	}
	late := outcomeArticle("late-after-marker", "late-after-marker")
	if _, err := driver.Add(context.Background(), legacyArticleEntity(t, late)); err != nil {
		t.Fatal(err)
	}

	result, err := repairLegacyArticleRows(context.Background(), driver)
	if err != nil || result.Migrated != 1 || result.Scanned == 0 {
		t.Fatalf("repair = %#v, %v", result, err)
	}
	if _, err := driver.Get(context.Background(), articlesPartition, late.ID); err != ErrNotFound {
		t.Fatalf("late legacy row remains: %v", err)
	}
	rowKey, _ := articleRowKey(late.ID, late.CreatedAt)
	if _, err := driver.Get(context.Background(), articlesPartition, rowKey); err != nil {
		t.Fatalf("current row missing: %v", err)
	}
	if complete, err := articleMigrationIsComplete(context.Background(), driver); err != nil || !complete {
		t.Fatalf("marker after repair = %t, %v", complete, err)
	}
}

func TestArticleMigrationReconcilesUnknownMarkerCommit(t *testing.T) {
	driver := newMemoryTableDriver()
	article := outcomeArticle("marker-unknown", "marker-unknown")
	if _, err := driver.Add(context.Background(), legacyArticleEntity(t, article)); err != nil {
		t.Fatal(err)
	}
	unknown := &unknownMarkerAddDriver{memoryTableDriver: driver}
	result, err := migrateLegacyArticleRows(context.Background(), unknown)
	if err != nil || result.Migrated != 1 || !unknown.failedAfterMarkerAdd {
		t.Fatalf("marker reconciliation = %#v driver=%#v, %v", result, unknown, err)
	}
	result, err = migrateLegacyArticleRows(context.Background(), driver)
	if err != nil || result.Scanned != 0 || result.Migrated != 0 {
		t.Fatalf("marker fast path = %#v, %v", result, err)
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

	duplicateDriver := newMemoryTableDriver()
	duplicate := outcomeArticle("legacy-duplicate", "legacy-duplicate")
	if _, err = duplicateDriver.Add(context.Background(), legacyArticleEntity(t, duplicate)); err != nil {
		t.Fatal(err)
	}
	encoded, err := marshalArticleEntity(duplicate)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = duplicateDriver.Add(context.Background(), encoded); err != nil {
		t.Fatal(err)
	}
	result, err = migrateLegacyArticleRows(context.Background(), duplicateDriver)
	if err != nil || result.Migrated != 1 {
		t.Fatalf("duplicate reconciliation = %#v, %v", result, err)
	}
	if _, err = duplicateDriver.Get(context.Background(), articlesPartition, duplicate.ID); err != ErrNotFound {
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

type migrationCountingDriver struct {
	tableDriver
	gets      int
	adds      int
	listPages int
}

func (driver *migrationCountingDriver) Get(ctx context.Context, partition, row string) (tableEntity, error) {
	driver.gets++
	return driver.tableDriver.Get(ctx, partition, row)
}

func (driver *migrationCountingDriver) Add(ctx context.Context, entity []byte) (string, error) {
	driver.adds++
	return driver.tableDriver.Add(ctx, entity)
}

func (driver *migrationCountingDriver) ListPage(ctx context.Context, filter string, top int32, continuation *tableContinuation) ([]tableEntity, *tableContinuation, error) {
	driver.listPages++
	return driver.tableDriver.ListPage(ctx, filter, top, continuation)
}

type unknownMarkerAddDriver struct {
	*memoryTableDriver
	failedAfterMarkerAdd bool
}

func (driver *unknownMarkerAddDriver) Add(ctx context.Context, entity []byte) (string, error) {
	_, row, err := entityKey(entity)
	if err != nil {
		return "", err
	}
	etag, err := driver.memoryTableDriver.Add(ctx, entity)
	if err == nil && row == articleMigrationMarkerRowKey && !driver.failedAfterMarkerAdd {
		driver.failedAfterMarkerAdd = true
		return "", context.DeadlineExceeded
	}
	return etag, err
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

func TestCompatArticleKeysetCursorRemainsStableAcrossLegacyBoundaryInsertions(t *testing.T) {
	t.Parallel()

	driver := newMemoryTableDriver()
	repository := newArticleMetadataRepositoryWithMode(driver, ArticleSchemaCompat)
	base := time.Date(2026, 9, 11, 10, 0, 0, 0, time.UTC)
	addLegacy := func(id string, createdAt time.Time) {
		t.Helper()
		article := outcomeArticle(id, id)
		article.CreatedAt, article.UpdatedAt, article.DraftBody.SavedAt = createdAt, createdAt, createdAt
		if _, err := driver.Add(context.Background(), legacyArticleEntity(t, article)); err != nil {
			t.Fatal(err)
		}
	}
	for index := range 20 {
		addLegacy(fmt.Sprintf("compat-boundary-%02d", index), base.Add(time.Duration(index)*time.Minute))
	}
	first, err := repository.List(context.Background(), articles.ListOptions{Limit: 5})
	if err != nil || first.NextCursor == "" {
		t.Fatalf("first page = %#v, %v", first, err)
	}
	addLegacy("compat-inserted-newer", base.Add(2*time.Hour))
	addLegacy("compat-inserted-older", base.Add(-time.Hour))
	seen := map[string]bool{}
	for _, item := range first.Items {
		seen[item.ID] = true
	}
	for cursor := first.NextCursor; cursor != ""; {
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
	if len(seen) != 21 || seen["compat-inserted-newer"] || !seen["compat-inserted-older"] {
		t.Fatalf("seen=%d newer=%v older=%v", len(seen), seen["compat-inserted-newer"], seen["compat-inserted-older"])
	}
}
