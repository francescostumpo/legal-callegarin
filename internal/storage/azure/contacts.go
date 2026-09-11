package azure

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/francescostumpo/legal-callegarin/internal/contacts"
)

type ContactRepository struct {
	table tableDriver
	now   func() time.Time
}

func newContactRepository(table tableDriver, now func() time.Time) *ContactRepository {
	return &ContactRepository{table: table, now: now}
}

func (repository *ContactRepository) Create(ctx context.Context, contact contacts.Contact) (contacts.Contact, error) {
	if err := contact.Validate(); err != nil {
		return contacts.Contact{}, err
	}
	if _, err := repository.findByID(ctx, contact.ID); err == nil {
		return contacts.Contact{}, contacts.ErrConflict
	} else if !errors.Is(err, contacts.ErrNotFound) {
		return contacts.Contact{}, err
	}
	encoded, err := marshalContactEntity(contact)
	if err != nil {
		return contacts.Contact{}, err
	}
	etag, err := repository.table.Add(ctx, encoded)
	if err != nil {
		if mutationOutcomeMayBeUnknown(err) {
			observed, getErr := repository.findByID(ctx, contact.ID)
			switch {
			case getErr == nil && sameContactState(observed, contact):
				return observed, nil
			case errors.Is(getErr, contacts.ErrNotFound):
				return contacts.Contact{}, mapContactError(err)
			default:
				return contacts.Contact{}, unknownCommitForContext(ctx, contacts.ErrCommitUnknown, err)
			}
		}
		return contacts.Contact{}, mapContactError(err)
	}
	contact.ETag = etag
	if etag == "" {
		return repository.refreshCommitted(ctx, contact)
	}
	return contact, nil
}
func (repository *ContactRepository) Get(ctx context.Context, id string) (contacts.Contact, error) {
	return repository.findByID(ctx, id)
}

func (repository *ContactRepository) findByID(ctx context.Context, id string) (contacts.Contact, error) {
	if !safeStorageSegment(id) {
		return contacts.Contact{}, contacts.ErrNotFound
	}
	filter := "PartitionKey eq 'contacts' and id eq '" + strings.ReplaceAll(id, "'", "''") + "'"
	entities, err := repository.table.List(ctx, filter, 2)
	if err != nil {
		return contacts.Contact{}, mapContactError(err)
	}
	for _, entity := range entities {
		var header entityHeader
		if err := decodeHeader(entity.Value, &header); err != nil {
			return contacts.Contact{}, err
		}
		if header.EntityType != contactEntityType {
			continue
		}
		contact, err := unmarshalContactEntity(entity.Value, entity.ETag)
		if err != nil {
			return contacts.Contact{}, err
		}
		if contact.ID == id {
			return contact, nil
		}
	}
	return contacts.Contact{}, contacts.ErrNotFound
}

func (repository *ContactRepository) List(ctx context.Context, options contacts.ListOptions) (contacts.ContactPage, error) {
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
	entities, err := repository.table.List(ctx, "PartitionKey eq 'contacts'", contacts.MaxAdminContactScan+1)
	if err != nil {
		return contacts.ContactPage{}, mapContactError(err)
	}
	if len(entities) > contacts.MaxAdminContactScan {
		return contacts.ContactPage{}, fmt.Errorf("%w: active contact set exceeds %d", contacts.ErrValidation, contacts.MaxAdminContactScan)
	}
	query := strings.ToLower(strings.TrimSpace(options.Query))
	items := make([]contacts.Contact, 0, len(entities))
	for _, entity := range entities {
		var header entityHeader
		if err := decodeHeader(entity.Value, &header); err != nil {
			return contacts.ContactPage{}, err
		}
		if header.EntityType != contactEntityType {
			continue
		}
		contact, err := unmarshalContactEntity(entity.Value, entity.ETag)
		if err != nil {
			return contacts.ContactPage{}, err
		}
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
		items = append(items, contact)
	}
	sort.Slice(items, func(i, j int) bool {
		if items[i].CreatedAt.Equal(items[j].CreatedAt) {
			return items[i].ID > items[j].ID
		}
		return items[i].CreatedAt.After(items[j].CreatedAt)
	})
	start, err := cursorStartContacts(items, options.Cursor)
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
	if err := contact.Validate(); err != nil {
		return contacts.Contact{}, err
	}
	stored, err := repository.findByID(ctx, contact.ID)
	if err != nil {
		return contacts.Contact{}, err
	}
	if stored.ETag != expectedETag {
		return contacts.Contact{}, contacts.ErrConflict
	}
	if !stored.CreatedAt.Equal(contact.CreatedAt) {
		return contacts.Contact{}, fmt.Errorf("%w: created timestamp cannot change", contacts.ErrValidation)
	}
	encoded, err := marshalContactEntity(contact)
	if err != nil {
		return contacts.Contact{}, err
	}
	etag, err := repository.table.Update(ctx, encoded, expectedETag)
	if err != nil {
		if mutationOutcomeMayBeUnknown(err) {
			observed, getErr := repository.findByID(ctx, contact.ID)
			switch {
			case getErr == nil && sameContactState(observed, contact):
				return observed, nil
			case getErr == nil && sameContactState(observed, stored):
				return contacts.Contact{}, mapContactError(err)
			default:
				return contacts.Contact{}, unknownCommitForContext(ctx, contacts.ErrCommitUnknown, err)
			}
		}
		return contacts.Contact{}, mapContactError(err)
	}
	contact.ETag = etag
	if etag == "" {
		return repository.refreshCommitted(ctx, contact)
	}
	return contact, nil
}

func (repository *ContactRepository) refreshCommitted(ctx context.Context, desired contacts.Contact) (contacts.Contact, error) {
	observed, err := repository.Get(ctx, desired.ID)
	if err != nil {
		return contacts.Contact{}, unknownCommitForContext(ctx, contacts.ErrCommitUnknown, err)
	}
	if !sameContactState(observed, desired) {
		return contacts.Contact{}, unknownCommitForContext(ctx, contacts.ErrCommitUnknown, errors.New("committed contact refresh did not match expected state"))
	}
	return observed, nil
}
func (repository *ContactRepository) Delete(ctx context.Context, id, expectedETag string) error {
	stored, err := repository.findByID(ctx, id)
	if err != nil {
		return err
	}
	if stored.ETag != expectedETag {
		return contacts.ErrConflict
	}
	row, err := contactRowKey(stored.ID, stored.CreatedAt)
	if err != nil {
		return err
	}
	err = repository.table.Delete(ctx, contactsPartition, row, expectedETag)
	if err == nil || !mutationOutcomeMayBeUnknown(err) {
		return mapContactError(err)
	}
	observed, getErr := repository.findByID(ctx, id)
	switch {
	case errors.Is(getErr, contacts.ErrNotFound):
		return nil
	case getErr == nil && sameContactState(observed, stored):
		return mapContactError(err)
	default:
		return unknownCommitForContext(ctx, contacts.ErrCommitUnknown, err)
	}
}
func contactMatches(contact contacts.Contact, query string) bool {
	return strings.Contains(strings.ToLower(contact.Name), query) || strings.Contains(strings.ToLower(contact.Email), query)
}
func cursorStartContacts(items []contacts.Contact, value string) (int, error) {
	if value == "" {
		return 0, nil
	}
	cursor, err := decodeCursor(value)
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
func mapContactError(err error) error {
	switch {
	case err == nil:
		return nil
	case errors.Is(err, ErrNotFound):
		return contacts.ErrNotFound
	case errors.Is(err, ErrConflict), errors.Is(err, ErrPrecondition):
		return contacts.ErrConflict
	default:
		return err
	}
}
