package memory

import (
	"context"
	"encoding/base64"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/francescostumpo/legal-callegarin/internal/articles"
)

type ArticleBodyStore struct {
	mu      sync.RWMutex
	now     func() time.Time
	bodies  map[string]articles.Body
	nextRef uint64
}

func NewArticleBodyStore(now func() time.Time) *ArticleBodyStore {
	return &ArticleBodyStore{now: now, bodies: make(map[string]articles.Body)}
}

func (store *ArticleBodyStore) Put(ctx context.Context, blobName string, body articles.Body) (articles.BodyRef, error) {
	if err := ctx.Err(); err != nil {
		return articles.BodyRef{}, err
	}
	if strings.TrimSpace(blobName) == "" || store.now == nil {
		return articles.BodyRef{}, fmt.Errorf("%w: blob name and clock are required", articles.ErrValidation)
	}
	if err := body.Validate(); err != nil {
		return articles.BodyRef{}, err
	}
	store.mu.Lock()
	defer store.mu.Unlock()
	store.nextRef++
	version := base64.RawURLEncoding.EncodeToString([]byte(strconv.FormatUint(store.nextRef, 10)))
	ref := articles.BodyRef{BlobName: blobName, Version: version, SavedAt: store.now()}
	store.bodies[bodyKey(ref)] = cloneBody(body)
	return ref, nil
}

func (store *ArticleBodyStore) Get(ctx context.Context, ref articles.BodyRef) (articles.Body, error) {
	if err := ctx.Err(); err != nil {
		return articles.Body{}, err
	}
	store.mu.RLock()
	defer store.mu.RUnlock()
	body, exists := store.bodies[bodyKey(ref)]
	if !exists {
		return articles.Body{}, articles.ErrNotFound
	}
	return cloneBody(body), nil
}

func (store *ArticleBodyStore) Delete(ctx context.Context, ref articles.BodyRef) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	store.mu.Lock()
	defer store.mu.Unlock()
	key := bodyKey(ref)
	if _, exists := store.bodies[key]; !exists {
		return articles.ErrNotFound
	}
	delete(store.bodies, key)
	return nil
}

func bodyKey(ref articles.BodyRef) string {
	return ref.BlobName + "\x00" + ref.Version
}

func cloneBody(body articles.Body) articles.Body {
	body.Document = append([]byte(nil), body.Document...)
	return body
}
