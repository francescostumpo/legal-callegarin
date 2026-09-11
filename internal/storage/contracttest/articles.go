package contracttest

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/francescostumpo/legal-callegarin/internal/articles"
)

func ArticleMetadataRepository(t *testing.T, factory func() articles.MetadataRepository) {
	t.Helper()

	t.Run("CRUD slug uniqueness and optimistic concurrency", func(t *testing.T) {
		repository := factory()
		ctx := context.Background()
		created, err := repository.Create(ctx, articleFixture("article-1", "primo-articolo", time.Date(2026, 9, 11, 10, 0, 0, 0, time.UTC)))
		if err != nil {
			t.Fatalf("Create() error = %v", err)
		}
		if created.ETag == "" {
			t.Fatal("Create() ETag is empty")
		}

		bySlug, err := repository.GetBySlug(ctx, created.Slug)
		if err != nil || bySlug.ID != created.ID {
			t.Fatalf("GetBySlug() = %#v, %v", bySlug, err)
		}

		duplicate := articleFixture("article-2", created.Slug, created.CreatedAt.Add(time.Minute))
		if _, err := repository.Create(ctx, duplicate); !errors.Is(err, articles.ErrSlugTaken) {
			t.Fatalf("duplicate Create() error = %v, want ErrSlugTaken", err)
		}

		updatedInput := created
		updatedInput.Title = "Titolo aggiornato"
		updated, err := repository.Update(ctx, updatedInput, created.ETag)
		if err != nil {
			t.Fatalf("Update() error = %v", err)
		}
		if updated.ETag == created.ETag {
			t.Fatal("Update() did not change ETag")
		}
		if _, err := repository.Update(ctx, updatedInput, created.ETag); !errors.Is(err, articles.ErrConflict) {
			t.Fatalf("stale Update() error = %v, want ErrConflict", err)
		}

		updated.DraftBody.BlobName = "mutated"
		stored, err := repository.Get(ctx, created.ID)
		if err != nil {
			t.Fatalf("Get() error = %v", err)
		}
		if stored.DraftBody.BlobName == "mutated" {
			t.Fatal("repository exposed stored BodyRef pointer")
		}
	})

	t.Run("pagination filtering and cursor validation", func(t *testing.T) {
		repository := factory()
		ctx := context.Background()
		base := time.Date(2026, 9, 11, 10, 0, 0, 0, time.UTC)
		for index, fixture := range []struct{ id, slug string }{{"older", "older-article"}, {"middle", "middle-article"}, {"newer", "newer-article"}} {
			if _, err := repository.Create(ctx, articleFixture(fixture.id, fixture.slug, base.Add(time.Duration(index)*time.Minute))); err != nil {
				t.Fatalf("Create(%q) error = %v", fixture.id, err)
			}
		}

		first, err := repository.List(ctx, articles.ListOptions{Limit: 2})
		if err != nil {
			t.Fatalf("first List() error = %v", err)
		}
		if len(first.Items) != 2 || first.Items[0].ID != "newer" || first.Items[1].ID != "middle" || first.NextCursor == "" {
			t.Fatalf("first List() = %#v", first)
		}
		second, err := repository.List(ctx, articles.ListOptions{Limit: 2, Cursor: first.NextCursor})
		if err != nil || len(second.Items) != 1 || second.Items[0].ID != "older" || second.NextCursor != "" {
			t.Fatalf("second List() = %#v, %v", second, err)
		}
		if _, err := repository.List(ctx, articles.ListOptions{Cursor: "not-a-cursor"}); !errors.Is(err, articles.ErrValidation) {
			t.Fatalf("invalid cursor error = %v, want ErrValidation", err)
		}
		trailingGarbage := base64.RawURLEncoding.EncodeToString([]byte(`{"createdAt":"2026-09-11T10:02:00Z","id":"newer"}x`))
		if _, err := repository.List(ctx, articles.ListOptions{Cursor: trailingGarbage}); !errors.Is(err, articles.ErrValidation) {
			t.Fatalf("cursor with trailing garbage error = %v, want ErrValidation", err)
		}

		published := articleFixture("published", "published-article", base.Add(4*time.Minute))
		published.Status = articles.StatusPublished
		published.PublishedBody = published.DraftBody
		published.Published = &articles.PublishedMetadata{
			Slug: published.Slug, Title: published.Title, Summary: published.Summary,
			Area: published.Area, CoverID: published.CoverID,
		}
		firstPublished := published.CreatedAt
		published.FirstPublishedAt = &firstPublished
		published.LastPublishedAt = &firstPublished
		if _, err := repository.Create(ctx, published); err != nil {
			t.Fatalf("Create(published) error = %v", err)
		}
		status := articles.StatusPublished
		page, err := repository.List(ctx, articles.ListOptions{Status: &status})
		if err != nil || len(page.Items) != 1 || page.Items[0].ID != "published" {
			t.Fatalf("filtered List() = %#v, %v", page, err)
		}
	})

	articlePaginationLimits(t, factory)

	t.Run("published canonical and historical slugs remain reserved", func(t *testing.T) {
		repository := factory()
		ctx := context.Background()
		createdAt := time.Date(2026, 9, 11, 10, 0, 0, 0, time.UTC)
		published := articleFixture("published-aliases", "current-draft-slug", createdAt)
		published.Status = articles.StatusPublished
		published.PublishedBody = published.DraftBody
		published.Published = &articles.PublishedMetadata{
			Slug: "public-canonical", Title: published.Title, Summary: published.Summary,
			Area: published.Area, CoverID: published.CoverID, HistoricalSlugs: []string{"public-historical"},
		}
		publishedAt := createdAt
		published.FirstPublishedAt = &publishedAt
		published.LastPublishedAt = &publishedAt
		created, err := repository.Create(ctx, published)
		if err != nil {
			t.Fatalf("Create(published aliases) error = %v", err)
		}
		for index, slug := range []string{"public-canonical", "public-historical"} {
			conflict := articleFixture(fmt.Sprintf("conflict-%d", index), slug, createdAt.Add(time.Duration(index+1)*time.Minute))
			if _, err := repository.Create(ctx, conflict); !errors.Is(err, articles.ErrSlugTaken) {
				t.Fatalf("Create(%q) error = %v, want ErrSlugTaken", slug, err)
			}
		}

		reuse := created
		reuse.Slug = "public-historical"
		reuse.UpdatedAt = reuse.UpdatedAt.Add(time.Minute)
		if _, err := repository.Update(ctx, reuse, created.ETag); !errors.Is(err, articles.ErrSlugTaken) {
			t.Fatalf("Update(same-owner historical slug) error = %v, want ErrSlugTaken", err)
		}

		removeAlias := created
		metadata := *created.Published
		metadata.HistoricalSlugs = nil
		removeAlias.Published = &metadata
		removeAlias.UpdatedAt = removeAlias.UpdatedAt.Add(time.Minute)
		if _, err := repository.Update(ctx, removeAlias, created.ETag); !errors.Is(err, articles.ErrValidation) {
			t.Fatalf("Update(remove historical alias) error = %v, want ErrValidation", err)
		}
	})
}

func articlePaginationLimits(t *testing.T, factory func() articles.MetadataRepository) {
	t.Helper()

	newPopulatedRepository := func(t *testing.T) articles.MetadataRepository {
		t.Helper()
		repository := factory()
		ctx := context.Background()
		base := time.Date(2026, 9, 11, 10, 0, 0, 0, time.UTC)
		for index := range 105 {
			id := fmt.Sprintf("limit-%03d", index)
			if _, err := repository.Create(ctx, articleFixture(id, id, base.Add(time.Duration(index)*time.Second))); err != nil {
				t.Fatalf("Create(%q) error = %v", id, err)
			}
		}
		return repository
	}

	t.Run("pagination limit zero defaults to 25", func(t *testing.T) {
		repository := newPopulatedRepository(t)
		ctx := context.Background()
		page, err := repository.List(ctx, articles.ListOptions{Limit: 0})
		if err != nil {
			t.Fatalf("List(Limit: 0) error = %v", err)
		}
		if len(page.Items) != 25 || page.Items[0].ID != "limit-104" || page.Items[24].ID != "limit-080" || page.NextCursor == "" {
			t.Fatalf("List(Limit: 0) = %#v", page)
		}
		next, err := repository.List(ctx, articles.ListOptions{Limit: 0, Cursor: page.NextCursor})
		if err != nil || len(next.Items) != 25 || next.Items[0].ID != "limit-079" {
			t.Fatalf("List(default limit, next cursor) = %#v, %v", next, err)
		}
	})

	t.Run("pagination limit above 100 is capped", func(t *testing.T) {
		repository := newPopulatedRepository(t)
		ctx := context.Background()
		page, err := repository.List(ctx, articles.ListOptions{Limit: 101})
		if err != nil {
			t.Fatalf("List(Limit: 101) error = %v", err)
		}
		if len(page.Items) != 100 || page.Items[0].ID != "limit-104" || page.Items[99].ID != "limit-005" || page.NextCursor == "" {
			t.Fatalf("List(Limit: 101) = %#v", page)
		}
		next, err := repository.List(ctx, articles.ListOptions{Limit: 101, Cursor: page.NextCursor})
		if err != nil || len(next.Items) != 5 || next.Items[0].ID != "limit-004" || next.Items[4].ID != "limit-000" || next.NextCursor != "" {
			t.Fatalf("List(capped limit, next cursor) = %#v, %v", next, err)
		}
	})
}

func ArticleBodyStore(t *testing.T, factory func() articles.BodyStore) {
	t.Helper()

	store := factory()
	ctx := context.Background()
	body := articles.Body{SchemaVersion: 1, Document: []byte(`{"type":"doc"}`), HTML: "<p>contenuto</p>", PlainText: "contenuto"}
	first, err := store.Put(ctx, "article-1", body)
	if err != nil {
		t.Fatalf("Put() error = %v", err)
	}
	second, err := store.Put(ctx, "article-1", body)
	if err != nil {
		t.Fatalf("second Put() error = %v", err)
	}
	if first.Version == second.Version || first.SavedAt.IsZero() {
		t.Fatalf("immutable versions = %#v and %#v", first, second)
	}

	body.Document[0] = 'x'
	stored, err := store.Get(ctx, first)
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if stored.Document[0] == 'x' {
		t.Fatal("Put() retained caller's json.RawMessage")
	}
	stored.Document[0] = 'y'
	again, err := store.Get(ctx, first)
	if err != nil || again.Document[0] == 'y' {
		t.Fatalf("Get() exposed stored json.RawMessage: %q, %v", again.Document, err)
	}
	if err := store.Delete(ctx, first); err != nil {
		t.Fatalf("Delete() error = %v", err)
	}
	if _, err := store.Get(ctx, first); !errors.Is(err, articles.ErrNotFound) {
		t.Fatalf("Get(deleted) error = %v, want ErrNotFound", err)
	}
}

func articleFixture(id, slug string, created time.Time) articles.Article {
	return articles.Article{
		ID:        id,
		Slug:      slug,
		Title:     "Titolo valido",
		Summary:   "Sommario sufficientemente lungo",
		Area:      "obbligazioni",
		CoverID:   "cover-1",
		Status:    articles.StatusDraft,
		DraftBody: &articles.BodyRef{BlobName: id, Version: "body-1", SavedAt: created},
		CreatedAt: created,
		UpdatedAt: created,
	}
}
