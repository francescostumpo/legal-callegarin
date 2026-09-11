package azure

import (
	"context"
	"encoding/json"
	"fmt"
	"io"

	"github.com/Azure/azure-sdk-for-go/sdk/azcore"
	"github.com/Azure/azure-sdk-for-go/sdk/azcore/policy"
	"github.com/Azure/azure-sdk-for-go/sdk/azcore/to"
	"github.com/Azure/azure-sdk-for-go/sdk/data/aztables"
	"github.com/Azure/azure-sdk-for-go/sdk/storage/azblob"
	"github.com/Azure/azure-sdk-for-go/sdk/storage/azblob/blob"
)

const applicationID = "legal-callegarin"

func azureClientOptions() azcore.ClientOptions {
	return azcore.ClientOptions{
		Retry:     policy.RetryOptions{MaxRetries: -1},
		Telemetry: policy.TelemetryOptions{ApplicationID: applicationID},
	}
}

type sdkTableDriver struct {
	client   *aztables.Client
	executor operationExecutor
}

func (driver *sdkTableDriver) Get(ctx context.Context, partitionKey, rowKey string) (result tableEntity, err error) {
	err = driver.executor.do(ctx, func(attempt context.Context) error {
		response, callErr := driver.client.GetEntity(attempt, partitionKey, rowKey, nil)
		if callErr == nil {
			result = tableEntity{Value: response.Value, ETag: string(response.ETag)}
		}
		return callErr
	})
	return result, err
}

func (driver *sdkTableDriver) Add(ctx context.Context, entity []byte) (etag string, err error) {
	err = driver.executor.do(ctx, func(attempt context.Context) error {
		response, callErr := driver.client.AddEntity(attempt, entity, nil)
		if callErr == nil {
			etag = string(response.ETag)
		}
		return callErr
	})
	return etag, err
}

func (driver *sdkTableDriver) Update(ctx context.Context, entity []byte, etag string) (nextETag string, err error) {
	err = driver.executor.do(ctx, func(attempt context.Context) error {
		match := azcore.ETag(etag)
		response, callErr := driver.client.UpdateEntity(attempt, entity, &aztables.UpdateEntityOptions{IfMatch: &match, UpdateMode: aztables.UpdateModeReplace})
		if callErr == nil {
			nextETag = string(response.ETag)
		}
		return callErr
	})
	return nextETag, err
}

func (driver *sdkTableDriver) Delete(ctx context.Context, partitionKey, rowKey, etag string) error {
	return driver.executor.do(ctx, func(attempt context.Context) error {
		match := azcore.ETag(etag)
		_, err := driver.client.DeleteEntity(attempt, partitionKey, rowKey, &aztables.DeleteEntityOptions{IfMatch: &match})
		return err
	})
}

func (driver *sdkTableDriver) List(ctx context.Context, filter string, maximum int32) (entities []tableEntity, err error) {
	err = driver.executor.do(ctx, func(attempt context.Context) error {
		entities = entities[:0]
		options := &aztables.ListEntitiesOptions{Filter: &filter, Top: &maximum}
		pager := driver.client.NewListEntitiesPager(options)
		for pager.More() && len(entities) < int(maximum) {
			page, pageErr := pager.NextPage(attempt)
			if pageErr != nil {
				return pageErr
			}
			for _, value := range page.Entities {
				var metadata struct {
					ETag string `json:"odata.etag"`
				}
				if unmarshalErr := json.Unmarshal(value, &metadata); unmarshalErr != nil {
					return fmt.Errorf("decode table ETag: %w", unmarshalErr)
				}
				entities = append(entities, tableEntity{Value: value, ETag: metadata.ETag})
				if len(entities) == int(maximum) {
					break
				}
			}
		}
		return nil
	})
	return entities, err
}

func (driver *sdkTableDriver) Transaction(ctx context.Context, actions []tableAction) error {
	converted := make([]aztables.TransactionAction, 0, len(actions))
	for _, action := range actions {
		convertedAction := aztables.TransactionAction{Entity: action.Entity}
		switch action.Kind {
		case tableAdd:
			convertedAction.ActionType = aztables.TransactionTypeAdd
		case tableReplace:
			convertedAction.ActionType = aztables.TransactionTypeUpdateReplace
		case tableDelete:
			convertedAction.ActionType = aztables.TransactionTypeDelete
		default:
			return fmt.Errorf("%w: unsupported operation", ErrBatchInvalid)
		}
		if action.ETag != "" {
			match := azcore.ETag(action.ETag)
			convertedAction.IfMatch = &match
		}
		converted = append(converted, convertedAction)
	}
	return driver.executor.do(ctx, func(attempt context.Context) error {
		_, err := driver.client.SubmitTransaction(attempt, converted, nil)
		return classifyTransactionError(err)
	})
}

func (driver *sdkTableDriver) Probe(ctx context.Context) error {
	_, err := driver.List(ctx, "PartitionKey eq '__readiness__'", 1)
	return err
}

type sdkBlobDriver struct {
	client    *azblob.Client
	container string
	executor  operationExecutor
}

func (driver *sdkBlobDriver) PutImmutable(ctx context.Context, name string, content []byte) error {
	return driver.executor.do(ctx, func(attempt context.Context) error {
		_, err := driver.client.UploadBuffer(attempt, driver.container, name, content, &azblob.UploadBufferOptions{
			HTTPHeaders: &blob.HTTPHeaders{BlobContentType: to.Ptr("application/json")},
			AccessConditions: &blob.AccessConditions{ModifiedAccessConditions: &blob.ModifiedAccessConditions{
				IfNoneMatch: to.Ptr(azcore.ETagAny),
			}},
		})
		return err
	})
}

func (driver *sdkBlobDriver) Get(ctx context.Context, name string) (content []byte, err error) {
	err = driver.executor.do(ctx, func(attempt context.Context) error {
		response, callErr := driver.client.DownloadStream(attempt, driver.container, name, nil)
		if callErr != nil {
			return callErr
		}
		defer response.Body.Close()
		content, callErr = io.ReadAll(io.LimitReader(response.Body, 2*1024*1024))
		return callErr
	})
	return content, err
}

func (driver *sdkBlobDriver) Delete(ctx context.Context, name string) error {
	return driver.executor.do(ctx, func(attempt context.Context) error {
		_, err := driver.client.DeleteBlob(attempt, driver.container, name, nil)
		return err
	})
}

func (driver *sdkBlobDriver) Probe(ctx context.Context) error {
	return driver.executor.do(ctx, func(attempt context.Context) error {
		_, err := driver.client.ServiceClient().NewContainerClient(driver.container).GetProperties(attempt, nil)
		return err
	})
}
