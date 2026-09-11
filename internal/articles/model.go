package articles

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"
)

const maxDocumentBytes = 512 * 1024

const (
	maxDocumentDepth = 32
	maxDocumentNodes = 5000
	maxLinkLength    = 2048
)

var (
	ErrNotFound          = errors.New("article not found")
	ErrConflict          = errors.New("article conflict")
	ErrValidation        = errors.New("article validation failed")
	ErrSlugTaken         = errors.New("article slug is already taken")
	ErrInvalidTransition = errors.New("invalid article transition")
)

type Status string

const (
	StatusDraft     Status = "draft"
	StatusPublished Status = "published"
	StatusWithdrawn Status = "withdrawn"
)

type BodyRef struct {
	BlobName string    `json:"blobName"`
	Version  string    `json:"version"`
	SavedAt  time.Time `json:"savedAt"`
}

type Article struct {
	ID               string
	Slug             string
	Title            string
	Summary          string
	Area             string
	CoverID          string
	Status           Status
	DraftBody        *BodyRef
	PublishedBody    *BodyRef
	FirstPublishedAt *time.Time
	LastPublishedAt  *time.Time
	CreatedAt        time.Time
	UpdatedAt        time.Time
	DeletedAt        *time.Time
	ETag             string
}

type Body struct {
	SchemaVersion int             `json:"schemaVersion"`
	Document      json.RawMessage `json:"document"`
	HTML          string          `json:"html"`
	PlainText     string          `json:"plainText"`
}

func NormalizeSlug(value string) (string, error) {
	value = strings.TrimSpace(value)
	var normalized strings.Builder
	separatorPending := false

	for _, character := range value {
		switch {
		case character >= 'A' && character <= 'Z':
			if separatorPending && normalized.Len() > 0 {
				normalized.WriteByte('-')
			}
			separatorPending = false
			normalized.WriteRune(character + ('a' - 'A'))
		case character >= 'a' && character <= 'z', character >= '0' && character <= '9':
			if separatorPending && normalized.Len() > 0 {
				normalized.WriteByte('-')
			}
			separatorPending = false
			normalized.WriteRune(character)
		case character == '-' || isASCIIWhitespace(character):
			separatorPending = normalized.Len() > 0
		default:
			return "", fmt.Errorf("%w: slug contains unsupported character %q", ErrValidation, character)
		}
	}

	result := normalized.String()
	if len(result) < 3 || len(result) > 100 {
		return "", fmt.Errorf("%w: slug must contain between 3 and 100 characters", ErrValidation)
	}
	return result, nil
}

func (article Article) Validate() error {
	if strings.TrimSpace(article.ID) == "" {
		return validationError("ID is required")
	}
	normalizedSlug, err := NormalizeSlug(article.Slug)
	if err != nil || normalizedSlug != article.Slug {
		return validationError("slug must be normalized")
	}
	if err := validateTrimmedRuneLength("title", article.Title, 5, 160); err != nil {
		return err
	}
	if err := validateTrimmedRuneLength("summary", article.Summary, 20, 320); err != nil {
		return err
	}
	if strings.TrimSpace(article.Area) == "" {
		return validationError("area is required")
	}
	if strings.TrimSpace(article.CoverID) == "" {
		return validationError("cover ID is required")
	}
	if !validStatus(article.Status) {
		return validationError("status is invalid")
	}
	if article.CreatedAt.IsZero() || article.UpdatedAt.IsZero() {
		return validationError("created and updated timestamps are required")
	}
	if article.UpdatedAt.Before(article.CreatedAt) {
		return validationError("updated timestamp cannot precede created timestamp")
	}
	if article.DraftBody != nil {
		if err := article.DraftBody.validate(); err != nil {
			return err
		}
	}
	if article.PublishedBody != nil {
		if err := article.PublishedBody.validate(); err != nil {
			return err
		}
	}
	if article.Status == StatusPublished || article.Status == StatusWithdrawn {
		if article.PublishedBody == nil || article.FirstPublishedAt == nil || article.LastPublishedAt == nil {
			return validationError("published or withdrawn article requires body and publication timestamps")
		}
	}
	if article.Status == StatusDraft && (article.PublishedBody != nil || article.FirstPublishedAt != nil || article.LastPublishedAt != nil) {
		return validationError("draft article cannot have publication data")
	}
	if article.FirstPublishedAt != nil && article.FirstPublishedAt.Before(article.CreatedAt) {
		return validationError("first publication cannot precede creation")
	}
	if article.FirstPublishedAt != nil && article.FirstPublishedAt.After(article.UpdatedAt) {
		return validationError("first publication cannot follow update")
	}
	if article.LastPublishedAt != nil {
		if article.FirstPublishedAt == nil || article.LastPublishedAt.Before(*article.FirstPublishedAt) {
			return validationError("last publication cannot precede first publication")
		}
		if article.LastPublishedAt.After(article.UpdatedAt) {
			return validationError("last publication cannot follow update")
		}
	}
	return nil
}

func (body Body) Validate() error {
	if body.SchemaVersion != 1 {
		return validationError("body schema version must be 1")
	}
	if len(body.Document) == 0 || len(body.Document) > maxDocumentBytes || !json.Valid(body.Document) {
		return validationError("body document must be valid JSON of at most 512 KiB")
	}
	var document any
	if err := json.Unmarshal(body.Document, &document); err != nil {
		return validationError("body document must be valid JSON")
	}
	nodeCount := 0
	if err := validateDocument(document, 0, &nodeCount); err != nil {
		return err
	}
	if strings.TrimSpace(body.HTML) == "" {
		return validationError("body HTML is required")
	}
	if strings.TrimSpace(body.PlainText) == "" {
		return validationError("body plain text is required")
	}
	return nil
}

func validateDocument(value any, nodeDepth int, nodeCount *int) error {
	switch typed := value.(type) {
	case map[string]any:
		if _, isNode := typed["type"].(string); isNode {
			nodeDepth++
			*nodeCount++
			if nodeDepth > maxDocumentDepth {
				return validationError("body document exceeds maximum depth")
			}
			if *nodeCount > maxDocumentNodes {
				return validationError("body document exceeds maximum node count")
			}
		}
		if href, ok := typed["href"].(string); ok && utf8.RuneCountInString(href) > maxLinkLength {
			return validationError("body document link exceeds maximum length")
		}
		for _, child := range typed {
			if err := validateDocument(child, nodeDepth, nodeCount); err != nil {
				return err
			}
		}
	case []any:
		for _, child := range typed {
			if err := validateDocument(child, nodeDepth, nodeCount); err != nil {
				return err
			}
		}
	}
	return nil
}

func ValidateTransition(from, to Status) error {
	if !validStatus(from) || !validStatus(to) {
		return fmt.Errorf("%w: %q to %q", ErrInvalidTransition, from, to)
	}
	allowed := from == to ||
		(from == StatusDraft && to == StatusPublished) ||
		(from == StatusPublished && to == StatusWithdrawn) ||
		(from == StatusWithdrawn && to == StatusPublished)
	if !allowed {
		return fmt.Errorf("%w: %q to %q", ErrInvalidTransition, from, to)
	}
	return nil
}

func (ref BodyRef) validate() error {
	if strings.TrimSpace(ref.BlobName) == "" || strings.TrimSpace(ref.Version) == "" || ref.SavedAt.IsZero() {
		return validationError("body reference is incomplete")
	}
	return nil
}

func validStatus(status Status) bool {
	return status == StatusDraft || status == StatusPublished || status == StatusWithdrawn
}

func validateTrimmedRuneLength(field, value string, minimum, maximum int) error {
	trimmed := strings.TrimSpace(value)
	length := utf8.RuneCountInString(trimmed)
	if trimmed != value || length < minimum || length > maximum {
		return validationError(fmt.Sprintf("%s must contain between %d and %d characters after trim", field, minimum, maximum))
	}
	return nil
}

func validationError(message string) error {
	return fmt.Errorf("%w: %s", ErrValidation, message)
}

func isASCIIWhitespace(character rune) bool {
	return character == ' ' || character == '\t' || character == '\n' || character == '\r' || character == '\f' || character == '\v'
}
