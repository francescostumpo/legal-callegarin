package memory

import (
	"context"
	"encoding/base64"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/francescostumpo/legal-callegarin/internal/contacts"
)

type ContactRepository struct {
	mu       sync.RWMutex
	now      func() time.Time
	contacts map[string]contacts.Contact
	nextETag uint64
}

func NewContactRepository(now func() time.Time) *ContactRepository {
	return &ContactRepository{now: now, contacts: make(map[string]contacts.Contact)}
}

func (repository *ContactRepository) Create(ctx context.Context, contact contacts.Contact) (contacts.Contact, error) {
	if err := ctx.Err(); err != nil {
		return contacts.Contact{}, err
	}
	if err := contact.Validate(); err != nil {
		return contacts.Contact{}, err
	}
	repository.mu.Lock()
	defer repository.mu.Unlock()
	if _, exists := repository.contacts[contact.ID]; exists {
		return contacts.Contact{}, contacts.ErrConflict
	}
	contact.ETag = repository.newETag()
	repository.contacts[contact.ID] = cloneContact(contact)
	return cloneContact(contact), nil
}

func (repository *ContactRepository) Get(ctx context.Context, id string) (contacts.Contact, error) {
	if err := ctx.Err(); err != nil {
		return contacts.Contact{}, err
	}
	repository.mu.RLock()
	defer repository.mu.RUnlock()
	contact, exists := repository.contacts[id]
	if !exists {
		return contacts.Contact{}, contacts.ErrNotFound
	}
	return cloneContact(contact), nil
}

func (repository *ContactRepository) List(ctx context.Context, options contacts.ListOptions) (contacts.ContactPage, error) {
	if err := ctx.Err(); err != nil {
		return contacts.ContactPage{}, err
	}
	if err := options.Validate(); err != nil {
		return contacts.ContactPage{}, err
	}
	limit, err := boundedLimit(options.Limit)
	if err != nil {
		return contacts.ContactPage{}, fmt.Errorf("%w: %v", contacts.ErrValidation, err)
	}
	if options.RetentionReview && repository.now == nil {
		return contacts.ContactPage{}, fmt.Errorf("%w: clock is required for retention review", contacts.ErrValidation)
	}
	query := strings.ToLower(strings.TrimSpace(options.Query))

	repository.mu.RLock()
	items := make([]contacts.Contact, 0, len(repository.contacts))
	for _, contact := range repository.contacts {
		if options.State != nil && contact.State != *options.State {
			continue
		}
		if options.RetentionReview && contact.ReviewDueAt.After(repository.now()) {
			continue
		}
		if options.DeletionScheduled && contact.DeletionDueAt == nil {
			continue
		}
		if query != "" && !contactMatches(contact, query) {
			continue
		}
		items = append(items, cloneContact(contact))
	}
	repository.mu.RUnlock()
	sort.Slice(items, func(left, right int) bool {
		if items[left].CreatedAt.Equal(items[right].CreatedAt) {
			return items[left].ID > items[right].ID
		}
		return items[left].CreatedAt.After(items[right].CreatedAt)
	})

	start, err := contactCursorStart(items, options.Cursor)
	if err != nil {
		return contacts.ContactPage{}, fmt.Errorf("%w: invalid cursor", contacts.ErrValidation)
	}
	end := min(start+limit, len(items))
	page := contacts.ContactPage{Items: items[start:end]}
	if end < len(items) {
		last := page.Items[len(page.Items)-1]
		page.NextCursor = encodeCursor(last.CreatedAt, last.ID)
	}
	return page, nil
}

func (repository *ContactRepository) Update(ctx context.Context, contact contacts.Contact, expectedETag string) (contacts.Contact, error) {
	if err := ctx.Err(); err != nil {
		return contacts.Contact{}, err
	}
	if err := contact.Validate(); err != nil {
		return contacts.Contact{}, err
	}
	repository.mu.Lock()
	defer repository.mu.Unlock()
	stored, exists := repository.contacts[contact.ID]
	if !exists {
		return contacts.Contact{}, contacts.ErrNotFound
	}
	if stored.ETag != expectedETag {
		return contacts.Contact{}, contacts.ErrConflict
	}
	contact.ETag = repository.newETag()
	repository.contacts[contact.ID] = cloneContact(contact)
	return cloneContact(contact), nil
}

func (repository *ContactRepository) Delete(ctx context.Context, id, expectedETag string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	repository.mu.Lock()
	defer repository.mu.Unlock()
	stored, exists := repository.contacts[id]
	if !exists {
		return contacts.ErrNotFound
	}
	if stored.ETag != expectedETag {
		return contacts.ErrConflict
	}
	delete(repository.contacts, id)
	return nil
}

func (repository *ContactRepository) newETag() string {
	repository.nextETag++
	return base64.RawURLEncoding.EncodeToString([]byte(strconv.FormatUint(repository.nextETag, 10)))
}

func contactMatches(contact contacts.Contact, query string) bool {
	return strings.Contains(strings.ToLower(contact.Name), query) ||
		strings.Contains(strings.ToLower(contact.Email), query)
}

func contactCursorStart(items []contacts.Contact, encoded string) (int, error) {
	if encoded == "" {
		return 0, nil
	}
	cursor, err := decodeCursor(encoded)
	if err != nil {
		return 0, err
	}
	for index, item := range items {
		if item.ID == cursor.ID && item.CreatedAt.Equal(cursor.CreatedAt) {
			return index + 1, nil
		}
	}
	return 0, contacts.ErrValidation
}

func cloneContact(contact contacts.Contact) contacts.Contact {
	contact.ReadAt = cloneTime(contact.ReadAt)
	contact.ArchivedAt = cloneTime(contact.ArchivedAt)
	contact.DeletionDueAt = cloneTime(contact.DeletionDueAt)
	return contact
}

func cloneTime(value *time.Time) *time.Time {
	if value == nil {
		return nil
	}
	copy := *value
	return &copy
}
