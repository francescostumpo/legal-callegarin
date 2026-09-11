package azure

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"testing"
	"time"

	"github.com/francescostumpo/legal-callegarin/internal/articles"
	"github.com/francescostumpo/legal-callegarin/internal/auth"
	"github.com/francescostumpo/legal-callegarin/internal/contacts"
)

type commitThenErrorTable struct {
	*memoryTableDriver
	err                    error
	failReadsAfterMutation bool
	readsFail              bool
	afterMutation          func()
}

func (driver *commitThenErrorTable) Transaction(ctx context.Context, actions []tableAction) error {
	if err := driver.memoryTableDriver.Transaction(ctx, actions); err != nil {
		return err
	}
	driver.readsFail = driver.failReadsAfterMutation
	if driver.afterMutation != nil {
		driver.afterMutation()
	}
	return driver.err
}
func (driver *commitThenErrorTable) Add(ctx context.Context, value []byte) (string, error) {
	_, err := driver.memoryTableDriver.Add(ctx, value)
	if err != nil {
		return "", err
	}
	driver.readsFail = driver.failReadsAfterMutation
	if driver.afterMutation != nil {
		driver.afterMutation()
	}
	return "", driver.err
}
func (driver *commitThenErrorTable) Update(ctx context.Context, value []byte, etag string) (string, error) {
	_, err := driver.memoryTableDriver.Update(ctx, value, etag)
	if err != nil {
		return "", err
	}
	driver.readsFail = driver.failReadsAfterMutation
	return "", driver.err
}
func (driver *commitThenErrorTable) Delete(ctx context.Context, partition, row, etag string) error {
	if err := driver.memoryTableDriver.Delete(ctx, partition, row, etag); err != nil {
		return err
	}
	driver.readsFail = driver.failReadsAfterMutation
	return driver.err
}
func (driver *commitThenErrorTable) Get(ctx context.Context, partition, row string) (tableEntity, error) {
	if driver.readsFail {
		return tableEntity{}, driver.err
	}
	return driver.memoryTableDriver.Get(ctx, partition, row)
}
func (driver *commitThenErrorTable) List(ctx context.Context, filter string, maximum int32) ([]tableEntity, error) {
	if driver.readsFail {
		return nil, driver.err
	}
	return driver.memoryTableDriver.List(ctx, filter, maximum)
}

func (driver *commitThenErrorTable) ListPage(ctx context.Context, filter string, maximum int32, continuation *tableContinuation) ([]tableEntity, *tableContinuation, error) {
	if driver.readsFail {
		return nil, nil, driver.err
	}
	return driver.memoryTableDriver.ListPage(ctx, filter, maximum, continuation)
}

func TestArticleCreateReconcilesCommittedTransactionAfterTransportError(t *testing.T) {
	driver := &commitThenErrorTable{memoryTableDriver: newMemoryTableDriver(), err: ErrTransient}
	repository := newArticleMetadataRepository(driver)
	article := outcomeArticle("article-1", "article-one")
	created, err := repository.Create(context.Background(), article)
	if err != nil || created.ID != article.ID || created.ETag == "" {
		t.Fatalf("Create() = %#v, %v", created, err)
	}
}

func TestArticleServicePreservesBodyWhenMetadataOutcomeCannotBeVerified(t *testing.T) {
	driver := &commitThenErrorTable{memoryTableDriver: newMemoryTableDriver(), err: ErrTransient, failReadsAfterMutation: true}
	blobs := newMemoryBlobDriver()
	now := time.Date(2026, 9, 11, 10, 0, 0, 0, time.UTC)
	service := articles.NewService(newArticleMetadataRepository(driver), newArticleBodyStore(blobs, func() time.Time { return now }, func() string { return "version-1" }), outcomeClock{now}, outcomeIDs{})
	_, err := service.CreateDraft(context.Background(), articles.DraftInput{Slug: "article-one", Title: "Titolo valido", Summary: "Sommario sufficientemente lungo", Area: "civile", CoverID: "cover", Body: articles.Body{SchemaVersion: 1, Document: []byte(`{"type":"doc"}`), HTML: "<p>body</p>", PlainText: "body"}})
	if !errors.Is(err, articles.ErrCommitUnknown) {
		t.Fatalf("CreateDraft() error = %v", err)
	}
	if _, ok := blobs.values["articles/article-1/version-1.json"]; !ok {
		t.Fatal("ambiguous metadata outcome deleted the new body")
	}
}

func TestContactCreateReconcilesCommittedAddAfterTransportError(t *testing.T) {
	driver := &commitThenErrorTable{memoryTableDriver: newMemoryTableDriver(), err: ErrTransient}
	repository := newContactRepository(driver, time.Now)
	contact := outcomeContact("contact-1")
	created, err := repository.Create(context.Background(), contact)
	if err != nil || created.ID != contact.ID || created.ETag == "" {
		t.Fatalf("Create() = %#v, %v", created, err)
	}
}

func TestArticleUpdateReconcilesCommittedTransactionAfterTransportError(t *testing.T) {
	base := newMemoryTableDriver()
	repository := newArticleMetadataRepository(base)
	created, err := repository.Create(context.Background(), outcomeArticle("article-1", "article-one"))
	if err != nil {
		t.Fatal(err)
	}
	driver := &commitThenErrorTable{memoryTableDriver: base, err: ErrTransient}
	updated := created
	updated.Title = "Titolo aggiornato"
	updated.UpdatedAt = updated.UpdatedAt.Add(time.Minute)
	result, err := newArticleMetadataRepository(driver).Update(context.Background(), updated, created.ETag)
	if err != nil || result.Title != updated.Title || result.ETag == created.ETag {
		t.Fatalf("Update() = %#v, %v", result, err)
	}
}

func TestContactUpdateAndDeleteReconcileCommittedMutations(t *testing.T) {
	base := newMemoryTableDriver()
	baseRepository := newContactRepository(base, time.Now)
	created, err := baseRepository.Create(context.Background(), outcomeContact("contact-1"))
	if err != nil {
		t.Fatal(err)
	}
	driver := &commitThenErrorTable{memoryTableDriver: base, err: ErrTransient}
	repository := newContactRepository(driver, time.Now)
	updated := created
	updated.Message = "Messaggio aggiornato sufficientemente lungo"
	updated.UpdatedAt = updated.UpdatedAt.Add(time.Minute)
	result, err := repository.Update(context.Background(), updated, created.ETag)
	if err != nil || result.Message != updated.Message {
		t.Fatalf("Update() = %#v, %v", result, err)
	}
	if err := repository.Delete(context.Background(), result.ID, result.ETag); err != nil {
		t.Fatalf("Delete() error = %v", err)
	}
}

func TestSessionMutationsReconcileCommittedResults(t *testing.T) {
	driver := &commitThenErrorTable{memoryTableDriver: newMemoryTableDriver(), err: ErrTransient}
	repository := newSessionRepository(driver)
	session := outcomeSession("one", time.Date(2026, 9, 11, 10, 0, 0, 0, time.UTC))
	if err := repository.Create(context.Background(), session); err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	stored, err := repository.Get(context.Background(), session.TokenHash)
	if err != nil {
		t.Fatal(err)
	}
	if err := repository.Revoke(context.Background(), stored.TokenHash, stored.ETag, stored.CreatedAt.Add(time.Minute)); err != nil {
		t.Fatalf("Revoke() error = %v", err)
	}
	expired := outcomeSession("expired", time.Date(2026, 9, 10, 0, 0, 0, 0, time.UTC))
	if err := repository.Create(context.Background(), expired); err != nil {
		t.Fatal(err)
	}
	deleted, err := repository.DeleteExpired(context.Background(), time.Date(2026, 9, 11, 12, 0, 0, 0, time.UTC))
	if err != nil || deleted != 1 {
		t.Fatalf("DeleteExpired() = %d, %v", deleted, err)
	}
}

func TestUnknownCommitPreservesCancellationCause(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	driver := &commitThenErrorTable{memoryTableDriver: newMemoryTableDriver(), err: ErrTransient, failReadsAfterMutation: true, afterMutation: cancel}
	_, err := newContactRepository(driver, time.Now).Create(ctx, outcomeContact("contact-1"))
	if !errors.Is(err, contacts.ErrCommitUnknown) || !errors.Is(err, context.Canceled) {
		t.Fatalf("Create() error = %v", err)
	}
}

func outcomeArticle(id, slug string) articles.Article {
	now := time.Date(2026, 9, 11, 10, 0, 0, 0, time.UTC)
	return articles.Article{ID: id, Slug: slug, Title: "Titolo valido", Summary: "Sommario sufficientemente lungo", Area: "civile", CoverID: "cover", Status: articles.StatusDraft, DraftBody: &articles.BodyRef{BlobName: "articles/" + id + "/v1.json", Version: "v1", SavedAt: now}, CreatedAt: now, UpdatedAt: now}
}
func outcomeContact(id string) contacts.Contact {
	now := time.Date(2026, 9, 11, 10, 0, 0, 0, time.UTC)
	return contacts.Contact{ID: id, Name: "Mario Rossi", Email: "mario@example.test", Message: "Messaggio sufficientemente lungo", ConsentVersion: "privacy-v1", PrivacyAcceptedAt: now, State: contacts.StateNew, CreatedAt: now, UpdatedAt: now, ReviewDueAt: now.AddDate(2, 0, 0)}
}

func outcomeSession(label string, created time.Time) auth.Session {
	digest := sha256.Sum256([]byte(label))
	return auth.Session{TokenHash: hex.EncodeToString(digest[:]), Username: "admin", CredentialVersion: "v1", CreatedAt: created, ExpiresAt: created.Add(8 * time.Hour)}
}

type outcomeClock struct{ now time.Time }

func (clock outcomeClock) Now() time.Time { return clock.now }

type outcomeIDs struct{}

func (outcomeIDs) NewID() string { return "article-1" }
