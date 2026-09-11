package memory

import (
	"context"
	"encoding/base64"
	"fmt"
	"slices"
	"sort"
	"strconv"
	"sync"

	"github.com/francescostumpo/legal-callegarin/internal/articles"
)

type ArticleMetadataRepository struct {
	mu       sync.RWMutex
	articles map[string]articles.Article
	slugs    map[string]string
	nextETag uint64
}

func NewArticleMetadataRepository() *ArticleMetadataRepository {
	return &ArticleMetadataRepository{articles: make(map[string]articles.Article), slugs: make(map[string]string)}
}

func (repository *ArticleMetadataRepository) Create(ctx context.Context, article articles.Article) (articles.Article, error) {
	if err := ctx.Err(); err != nil {
		return articles.Article{}, err
	}
	if err := article.Validate(); err != nil {
		return articles.Article{}, err
	}

	repository.mu.Lock()
	defer repository.mu.Unlock()
	if _, exists := repository.articles[article.ID]; exists {
		return articles.Article{}, articles.ErrConflict
	}
	for _, slug := range articleSlugs(article) {
		if repository.slugOwnedByOther(slug, article.ID) {
			return articles.Article{}, articles.ErrSlugTaken
		}
	}
	article.ETag = repository.newETag()
	repository.articles[article.ID] = cloneArticle(article)
	repository.slugs[article.Slug] = article.ID
	return cloneArticle(article), nil
}

func (repository *ArticleMetadataRepository) Get(ctx context.Context, id string) (articles.Article, error) {
	if err := ctx.Err(); err != nil {
		return articles.Article{}, err
	}
	repository.mu.RLock()
	defer repository.mu.RUnlock()
	article, exists := repository.articles[id]
	if !exists {
		return articles.Article{}, articles.ErrNotFound
	}
	return cloneArticle(article), nil
}

func (repository *ArticleMetadataRepository) GetBySlug(ctx context.Context, slug string) (articles.Article, error) {
	if err := ctx.Err(); err != nil {
		return articles.Article{}, err
	}
	repository.mu.RLock()
	defer repository.mu.RUnlock()
	id, exists := repository.slugs[slug]
	if !exists {
		return articles.Article{}, articles.ErrNotFound
	}
	return cloneArticle(repository.articles[id]), nil
}

func (repository *ArticleMetadataRepository) GetPublishedBySlug(ctx context.Context, slug string) (articles.Article, error) {
	if err := ctx.Err(); err != nil {
		return articles.Article{}, err
	}
	repository.mu.RLock()
	defer repository.mu.RUnlock()
	for _, article := range repository.articles {
		if article.Published == nil {
			continue
		}
		if article.Published.Slug == slug || slices.Contains(article.Published.HistoricalSlugs, slug) {
			return cloneArticle(article), nil
		}
	}
	return articles.Article{}, articles.ErrNotFound
}

func (repository *ArticleMetadataRepository) List(ctx context.Context, options articles.ListOptions) (articles.ArticlePage, error) {
	if err := ctx.Err(); err != nil {
		return articles.ArticlePage{}, err
	}
	limit, err := boundedLimit(options.Limit)
	if err != nil {
		return articles.ArticlePage{}, fmt.Errorf("%w: %v", articles.ErrValidation, err)
	}

	repository.mu.RLock()
	items := make([]articles.Article, 0, len(repository.articles))
	for _, article := range repository.articles {
		if options.Status == nil || article.Status == *options.Status {
			items = append(items, cloneArticle(article))
		}
	}
	repository.mu.RUnlock()
	sort.Slice(items, func(left, right int) bool {
		if items[left].CreatedAt.Equal(items[right].CreatedAt) {
			return items[left].ID > items[right].ID
		}
		return items[left].CreatedAt.After(items[right].CreatedAt)
	})

	start, err := articleCursorStart(items, options.Cursor)
	if err != nil {
		return articles.ArticlePage{}, fmt.Errorf("%w: invalid cursor", articles.ErrValidation)
	}
	end := min(start+limit, len(items))
	page := articles.ArticlePage{Items: items[start:end], PageNumber: start/limit + 1}
	if end < len(items) {
		last := page.Items[len(page.Items)-1]
		page.NextCursor = encodeCursor(last.CreatedAt, last.ID)
	}
	return page, nil
}

func (repository *ArticleMetadataRepository) Update(ctx context.Context, article articles.Article, expectedETag string) (articles.Article, error) {
	if err := ctx.Err(); err != nil {
		return articles.Article{}, err
	}
	if err := article.Validate(); err != nil {
		return articles.Article{}, err
	}

	repository.mu.Lock()
	defer repository.mu.Unlock()
	stored, exists := repository.articles[article.ID]
	if !exists {
		return articles.Article{}, articles.ErrNotFound
	}
	if stored.ETag != expectedETag {
		return articles.Article{}, articles.ErrConflict
	}
	if err := validatePermanentPublishedSlugs(stored, article); err != nil {
		return articles.Article{}, err
	}
	for _, slug := range articleSlugs(article) {
		if repository.slugOwnedByOther(slug, article.ID) {
			return articles.Article{}, articles.ErrSlugTaken
		}
	}
	if stored.Slug != article.Slug {
		delete(repository.slugs, stored.Slug)
		repository.slugs[article.Slug] = article.ID
	}
	article.ETag = repository.newETag()
	repository.articles[article.ID] = cloneArticle(article)
	return cloneArticle(article), nil
}

func validatePermanentPublishedSlugs(stored, updated articles.Article) error {
	if stored.Published == nil {
		return nil
	}
	if slices.Contains(stored.Published.HistoricalSlugs, updated.Slug) {
		return articles.ErrSlugTaken
	}
	if updated.Published == nil {
		return fmt.Errorf("%w: published aliases cannot be removed", articles.ErrValidation)
	}
	if slices.Contains(stored.Published.HistoricalSlugs, updated.Published.Slug) {
		return articles.ErrSlugTaken
	}
	for _, slug := range stored.Published.HistoricalSlugs {
		if !slices.Contains(updated.Published.HistoricalSlugs, slug) {
			return fmt.Errorf("%w: historical published aliases cannot be removed", articles.ErrValidation)
		}
	}
	if stored.Published.Slug != updated.Published.Slug && !slices.Contains(updated.Published.HistoricalSlugs, stored.Published.Slug) {
		return fmt.Errorf("%w: replaced canonical slug must remain a historical alias", articles.ErrValidation)
	}
	return nil
}

func (repository *ArticleMetadataRepository) newETag() string {
	repository.nextETag++
	return base64.RawURLEncoding.EncodeToString([]byte(strconv.FormatUint(repository.nextETag, 10)))
}

func (repository *ArticleMetadataRepository) slugOwnedByOther(slug, articleID string) bool {
	for id, stored := range repository.articles {
		if id == articleID {
			continue
		}
		if stored.Slug == slug || stored.Published != nil && (stored.Published.Slug == slug || slices.Contains(stored.Published.HistoricalSlugs, slug)) {
			return true
		}
	}
	return false
}

func articleSlugs(article articles.Article) []string {
	slugs := []string{article.Slug}
	if article.Published != nil {
		slugs = append(slugs, article.Published.Slug)
		slugs = append(slugs, article.Published.HistoricalSlugs...)
	}
	return slugs
}

func articleCursorStart(items []articles.Article, encoded string) (int, error) {
	if encoded == "" {
		return 0, nil
	}
	cursor, err := decodeCursor(encoded)
	if err != nil {
		return 0, err
	}
	for index, item := range items {
		if item.ID == cursor.ID && item.CreatedAt.Equal(cursor.CreatedAt) {
			return index + 1, nil
		}
	}
	return 0, articles.ErrValidation
}

func cloneArticle(article articles.Article) articles.Article {
	article.DraftBody = cloneBodyRef(article.DraftBody)
	article.PublishedBody = cloneBodyRef(article.PublishedBody)
	article.Published = clonePublishedMetadata(article.Published)
	article.FirstPublishedAt = cloneTime(article.FirstPublishedAt)
	article.LastPublishedAt = cloneTime(article.LastPublishedAt)
	article.DeletedAt = cloneTime(article.DeletedAt)
	return article
}

func clonePublishedMetadata(metadata *articles.PublishedMetadata) *articles.PublishedMetadata {
	if metadata == nil {
		return nil
	}
	clone := *metadata
	clone.HistoricalSlugs = append([]string(nil), metadata.HistoricalSlugs...)
	return &clone
}

func cloneBodyRef(ref *articles.BodyRef) *articles.BodyRef {
	if ref == nil {
		return nil
	}
	copy := *ref
	return &copy
}
