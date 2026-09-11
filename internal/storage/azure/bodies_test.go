package azure

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/francescostumpo/legal-callegarin/internal/articles"
	"github.com/francescostumpo/legal-callegarin/internal/storage/contracttest"
)

func TestArticleBodyStoreContract(t *testing.T) {
	now := time.Date(2026, 9, 11, 12, 0, 0, 0, time.UTC)
	var sequence int
	contracttest.ArticleBodyStore(t, func() articles.BodyStore {
		return newArticleBodyStore(newMemoryBlobDriver(), func() time.Time { return now.Add(time.Duration(sequence) * time.Second) }, func() string { sequence++; return fmt.Sprintf("version-%d", sequence) })
	})
}

func TestArticleBodyStoreRejectsAnExistingVersion(t *testing.T) {
	store := newArticleBodyStore(newMemoryBlobDriver(), time.Now, func() string { return "fixed-version" })
	body, compileErr := articles.CompileDocument(1, []byte(`{"type":"doc","content":[{"type":"paragraph","content":[{"type":"text","text":"body"}]}]}`))
	if compileErr != nil {
		t.Fatal(compileErr)
	}
	first, err := store.Put(context.Background(), "article-1", body)
	if err != nil || first.BlobName != "articles/article-1/fixed-version.json" {
		t.Fatalf("first Put() = %#v, %v", first, err)
	}
	if _, err := store.Put(context.Background(), "article-1", body); !errors.Is(err, articles.ErrConflict) {
		t.Fatalf("second Put() error = %v, want ErrConflict", err)
	}
}

func TestBodyDownloadLimitRejectsOversizeWithoutTrustingContentLength(t *testing.T) {
	limit := int64(maxBodyEnvelopeBytes + 1)
	for _, test := range []struct {
		name     string
		download blobDownload
		wantErr  bool
	}{
		{name: "exact boundary", download: blobDownload{Value: make([]byte, maxBodyEnvelopeBytes)}},
		{name: "missing content length", download: blobDownload{Value: make([]byte, limit)}, wantErr: true},
		{name: "wrong low content length", download: blobDownload{Value: make([]byte, limit), ContentLength: int64Pointer(1)}, wantErr: true},
		{name: "declared oversize", download: blobDownload{Value: []byte(`{}`), ContentLength: int64Pointer(limit)}, wantErr: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			_, err := boundedBodyValue(test.download)
			if errors.Is(err, articles.ErrBodyTooLarge) != test.wantErr {
				t.Fatalf("boundedBodyValue() error = %v", err)
			}
		})
	}
}

func TestBodyStoreRequestsLimitPlusOneForOversizeStreamDetection(t *testing.T) {
	driver := &limitRecordingBlobDriver{memoryBlobDriver: newMemoryBlobDriver()}
	driver.values["articles/article-1/version-1.json"] = make([]byte, maxBodyEnvelopeBytes+2)
	store := newArticleBodyStore(driver, time.Now, func() string { return "version-1" })
	ref := articles.BodyRef{BlobName: "articles/article-1/version-1.json", Version: "version-1", SavedAt: time.Now()}
	_, err := store.Get(context.Background(), ref)
	if !errors.Is(err, articles.ErrBodyTooLarge) || driver.maximumRead != maxBodyEnvelopeBytes+1 {
		t.Fatalf("Get() error = %v, maximumRead = %d", err, driver.maximumRead)
	}
}

func TestMaximumDomainBodyFitsStorageEnvelope(t *testing.T) {
	text := strings.Repeat("x", 500*1024)
	body, compileErr := articles.CompileDocument(1, []byte(`{"type":"doc","content":[{"type":"paragraph","content":[{"type":"text","text":"`+text+`"}]}]}`))
	if compileErr != nil {
		t.Fatal(compileErr)
	}
	encoded, err := marshalBodyEnvelope(body)
	if err != nil || len(encoded) > maxBodyEnvelopeBytes {
		t.Fatalf("marshalBodyEnvelope() size = %d, error = %v", len(encoded), err)
	}
}

func TestBodyMutationsReconcileCommittedResults(t *testing.T) {
	driver := &commitThenErrorBlob{memoryBlobDriver: newMemoryBlobDriver(), err: ErrTransient}
	store := newArticleBodyStore(driver, func() time.Time { return time.Date(2026, 9, 11, 12, 0, 0, 0, time.UTC) }, func() string { return "version-1" })
	body, compileErr := articles.CompileDocument(1, []byte(`{"type":"doc","content":[{"type":"paragraph","content":[{"type":"text","text":"body"}]}]}`))
	if compileErr != nil {
		t.Fatal(compileErr)
	}
	ref, err := store.Put(context.Background(), "article-1", body)
	if err != nil || ref.BlobName == "" {
		t.Fatalf("Put() = %#v, %v", ref, err)
	}
	if err := store.Delete(context.Background(), ref); err != nil {
		t.Fatalf("Delete() error = %v", err)
	}
}

type limitRecordingBlobDriver struct {
	*memoryBlobDriver
	maximumRead int64
}

func (driver *limitRecordingBlobDriver) Get(ctx context.Context, name string, maximumRead int64) (blobDownload, error) {
	driver.maximumRead = maximumRead
	return driver.memoryBlobDriver.Get(ctx, name, maximumRead)
}

func int64Pointer(value int64) *int64 { return &value }

type commitThenErrorBlob struct {
	*memoryBlobDriver
	err error
}

func (driver *commitThenErrorBlob) PutImmutable(ctx context.Context, name string, value []byte) error {
	if err := driver.memoryBlobDriver.PutImmutable(ctx, name, value); err != nil {
		return err
	}
	return driver.err
}

func (driver *commitThenErrorBlob) Delete(ctx context.Context, name string) error {
	if err := driver.memoryBlobDriver.Delete(ctx, name); err != nil {
		return err
	}
	return driver.err
}
