package articles

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/francescostumpo/legal-callegarin/internal/webassets"
)

const (
	maxDocumentBytes  = 512 * 1024
	maxHTMLBytes      = 512 * 1024
	maxPlainTextBytes = 512 * 1024
)

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
	ErrCommitUnknown     = errors.New("article persistence outcome is unknown")
	ErrBodyTooLarge      = errors.New("article body exceeds the storage limit")
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

type PublishedMetadata struct {
	Slug            string
	Title           string
	Summary         string
	Area            string
	CoverID         string
	HistoricalSlugs []string
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
	Published        *PublishedMetadata
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
	if !ValidArea(article.Area) {
		return validationError("area is invalid")
	}
	if !webassets.ValidCoverID(article.CoverID) {
		return validationError("cover ID is invalid")
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
	if article.Published != nil {
		if err := article.Published.validate(); err != nil {
			return err
		}
	}
	if article.Status == StatusPublished || article.Status == StatusWithdrawn {
		if article.PublishedBody == nil || article.Published == nil || article.FirstPublishedAt == nil || article.LastPublishedAt == nil {
			return validationError("published or withdrawn article requires metadata, body, and publication timestamps")
		}
	}
	if article.Status == StatusDraft && (article.PublishedBody != nil || article.Published != nil || article.FirstPublishedAt != nil || article.LastPublishedAt != nil) {
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

func (published PublishedMetadata) validate() error {
	normalizedSlug, err := NormalizeSlug(published.Slug)
	if err != nil || normalizedSlug != published.Slug {
		return validationError("published slug must be normalized")
	}
	if err := validateTrimmedRuneLength("published title", published.Title, 5, 160); err != nil {
		return err
	}
	if err := validateTrimmedRuneLength("published summary", published.Summary, 20, 320); err != nil {
		return err
	}
	if !ValidArea(published.Area) || !webassets.ValidCoverID(published.CoverID) {
		return validationError("published area or cover ID is invalid")
	}
	seen := map[string]bool{published.Slug: true}
	for _, slug := range published.HistoricalSlugs {
		normalized, err := NormalizeSlug(slug)
		if err != nil || normalized != slug || seen[slug] {
			return validationError("historical published slugs must be normalized and unique")
		}
		seen[slug] = true
	}
	return nil
}

func (body Body) Validate() error {
	compiled, err := CompileDocument(body.SchemaVersion, body.Document)
	if err != nil {
		return err
	}
	if body.HTML != compiled.HTML || body.PlainText != compiled.PlainText {
		return validationError("body derived fields are not canonical")
	}
	return nil
}

var validAreas = map[string]bool{"famiglia-e-persone": true, "successioni-e-donazioni": true, "obbligazioni-e-contratti": true, "recupero-crediti": true, "risarcimento-danni": true, "diritti-reali": true, "diritto-penale": true, "diritto-tributario": true}

func ValidArea(area string) bool { return validAreas[area] }

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
