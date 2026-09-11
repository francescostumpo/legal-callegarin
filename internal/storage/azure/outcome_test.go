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

type successfulMutationUnreadableTable struct {
	*memoryTableDriver
	readsFail bool
}

func (driver *successfulMutationUnreadableTable) Transaction(ctx context.Context, actions []tableAction) error {
	if err := driver.memoryTableDriver.Transaction(ctx, actions); err != nil {
		return err
	}
	driver.readsFail = true
	return nil
}
func (driver *successfulMutationUnreadableTable) Add(ctx context.Context, value []byte) (string, error) {
	if _, err := driver.memoryTableDriver.Add(ctx, value); err != nil {
		return "", err
	}
	driver.readsFail = true
	return "", nil
}
func (driver *successfulMutationUnreadableTable) Update(ctx context.Context, value []byte, etag string) (string, error) {
	if _, err := driver.memoryTableDriver.Update(ctx, value, etag); err != nil {
		return "", err
	}
	driver.readsFail = true
	return "", nil
}
func (driver *successfulMutationUnreadableTable) Get(ctx context.Context, partition, row string) (tableEntity, error) {
	if driver.readsFail {
		return tableEntity{}, ErrTransient
	}
	return driver.memoryTableDriver.Get(ctx, partition, row)
}
func (driver *successfulMutationUnreadableTable) List(ctx context.Context, filter string, maximum int32) ([]tableEntity, error) {
	if driver.readsFail {
		return nil, ErrTransient
	}
	return driver.memoryTableDriver.List(ctx, filter, maximum)
}

type mutationErrorTable struct {
	*memoryTableDriver
	partialArticle bool
	contactValue   []byte
}

func (driver *mutationErrorTable) Transaction(ctx context.Context, actions []tableAction) error {
	if driver.partialArticle {
		if err := driver.memoryTableDriver.Transaction(ctx, actions[:1]); err != nil {
			return err
		}
	}
	return ErrTransient
}
func (driver *mutationErrorTable) Update(ctx context.Context, value []byte, etag string) (string, error) {
	if driver.contactValue != nil {
		if _, err := driver.memoryTableDriver.Update(ctx, driver.contactValue, etag); err != nil {
			return "", err
		}
	}
	return "", ErrTransient
}

type successfulTransactionStaleArticleTable struct {
	*memoryTableDriver
	stalePartition string
	staleRow       string
	stale          tableEntity
	returnStale    bool
}

func (driver *successfulTransactionStaleArticleTable) Transaction(ctx context.Context, actions []tableAction) error {
	prior, err := driver.memoryTableDriver.Get(ctx, actions[0].PartitionKey, actions[0].RowKey)
	if err != nil {
		return err
	}
	if err := driver.memoryTableDriver.Transaction(ctx, actions); err != nil {
		return err
	}
	driver.stalePartition, driver.staleRow, driver.stale = actions[0].PartitionKey, actions[0].RowKey, prior
	driver.returnStale = true
	return nil
}
func (driver *successfulTransactionStaleArticleTable) Get(ctx context.Context, partition, row string) (tableEntity, error) {
	if driver.returnStale && partition == driver.stalePartition && row == driver.staleRow {
		return driver.stale, nil
	}
	return driver.memoryTableDriver.Get(ctx, partition, row)
}

type successfulUpdateStaleContactTable struct {
	*memoryTableDriver
	stale     tableEntity
	postWrite bool
}

func (driver *successfulUpdateStaleContactTable) Update(ctx context.Context, value []byte, etag string) (string, error) {
	partition, row, err := entityKey(value)
	if err != nil {
		return "", err
	}
	prior, err := driver.memoryTableDriver.Get(ctx, partition, row)
	if err != nil {
		return "", err
	}
	if _, err := driver.memoryTableDriver.Update(ctx, value, etag); err != nil {
		return "", err
	}
	driver.stale = prior
	driver.postWrite = true
	return "", nil
}
func (driver *successfulUpdateStaleContactTable) List(ctx context.Context, filter string, maximum int32) ([]tableEntity, error) {
	if driver.postWrite {
		return []tableEntity{driver.stale}, nil
	}
	return driver.memoryTableDriver.List(ctx, filter, maximum)
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

func TestArticleServicePreservesBodyWhenSuccessfulCreateRefreshFails(t *testing.T) {
	driver := &successfulMutationUnreadableTable{memoryTableDriver: newMemoryTableDriver()}
	blobs := newMemoryBlobDriver()
	now := time.Date(2026, 9, 11, 10, 0, 0, 0, time.UTC)
	service := articles.NewService(newArticleMetadataRepository(driver), newArticleBodyStore(blobs, func() time.Time { return now }, func() string { return "version-1" }), outcomeClock{now}, outcomeIDs{})
	_, err := service.CreateDraft(context.Background(), outcomeDraft("article-one", "body"))
	if !errors.Is(err, articles.ErrCommitUnknown) || !errors.Is(err, ErrTransient) {
		t.Fatalf("CreateDraft() error = %v", err)
	}
	if _, ok := blobs.values["articles/article-1/version-1.json"]; !ok {
		t.Fatal("known-committed metadata with failed refresh deleted the body")
	}
}

func TestArticleServicePreservesNewBodyWhenSuccessfulUpdateRefreshFails(t *testing.T) {
	base := newMemoryTableDriver()
	blobs := newMemoryBlobDriver()
	now := time.Date(2026, 9, 11, 10, 0, 0, 0, time.UTC)
	versions := []string{"version-1", "version-2"}
	versionIndex := 0
	bodyStore := newArticleBodyStore(blobs, func() time.Time { return now }, func() string { value := versions[versionIndex]; versionIndex++; return value })
	baseService := articles.NewService(newArticleMetadataRepository(base), bodyStore, outcomeClock{now}, outcomeIDs{})
	created, err := baseService.CreateDraft(context.Background(), outcomeDraft("article-one", "first body"))
	if err != nil {
		t.Fatal(err)
	}
	driver := &successfulMutationUnreadableTable{memoryTableDriver: base}
	service := articles.NewService(newArticleMetadataRepository(driver), bodyStore, outcomeClock{now: now.Add(time.Minute)}, outcomeIDs{})
	_, err = service.SaveDraft(context.Background(), created.ID, outcomeDraft("article-one", "second body"), created.ETag)
	if !errors.Is(err, articles.ErrCommitUnknown) || !errors.Is(err, ErrTransient) {
		t.Fatalf("SaveDraft() error = %v", err)
	}
	if _, ok := blobs.values["articles/article-1/version-2.json"]; !ok {
		t.Fatal("known-committed metadata with failed refresh deleted the new body")
	}
}

func TestContactSuccessfulEmptyETagRefreshFailureIsCommitUnknown(t *testing.T) {
	t.Run("create", func(t *testing.T) {
		driver := &successfulMutationUnreadableTable{memoryTableDriver: newMemoryTableDriver()}
		_, err := newContactRepository(driver, time.Now).Create(context.Background(), outcomeContact("contact-1"))
		if !errors.Is(err, contacts.ErrCommitUnknown) || !errors.Is(err, ErrTransient) {
			t.Fatalf("Create() error = %v", err)
		}
	})
	t.Run("update", func(t *testing.T) {
		base := newMemoryTableDriver()
		created, err := newContactRepository(base, time.Now).Create(context.Background(), outcomeContact("contact-1"))
		if err != nil {
			t.Fatal(err)
		}
		updated := created
		updated.Message = "Messaggio aggiornato sufficientemente lungo"
		updated.UpdatedAt = updated.UpdatedAt.Add(time.Minute)
		driver := &successfulMutationUnreadableTable{memoryTableDriver: base}
		_, err = newContactRepository(driver, time.Now).Update(context.Background(), updated, created.ETag)
		if !errors.Is(err, contacts.ErrCommitUnknown) || !errors.Is(err, ErrTransient) {
			t.Fatalf("Update() error = %v", err)
		}
	})
}

func TestArticleReconciliationDistinguishesPriorAndMixedStates(t *testing.T) {
	base := newMemoryTableDriver()
	created, err := newArticleMetadataRepository(base).Create(context.Background(), outcomeArticle("article-1", "article-one"))
	if err != nil {
		t.Fatal(err)
	}
	updated := created
	updated.Slug = "article-two"
	updated.Title = "Titolo aggiornato"
	updated.UpdatedAt = updated.UpdatedAt.Add(time.Minute)
	for _, test := range []struct {
		name        string
		partial     bool
		wantUnknown bool
	}{
		{name: "prior state"},
		{name: "mixed state", partial: true, wantUnknown: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			copyDriver := cloneMemoryTableDriver(base)
			driver := &mutationErrorTable{memoryTableDriver: copyDriver, partialArticle: test.partial}
			_, err := newArticleMetadataRepository(driver).Update(context.Background(), updated, created.ETag)
			if !errors.Is(err, ErrTransient) || errors.Is(err, articles.ErrCommitUnknown) != test.wantUnknown {
				t.Fatalf("Update() error = %v", err)
			}
		})
	}
}

func TestSuccessfulArticleRefreshRejectsStaleMixedState(t *testing.T) {
	base := newMemoryTableDriver()
	created, err := newArticleMetadataRepository(base).Create(context.Background(), outcomeArticle("article-1", "article-one"))
	if err != nil {
		t.Fatal(err)
	}
	updated := created
	updated.Slug = "article-two"
	updated.Title = "Titolo aggiornato"
	updated.UpdatedAt = updated.UpdatedAt.Add(time.Minute)
	driver := &successfulTransactionStaleArticleTable{memoryTableDriver: base}
	_, err = newArticleMetadataRepository(driver).Update(context.Background(), updated, created.ETag)
	if !errors.Is(err, articles.ErrCommitUnknown) {
		t.Fatalf("Update() error = %v", err)
	}
}

func TestSuccessfulContactRefreshRejectsStaleState(t *testing.T) {
	base := newMemoryTableDriver()
	created, err := newContactRepository(base, time.Now).Create(context.Background(), outcomeContact("contact-1"))
	if err != nil {
		t.Fatal(err)
	}
	updated := created
	updated.Message = "Messaggio aggiornato sufficientemente lungo"
	updated.UpdatedAt = updated.UpdatedAt.Add(time.Minute)
	driver := &successfulUpdateStaleContactTable{memoryTableDriver: base}
	_, err = newContactRepository(driver, time.Now).Update(context.Background(), updated, created.ETag)
	if !errors.Is(err, contacts.ErrCommitUnknown) {
		t.Fatalf("Update() error = %v", err)
	}
}

func TestContactReconciliationDistinguishesPriorAndMixedStates(t *testing.T) {
	base := newMemoryTableDriver()
	created, err := newContactRepository(base, time.Now).Create(context.Background(), outcomeContact("contact-1"))
	if err != nil {
		t.Fatal(err)
	}
	updated := created
	updated.Message = "Messaggio aggiornato sufficientemente lungo"
	updated.UpdatedAt = updated.UpdatedAt.Add(time.Minute)
	mixed := updated
	mixed.Message = "Stato concorrente sufficientemente differente"
	mixedValue, err := marshalContactEntity(mixed)
	if err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		name        string
		value       []byte
		wantUnknown bool
	}{
		{name: "prior state"},
		{name: "mixed state", value: mixedValue, wantUnknown: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			driver := &mutationErrorTable{memoryTableDriver: cloneMemoryTableDriver(base), contactValue: test.value}
			_, err := newContactRepository(driver, time.Now).Update(context.Background(), updated, created.ETag)
			if !errors.Is(err, ErrTransient) || errors.Is(err, contacts.ErrCommitUnknown) != test.wantUnknown {
				t.Fatalf("Update() error = %v", err)
			}
		})
	}
}

func cloneMemoryTableDriver(source *memoryTableDriver) *memoryTableDriver {
	source.mu.Lock()
	defer source.mu.Unlock()
	clone := newMemoryTableDriver()
	clone.nextETag = source.nextETag
	for key, entity := range source.entities {
		clone.entities[key] = tableEntity{Value: append([]byte(nil), entity.Value...), ETag: entity.ETag}
	}
	return clone
}

func outcomeArticle(id, slug string) articles.Article {
	now := time.Date(2026, 9, 11, 10, 0, 0, 0, time.UTC)
	return articles.Article{ID: id, Slug: slug, Title: "Titolo valido", Summary: "Sommario sufficientemente lungo", Area: "civile", CoverID: "cover", Status: articles.StatusDraft, DraftBody: &articles.BodyRef{BlobName: "articles/" + id + "/v1.json", Version: "v1", SavedAt: now}, CreatedAt: now, UpdatedAt: now}
}
func outcomeContact(id string) contacts.Contact {
	now := time.Date(2026, 9, 11, 10, 0, 0, 0, time.UTC)
	return contacts.Contact{ID: id, Name: "Mario Rossi", Email: "mario@example.test", Message: "Messaggio sufficientemente lungo", ConsentVersion: "privacy-v1", PrivacyAcceptedAt: now, State: contacts.StateNew, CreatedAt: now, UpdatedAt: now, ReviewDueAt: now.AddDate(2, 0, 0)}
}

func outcomeDraft(slug, plainText string) articles.DraftInput {
	return articles.DraftInput{Slug: slug, Title: "Titolo valido", Summary: "Sommario sufficientemente lungo", Area: "civile", CoverID: "cover", Body: articles.Body{SchemaVersion: 1, Document: []byte(`{"type":"doc"}`), HTML: "<p>" + plainText + "</p>", PlainText: plainText}}
}

func outcomeSession(label string, created time.Time) auth.Session {
	digest := sha256.Sum256([]byte(label))
	return auth.Session{TokenHash: hex.EncodeToString(digest[:]), Username: "admin", CredentialVersion: "v1", CreatedAt: created, ExpiresAt: created.Add(8 * time.Hour)}
}

type outcomeClock struct{ now time.Time }

func (clock outcomeClock) Now() time.Time { return clock.now }

type outcomeIDs struct{}

func (outcomeIDs) NewID() string { return "article-1" }
