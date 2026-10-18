package azure

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/francescostumpo/legal-callegarin/internal/articles"
	"github.com/francescostumpo/legal-callegarin/internal/auth"
	"github.com/francescostumpo/legal-callegarin/internal/contacts"
)

const (
	storageSchemaVersion = 1
	articlesPartition    = "articles"
	contactsPartition    = "contacts"
	sessionsPartition    = "sessions"
	articleEntityType    = "article"
	slugEntityType       = "slug"
	contactEntityType    = "contact"
	sessionEntityType    = "session"
)

type entityHeader struct {
	PartitionKey  string `json:"PartitionKey"`
	RowKey        string `json:"RowKey"`
	EntityType    string `json:"entityType"`
	SchemaVersion int    `json:"schemaVersion"`
}

type articleEntity struct {
	entityHeader
	ID        string          `json:"id,omitempty"`
	Slug      string          `json:"slug"`
	Title     string          `json:"title"`
	Summary   string          `json:"summary"`
	Area      string          `json:"area"`
	CoverID   string          `json:"coverId"`
	Status    articles.Status `json:"status"`
	CreatedAt time.Time       `json:"createdAt"`
	UpdatedAt time.Time       `json:"updatedAt"`
	DeletedAt *time.Time      `json:"deletedAt,omitempty"`

	DraftBlobName string     `json:"draftBlobName,omitempty"`
	DraftVersion  string     `json:"draftVersion,omitempty"`
	DraftSavedAt  *time.Time `json:"draftSavedAt,omitempty"`

	PublishedBlobName string     `json:"publishedBlobName,omitempty"`
	PublishedVersion  string     `json:"publishedVersion,omitempty"`
	PublishedSavedAt  *time.Time `json:"publishedSavedAt,omitempty"`

	PublishedSlug           string     `json:"publishedSlug,omitempty"`
	PublishedTitle          string     `json:"publishedTitle,omitempty"`
	PublishedSummary        string     `json:"publishedSummary,omitempty"`
	PublishedArea           string     `json:"publishedArea,omitempty"`
	PublishedCoverID        string     `json:"publishedCoverId,omitempty"`
	PublishedHistoricalJSON string     `json:"publishedHistoricalSlugs,omitempty"`
	FirstPublishedAt        *time.Time `json:"firstPublishedAt,omitempty"`
	LastPublishedAt         *time.Time `json:"lastPublishedAt,omitempty"`
}

type slugRecord struct {
	Slug            string
	ArticleID       string
	PublishedTarget string
	ETag            string
}

type slugEntity struct {
	entityHeader
	Slug            string `json:"slug"`
	ArticleID       string `json:"articleId"`
	PublishedTarget string `json:"publishedTarget,omitempty"`
}

type contactEntity struct {
	entityHeader
	ID                string         `json:"id"`
	Name              string         `json:"name"`
	Email             string         `json:"email"`
	Phone             string         `json:"phone,omitempty"`
	Message           string         `json:"message"`
	ConsentVersion    string         `json:"consentVersion"`
	PrivacyAcceptedAt time.Time      `json:"privacyAcceptedAt"`
	State             contacts.State `json:"state"`
	CreatedAt         time.Time      `json:"createdAt"`
	UpdatedAt         time.Time      `json:"updatedAt"`
	ReadAt            *time.Time     `json:"readAt,omitempty"`
	ArchivedAt        *time.Time     `json:"archivedAt,omitempty"`
	ReviewDueAt       time.Time      `json:"reviewDueAt"`
	DeletionDueAt     *time.Time     `json:"deletionDueAt,omitempty"`
}

type sessionEntity struct {
	entityHeader
	TokenHash         string     `json:"tokenHash"`
	Username          string     `json:"username"`
	CredentialVersion string     `json:"credentialVersion"`
	CreatedAt         time.Time  `json:"createdAt"`
	ExpiresAt         time.Time  `json:"expiresAt"`
	RevokedAt         *time.Time `json:"revokedAt,omitempty"`
}

type bodyEnvelope struct {
	StorageSchemaVersion int             `json:"storageSchemaVersion"`
	SchemaVersion        int             `json:"schemaVersion"`
	Document             json.RawMessage `json:"document"`
	HTML                 string          `json:"html"`
	PlainText            string          `json:"plainText"`
}

func marshalArticleEntity(article articles.Article) ([]byte, error) {
	if err := article.Validate(); err != nil {
		return nil, err
	}
	if !safeStorageSegment(article.ID) {
		return nil, fmt.Errorf("%w: article ID is not storage-safe", articles.ErrValidation)
	}
	rowKey, err := articleRowKey(article.ID, article.CreatedAt)
	if err != nil {
		return nil, err
	}
	return marshalArticleEntityAtRow(article, rowKey)
}

func marshalArticleEntityAtRow(article articles.Article, rowKey string) ([]byte, error) {
	if err := article.Validate(); err != nil {
		return nil, err
	}
	wanted, err := articleRowKey(article.ID, article.CreatedAt)
	if err != nil {
		return nil, err
	}
	if rowKey != wanted && rowKey != article.ID {
		return nil, fmt.Errorf("%w: article storage row is invalid", articles.ErrValidation)
	}
	entity := articleEntity{
		entityHeader: entityHeader{PartitionKey: articlesPartition, RowKey: rowKey, EntityType: articleEntityType, SchemaVersion: storageSchemaVersion}, ID: article.ID,
		Slug: article.Slug, Title: article.Title, Summary: article.Summary, Area: article.Area, CoverID: article.CoverID, Status: article.Status,
		CreatedAt: article.CreatedAt.UTC(), UpdatedAt: article.UpdatedAt.UTC(), DeletedAt: utcTimePointer(article.DeletedAt),
		FirstPublishedAt: utcTimePointer(article.FirstPublishedAt), LastPublishedAt: utcTimePointer(article.LastPublishedAt),
	}
	if article.DraftBody != nil {
		entity.DraftBlobName = article.DraftBody.BlobName
		entity.DraftVersion = article.DraftBody.Version
		entity.DraftSavedAt = utcTimePointer(&article.DraftBody.SavedAt)
	}
	if article.PublishedBody != nil {
		entity.PublishedBlobName = article.PublishedBody.BlobName
		entity.PublishedVersion = article.PublishedBody.Version
		entity.PublishedSavedAt = utcTimePointer(&article.PublishedBody.SavedAt)
	}
	if article.Published != nil {
		history, err := json.Marshal(article.Published.HistoricalSlugs)
		if err != nil {
			return nil, fmt.Errorf("marshal published slug history: %w", err)
		}
		entity.PublishedSlug = article.Published.Slug
		entity.PublishedTitle = article.Published.Title
		entity.PublishedSummary = article.Published.Summary
		entity.PublishedArea = article.Published.Area
		entity.PublishedCoverID = article.Published.CoverID
		entity.PublishedHistoricalJSON = string(history)
	}
	return json.Marshal(entity)
}

func unmarshalArticleEntity(encoded []byte, etag string) (articles.Article, error) {
	var entity articleEntity
	if err := json.Unmarshal(encoded, &entity); err != nil {
		return articles.Article{}, fmt.Errorf("decode article entity: %w", err)
	}
	if err := validateEntityHeader(entity.entityHeader, articlesPartition, articleEntityType); err != nil {
		return articles.Article{}, err
	}
	id := entity.ID
	if id == "" {
		id = entity.RowKey
	}
	article := articles.Article{
		ID: id, Slug: entity.Slug, Title: entity.Title, Summary: entity.Summary, Area: entity.Area, CoverID: entity.CoverID,
		Status: entity.Status, CreatedAt: entity.CreatedAt.UTC(), UpdatedAt: entity.UpdatedAt.UTC(), DeletedAt: utcTimePointer(entity.DeletedAt),
		FirstPublishedAt: utcTimePointer(entity.FirstPublishedAt), LastPublishedAt: utcTimePointer(entity.LastPublishedAt), ETag: etag,
	}
	if entity.DraftBlobName != "" || entity.DraftVersion != "" || entity.DraftSavedAt != nil {
		article.DraftBody = &articles.BodyRef{BlobName: entity.DraftBlobName, Version: entity.DraftVersion, SavedAt: utcValue(entity.DraftSavedAt)}
	}
	if entity.PublishedBlobName != "" || entity.PublishedVersion != "" || entity.PublishedSavedAt != nil {
		article.PublishedBody = &articles.BodyRef{BlobName: entity.PublishedBlobName, Version: entity.PublishedVersion, SavedAt: utcValue(entity.PublishedSavedAt)}
	}
	if entity.PublishedSlug != "" || entity.PublishedHistoricalJSON != "" {
		var history []string
		if err := json.Unmarshal([]byte(entity.PublishedHistoricalJSON), &history); err != nil {
			return articles.Article{}, fmt.Errorf("decode published slug history: %w", err)
		}
		article.Published = &articles.PublishedMetadata{
			Slug: entity.PublishedSlug, Title: entity.PublishedTitle, Summary: entity.PublishedSummary,
			Area: entity.PublishedArea, CoverID: entity.PublishedCoverID, HistoricalSlugs: history,
		}
	}
	if err := article.Validate(); err != nil {
		return articles.Article{}, err
	}
	wanted, err := articleRowKey(article.ID, article.CreatedAt)
	if err != nil || entity.RowKey != wanted && entity.RowKey != article.ID {
		return articles.Article{}, fmt.Errorf("%w: article entity key is invalid", articles.ErrValidation)
	}
	return article, nil
}

func articleRowKey(id string, createdAt time.Time) (string, error) {
	if !safeStorageSegment(id) || createdAt.IsZero() || createdAt.UnixNano() < 0 {
		return "", fmt.Errorf("%w: article storage key is invalid", articles.ErrValidation)
	}
	reverse := ^uint64(0) - uint64(createdAt.UTC().UnixNano())
	var tie strings.Builder
	for i := 0; i < len(id); i++ {
		fmt.Fprintf(&tie, "%02x", ^id[i])
	}
	return fmt.Sprintf("%020d:%sz", reverse, tie.String()), nil
}

func marshalSlugEntity(record slugRecord) ([]byte, error) {
	normalized, err := articles.NormalizeSlug(record.Slug)
	if err != nil || normalized != record.Slug || !safeStorageSegment(record.ArticleID) {
		return nil, fmt.Errorf("%w: slug record is invalid", articles.ErrValidation)
	}
	entity := slugEntity{
		entityHeader: entityHeader{PartitionKey: articlesPartition, RowKey: slugRowKey(record.Slug), EntityType: slugEntityType, SchemaVersion: storageSchemaVersion},
		Slug:         record.Slug, ArticleID: record.ArticleID, PublishedTarget: record.PublishedTarget,
	}
	return json.Marshal(entity)
}

func unmarshalSlugEntity(encoded []byte, etag string) (slugRecord, error) {
	var entity slugEntity
	if err := json.Unmarshal(encoded, &entity); err != nil {
		return slugRecord{}, fmt.Errorf("decode slug entity: %w", err)
	}
	if err := validateEntityHeader(entity.entityHeader, articlesPartition, slugEntityType); err != nil {
		return slugRecord{}, err
	}
	if entity.RowKey != slugRowKey(entity.Slug) || !safeStorageSegment(entity.ArticleID) {
		return slugRecord{}, fmt.Errorf("%w: slug entity keys are invalid", articles.ErrValidation)
	}
	return slugRecord{Slug: entity.Slug, ArticleID: entity.ArticleID, PublishedTarget: entity.PublishedTarget, ETag: etag}, nil
}

func slugRowKey(slug string) string { return "slug:" + slug }

func marshalContactEntity(contact contacts.Contact) ([]byte, error) {
	if err := contact.Validate(); err != nil {
		return nil, err
	}
	rowKey, err := contactRowKey(contact.ID, contact.CreatedAt)
	if err != nil {
		return nil, err
	}
	entity := contactEntity{
		entityHeader: entityHeader{PartitionKey: contactsPartition, RowKey: rowKey, EntityType: contactEntityType, SchemaVersion: storageSchemaVersion},
		ID:           contact.ID, Name: contact.Name, Email: contact.Email, Phone: contact.Phone, Message: contact.Message,
		ConsentVersion: contact.ConsentVersion, PrivacyAcceptedAt: contact.PrivacyAcceptedAt.UTC(), State: contact.State,
		CreatedAt: contact.CreatedAt.UTC(), UpdatedAt: contact.UpdatedAt.UTC(), ReadAt: utcTimePointer(contact.ReadAt), ArchivedAt: utcTimePointer(contact.ArchivedAt),
		ReviewDueAt: contact.ReviewDueAt.UTC(), DeletionDueAt: utcTimePointer(contact.DeletionDueAt),
	}
	return json.Marshal(entity)
}

func unmarshalContactEntity(encoded []byte, etag string) (contacts.Contact, error) {
	var entity contactEntity
	if err := json.Unmarshal(encoded, &entity); err != nil {
		return contacts.Contact{}, fmt.Errorf("decode contact entity: %w", err)
	}
	if err := validateEntityHeader(entity.entityHeader, contactsPartition, contactEntityType); err != nil {
		return contacts.Contact{}, err
	}
	wantRowKey, err := contactRowKey(entity.ID, entity.CreatedAt)
	if err != nil || wantRowKey != entity.RowKey {
		return contacts.Contact{}, fmt.Errorf("%w: contact entity keys are invalid", contacts.ErrValidation)
	}
	contact := contacts.Contact{
		ID: entity.ID, Name: entity.Name, Email: entity.Email, Phone: entity.Phone, Message: entity.Message,
		ConsentVersion: entity.ConsentVersion, PrivacyAcceptedAt: entity.PrivacyAcceptedAt.UTC(), State: entity.State,
		CreatedAt: entity.CreatedAt.UTC(), UpdatedAt: entity.UpdatedAt.UTC(), ReadAt: utcTimePointer(entity.ReadAt), ArchivedAt: utcTimePointer(entity.ArchivedAt),
		ReviewDueAt: entity.ReviewDueAt.UTC(), DeletionDueAt: utcTimePointer(entity.DeletionDueAt), ETag: etag,
	}
	if err := contact.Validate(); err != nil {
		return contacts.Contact{}, err
	}
	return contact, nil
}

func contactRowKey(id string, createdAt time.Time) (string, error) {
	if !safeStorageSegment(id) || createdAt.IsZero() || createdAt.UnixNano() < 0 {
		return "", fmt.Errorf("%w: contact storage key is invalid", contacts.ErrValidation)
	}
	reverse := ^uint64(0) - uint64(createdAt.UTC().UnixNano())
	return fmt.Sprintf("%020d:%s", reverse, id), nil
}

func marshalSessionEntity(session auth.Session) ([]byte, error) {
	if err := session.Validate(); err != nil {
		return nil, err
	}
	entity := sessionEntity{
		entityHeader: entityHeader{PartitionKey: sessionsPartition, RowKey: session.TokenHash, EntityType: sessionEntityType, SchemaVersion: storageSchemaVersion},
		TokenHash:    session.TokenHash, Username: session.Username, CredentialVersion: session.CredentialVersion,
		CreatedAt: session.CreatedAt.UTC(), ExpiresAt: session.ExpiresAt.UTC(), RevokedAt: utcTimePointer(session.RevokedAt),
	}
	return json.Marshal(entity)
}

func unmarshalSessionEntity(encoded []byte, etag string) (auth.Session, error) {
	var entity sessionEntity
	if err := json.Unmarshal(encoded, &entity); err != nil {
		return auth.Session{}, fmt.Errorf("decode session entity: %w", err)
	}
	if err := validateEntityHeader(entity.entityHeader, sessionsPartition, sessionEntityType); err != nil {
		return auth.Session{}, err
	}
	if entity.RowKey != entity.TokenHash {
		return auth.Session{}, fmt.Errorf("%w: session entity keys are invalid", auth.ErrValidation)
	}
	session := auth.Session{
		TokenHash: entity.TokenHash, Username: entity.Username, CredentialVersion: entity.CredentialVersion,
		CreatedAt: entity.CreatedAt.UTC(), ExpiresAt: entity.ExpiresAt.UTC(), RevokedAt: utcTimePointer(entity.RevokedAt), ETag: etag,
	}
	if err := session.Validate(); err != nil {
		return auth.Session{}, err
	}
	return session, nil
}

func marshalBodyEnvelope(body articles.Body) ([]byte, error) {
	if err := body.Validate(); err != nil {
		return nil, err
	}
	return json.Marshal(bodyEnvelope{
		StorageSchemaVersion: storageSchemaVersion, SchemaVersion: body.SchemaVersion,
		Document: append(json.RawMessage(nil), body.Document...), HTML: body.HTML, PlainText: body.PlainText,
	})
}

func unmarshalBodyEnvelope(encoded []byte) (articles.Body, error) {
	var envelope bodyEnvelope
	if err := json.Unmarshal(encoded, &envelope); err != nil {
		return articles.Body{}, fmt.Errorf("decode article body envelope: %w", err)
	}
	if envelope.StorageSchemaVersion != storageSchemaVersion {
		return articles.Body{}, fmt.Errorf("%w: unsupported body storage schema", articles.ErrValidation)
	}
	body := articles.Body{SchemaVersion: envelope.SchemaVersion, Document: append(json.RawMessage(nil), envelope.Document...), HTML: envelope.HTML, PlainText: envelope.PlainText}
	if err := body.Validate(); err != nil {
		return articles.Body{}, err
	}
	return body, nil
}

func articleBlobName(articleID, version string) (string, error) {
	if !safeStorageSegment(articleID) || !safeStorageSegment(version) {
		return "", fmt.Errorf("%w: article body storage key is invalid", articles.ErrValidation)
	}
	return "articles/" + articleID + "/" + version + ".json", nil
}

func validateEntityHeader(header entityHeader, partition, entityType string) error {
	if header.PartitionKey != partition || header.RowKey == "" || header.EntityType != entityType || header.SchemaVersion != storageSchemaVersion {
		return fmt.Errorf("invalid %s storage entity schema", entityType)
	}
	return nil
}

func safeStorageSegment(value string) bool {
	if value == "" {
		return false
	}
	for _, character := range value {
		if !(character >= 'a' && character <= 'z' || character >= 'A' && character <= 'Z' || character >= '0' && character <= '9' || character == '-' || character == '_') {
			return false
		}
	}
	return true
}

func utcTimePointer(value *time.Time) *time.Time {
	if value == nil {
		return nil
	}
	utc := value.UTC()
	return &utc
}

func utcValue(value *time.Time) time.Time {
	if value == nil {
		return time.Time{}
	}
	return value.UTC()
}
