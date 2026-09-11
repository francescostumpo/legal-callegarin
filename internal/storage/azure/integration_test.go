//go:build integration

package azure

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Azure/azure-sdk-for-go/sdk/data/aztables"
	"github.com/Azure/azure-sdk-for-go/sdk/storage/azblob"
	"github.com/francescostumpo/legal-callegarin/internal/articles"
	"github.com/francescostumpo/legal-callegarin/internal/auth"
	"github.com/francescostumpo/legal-callegarin/internal/contacts"
	"github.com/francescostumpo/legal-callegarin/internal/storage/contracttest"
)

func TestAzuriteLegacyArticleLifecycleAndMixedPaging(t *testing.T) {
	connectionString := os.Getenv("AZURITE_CONNECTION_STRING")
	if connectionString == "" {
		t.Fatal("AZURITE_CONNECTION_STRING is required for integration tests")
	}
	suffix := fmt.Sprintf("%d", time.Now().UnixNano())
	names := ResourceNames{ArticlesTable: "al" + suffix, ContactsTable: "cl" + suffix, SessionsTable: "sl" + suffix, BodiesContainer: "bl-" + suffix}
	if err := EnsureFromConnectionString(context.Background(), connectionString, names); err != nil {
		t.Fatal(err)
	}
	options := azureClientOptions()
	tableService, err := aztables.NewServiceClientFromConnectionString(connectionString, &aztables.ClientOptions{ClientOptions: options})
	if err != nil {
		t.Fatal(err)
	}
	blobClient, err := azblob.NewClientFromConnectionString(connectionString, &azblob.ClientOptions{ClientOptions: options})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		_, _ = tableService.DeleteTable(ctx, names.ArticlesTable, nil)
		_, _ = tableService.DeleteTable(ctx, names.ContactsTable, nil)
		_, _ = tableService.DeleteTable(ctx, names.SessionsTable, nil)
		_, _ = blobClient.DeleteContainer(ctx, names.BodiesContainer, nil)
	})

	clock := &integrationArticleClock{now: time.Date(2026, 9, 11, 10, 0, 0, 0, time.UTC)}
	bundle, err := OpenFromConnectionString(context.Background(), connectionString, names, clock.Now)
	if err != nil {
		t.Fatal(err)
	}
	driver := &sdkTableDriver{client: tableService.NewClient(names.ArticlesTable), executor: defaultExecutor()}
	body := mustCompileIntegrationBody(t, "prima versione")
	ref, err := bundle.Bodies.Put(context.Background(), "legacy-article", body)
	if err != nil {
		t.Fatal(err)
	}
	legacy := articles.Article{
		ID: "legacy-article", Slug: "legacy-article", Title: "Titolo legacy iniziale", Summary: "Sommario legacy iniziale sufficientemente lungo",
		Area: "diritti-reali", Status: articles.StatusDraft, DraftBody: &ref, CreatedAt: clock.now, UpdatedAt: clock.now,
	}
	storedETag, err := driver.Add(context.Background(), legacyArticleEntity(t, legacy))
	if err != nil {
		t.Fatal(err)
	}
	legacySlug, err := marshalSlugEntity(slugRecord{Slug: legacy.Slug, ArticleID: legacy.ID})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = driver.Add(context.Background(), legacySlug); err != nil {
		t.Fatal(err)
	}
	service := articles.NewService(bundle.Articles, bundle.Bodies, clock, integrationArticleIDs{})
	loaded, err := service.Get(context.Background(), legacy.ID)
	if err != nil || loaded.ETag != storedETag {
		t.Fatalf("legacy Get = %#v, %v", loaded, err)
	}
	rowKey, _ := articleRowKey(legacy.ID, legacy.CreatedAt)
	if _, err = driver.Get(context.Background(), articlesPartition, rowKey); !errors.Is(err, ErrNotFound) {
		t.Fatalf("GET migrated legacy row or unexpected error: %v", err)
	}

	clock.now = clock.now.Add(time.Minute)
	saved, err := service.SaveDraft(context.Background(), legacy.ID, articles.DraftInput{
		Slug: "legacy-updated", Title: "Titolo legacy aggiornato", Summary: "Sommario legacy aggiornato sufficientemente lungo", Area: "diritti-reali",
		Body: mustCompileIntegrationBody(t, "seconda versione"),
	}, loaded.ETag)
	if err != nil {
		t.Fatalf("SaveDraft legacy: %v", err)
	}
	clock.now = clock.now.Add(time.Minute)
	if _, err = service.Publish(context.Background(), saved.ID, loaded.ETag); !errors.Is(err, articles.ErrConflict) {
		t.Fatalf("stale legacy ETag Publish error = %v", err)
	}
	published, err := service.Publish(context.Background(), saved.ID, saved.ETag)
	if err != nil || published.Status != articles.StatusPublished {
		t.Fatalf("Publish legacy = %#v, %v", published, err)
	}
	clock.now = clock.now.Add(time.Minute)
	withdrawn, err := service.Withdraw(context.Background(), published.ID, published.ETag)
	if err != nil || withdrawn.Status != articles.StatusWithdrawn {
		t.Fatalf("Withdraw legacy = %#v, %v", withdrawn, err)
	}
	preview, err := service.GetPreview(context.Background(), withdrawn.ID)
	if err != nil || !strings.Contains(preview.Body.PlainText, "seconda versione") {
		t.Fatalf("legacy preview after lifecycle = %#v, %v", preview, err)
	}
	if _, err = driver.Get(context.Background(), articlesPartition, legacy.ID); err != nil {
		t.Fatalf("legacy row disappeared: %v", err)
	}
	if _, err = driver.Get(context.Background(), articlesPartition, rowKey); !errors.Is(err, ErrNotFound) {
		t.Fatalf("legacy lifecycle unexpectedly migrated row: %v", err)
	}

	base := clock.now.Add(time.Hour)
	for index := range 8 {
		item := legacy
		item.ID = fmt.Sprintf("mixed-live-%02d", index)
		item.Slug = item.ID
		item.Title = fmt.Sprintf("Titolo misto %02d", index)
		item.Status = articles.StatusDraft
		item.PublishedBody, item.Published, item.FirstPublishedAt, item.LastPublishedAt = nil, nil, nil, nil
		item.CreatedAt = base.Add(time.Duration(index) * time.Minute)
		item.UpdatedAt = item.CreatedAt
		item.ETag = ""
		var encoded []byte
		if index%2 == 0 {
			encoded = legacyArticleEntity(t, item)
		} else if encoded, err = marshalArticleEntity(item); err != nil {
			t.Fatal(err)
		}
		if _, err = driver.Add(context.Background(), encoded); err != nil {
			t.Fatal(err)
		}
	}
	cursor := ""
	seen := map[string]bool{}
	var previous time.Time
	for {
		page, listErr := bundle.Articles.List(context.Background(), articles.ListOptions{Limit: 3, Cursor: cursor})
		if listErr != nil {
			t.Fatal(listErr)
		}
		for _, item := range page.Items {
			if seen[item.ID] || !previous.IsZero() && !item.CreatedAt.Before(previous) {
				t.Fatalf("mixed paging duplicate/order violation: %s after %v", item.ID, previous)
			}
			seen[item.ID] = true
			previous = item.CreatedAt
		}
		if page.NextCursor == "" {
			break
		}
		cursor = page.NextCursor
	}
	if len(seen) != 9 {
		t.Fatalf("mixed paging saw %d articles, want 9", len(seen))
	}
}

type integrationArticleClock struct{ now time.Time }

func (clock *integrationArticleClock) Now() time.Time { return clock.now }

type integrationArticleIDs struct{}

func (integrationArticleIDs) NewID() string { return "unused" }

func mustCompileIntegrationBody(t *testing.T, text string) articles.Body {
	t.Helper()
	body, err := articles.CompileDocument(1, []byte(`{"type":"doc","content":[{"type":"paragraph","content":[{"type":"text","text":"`+text+`"}]}]}`))
	if err != nil {
		t.Fatal(err)
	}
	return body
}

func TestAzuriteContracts(t *testing.T) {
	connectionString := os.Getenv("AZURITE_CONNECTION_STRING")
	if connectionString == "" {
		t.Fatal("AZURITE_CONNECTION_STRING is required for integration tests")
	}
	options := azureClientOptions()
	tableService, err := aztables.NewServiceClientFromConnectionString(connectionString, &aztables.ClientOptions{ClientOptions: options})
	if err != nil {
		t.Fatalf("create cleanup table client: %v", err)
	}
	blobClient, err := azblob.NewClientFromConnectionString(connectionString, &azblob.ClientOptions{ClientOptions: options})
	if err != nil {
		t.Fatalf("create cleanup blob client: %v", err)
	}

	var sequence atomic.Uint64
	resources := make([]ResourceNames, 0, 16)
	open := func(t *testing.T) *storageComponents {
		t.Helper()
		suffix := fmt.Sprintf("%d%d", time.Now().UnixNano(), sequence.Add(1))
		names := ResourceNames{
			ArticlesTable: "a" + suffix, ContactsTable: "c" + suffix, SessionsTable: "s" + suffix,
			BodiesContainer: "b-" + suffix,
		}
		if err := EnsureFromConnectionString(context.Background(), connectionString, names); err != nil {
			t.Fatalf("EnsureFromConnectionString(): %v", err)
		}
		bundle, err := OpenFromConnectionString(context.Background(), connectionString, names, time.Now)
		if err != nil {
			t.Fatalf("OpenFromConnectionString(): %v", err)
		}
		resources = append(resources, names)
		properties, err := blobClient.ServiceClient().NewContainerClient(names.BodiesContainer).GetProperties(context.Background(), nil)
		if err != nil || properties.BlobPublicAccess != nil {
			t.Fatalf("body container is not private: access=%v, error=%v", properties.BlobPublicAccess, err)
		}
		return &storageComponents{articles: bundle.Articles, bodies: bundle.Bodies, contacts: bundle.Contacts, sessions: bundle.Sessions, readiness: bundle.Readiness}
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		for _, names := range resources {
			_, _ = tableService.DeleteTable(ctx, names.ArticlesTable, nil)
			_, _ = tableService.DeleteTable(ctx, names.ContactsTable, nil)
			_, _ = tableService.DeleteTable(ctx, names.SessionsTable, nil)
			_, _ = blobClient.DeleteContainer(ctx, names.BodiesContainer, nil)
		}
	})

	t.Run("articles", func(t *testing.T) {
		contracttest.ArticleMetadataRepository(t, func() articles.MetadataRepository { return open(t).articles })
	})
	t.Run("bodies", func(t *testing.T) {
		contracttest.ArticleBodyStore(t, func() articles.BodyStore { return open(t).bodies })
	})
	t.Run("contacts", func(t *testing.T) {
		contracttest.ContactRepository(t, func() contacts.Repository { return open(t).contacts })
	})
	t.Run("sessions", func(t *testing.T) {
		contracttest.SessionRepository(t, func() auth.SessionRepository { return open(t).sessions })
	})
	t.Run("readiness", func(t *testing.T) {
		if err := open(t).readiness.Ready(context.Background()); err != nil {
			t.Fatalf("Ready() error = %v", err)
		}
	})
}

type storageComponents struct {
	articles  articles.MetadataRepository
	bodies    articles.BodyStore
	contacts  contacts.Repository
	sessions  auth.SessionRepository
	readiness interface{ Ready(context.Context) error }
}
