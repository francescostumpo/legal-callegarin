package azure

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/Azure/azure-sdk-for-go/sdk/azidentity"
	"github.com/Azure/azure-sdk-for-go/sdk/data/aztables"
	"github.com/Azure/azure-sdk-for-go/sdk/storage/azblob"
	"github.com/francescostumpo/legal-callegarin/internal/storage"
)

const (
	defaultArticlesTable   = "articles"
	defaultContactsTable   = "contacts"
	defaultSessionsTable   = "sessions"
	defaultBodiesContainer = "article-bodies"
)

type ResourceNames struct{ ArticlesTable, ContactsTable, SessionsTable, BodiesContainer string }

func DefaultResourceNames() ResourceNames {
	return ResourceNames{defaultArticlesTable, defaultContactsTable, defaultSessionsTable, defaultBodiesContainer}
}
func (names ResourceNames) validate() error {
	if names.ArticlesTable == "" || names.ContactsTable == "" || names.SessionsTable == "" || names.BodiesContainer == "" {
		return errors.New("azure storage resource names are required")
	}
	return nil
}

type azureReadiness struct {
	tables []tableDriver
	blobs  blobDriver
}

func (readiness *azureReadiness) Ready(ctx context.Context) error {
	for _, table := range readiness.tables {
		if err := table.Probe(ctx); err != nil {
			return fmt.Errorf("probe Azure Table Storage: %w", err)
		}
	}
	if err := readiness.blobs.Probe(ctx); err != nil {
		return fmt.Errorf("probe Azure Blob Storage: %w", err)
	}
	return nil
}

func Open(ctx context.Context, accountURL string, names ResourceNames, now func() time.Time) (*storage.Bundle, error) {
	if err := names.validate(); err != nil {
		return nil, err
	}
	tableURL, blobURL, err := storageEndpoints(accountURL)
	if err != nil {
		return nil, err
	}
	credential, err := azidentity.NewDefaultAzureCredential(nil)
	if err != nil {
		return nil, errors.New("initialize Azure default credential")
	}
	options := azureClientOptions()
	tableService, err := aztables.NewServiceClient(tableURL, credential, &aztables.ClientOptions{ClientOptions: options})
	if err != nil {
		return nil, errors.New("initialize Azure Table client")
	}
	blobClient, err := azblob.NewClient(blobURL, credential, &azblob.ClientOptions{ClientOptions: options})
	if err != nil {
		return nil, errors.New("initialize Azure Blob client")
	}
	return openWithClients(tableService, blobClient, names, now)
}

func OpenFromConnectionString(ctx context.Context, connectionString string, names ResourceNames, now func() time.Time) (*storage.Bundle, error) {
	if err := names.validate(); err != nil {
		return nil, err
	}
	if connectionString == "" {
		return nil, errors.New("azure storage connection string is required")
	}
	options := azureClientOptions()
	tableService, err := aztables.NewServiceClientFromConnectionString(connectionString, &aztables.ClientOptions{ClientOptions: options})
	if err != nil {
		return nil, errors.New("initialize Azure Table client from connection string")
	}
	blobClient, err := azblob.NewClientFromConnectionString(connectionString, &azblob.ClientOptions{ClientOptions: options})
	if err != nil {
		return nil, errors.New("initialize Azure Blob client from connection string")
	}
	return openWithClients(tableService, blobClient, names, now)
}

// EnsureFromConnectionString provisions the development/test resources. Production
// resources are provisioned by infrastructure-as-code and Open never creates them.
func EnsureFromConnectionString(ctx context.Context, connectionString string, names ResourceNames) error {
	if err := names.validate(); err != nil {
		return err
	}
	if connectionString == "" {
		return errors.New("azure storage connection string is required")
	}
	options := azureClientOptions()
	tableService, err := aztables.NewServiceClientFromConnectionString(connectionString, &aztables.ClientOptions{ClientOptions: options})
	if err != nil {
		return errors.New("initialize Azure Table client from connection string")
	}
	blobClient, err := azblob.NewClientFromConnectionString(connectionString, &azblob.ClientOptions{ClientOptions: options})
	if err != nil {
		return errors.New("initialize Azure Blob client from connection string")
	}
	return ensureResources(ctx, tableService, blobClient, names)
}

func ensureResources(ctx context.Context, tableService *aztables.ServiceClient, blobClient *azblob.Client, names ResourceNames) error {
	executor := defaultExecutor()
	for _, name := range []string{names.ArticlesTable, names.ContactsTable, names.SessionsTable} {
		err := executor.mutate(ctx, func(attempt context.Context) error {
			_, callErr := tableService.CreateTable(attempt, name, nil)
			return callErr
		})
		if err != nil && !errors.Is(err, ErrConflict) {
			return fmt.Errorf("ensure Azure table %q: %w", name, err)
		}
	}
	err := executor.mutate(ctx, func(attempt context.Context) error {
		_, callErr := blobClient.CreateContainer(attempt, names.BodiesContainer, nil)
		return callErr
	})
	if err != nil && !errors.Is(err, ErrConflict) {
		return fmt.Errorf("ensure private Azure blob container: %w", err)
	}
	return nil
}

func openWithClients(tableService *aztables.ServiceClient, blobClient *azblob.Client, names ResourceNames, now func() time.Time) (*storage.Bundle, error) {
	executor := defaultExecutor()
	articlesTable := &sdkTableDriver{client: tableService.NewClient(names.ArticlesTable), executor: executor}
	contactsTable := &sdkTableDriver{client: tableService.NewClient(names.ContactsTable), executor: executor}
	sessionsTable := &sdkTableDriver{client: tableService.NewClient(names.SessionsTable), executor: executor}
	blobs := &sdkBlobDriver{client: blobClient, container: names.BodiesContainer, executor: executor}
	if now == nil {
		now = time.Now
	}
	return &storage.Bundle{Articles: newArticleMetadataRepository(articlesTable), Bodies: newArticleBodyStore(blobs, now, defaultVersion), Contacts: newContactRepository(contactsTable, now), Sessions: newSessionRepository(sessionsTable), Readiness: &azureReadiness{tables: []tableDriver{articlesTable, contactsTable, sessionsTable}, blobs: blobs}}, nil
}
