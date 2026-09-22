package azure

import (
	"context"
	"errors"
	"strings"
	"testing"
)

type recordingTable struct {
	actions []tableAction
	err     error
}

func (table *recordingTable) Get(context.Context, string, string) (tableEntity, error) {
	return tableEntity{}, table.err
}
func (table *recordingTable) Add(context.Context, []byte) (string, error) { return "", table.err }
func (table *recordingTable) Update(context.Context, []byte, string) (string, error) {
	return "", table.err
}
func (table *recordingTable) Delete(context.Context, string, string, string) error { return table.err }
func (table *recordingTable) List(context.Context, string, int32) ([]tableEntity, error) {
	return nil, table.err
}
func (table *recordingTable) ListPage(context.Context, string, int32, *tableContinuation) ([]tableEntity, *tableContinuation, error) {
	return nil, nil, table.err
}
func (table *recordingTable) Transaction(_ context.Context, actions []tableAction) error {
	table.actions = append([]tableAction(nil), actions...)
	return table.err
}
func (table *recordingTable) Probe(context.Context) error { return table.err }

func TestSubmitTransactionPreflightLimits(t *testing.T) {
	driver := &recordingTable{}
	for _, test := range []struct {
		name    string
		actions []tableAction
	}{
		{"more than 100 operations", make([]tableAction, 101)},
		{"more than 4 MiB", []tableAction{{Entity: []byte(strings.Repeat("x", maxTransactionBytes+1))}}},
		{"mixed partitions", []tableAction{{PartitionKey: "one", Entity: []byte(`{}`)}, {PartitionKey: "two", Entity: []byte(`{}`)}}},
	} {
		t.Run(test.name, func(t *testing.T) {
			if err := submitTransaction(context.Background(), driver, test.actions); !errors.Is(err, ErrBatchInvalid) {
				t.Fatalf("submitTransaction() error = %v", err)
			}
			if len(driver.actions) != 0 {
				t.Fatal("invalid batch reached driver")
			}
		})
	}
}

func TestStorageEndpoints(t *testing.T) {
	tableURL, blobURL, err := storageEndpoints("https://account.blob.core.windows.net")
	if err != nil || tableURL != "https://account.table.core.windows.net" || blobURL != "https://account.blob.core.windows.net" {
		t.Fatalf("storageEndpoints() = %q, %q, %v", tableURL, blobURL, err)
	}
	for _, invalid := range []string{
		"https://account.table.core.windows.net",
		"https://user@account.blob.core.windows.net",
		"https://account.blob.core.windows.net:443",
		"https://nested.account.blob.core.windows.net",
	} {
		if _, _, err := storageEndpoints(invalid); err == nil {
			t.Fatalf("storageEndpoints(%q) error = nil", invalid)
		}
	}
}
