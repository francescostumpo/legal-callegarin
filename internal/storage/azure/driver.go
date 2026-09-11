package azure

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"strings"
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

type tableDriver interface {
	Get(context.Context, string, string) (tableEntity, error)
	Add(context.Context, []byte) (string, error)
	Update(context.Context, []byte, string) (string, error)
	Delete(context.Context, string, string, string) error
	List(context.Context, string, int32) ([]tableEntity, error)
	Transaction(context.Context, []tableAction) error
	Probe(context.Context) error
}

type blobDriver interface {
	PutImmutable(context.Context, string, []byte) error
	Get(context.Context, string) ([]byte, error)
	Delete(context.Context, string) error
	Probe(context.Context) error
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

func storageEndpoints(accountURL string) (tableURL, blobURL string, err error) {
	parsed, err := url.Parse(accountURL)
	if err != nil || parsed.Scheme != "https" || parsed.User != nil || parsed.Port() != "" || parsed.Path != "" && parsed.Path != "/" || parsed.RawQuery != "" || parsed.Fragment != "" {
		return "", "", errors.New("invalid Azure Storage account URL")
	}
	host := strings.ToLower(parsed.Hostname())
	const suffix = ".blob.core.windows.net"
	account := strings.TrimSuffix(host, suffix)
	if parsed.Hostname() != host || account == host || account == "" || strings.Contains(account, ".") {
		return "", "", errors.New("azure storage account URL must be the canonical Blob endpoint")
	}
	parsed.Path = ""
	parsed.Host = host
	blobURL = strings.TrimSuffix(parsed.String(), "/")
	parsed.Host = account + ".table.core.windows.net"
	return strings.TrimSuffix(parsed.String(), "/"), blobURL, nil
}
