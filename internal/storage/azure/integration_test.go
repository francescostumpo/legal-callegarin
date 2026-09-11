//go:build integration

package azure

import (
	"context"
	"fmt"
	"os"
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
