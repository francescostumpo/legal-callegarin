package azure

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"sort"

	"github.com/francescostumpo/legal-callegarin/internal/articles"
)

const maxArticleScan = 1000

type ArticleMetadataRepository struct{ table tableDriver }

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
	if _, err = repository.table.Get(ctx, articlesPartition, article.ID); err == nil {
		return articles.Article{}, articles.ErrConflict
	} else if !errors.Is(err, ErrNotFound) {
		return articles.Article{}, mapArticleError(err, false)
	}
	actions := []tableAction{{Kind: tableAdd, PartitionKey: articlesPartition, RowKey: article.ID, Entity: encoded}}
	for _, record := range slugRecords(article) {
		value, marshalErr := marshalSlugEntity(record)
		if marshalErr != nil {
			return articles.Article{}, marshalErr
		}
		actions = append(actions, tableAction{Kind: tableAdd, PartitionKey: articlesPartition, RowKey: slugRowKey(record.Slug), Entity: value})
	}
	if err = submitTransaction(ctx, repository.table, actions); err != nil {
		if errors.Is(err, ErrConflict) {
			if _, getErr := repository.table.Get(ctx, articlesPartition, article.ID); getErr == nil {
				return articles.Article{}, articles.ErrConflict
			}
			return articles.Article{}, articles.ErrSlugTaken
		}
		if mutationOutcomeMayBeUnknown(err) {
			return repository.reconcileCreate(ctx, article, err)
		}
		return articles.Article{}, mapArticleError(err, false)
	}
	return repository.Get(ctx, article.ID)
}

func (repository *ArticleMetadataRepository) Get(ctx context.Context, id string) (articles.Article, error) {
	entity, err := repository.table.Get(ctx, articlesPartition, id)
	if err != nil {
		return articles.Article{}, mapArticleError(err, false)
	}
	return unmarshalArticleEntity(entity.Value, entity.ETag)
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
	entities, err := repository.table.List(ctx, "PartitionKey eq 'articles'", maxArticleScan+1)
	if err != nil {
		return articles.ArticlePage{}, mapArticleError(err, false)
	}
	if len(entities) > maxArticleScan {
		return articles.ArticlePage{}, fmt.Errorf("%w: active article set exceeds %d", articles.ErrValidation, maxArticleScan)
	}
	items := make([]articles.Article, 0, len(entities))
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
		if options.Status == nil || article.Status == *options.Status {
			items = append(items, article)
		}
	}
	sort.Slice(items, func(i, j int) bool {
		if items[i].CreatedAt.Equal(items[j].CreatedAt) {
			return items[i].ID > items[j].ID
		}
		return items[i].CreatedAt.After(items[j].CreatedAt)
	})
	start, err := cursorStartArticles(items, options.Cursor)
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
	if err := article.Validate(); err != nil {
		return articles.Article{}, err
	}
	stored, err := repository.Get(ctx, article.ID)
	if err != nil {
		return articles.Article{}, err
	}
	if stored.ETag != expectedETag {
		return articles.Article{}, articles.ErrConflict
	}
	if err := validatePermanentPublishedSlugs(stored, article); err != nil {
		return articles.Article{}, err
	}
	encoded, err := marshalArticleEntity(article)
	if err != nil {
		return articles.Article{}, err
	}
	actions := []tableAction{{Kind: tableReplace, PartitionKey: articlesPartition, RowKey: article.ID, Entity: encoded, ETag: expectedETag}}
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
	return repository.Get(ctx, article.ID)
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
func cursorStartArticles(items []articles.Article, value string) (int, error) {
	if value == "" {
		return 0, nil
	}
	cursor, err := decodeCursor(value)
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
