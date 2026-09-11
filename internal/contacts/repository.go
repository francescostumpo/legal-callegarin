package contacts

import (
	"context"
	"fmt"
	"strings"
	"unicode/utf8"
)

const (
	MaxAdminSearchRunes = 120
	MaxAdminCursorBytes = 1024
)

type ListOptions struct {
	State             *State
	Query             string
	Cursor            string
	Limit             int
	RetentionReview   bool
	DeletionScheduled bool
}

func (options ListOptions) Validate() error {
	if options.State != nil && !validState(*options.State) {
		return fmt.Errorf("%w: invalid state filter", ErrValidation)
	}
	if utf8.RuneCountInString(strings.TrimSpace(options.Query)) > MaxAdminSearchRunes {
		return fmt.Errorf("%w: query exceeds %d characters", ErrValidation, MaxAdminSearchRunes)
	}
	if len(options.Cursor) > MaxAdminCursorBytes {
		return fmt.Errorf("%w: cursor exceeds %d bytes", ErrValidation, MaxAdminCursorBytes)
	}
	if options.Limit < 0 {
		return fmt.Errorf("%w: limit cannot be negative", ErrValidation)
	}
	return nil
}

type ContactPage struct {
	Items      []Contact
	NextCursor string
}

type Repository interface {
	Create(context.Context, Contact) (Contact, error)
	Get(context.Context, string) (Contact, error)
	List(context.Context, ListOptions) (ContactPage, error)
	Update(context.Context, Contact, string) (Contact, error)
	Delete(context.Context, string, string) error
}
