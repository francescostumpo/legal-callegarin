package articles_test

import (
	"context"
	"errors"
	"slices"
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

func TestArticleServicePublishesVersionedMetadataAndPreservesHistoricalSlug(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	clock := &articleClock{now: time.Date(2026, 9, 11, 10, 0, 0, 0, time.UTC)}
	repository := memory.NewArticleMetadataRepository()
	bodies := memory.NewArticleBodyStore(clock.Now)
	events := &articleEvents{}
	service := articles.NewService(repository, bodies, clock, &articleIDs{values: []string{"article-1"}}, events)

	created, err := service.CreateDraft(ctx, articleDraft("prima-versione", "prima"))
	if err != nil {
		t.Fatalf("CreateDraft() error = %v", err)
	}
	published, err := service.Publish(ctx, created.ID, created.ETag)
	if err != nil {
		t.Fatalf("Publish() error = %v", err)
	}
	if len(events.changes) != 1 {
		t.Fatalf("publication event count = %d, want 1", len(events.changes))
	}

	clock.now = clock.now.Add(time.Hour)
	replacement := articleDraft("versione-sostitutiva", "seconda")
	replacement.Title = "Titolo della versione sostitutiva"
	replacement.Summary = "Sommario aggiornato della versione sostitutiva"
	replacement.Area = "diritto-tributario"
	replacement.CoverID = "tax-ledger"
	saved, err := service.SaveDraft(ctx, published.ID, replacement, published.ETag)
	if err != nil {
		t.Fatalf("SaveDraft() error = %v", err)
	}
	if len(events.changes) != 1 {
		t.Fatalf("draft-only save emitted a public event; count = %d", len(events.changes))
	}
	live, err := service.GetPublished(ctx, "prima-versione")
	if err != nil {
		t.Fatalf("GetPublished(old slug before replacement publish) error = %v", err)
	}
	if live.Article.Slug != "prima-versione" || live.Article.Title != "Titolo valido" || live.Body.PlainText != "prima versione del contenuto" {
		t.Fatalf("live version changed on draft-only save: %#v", live)
	}
	if _, err := service.GetPublished(ctx, replacement.Slug); !errors.Is(err, articles.ErrNotFound) {
		t.Fatalf("GetPublished(unpublished new slug) error = %v, want ErrNotFound", err)
	}

	clock.now = clock.now.Add(time.Hour)
	republished, err := service.Publish(ctx, saved.ID, saved.ETag)
	if err != nil {
		t.Fatalf("replacement Publish() error = %v", err)
	}
	if republished.Published == nil || republished.Published.Slug != replacement.Slug {
		t.Fatalf("replacement published metadata = %#v", republished.Published)
	}
	redirected, err := service.GetPublished(ctx, "prima-versione")
	if err != nil {
		t.Fatalf("GetPublished(historical slug) error = %v", err)
	}
	if redirected.Article.Slug != replacement.Slug || redirected.Article.Title != replacement.Title || redirected.Body.PlainText != "seconda versione del contenuto" {
		t.Fatalf("historical slug lookup = %#v", redirected)
	}
	if len(events.changes) != 2 || events.changes[1].before.Published.Slug != "prima-versione" || events.changes[1].after.Published.Slug != replacement.Slug {
		t.Fatalf("publication events = %#v", events.changes)
	}
}

func TestArticleServiceListsOnlyPublishedSnapshots(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	clock := &articleClock{now: time.Date(2026, 9, 11, 10, 0, 0, 0, time.UTC)}
	repository := memory.NewArticleMetadataRepository()
	bodies := memory.NewArticleBodyStore(clock.Now)
	service := articles.NewService(repository, bodies, clock, &articleIDs{values: []string{"published", "draft"}})
	publishedDraft, err := service.CreateDraft(ctx, articleDraft("published-article", "pubblicata"))
	if err != nil {
		t.Fatalf("CreateDraft(published) error = %v", err)
	}
	if _, err := service.Publish(ctx, publishedDraft.ID, publishedDraft.ETag); err != nil {
		t.Fatalf("Publish() error = %v", err)
	}
	if _, err := service.CreateDraft(ctx, articleDraft("draft-article", "bozza")); err != nil {
		t.Fatalf("CreateDraft(draft) error = %v", err)
	}

	page, err := service.ListPublished(ctx, articles.ListOptions{Limit: 10})
	if err != nil {
		t.Fatalf("ListPublished() error = %v", err)
	}
	if len(page.Items) != 1 || page.Items[0].Slug != "published-article" || page.Items[0].Status != articles.StatusPublished {
		t.Fatalf("ListPublished() = %#v", page)
	}
}

func TestArticleServiceRejectsReturningToAPermanentlyReservedHistoricalSlug(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	clock := &articleClock{now: time.Date(2026, 9, 11, 10, 0, 0, 0, time.UTC)}
	repository := memory.NewArticleMetadataRepository()
	bodies := memory.NewArticleBodyStore(clock.Now)
	service := articles.NewService(repository, bodies, clock, &articleIDs{values: []string{"article-1"}})
	article, err := service.CreateDraft(ctx, articleDraft("slug-a", "prima"))
	if err != nil {
		t.Fatalf("CreateDraft() error = %v", err)
	}
	article, err = service.Publish(ctx, article.ID, article.ETag)
	if err != nil {
		t.Fatalf("Publish(slug-a) error = %v", err)
	}
	clock.now = clock.now.Add(time.Minute)
	article, err = service.SaveDraft(ctx, article.ID, articleDraft("slug-b", "seconda"), article.ETag)
	if err != nil {
		t.Fatalf("SaveDraft(slug-b) error = %v", err)
	}
	article, err = service.Publish(ctx, article.ID, article.ETag)
	if err != nil {
		t.Fatalf("Publish(slug-b) error = %v", err)
	}
	clock.now = clock.now.Add(time.Minute)
	article, err = service.SaveDraft(ctx, article.ID, articleDraft("slug-a", "terza"), article.ETag)
	if !errors.Is(err, articles.ErrSlugTaken) {
		t.Fatalf("SaveDraft(return slug-a) error = %v, want ErrSlugTaken", err)
	}
	live, err := service.GetPublished(ctx, "slug-a")
	if err != nil {
		t.Fatalf("GetPublished(permanent alias) error = %v", err)
	}
	if live.Article.Slug != "slug-b" || live.Article.Published == nil || !slices.Equal(live.Article.Published.HistoricalSlugs, []string{"slug-a"}) {
		t.Fatalf("published alias reservation = %#v", live.Article.Published)
	}
}

func TestArticleServiceDoesNotPublishEventWhenMetadataWriteFails(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	clock := &articleClock{now: time.Date(2026, 9, 11, 10, 0, 0, 0, time.UTC)}
	repository := memory.NewArticleMetadataRepository()
	bodies := memory.NewArticleBodyStore(clock.Now)
	service := articles.NewService(repository, bodies, clock, &articleIDs{values: []string{"article-1"}})
	draft, err := service.CreateDraft(ctx, articleDraft("write-failure", "prima"))
	if err != nil {
		t.Fatalf("CreateDraft() error = %v", err)
	}

	events := &articleEvents{}
	failingService := articles.NewService(
		failingUpdateRepository{MetadataRepository: repository, err: errors.New("metadata storage unavailable")},
		bodies,
		clock,
		&articleIDs{},
		events,
	)
	if _, err := failingService.Publish(ctx, draft.ID, draft.ETag); err == nil {
		t.Fatal("Publish() error = nil, want metadata storage error")
	}
	if len(events.changes) != 0 {
		t.Fatalf("publication event count = %d, want 0 after failed write", len(events.changes))
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

type articleEvents struct {
	changes []articleChange
}

type articleChange struct {
	before articles.Article
	after  articles.Article
}

type failingUpdateRepository struct {
	articles.MetadataRepository
	err error
}

func (repository failingUpdateRepository) Update(context.Context, articles.Article, string) (articles.Article, error) {
	return articles.Article{}, repository.err
}

func (events *articleEvents) PublicArticleChanged(_ context.Context, before, after articles.Article) {
	events.changes = append(events.changes, articleChange{before: before, after: after})
}

func (ids *articleIDs) NewID() string {
	value := ids.values[ids.index]
	ids.index++
	return value
}
