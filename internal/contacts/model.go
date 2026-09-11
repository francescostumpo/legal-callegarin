package contacts

import (
	"errors"
	"fmt"
	"net/mail"
	"strings"
	"time"
	"unicode/utf8"
)

var (
	ErrNotFound          = errors.New("contact not found")
	ErrConflict          = errors.New("contact conflict")
	ErrValidation        = errors.New("contact validation failed")
	ErrInvalidTransition = errors.New("invalid contact transition")
	ErrCommitUnknown     = errors.New("contact persistence outcome is unknown")
)

type State string

const (
	StateNew      State = "new"
	StateRead     State = "read"
	StateArchived State = "archived"
)

type Contact struct {
	ID                string
	Name              string
	Email             string
	Phone             string
	Message           string
	ConsentVersion    string
	PrivacyAcceptedAt time.Time
	State             State
	CreatedAt         time.Time
	UpdatedAt         time.Time
	ReadAt            *time.Time
	ArchivedAt        *time.Time
	ReviewDueAt       time.Time
	DeletionDueAt     *time.Time
	ETag              string
}

func (contact Contact) Validate() error {
	if strings.TrimSpace(contact.ID) == "" {
		return validationError("ID is required")
	}
	if err := validateTrimmedRuneLength("name", contact.Name, 2, 120); err != nil {
		return err
	}
	if contact.Email != strings.TrimSpace(contact.Email) || len(contact.Email) > 254 {
		return validationError("email must contain at most 254 characters")
	}
	parsedEmail, err := mail.ParseAddress(contact.Email)
	if err != nil || parsedEmail.Address != contact.Email {
		return validationError("email is invalid")
	}
	if contact.Phone != strings.TrimSpace(contact.Phone) || utf8.RuneCountInString(contact.Phone) > 40 {
		return validationError("phone must contain at most 40 characters")
	}
	if err := validateTrimmedRuneLength("message", contact.Message, 20, 2000); err != nil {
		return err
	}
	if strings.TrimSpace(contact.ConsentVersion) == "" {
		return validationError("consent version is required")
	}
	if contact.PrivacyAcceptedAt.IsZero() || contact.CreatedAt.IsZero() || contact.UpdatedAt.IsZero() || contact.ReviewDueAt.IsZero() {
		return validationError("privacy, created, updated, and review timestamps are required")
	}
	if contact.UpdatedAt.Before(contact.CreatedAt) {
		return validationError("updated timestamp cannot precede created timestamp")
	}
	if !contact.ReviewDueAt.Equal(contact.CreatedAt.AddDate(2, 0, 0)) {
		return validationError("review due timestamp must be 24 months after creation")
	}
	if !validState(contact.State) {
		return validationError("state is invalid")
	}
	switch contact.State {
	case StateNew:
		if contact.ReadAt != nil || contact.ArchivedAt != nil {
			return validationError("new contact cannot have read or archive timestamps")
		}
	case StateRead:
		if contact.ReadAt == nil || contact.ArchivedAt != nil {
			return validationError("read contact requires only a read timestamp")
		}
	case StateArchived:
		if contact.ReadAt == nil || contact.ArchivedAt == nil {
			return validationError("archived contact requires read and archive timestamps")
		}
	}
	if contact.ReadAt != nil && (contact.ReadAt.Before(contact.CreatedAt) || contact.ReadAt.After(contact.UpdatedAt)) {
		return validationError("read timestamp must be between creation and last update")
	}
	if contact.ArchivedAt != nil && (contact.ReadAt == nil || contact.ArchivedAt.Before(*contact.ReadAt) || contact.ArchivedAt.After(contact.UpdatedAt)) {
		return validationError("archive timestamp must be between reading and last update")
	}
	if contact.DeletionDueAt != nil && !contact.DeletionDueAt.After(contact.CreatedAt) {
		return validationError("deletion due timestamp must follow creation")
	}
	return nil
}

func ValidateTransition(from, to State) error {
	if !validState(from) || !validState(to) {
		return fmt.Errorf("%w: %q to %q", ErrInvalidTransition, from, to)
	}
	allowed := from == to ||
		(from == StateNew && to == StateRead) ||
		(from == StateRead && to == StateArchived) ||
		(from == StateArchived && to == StateRead)
	if !allowed {
		return fmt.Errorf("%w: %q to %q", ErrInvalidTransition, from, to)
	}
	return nil
}

func validState(state State) bool {
	return state == StateNew || state == StateRead || state == StateArchived
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
