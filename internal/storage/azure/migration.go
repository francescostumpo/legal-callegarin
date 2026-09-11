package azure

import (
	"context"
	"errors"
	"fmt"

	"github.com/Azure/azure-sdk-for-go/sdk/azidentity"
	"github.com/Azure/azure-sdk-for-go/sdk/data/aztables"
	"github.com/francescostumpo/legal-callegarin/internal/articles"
)

// ArticleMigrationResult reports the bounded, explicit legacy-row migration.
// A zero Migrated count means the article table already uses current keys.
type ArticleMigrationResult struct {
	Migrated int
	Scanned  int
}

// MigrateArticleRows migrates legacy direct-ID article rows using the runtime
// managed identity. It does not create or otherwise provision resources.
func MigrateArticleRows(ctx context.Context, accountURL string, names ResourceNames) (ArticleMigrationResult, error) {
	if err := names.validate(); err != nil {
		return ArticleMigrationResult{}, err
	}
	tableURL, _, err := storageEndpoints(accountURL)
	if err != nil {
		return ArticleMigrationResult{}, err
	}
	credential, err := azidentity.NewDefaultAzureCredential(nil)
	if err != nil {
		return ArticleMigrationResult{}, errors.New("initialize Azure default credential for article migration")
	}
	service, err := aztables.NewServiceClient(tableURL, credential, &aztables.ClientOptions{ClientOptions: azureClientOptions()})
	if err != nil {
		return ArticleMigrationResult{}, errors.New("initialize Azure Table client for article migration")
	}
	return migrateArticleRowsWithService(ctx, service, names)
}

// MigrateArticleRowsFromConnectionString is the development/test counterpart
// to MigrateArticleRows. Resource provisioning remains a separate operation.
func MigrateArticleRowsFromConnectionString(ctx context.Context, connectionString string, names ResourceNames) (ArticleMigrationResult, error) {
	if err := names.validate(); err != nil {
		return ArticleMigrationResult{}, err
	}
	if connectionString == "" {
		return ArticleMigrationResult{}, errors.New("azure storage connection string is required")
	}
	service, err := aztables.NewServiceClientFromConnectionString(connectionString, &aztables.ClientOptions{ClientOptions: azureClientOptions()})
	if err != nil {
		return ArticleMigrationResult{}, errors.New("initialize Azure Table client from connection string for article migration")
	}
	return migrateArticleRowsWithService(ctx, service, names)
}

func migrateArticleRowsWithService(ctx context.Context, service *aztables.ServiceClient, names ResourceNames) (ArticleMigrationResult, error) {
	driver := &sdkTableDriver{client: service.NewClient(names.ArticlesTable), executor: defaultExecutor()}
	result, err := migrateLegacyArticleRows(ctx, driver)
	if err != nil {
		return result, fmt.Errorf("migrate legacy Azure article rows: %w", err)
	}
	return result, nil
}

func migrateLegacyArticleRows(ctx context.Context, table tableDriver) (ArticleMigrationResult, error) {
	result := ArticleMigrationResult{}
	for {
		migratedThisPass := 0
		var continuation *tableContinuation
		for {
			entities, next, err := table.ListPage(ctx, "PartitionKey eq 'articles' and entityType eq 'article'", articleRawPageSize, continuation)
			if err != nil {
				return result, err
			}
			if next != nil && continuation != nil && *next == *continuation {
				return result, errors.New("article migration continuation did not advance")
			}
			for _, entity := range entities {
				result.Scanned++
				var header entityHeader
				if err = decodeHeader(entity.Value, &header); err != nil {
					return result, err
				}
				if header.EntityType != articleEntityType {
					continue
				}
				article, decodeErr := unmarshalArticleEntity(entity.Value, entity.ETag)
				if decodeErr != nil {
					return result, decodeErr
				}
				targetRowKey, keyErr := articleRowKey(article.ID, article.CreatedAt)
				if keyErr != nil {
					return result, keyErr
				}
				if header.RowKey == targetRowKey {
					continue
				}
				if header.RowKey != article.ID {
					return result, fmt.Errorf("unexpected legacy article row key %q", header.RowKey)
				}
				if err = migrateLegacyArticleRow(ctx, table, article, entity.ETag, header.RowKey, targetRowKey); err != nil {
					return result, err
				}
				result.Migrated++
				migratedThisPass++
			}
			if next == nil {
				break
			}
			continuation = next
		}
		if migratedThisPass == 0 {
			return result, nil
		}
	}
}

func migrateLegacyArticleRow(ctx context.Context, table tableDriver, article articles.Article, legacyETag, legacyRowKey, targetRowKey string) error {
	target, err := marshalArticleEntity(article)
	if err != nil {
		return err
	}
	actions := []tableAction{
		{Kind: tableAdd, PartitionKey: articlesPartition, RowKey: targetRowKey, Entity: target},
		{Kind: tableDelete, PartitionKey: articlesPartition, RowKey: legacyRowKey, Entity: articleEntityKey(legacyRowKey), ETag: legacyETag},
	}
	if err = submitTransaction(ctx, table, actions); err == nil {
		return nil
	}
	return reconcileLegacyArticleMigration(ctx, table, article, legacyRowKey, targetRowKey, err)
}

func reconcileLegacyArticleMigration(ctx context.Context, table tableDriver, desired articles.Article, legacyRowKey, targetRowKey string, cause error) error {
	legacy, legacyPresent, legacyErr := migrationArticleAt(ctx, table, legacyRowKey)
	target, targetPresent, targetErr := migrationArticleAt(ctx, table, targetRowKey)
	if legacyErr != nil || targetErr != nil {
		return fmt.Errorf("reconcile article migration after %v: legacy=%v target=%v", cause, legacyErr, targetErr)
	}
	if targetPresent && !sameArticleState(target, desired) {
		return fmt.Errorf("article migration target %q conflicts with existing state", targetRowKey)
	}
	if legacyPresent && !sameArticleState(legacy, desired) {
		return fmt.Errorf("legacy article %q changed during migration", legacyRowKey)
	}
	if targetPresent && !legacyPresent {
		return nil
	}
	if targetPresent && legacyPresent {
		entity, getErr := table.Get(ctx, articlesPartition, legacyRowKey)
		if getErr != nil {
			if errors.Is(getErr, ErrNotFound) {
				return nil
			}
			return getErr
		}
		deleteErr := submitTransaction(ctx, table, []tableAction{{Kind: tableDelete, PartitionKey: articlesPartition, RowKey: legacyRowKey, Entity: articleEntityKey(legacyRowKey), ETag: entity.ETag}})
		if deleteErr == nil {
			return nil
		}
		if _, verifyErr := table.Get(ctx, articlesPartition, legacyRowKey); errors.Is(verifyErr, ErrNotFound) {
			return nil
		}
		return fmt.Errorf("finish reconciled article migration: %w", deleteErr)
	}
	return cause
}

func migrationArticleAt(ctx context.Context, table tableDriver, rowKey string) (articles.Article, bool, error) {
	entity, err := table.Get(ctx, articlesPartition, rowKey)
	if errors.Is(err, ErrNotFound) {
		return articles.Article{}, false, nil
	}
	if err != nil {
		return articles.Article{}, false, err
	}
	article, err := unmarshalArticleEntity(entity.Value, entity.ETag)
	return article, err == nil, err
}

func articleEntityKey(rowKey string) []byte {
	return []byte(fmt.Sprintf(`{"PartitionKey":"%s","RowKey":"%s"}`, articlesPartition, rowKey))
}
