package articles

import (
	"context"
	"fmt"
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
}

type service struct {
	repository MetadataRepository
	bodies     BodyStore
	clock      Clock
	ids        IDGenerator
}

func NewService(repository MetadataRepository, bodies BodyStore, clock Clock, ids IDGenerator) ArticleService {
	return &service{repository: repository, bodies: bodies, clock: clock, ids: ids}
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
		_ = service.bodies.Delete(ctx, ref)
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
	if stored.FirstPublishedAt != nil && normalized.Slug != stored.Slug {
		return Article{}, fmt.Errorf("%w: published slug cannot change", ErrValidation)
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
		_ = service.bodies.Delete(ctx, newRef)
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
	now := service.clock.Now()
	updated := stored
	updated.Status = StatusPublished
	updated.PublishedBody = cloneBodyReference(stored.DraftBody)
	if updated.FirstPublishedAt == nil {
		updated.FirstPublishedAt = &now
	}
	updated.LastPublishedAt = &now
	updated.UpdatedAt = now
	return service.repository.Update(ctx, updated, expectedETag)
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
	stored.Status = StatusWithdrawn
	stored.UpdatedAt = now
	return service.repository.Update(ctx, stored, expectedETag)
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
