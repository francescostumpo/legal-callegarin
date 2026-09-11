package azure

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/francescostumpo/legal-callegarin/internal/articles"
	"github.com/francescostumpo/legal-callegarin/internal/auth"
	"github.com/francescostumpo/legal-callegarin/internal/contacts"
)

func TestArticleEntityRoundTripUsesStableKeysUTCAndSchema(t *testing.T) {
	t.Parallel()

	zone := time.FixedZone("CEST", 2*60*60)
	created := time.Date(2026, 9, 11, 12, 0, 0, 123, zone)
	first := created.Add(time.Hour)
	last := first.Add(time.Hour)
	deleted := last.Add(time.Hour)
	article := articles.Article{
		ID: "article_01", Slug: "bozza-corrente", Title: "Titolo valido", Summary: "Sommario sufficientemente lungo",
		Area: "obbligazioni-e-contratti", CoverID: "contracts-pen", Status: articles.StatusPublished,
		DraftBody:        &articles.BodyRef{BlobName: "articles/article_01/draft.json", Version: "draft", SavedAt: created},
		PublishedBody:    &articles.BodyRef{BlobName: "articles/article_01/live.json", Version: "live", SavedAt: first},
		Published:        &articles.PublishedMetadata{Slug: "slug-pubblico", Title: "Titolo pubblico", Summary: "Sommario pubblico sufficientemente lungo", Area: "obbligazioni-e-contratti", CoverID: "contracts-pen", HistoricalSlugs: []string{"slug-storico"}},
		FirstPublishedAt: &first, LastPublishedAt: &last, CreatedAt: created, UpdatedAt: last, DeletedAt: &deleted, ETag: `W/"domain-etag"`,
	}

	encoded, err := marshalArticleEntity(article)
	if err != nil {
		t.Fatalf("marshalArticleEntity() error = %v", err)
	}
	rowKey, rowErr := articleRowKey(article.ID, article.CreatedAt)
	if rowErr != nil {
		t.Fatal(rowErr)
	}
	assertEntityHeader(t, encoded, articlesPartition, rowKey, articleEntityType)
	if !strings.Contains(string(encoded), `"schemaVersion":1`) {
		t.Fatalf("article entity lacks schema version: %s", encoded)
	}
	if strings.Contains(string(encoded), article.ETag) {
		t.Fatalf("article entity persisted domain ETag: %s", encoded)
	}

	decoded, err := unmarshalArticleEntity(encoded, `W/"azure-etag"`)
	if err != nil {
		t.Fatalf("unmarshalArticleEntity() error = %v", err)
	}
	want := article
	want.ETag = `W/"azure-etag"`
	normalizeArticleTimes(&want)
	if !reflect.DeepEqual(decoded, want) {
		t.Fatalf("article round trip = %#v, want %#v", decoded, want)
	}
}

func TestArticleEntityRoundTripPreservesNilPointersAndLists(t *testing.T) {
	t.Parallel()

	created := time.Date(2026, 9, 11, 10, 0, 0, 0, time.UTC)
	article := articles.Article{
		ID: "draft-1", Slug: "draft-slug", Title: "Titolo valido", Summary: "Sommario sufficientemente lungo",
		Area: "obbligazioni-e-contratti", CoverID: "contracts-pen", Status: articles.StatusDraft,
		DraftBody: &articles.BodyRef{BlobName: "articles/draft-1/v1.json", Version: "v1", SavedAt: created},
		CreatedAt: created, UpdatedAt: created,
	}
	encoded, err := marshalArticleEntity(article)
	if err != nil {
		t.Fatalf("marshalArticleEntity() error = %v", err)
	}
	decoded, err := unmarshalArticleEntity(encoded, "etag")
	if err != nil {
		t.Fatalf("unmarshalArticleEntity() error = %v", err)
	}
	if decoded.PublishedBody != nil || decoded.Published != nil || decoded.FirstPublishedAt != nil || decoded.LastPublishedAt != nil || decoded.DeletedAt != nil {
		t.Fatalf("nil article fields were materialized: %#v", decoded)
	}
}

func TestSlugEntityUsesNormalizedReservedRowAndRedirectTarget(t *testing.T) {
	t.Parallel()

	record := slugRecord{Slug: "diritto-civile", ArticleID: "article-1", PublishedTarget: "nuovo-slug"}
	encoded, err := marshalSlugEntity(record)
	if err != nil {
		t.Fatalf("marshalSlugEntity() error = %v", err)
	}
	assertEntityHeader(t, encoded, articlesPartition, "slug:diritto-civile", slugEntityType)
	decoded, err := unmarshalSlugEntity(encoded, `W/"slug-etag"`)
	if err != nil || decoded != (slugRecord{Slug: record.Slug, ArticleID: record.ArticleID, PublishedTarget: record.PublishedTarget, ETag: `W/"slug-etag"`}) {
		t.Fatalf("slug round trip = %#v, %v", decoded, err)
	}
	if _, err := marshalSlugEntity(slugRecord{Slug: "Not Normalized", ArticleID: "article-1"}); err == nil {
		t.Fatal("marshalSlugEntity(unnormalized) error = nil")
	}
}

func TestContactEntityRoundTripUsesReverseSortableKeyAndUTC(t *testing.T) {
	t.Parallel()

	zone := time.FixedZone("CEST", 2*60*60)
	created := time.Date(2026, 9, 11, 12, 0, 0, 0, zone)
	read := created.Add(time.Minute)
	archived := read.Add(time.Minute)
	deletion := archived.Add(30 * 24 * time.Hour)
	contact := contacts.Contact{
		ID: "contact_01", Name: "Mario Rossi", Email: "mario@example.test", Phone: "+39 000 000000",
		Message: "Messaggio sufficientemente lungo", ConsentVersion: "privacy-v1", PrivacyAcceptedAt: created,
		State: contacts.StateArchived, CreatedAt: created, UpdatedAt: archived, ReadAt: &read, ArchivedAt: &archived,
		ReviewDueAt: created.AddDate(2, 0, 0), DeletionDueAt: &deletion, ETag: `W/"domain"`,
	}
	encoded, err := marshalContactEntity(contact)
	if err != nil {
		t.Fatalf("marshalContactEntity() error = %v", err)
	}
	var header entityHeader
	if err := json.Unmarshal(encoded, &header); err != nil {
		t.Fatalf("decode header: %v", err)
	}
	if header.PartitionKey != contactsPartition || !strings.HasSuffix(header.RowKey, ":contact_01") || header.EntityType != contactEntityType {
		t.Fatalf("contact header = %#v", header)
	}
	newerKey, err := contactRowKey("contact_02", created.Add(time.Second))
	if err != nil {
		t.Fatalf("contactRowKey(newer) error = %v", err)
	}
	if newerKey >= header.RowKey {
		t.Fatalf("newer row key %q does not sort before older %q", newerKey, header.RowKey)
	}
	decoded, err := unmarshalContactEntity(encoded, `W/"azure"`)
	if err != nil {
		t.Fatalf("unmarshalContactEntity() error = %v", err)
	}
	want := contact
	want.ETag = `W/"azure"`
	normalizeContactTimes(&want)
	if !reflect.DeepEqual(decoded, want) {
		t.Fatalf("contact round trip = %#v, want %#v", decoded, want)
	}
}

func TestSessionEntityContainsOnlySHA256HashAndPreservesNilRevocation(t *testing.T) {
	t.Parallel()

	rawToken := "raw-session-token-must-never-be-stored"
	digest := sha256.Sum256([]byte(rawToken))
	tokenHash := hex.EncodeToString(digest[:])
	created := time.Date(2026, 9, 11, 10, 0, 0, 0, time.FixedZone("CEST", 2*60*60))
	session := auth.Session{TokenHash: tokenHash, Username: "admin", CredentialVersion: "credential-v1", CreatedAt: created, ExpiresAt: created.Add(8 * time.Hour), ETag: "domain"}
	encoded, err := marshalSessionEntity(session)
	if err != nil {
		t.Fatalf("marshalSessionEntity() error = %v", err)
	}
	assertEntityHeader(t, encoded, sessionsPartition, tokenHash, sessionEntityType)
	if strings.Contains(string(encoded), rawToken) || strings.Contains(string(encoded), session.ETag) {
		t.Fatalf("session entity leaked raw token or ETag: %s", encoded)
	}
	decoded, err := unmarshalSessionEntity(encoded, "azure")
	if err != nil {
		t.Fatalf("unmarshalSessionEntity() error = %v", err)
	}
	want := session
	want.CreatedAt = want.CreatedAt.UTC()
	want.ExpiresAt = want.ExpiresAt.UTC()
	want.ETag = "azure"
	if !reflect.DeepEqual(decoded, want) || decoded.RevokedAt != nil {
		t.Fatalf("session round trip = %#v, want %#v", decoded, want)
	}
}

func TestBodyEnvelopeRoundTripAndBlobName(t *testing.T) {
	t.Parallel()

	body, compileErr := articles.CompileDocument(1, json.RawMessage(`{"type":"doc","content":[{"type":"paragraph","content":[{"type":"text","text":"Contenuto"}]}]}`))
	if compileErr != nil {
		t.Fatal(compileErr)
	}
	encoded, err := marshalBodyEnvelope(body)
	if err != nil {
		t.Fatalf("marshalBodyEnvelope() error = %v", err)
	}
	if !strings.Contains(string(encoded), `"storageSchemaVersion":1`) {
		t.Fatalf("body envelope lacks storage schema: %s", encoded)
	}
	decoded, err := unmarshalBodyEnvelope(encoded)
	if err != nil || !reflect.DeepEqual(decoded, body) {
		t.Fatalf("body round trip = %#v, %v", decoded, err)
	}
	name, err := articleBlobName("article_01", "version_01")
	if err != nil || name != "articles/article_01/version_01.json" || strings.Contains(name, "://") {
		t.Fatalf("articleBlobName() = %q, %v", name, err)
	}
	if _, err := articleBlobName("../article", "version"); err == nil {
		t.Fatal("articleBlobName(unsafe ID) error = nil")
	}
}

func assertEntityHeader(t *testing.T, encoded []byte, partition, row, entityType string) {
	t.Helper()
	var header entityHeader
	if err := json.Unmarshal(encoded, &header); err != nil {
		t.Fatalf("decode entity header: %v", err)
	}
	if header.PartitionKey != partition || header.RowKey != row || header.EntityType != entityType || header.SchemaVersion != storageSchemaVersion {
		t.Fatalf("entity header = %#v", header)
	}
}

func normalizeArticleTimes(article *articles.Article) {
	article.CreatedAt = article.CreatedAt.UTC()
	article.UpdatedAt = article.UpdatedAt.UTC()
	for _, value := range []*time.Time{article.FirstPublishedAt, article.LastPublishedAt, article.DeletedAt} {
		if value != nil {
			*value = value.UTC()
		}
	}
	for _, ref := range []*articles.BodyRef{article.DraftBody, article.PublishedBody} {
		if ref != nil {
			ref.SavedAt = ref.SavedAt.UTC()
		}
	}
}

func normalizeContactTimes(contact *contacts.Contact) {
	contact.PrivacyAcceptedAt = contact.PrivacyAcceptedAt.UTC()
	contact.CreatedAt = contact.CreatedAt.UTC()
	contact.UpdatedAt = contact.UpdatedAt.UTC()
	contact.ReviewDueAt = contact.ReviewDueAt.UTC()
	for _, value := range []*time.Time{contact.ReadAt, contact.ArchivedAt, contact.DeletionDueAt} {
		if value != nil {
			*value = value.UTC()
		}
	}
}
