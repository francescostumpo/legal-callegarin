package azure

import (
	"context"
	"errors"
	"fmt"

	"github.com/francescostumpo/legal-callegarin/internal/storage/azureurl"
)

const (
	maxTransactionOperations = 100
	maxTransactionBytes      = 4 * 1024 * 1024
)

var ErrBatchInvalid = errors.New("azure table transaction is invalid")

type tableActionKind uint8

const (
	tableAdd tableActionKind = iota + 1
	tableReplace
	tableDelete
)

type tableAction struct {
	Kind         tableActionKind
	PartitionKey string
	RowKey       string
	Entity       []byte
	ETag         string
}

type tableEntity struct {
	Value []byte
	ETag  string
}

type tableContinuation struct {
	PartitionKey string
	RowKey       string
}

type tableDriver interface {
	Get(context.Context, string, string) (tableEntity, error)
	Add(context.Context, []byte) (string, error)
	Update(context.Context, []byte, string) (string, error)
	Delete(context.Context, string, string, string) error
	List(context.Context, string, int32) ([]tableEntity, error)
	ListPage(context.Context, string, int32, *tableContinuation) ([]tableEntity, *tableContinuation, error)
	Transaction(context.Context, []tableAction) error
	Probe(context.Context) error
}

type blobDriver interface {
	PutImmutable(context.Context, string, []byte) error
	Get(context.Context, string, int64) (blobDownload, error)
	Delete(context.Context, string) error
	Probe(context.Context) error
}

type blobDownload struct {
	Value         []byte
	ContentLength *int64
}

func submitTransaction(ctx context.Context, driver tableDriver, actions []tableAction) error {
	if len(actions) == 0 || len(actions) > maxTransactionOperations {
		return fmt.Errorf("%w: operation count must be between 1 and %d", ErrBatchInvalid, maxTransactionOperations)
	}
	partition := actions[0].PartitionKey
	size := 0
	for _, action := range actions {
		if partition == "" || action.PartitionKey != partition {
			return fmt.Errorf("%w: all operations must share one partition", ErrBatchInvalid)
		}
		size += len(action.Entity) + len(action.PartitionKey) + len(action.RowKey) + len(action.ETag) + 256
		if size > maxTransactionBytes {
			return fmt.Errorf("%w: payload exceeds %d bytes", ErrBatchInvalid, maxTransactionBytes)
		}
	}
	return driver.Transaction(ctx, actions)
}

func storageEndpoints(accountURL string) (string, string, error) {
	return azureurl.Endpoints(accountURL)
}
