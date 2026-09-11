package articles

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"
)

type DraftInput struct {
	Slug    string
	Title   string
	Summary string
	Area    string
	CoverID string
	Body    Body
}

type ArticleWithBody struct {
	Article Article
	Body    Body
}

type Clock interface {
	Now() time.Time
}

type IDGenerator interface {
	NewID() string
}

type ArticleService interface {
	CreateDraft(context.Context, DraftInput) (Article, error)
	SaveDraft(context.Context, string, DraftInput, string) (Article, error)
	Publish(context.Context, string, string) (Article, error)
	Withdraw(context.Context, string, string) (Article, error)
	GetPreview(context.Context, string) (ArticleWithBody, error)
	GetPublished(context.Context, string) (ArticleWithBody, error)
	ListPublished(context.Context, ListOptions) (ArticlePage, error)
}

type ArticleEvents interface {
	PublicArticleChanged(context.Context, Article, Article)
}

type service struct {
	repository MetadataRepository
	bodies     BodyStore
	clock      Clock
	ids        IDGenerator
	events     ArticleEvents
}

func NewService(repository MetadataRepository, bodies BodyStore, clock Clock, ids IDGenerator, eventObservers ...ArticleEvents) ArticleService {
	var events ArticleEvents
	if len(eventObservers) > 0 {
		events = eventObservers[0]
	}
	return &service{repository: repository, bodies: bodies, clock: clock, ids: ids, events: events}
}

func (service *service) CreateDraft(ctx context.Context, input DraftInput) (Article, error) {
	normalized, err := normalizeDraftInput(input)
	if err != nil {
		return Article{}, err
	}
	now := service.clock.Now()
	article := Article{
		ID:        service.ids.NewID(),
		Slug:      normalized.Slug,
		Title:     normalized.Title,
		Summary:   normalized.Summary,
		Area:      normalized.Area,
		CoverID:   normalized.CoverID,
		Status:    StatusDraft,
		CreatedAt: now,
		UpdatedAt: now,
	}
	if err := article.Validate(); err != nil {
		return Article{}, err
	}

	ref, err := service.bodies.Put(ctx, article.ID, normalized.Body)
	if err != nil {
		return Article{}, err
	}
	article.DraftBody = &ref
	created, err := service.repository.Create(ctx, article)
	if err != nil {
		if !errors.Is(err, ErrCommitUnknown) {
			_ = service.bodies.Delete(ctx, ref)
		}
		return Article{}, err
	}
	return created, nil
}

func (service *service) SaveDraft(ctx context.Context, id string, input DraftInput, expectedETag string) (Article, error) {
	stored, err := service.repository.Get(ctx, id)
	if err != nil {
		return Article{}, err
	}
	normalized, err := normalizeDraftInput(input)
	if err != nil {
		return Article{}, err
	}
	if stored.Published != nil && slices.Contains(stored.Published.HistoricalSlugs, normalized.Slug) {
		return Article{}, ErrSlugTaken
	}
	newRef, err := service.bodies.Put(ctx, stored.ID, normalized.Body)
	if err != nil {
		return Article{}, err
	}
	updated := stored
	updated.Slug = normalized.Slug
	updated.Title = normalized.Title
	updated.Summary = normalized.Summary
	updated.Area = normalized.Area
	updated.CoverID = normalized.CoverID
	updated.DraftBody = &newRef
	updated.UpdatedAt = service.clock.Now()
	if err := updated.Validate(); err != nil {
		_ = service.bodies.Delete(ctx, newRef)
		return Article{}, err
	}

	result, err := service.repository.Update(ctx, updated, expectedETag)
	if err != nil {
		if !errors.Is(err, ErrCommitUnknown) {
			_ = service.bodies.Delete(ctx, newRef)
		}
		return Article{}, err
	}
	if stored.DraftBody != nil && !sameBodyRef(stored.DraftBody, stored.PublishedBody) {
		_ = service.bodies.Delete(ctx, *stored.DraftBody)
	}
	return result, nil
}

func (service *service) Publish(ctx context.Context, id, expectedETag string) (Article, error) {
	stored, err := service.repository.Get(ctx, id)
	if err != nil {
		return Article{}, err
	}
	if err := ValidateTransition(stored.Status, StatusPublished); err != nil {
		return Article{}, err
	}
	if stored.DraftBody == nil {
		return Article{}, fmt.Errorf("%w: a saved draft is required", ErrInvalidTransition)
	}
	if stored.Published != nil && slices.Contains(stored.Published.HistoricalSlugs, stored.Slug) {
		return Article{}, ErrSlugTaken
	}
	now := service.clock.Now()
	updated := stored
	updated.Status = StatusPublished
	updated.PublishedBody = cloneBodyReference(stored.DraftBody)
	updated.Published = nextPublishedMetadata(stored.Published, stored)
	if updated.FirstPublishedAt == nil {
		updated.FirstPublishedAt = &now
	}
	updated.LastPublishedAt = &now
	updated.UpdatedAt = now
	result, err := service.repository.Update(ctx, updated, expectedETag)
	if err != nil {
		return Article{}, err
	}
	service.notifyPublicChange(ctx, stored, result)
	return result, nil
}

func (service *service) Withdraw(ctx context.Context, id, expectedETag string) (Article, error) {
	stored, err := service.repository.Get(ctx, id)
	if err != nil {
		return Article{}, err
	}
	if stored.Status != StatusPublished {
		return Article{}, fmt.Errorf("%w: only a published article can be withdrawn", ErrInvalidTransition)
	}
	now := service.clock.Now()
	before := stored
	stored.Status = StatusWithdrawn
	stored.UpdatedAt = now
	result, err := service.repository.Update(ctx, stored, expectedETag)
	if err != nil {
		return Article{}, err
	}
	service.notifyPublicChange(ctx, before, result)
	return result, nil
}

func (service *service) GetPreview(ctx context.Context, id string) (ArticleWithBody, error) {
	article, err := service.repository.Get(ctx, id)
	if err != nil {
		return ArticleWithBody{}, err
	}
	if article.DraftBody == nil {
		return ArticleWithBody{}, fmt.Errorf("%w: no saved draft", ErrNotFound)
	}
	body, err := service.bodies.Get(ctx, *article.DraftBody)
	if err != nil {
		return ArticleWithBody{}, err
	}
	return ArticleWithBody{Article: article, Body: body}, nil
}

func (service *service) GetPublished(ctx context.Context, slug string) (ArticleWithBody, error) {
	article, err := service.repository.GetPublishedBySlug(ctx, slug)
	if err != nil {
		return ArticleWithBody{}, err
	}
	if article.Status != StatusPublished || article.Published == nil || article.PublishedBody == nil {
		return ArticleWithBody{}, ErrNotFound
	}
	body, err := service.bodies.Get(ctx, *article.PublishedBody)
	if err != nil {
		return ArticleWithBody{}, err
	}
	return ArticleWithBody{Article: publishedProjection(article), Body: body}, nil
}

func (service *service) ListPublished(ctx context.Context, options ListOptions) (ArticlePage, error) {
	status := StatusPublished
	options.Status = &status
	page, err := service.repository.List(ctx, options)
	if err != nil {
		return ArticlePage{}, err
	}
	for index, article := range page.Items {
		if article.Published == nil || article.PublishedBody == nil {
			return ArticlePage{}, fmt.Errorf("%w: published article lacks public snapshot", ErrValidation)
		}
		page.Items[index] = publishedProjection(article)
	}
	return page, nil
}

func normalizeDraftInput(input DraftInput) (DraftInput, error) {
	slug, err := NormalizeSlug(input.Slug)
	if err != nil {
		return DraftInput{}, err
	}
	input.Slug = slug
	input.Title = strings.TrimSpace(input.Title)
	input.Summary = strings.TrimSpace(input.Summary)
	input.Area = strings.TrimSpace(input.Area)
	input.CoverID = strings.TrimSpace(input.CoverID)
	if err := input.Body.Validate(); err != nil {
		return DraftInput{}, err
	}
	return input, nil
}

func sameBodyRef(left, right *BodyRef) bool {
	return left != nil && right != nil && *left == *right
}

func cloneBodyReference(ref *BodyRef) *BodyRef {
	if ref == nil {
		return nil
	}
	copy := *ref
	return &copy
}

func nextPublishedMetadata(previous *PublishedMetadata, draft Article) *PublishedMetadata {
	history := make([]string, 0)
	seen := make(map[string]bool)
	if previous != nil {
		for _, slug := range previous.HistoricalSlugs {
			if !seen[slug] {
				history = append(history, slug)
				seen[slug] = true
			}
		}
		if previous.Slug != draft.Slug && !seen[previous.Slug] {
			history = append(history, previous.Slug)
		}
	}
	return &PublishedMetadata{
		Slug: draft.Slug, Title: draft.Title, Summary: draft.Summary,
		Area: draft.Area, CoverID: draft.CoverID, HistoricalSlugs: history,
	}
}

func publishedProjection(article Article) Article {
	published := article.Published
	article.Slug = published.Slug
	article.Title = published.Title
	article.Summary = published.Summary
	article.Area = published.Area
	article.CoverID = published.CoverID
	article.DraftBody = nil
	if article.LastPublishedAt != nil {
		article.UpdatedAt = *article.LastPublishedAt
	}
	return article
}

func (service *service) notifyPublicChange(ctx context.Context, before, after Article) {
	if service.events != nil {
		service.events.PublicArticleChanged(ctx, before, after)
	}
}
