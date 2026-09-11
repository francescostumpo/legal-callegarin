package azure

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/francescostumpo/legal-callegarin/internal/articles"
)

type ArticleBodyStore struct {
	blobs   blobDriver
	now     func() time.Time
	version func() string
}

func newArticleBodyStore(blobs blobDriver, now func() time.Time, version func() string) *ArticleBodyStore {
	return &ArticleBodyStore{blobs: blobs, now: now, version: version}
}
func defaultVersion() string {
	value := make([]byte, 18)
	if _, err := rand.Read(value); err != nil {
		panic("generate body version")
	}
	return base64.RawURLEncoding.EncodeToString(value)
}

func (store *ArticleBodyStore) Put(ctx context.Context, articleID string, body articles.Body) (articles.BodyRef, error) {
	if store.now == nil || store.version == nil {
		return articles.BodyRef{}, fmt.Errorf("%w: body store dependencies are required", articles.ErrValidation)
	}
	encoded, err := marshalBodyEnvelope(body)
	if err != nil {
		return articles.BodyRef{}, err
	}
	version := store.version()
	name, err := articleBlobName(articleID, version)
	if err != nil {
		return articles.BodyRef{}, err
	}
	if err = store.blobs.PutImmutable(ctx, name, encoded); err != nil {
		if errors.Is(err, ErrPrecondition) || errors.Is(err, ErrConflict) {
			return articles.BodyRef{}, articles.ErrConflict
		}
		return articles.BodyRef{}, mapArticleError(err, false)
	}
	return articles.BodyRef{BlobName: name, Version: version, SavedAt: store.now().UTC()}, nil
}
func (store *ArticleBodyStore) Get(ctx context.Context, ref articles.BodyRef) (articles.Body, error) {
	if err := validateBodyRef(ref); err != nil {
		return articles.Body{}, err
	}
	encoded, err := store.blobs.Get(ctx, ref.BlobName)
	if err != nil {
		return articles.Body{}, mapArticleError(err, false)
	}
	return unmarshalBodyEnvelope(encoded)
}
func (store *ArticleBodyStore) Delete(ctx context.Context, ref articles.BodyRef) error {
	if err := validateBodyRef(ref); err != nil {
		return err
	}
	err := store.blobs.Delete(ctx, ref.BlobName)
	return mapArticleError(err, false)
}
func validateBodyRef(ref articles.BodyRef) error {
	suffix := "/" + ref.Version + ".json"
	if !safeStorageSegment(ref.Version) || !strings.HasPrefix(ref.BlobName, "articles/") || !strings.HasSuffix(ref.BlobName, suffix) || strings.Contains(ref.BlobName, "://") {
		return fmt.Errorf("%w: invalid body reference", articles.ErrValidation)
	}
	articleID := strings.TrimSuffix(strings.TrimPrefix(ref.BlobName, "articles/"), suffix)
	expected, err := articleBlobName(articleID, ref.Version)
	if err != nil || expected != ref.BlobName {
		return fmt.Errorf("%w: invalid body reference", articles.ErrValidation)
	}
	return nil
}
