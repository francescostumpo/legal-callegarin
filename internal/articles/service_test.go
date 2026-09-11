package articles_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/francescostumpo/legal-callegarin/internal/articles"
	"github.com/francescostumpo/legal-callegarin/internal/storage/memory"
)

func TestArticleServicePublishedBodySurvivesLaterDraft(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	clock := &articleClock{now: time.Date(2026, 9, 11, 10, 0, 0, 0, time.UTC)}
	repository := memory.NewArticleMetadataRepository()
	bodies := memory.NewArticleBodyStore(clock.Now)
	service := articles.NewService(repository, bodies, clock, &articleIDs{values: []string{"article-1"}})

	created, err := service.CreateDraft(ctx, articleDraft("  DIRITTO -- civile ", "prima"))
	if err != nil {
		t.Fatalf("CreateDraft() error = %v", err)
	}
	if created.Slug != "diritto-civile" || created.Status != articles.StatusDraft || created.DraftBody == nil {
		t.Fatalf("CreateDraft() = %#v", created)
	}

	clock.now = clock.now.Add(time.Hour)
	published, err := service.Publish(ctx, created.ID, created.ETag)
	if err != nil {
		t.Fatalf("Publish() error = %v", err)
	}
	liveRef := *published.PublishedBody
	if published.Status != articles.StatusPublished || published.FirstPublishedAt == nil || !published.FirstPublishedAt.Equal(clock.now) {
		t.Fatalf("Publish() = %#v", published)
	}

	clock.now = clock.now.Add(time.Hour)
	saved, err := service.SaveDraft(ctx, published.ID, articleDraft(published.Slug, "seconda"), published.ETag)
	if err != nil {
		t.Fatalf("SaveDraft() error = %v", err)
	}
	if saved.Status != articles.StatusPublished || saved.PublishedBody == nil || *saved.PublishedBody != liveRef || saved.DraftBody == nil || *saved.DraftBody == liveRef {
		t.Fatalf("SaveDraft() did not preserve published body: %#v", saved)
	}
	if _, err := bodies.Get(ctx, liveRef); err != nil {
		t.Fatalf("published body was deleted: %v", err)
	}
	preview, err := service.GetPreview(ctx, saved.ID)
	if err != nil || preview.Body.PlainText != "seconda versione del contenuto" {
		t.Fatalf("GetPreview() = %#v, %v", preview, err)
	}

	clock.now = clock.now.Add(time.Hour)
	republished, err := service.Publish(ctx, saved.ID, saved.ETag)
	if err != nil {
		t.Fatalf("replacement Publish() error = %v", err)
	}
	if republished.PublishedBody == nil || *republished.PublishedBody != *saved.DraftBody || !republished.FirstPublishedAt.Equal(*published.FirstPublishedAt) || !republished.LastPublishedAt.Equal(clock.now) {
		t.Fatalf("replacement Publish() = %#v", republished)
	}
}

func TestArticleServiceTransitionsAndDraftCleanup(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	clock := &articleClock{now: time.Date(2026, 9, 11, 10, 0, 0, 0, time.UTC)}
	repository := memory.NewArticleMetadataRepository()
	bodies := memory.NewArticleBodyStore(clock.Now)
	service := articles.NewService(repository, bodies, clock, &articleIDs{values: []string{"article-1"}})
	created, err := service.CreateDraft(ctx, articleDraft("first-slug", "prima"))
	if err != nil {
		t.Fatalf("CreateDraft() error = %v", err)
	}
	oldDraft := *created.DraftBody

	clock.now = clock.now.Add(time.Minute)
	saved, err := service.SaveDraft(ctx, created.ID, articleDraft("second-slug", "seconda"), created.ETag)
	if err != nil {
		t.Fatalf("SaveDraft() error = %v", err)
	}
	if _, err := bodies.Get(ctx, oldDraft); !errors.Is(err, articles.ErrNotFound) {
		t.Fatalf("old unreferenced draft Get() error = %v, want ErrNotFound", err)
	}
	if _, err := service.Withdraw(ctx, saved.ID, saved.ETag); !errors.Is(err, articles.ErrInvalidTransition) {
		t.Fatalf("draft Withdraw() error = %v, want ErrInvalidTransition", err)
	}

	published, err := service.Publish(ctx, saved.ID, saved.ETag)
	if err != nil {
		t.Fatalf("Publish() error = %v", err)
	}
	clock.now = clock.now.Add(time.Minute)
	withdrawn, err := service.Withdraw(ctx, published.ID, published.ETag)
	if err != nil || withdrawn.Status != articles.StatusWithdrawn {
		t.Fatalf("Withdraw() = %#v, %v", withdrawn, err)
	}
	clock.now = clock.now.Add(time.Minute)
	republished, err := service.Publish(ctx, withdrawn.ID, withdrawn.ETag)
	if err != nil || republished.Status != articles.StatusPublished {
		t.Fatalf("republish withdrawn = %#v, %v", republished, err)
	}

	if _, err := service.SaveDraft(ctx, republished.ID, articleDraft("changed-after-publication", "terza"), republished.ETag); !errors.Is(err, articles.ErrValidation) {
		t.Fatalf("published slug change error = %v, want ErrValidation", err)
	}
}

func articleDraft(slug, content string) articles.DraftInput {
	return articles.DraftInput{
		Slug:    slug,
		Title:   "Titolo valido",
		Summary: "Sommario sufficientemente lungo",
		Area:    "obbligazioni",
		CoverID: "cover-1",
		Body: articles.Body{
			SchemaVersion: 1,
			Document:      []byte(`{"type":"doc"}`),
			HTML:          "<p>" + content + " versione del contenuto</p>",
			PlainText:     content + " versione del contenuto",
		},
	}
}

type articleClock struct{ now time.Time }

func (clock *articleClock) Now() time.Time { return clock.now }

type articleIDs struct {
	values []string
	index  int
}

func (ids *articleIDs) NewID() string {
	value := ids.values[ids.index]
	ids.index++
	return value
}
