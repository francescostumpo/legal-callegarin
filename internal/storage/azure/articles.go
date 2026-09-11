package azure

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"sort"
	"strings"

	"github.com/francescostumpo/legal-callegarin/internal/articles"
)

type ArticleMetadataRepository struct{ table tableDriver }

const articleRawPageSize = 100

func newArticleMetadataRepository(table tableDriver) *ArticleMetadataRepository {
	return &ArticleMetadataRepository{table: table}
}

func (repository *ArticleMetadataRepository) Create(ctx context.Context, article articles.Article) (articles.Article, error) {
	if err := article.Validate(); err != nil {
		return articles.Article{}, err
	}
	encoded, err := marshalArticleEntity(article)
	if err != nil {
		return articles.Article{}, err
	}
	if _, err = repository.Get(ctx, article.ID); err == nil {
		return articles.Article{}, articles.ErrConflict
	} else if !errors.Is(err, articles.ErrNotFound) {
		return articles.Article{}, mapArticleError(err, false)
	}
	rowKey, _ := articleRowKey(article.ID, article.CreatedAt)
	actions := []tableAction{{Kind: tableAdd, PartitionKey: articlesPartition, RowKey: rowKey, Entity: encoded}}
	for _, record := range slugRecords(article) {
		value, marshalErr := marshalSlugEntity(record)
		if marshalErr != nil {
			return articles.Article{}, marshalErr
		}
		actions = append(actions, tableAction{Kind: tableAdd, PartitionKey: articlesPartition, RowKey: slugRowKey(record.Slug), Entity: value})
	}
	if err = submitTransaction(ctx, repository.table, actions); err != nil {
		if errors.Is(err, ErrConflict) {
			if _, getErr := repository.Get(ctx, article.ID); getErr == nil {
				return articles.Article{}, articles.ErrConflict
			}
			return articles.Article{}, articles.ErrSlugTaken
		}
		if mutationOutcomeMayBeUnknown(err) {
			return repository.reconcileCreate(ctx, article, err)
		}
		return articles.Article{}, mapArticleError(err, false)
	}
	return repository.refreshCommitted(ctx, article, nil)
}

func (repository *ArticleMetadataRepository) Get(ctx context.Context, id string) (articles.Article, error) {
	article, _, err := repository.getWithRowKey(ctx, id)
	return article, err
}

func (repository *ArticleMetadataRepository) getWithRowKey(ctx context.Context, id string) (articles.Article, string, error) {
	if !safeStorageSegment(id) {
		return articles.Article{}, "", articles.ErrNotFound
	}
	filter := "PartitionKey eq 'articles' and id eq '" + strings.ReplaceAll(id, "'", "''") + "'"
	entities, err := repository.table.List(ctx, filter, 2)
	if err != nil {
		return articles.Article{}, "", mapArticleError(err, false)
	}
	for _, entity := range entities {
		var header entityHeader
		if decodeHeader(entity.Value, &header) == nil && header.EntityType == articleEntityType {
			article, decodeErr := unmarshalArticleEntity(entity.Value, entity.ETag)
			return article, header.RowKey, decodeErr
		}
	}
	// Read legacy v1 rows while deployments roll forward.
	entity, err := repository.table.Get(ctx, articlesPartition, id)
	if err != nil {
		return articles.Article{}, "", mapArticleError(err, false)
	}
	article, decodeErr := unmarshalArticleEntity(entity.Value, entity.ETag)
	return article, id, decodeErr
}

func (repository *ArticleMetadataRepository) GetBySlug(ctx context.Context, slug string) (articles.Article, error) {
	record, err := repository.getSlug(ctx, slug)
	if err != nil {
		return articles.Article{}, err
	}
	return repository.Get(ctx, record.ArticleID)
}

func (repository *ArticleMetadataRepository) GetPublishedBySlug(ctx context.Context, slug string) (articles.Article, error) {
	record, err := repository.getSlug(ctx, slug)
	if err != nil {
		return articles.Article{}, err
	}
	if record.PublishedTarget == "" {
		return articles.Article{}, articles.ErrNotFound
	}
	article, err := repository.Get(ctx, record.ArticleID)
	if err != nil {
		return articles.Article{}, err
	}
	if article.Published == nil {
		return articles.Article{}, articles.ErrNotFound
	}
	return article, nil
}

func (repository *ArticleMetadataRepository) getSlug(ctx context.Context, slug string) (slugRecord, error) {
	normalized, err := articles.NormalizeSlug(slug)
	if err != nil || normalized != slug {
		return slugRecord{}, articles.ErrNotFound
	}
	entity, err := repository.table.Get(ctx, articlesPartition, slugRowKey(slug))
	if err != nil {
		return slugRecord{}, mapArticleError(err, false)
	}
	return unmarshalSlugEntity(entity.Value, entity.ETag)
}

func (repository *ArticleMetadataRepository) List(ctx context.Context, options articles.ListOptions) (articles.ArticlePage, error) {
	limit, err := boundedLimit(options.Limit)
	if err != nil {
		return articles.ArticlePage{}, fmt.Errorf("%w: %v", articles.ErrValidation, err)
	}
	var cursor articlePageCursor
	if options.Cursor != "" {
		cursor, err = decodeArticlePageCursor(options.Cursor)
		if err != nil {
			return articles.ArticlePage{}, fmt.Errorf("%w: invalid cursor", articles.ErrValidation)
		}
	}
	items := make([]articles.Article, 0, limit+1)
	filter := "PartitionKey eq 'articles' and entityType eq 'article'"
	if options.Status != nil {
		filter += " and status eq '" + string(*options.Status) + "'"
	}
	if options.Cursor != "" {
		rowKey, rowErr := articleRowKey(cursor.ID, cursor.CreatedAt)
		if rowErr != nil {
			return articles.ArticlePage{}, fmt.Errorf("%w: invalid cursor", articles.ErrValidation)
		}
		filter += " and RowKey gt '" + rowKey + "'"
	}
	var continuation *tableContinuation
	more := false
	for len(items) < limit {
		top := int32(min(limit-len(items), articleRawPageSize))
		entities, next, listErr := repository.table.ListPage(ctx, filter, top, continuation)
		if listErr != nil {
			return articles.ArticlePage{}, mapArticleError(listErr, false)
		}
		if next != nil && continuation != nil && *next == *continuation {
			return articles.ArticlePage{}, errors.New("article storage continuation did not advance")
		}
		for _, entity := range entities {
			var header entityHeader
			if err := decodeHeader(entity.Value, &header); err != nil {
				return articles.ArticlePage{}, err
			}
			if header.EntityType != articleEntityType {
				continue
			}
			article, err := unmarshalArticleEntity(entity.Value, entity.ETag)
			if err != nil {
				return articles.ArticlePage{}, err
			}
			wantedRowKey, keyErr := articleRowKey(article.ID, article.CreatedAt)
			if keyErr != nil || header.RowKey != wantedRowKey {
				return articles.ArticlePage{}, errors.New("legacy article rows require explicit migration before listing")
			}
			items = append(items, article)
		}
		if next == nil {
			break
		}
		if len(items) == limit {
			more = true
			break
		}
		continuation = next
	}
	page := articles.ArticlePage{Items: items, PageNumber: cursor.Page + 1}
	if more {
		last := page.Items[len(page.Items)-1]
		page.NextCursor = encodeArticlePageCursor(articlePageCursor{Page: cursor.Page + 1, CreatedAt: last.CreatedAt, ID: last.ID})
	}
	return page, nil
}

func (repository *ArticleMetadataRepository) Update(ctx context.Context, article articles.Article, expectedETag string) (articles.Article, error) {
	if err := article.Validate(); err != nil {
		return articles.Article{}, err
	}
	stored, storedRowKey, err := repository.getWithRowKey(ctx, article.ID)
	if err != nil {
		return articles.Article{}, err
	}
	if stored.ETag != expectedETag {
		return articles.Article{}, articles.ErrConflict
	}
	if err := validatePermanentPublishedSlugs(stored, article); err != nil {
		return articles.Article{}, err
	}
	encoded, err := marshalArticleEntityAtRow(article, storedRowKey)
	if err != nil {
		return articles.Article{}, err
	}
	actions := []tableAction{{Kind: tableReplace, PartitionKey: articlesPartition, RowKey: storedRowKey, Entity: encoded, ETag: expectedETag}}
	desired := slugRecordMap(article)
	old := slugRecordMap(stored)
	for slug, record := range desired {
		existing, getErr := repository.getSlug(ctx, slug)
		switch {
		case getErr == nil:
			if existing.ArticleID != article.ID {
				return articles.Article{}, articles.ErrSlugTaken
			}
			if slug == article.Slug && existing.PublishedTarget != "" && slug != publishedCanonical(article) {
				return articles.Article{}, articles.ErrSlugTaken
			}
			if existing.PublishedTarget != record.PublishedTarget {
				value, _ := marshalSlugEntity(record)
				actions = append(actions, tableAction{Kind: tableReplace, PartitionKey: articlesPartition, RowKey: slugRowKey(slug), Entity: value, ETag: existing.ETag})
			}
		case errors.Is(getErr, articles.ErrNotFound):
			value, _ := marshalSlugEntity(record)
			actions = append(actions, tableAction{Kind: tableAdd, PartitionKey: articlesPartition, RowKey: slugRowKey(slug), Entity: value})
		default:
			return articles.Article{}, getErr
		}
	}
	for slug := range old {
		if _, keep := desired[slug]; keep {
			continue
		}
		existing, getErr := repository.getSlug(ctx, slug)
		if getErr != nil {
			return articles.Article{}, getErr
		}
		value, _ := marshalSlugEntity(existing)
		actions = append(actions, tableAction{Kind: tableDelete, PartitionKey: articlesPartition, RowKey: slugRowKey(slug), Entity: value, ETag: existing.ETag})
	}
	if err = submitTransaction(ctx, repository.table, actions); err != nil {
		if errors.Is(err, ErrConflict) {
			return articles.Article{}, articles.ErrSlugTaken
		}
		if mutationOutcomeMayBeUnknown(err) {
			return repository.reconcileUpdate(ctx, stored, article, err)
		}
		return articles.Article{}, mapArticleError(err, false)
	}
	return repository.refreshCommitted(ctx, article, &stored)
}

func (repository *ArticleMetadataRepository) refreshCommitted(ctx context.Context, desired articles.Article, alternate *articles.Article) (articles.Article, error) {
	observed, err := repository.Get(ctx, desired.ID)
	if err != nil {
		return articles.Article{}, unknownCommitForContext(ctx, articles.ErrCommitUnknown, err)
	}
	if !sameArticleState(observed, desired) {
		return articles.Article{}, unknownCommitForContext(ctx, articles.ErrCommitUnknown, errors.New("committed article refresh did not match expected state"))
	}
	var alternateSlugs map[string]slugRecord
	if alternate != nil {
		alternateSlugs = slugRecordMap(*alternate)
	}
	ok, err := repository.slugStateMatches(ctx, slugRecordMap(desired), alternateSlugs)
	if err != nil {
		return articles.Article{}, unknownCommitForContext(ctx, articles.ErrCommitUnknown, err)
	}
	if !ok {
		return articles.Article{}, unknownCommitForContext(ctx, articles.ErrCommitUnknown, errors.New("committed article slug refresh did not match expected state"))
	}
	return observed, nil
}

func (repository *ArticleMetadataRepository) reconcileCreate(ctx context.Context, desired articles.Article, cause error) (articles.Article, error) {
	observed, err := repository.Get(ctx, desired.ID)
	if errors.Is(err, articles.ErrNotFound) {
		return articles.Article{}, mapArticleError(cause, false)
	}
	if err != nil || !sameArticleState(observed, desired) {
		return articles.Article{}, unknownCommitForContext(ctx, articles.ErrCommitUnknown, cause)
	}
	if ok, verifyErr := repository.slugStateMatches(ctx, slugRecordMap(desired), nil); verifyErr != nil || !ok {
		return articles.Article{}, unknownCommitForContext(ctx, articles.ErrCommitUnknown, cause)
	}
	return observed, nil
}

func (repository *ArticleMetadataRepository) reconcileUpdate(ctx context.Context, prior, desired articles.Article, cause error) (articles.Article, error) {
	observed, err := repository.Get(ctx, desired.ID)
	if err != nil {
		return articles.Article{}, unknownCommitForContext(ctx, articles.ErrCommitUnknown, cause)
	}
	desiredSlugs, priorSlugs := slugRecordMap(desired), slugRecordMap(prior)
	if sameArticleState(observed, desired) {
		if ok, verifyErr := repository.slugStateMatches(ctx, desiredSlugs, priorSlugs); verifyErr == nil && ok {
			return observed, nil
		}
		return articles.Article{}, unknownCommitForContext(ctx, articles.ErrCommitUnknown, cause)
	}
	if sameArticleState(observed, prior) {
		if ok, verifyErr := repository.slugStateMatches(ctx, priorSlugs, desiredSlugs); verifyErr == nil && ok {
			return articles.Article{}, mapArticleError(cause, false)
		}
	}
	return articles.Article{}, unknownCommitForContext(ctx, articles.ErrCommitUnknown, cause)
}

// slugStateMatches verifies wanted records and verifies that records present only
// in the alternate state are absent. ETags are intentionally ignored.
func (repository *ArticleMetadataRepository) slugStateMatches(ctx context.Context, wanted, alternate map[string]slugRecord) (bool, error) {
	for slug, expected := range wanted {
		entity, err := repository.table.Get(ctx, articlesPartition, slugRowKey(slug))
		if err != nil {
			return false, err
		}
		observed, err := unmarshalSlugEntity(entity.Value, entity.ETag)
		if err != nil || observed.Slug != expected.Slug || observed.ArticleID != expected.ArticleID || observed.PublishedTarget != expected.PublishedTarget {
			return false, err
		}
	}
	for slug := range alternate {
		if _, keep := wanted[slug]; keep {
			continue
		}
		if _, err := repository.table.Get(ctx, articlesPartition, slugRowKey(slug)); !errors.Is(err, ErrNotFound) {
			return false, err
		}
	}
	return true, nil
}

func slugRecords(article articles.Article) []slugRecord {
	records := slugRecordMap(article)
	keys := make([]string, 0, len(records))
	for key := range records {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	out := make([]slugRecord, 0, len(keys))
	for _, key := range keys {
		out = append(out, records[key])
	}
	return out
}
func slugRecordMap(article articles.Article) map[string]slugRecord {
	result := map[string]slugRecord{}
	canonical := publishedCanonical(article)
	result[article.Slug] = slugRecord{Slug: article.Slug, ArticleID: article.ID}
	if article.Published != nil {
		for _, slug := range append([]string{article.Published.Slug}, article.Published.HistoricalSlugs...) {
			result[slug] = slugRecord{Slug: slug, ArticleID: article.ID, PublishedTarget: canonical}
		}
	}
	return result
}
func publishedCanonical(article articles.Article) string {
	if article.Published == nil {
		return ""
	}
	return article.Published.Slug
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
func decodeHeader(value []byte, header *entityHeader) error { return json.Unmarshal(value, header) }
func mapArticleError(err error, slug bool) error {
	switch {
	case errors.Is(err, ErrBatchInvalid):
		return fmt.Errorf("%w: %v", articles.ErrValidation, err)
	case errors.Is(err, ErrNotFound):
		return articles.ErrNotFound
	case errors.Is(err, ErrPrecondition):
		return articles.ErrConflict
	case errors.Is(err, ErrConflict) && slug:
		return articles.ErrSlugTaken
	case errors.Is(err, ErrConflict):
		return articles.ErrConflict
	default:
		return err
	}
}
