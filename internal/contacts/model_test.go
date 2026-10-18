package contacts

import (
	"errors"
	"strings"
	"testing"
	"time"
)

func TestContactValidate(t *testing.T) {
	t.Parallel()

	valid := validContact()
	if err := valid.Validate(); err != nil {
		t.Fatalf("valid Contact.Validate() error = %v", err)
	}

	tests := []struct {
		name   string
		mutate func(*Contact)
	}{
		{name: "missing ID", mutate: func(contact *Contact) { contact.ID = "" }},
		{name: "short name", mutate: func(contact *Contact) { contact.Name = "A" }},
		{name: "long Unicode name", mutate: func(contact *Contact) { contact.Name = strings.Repeat("è", 121) }},
		{name: "invalid email", mutate: func(contact *Contact) { contact.Email = "not-an-email" }},
		{name: "long email", mutate: func(contact *Contact) { contact.Email = strings.Repeat("a", 250) + "@x.it" }},
		{name: "long phone", mutate: func(contact *Contact) { contact.Phone = strings.Repeat("1", 41) }},
		{name: "short message", mutate: func(contact *Contact) { contact.Message = strings.Repeat("a", 19) }},
		{name: "long message", mutate: func(contact *Contact) { contact.Message = strings.Repeat("a", 2001) }},
		{name: "missing consent version", mutate: func(contact *Contact) { contact.ConsentVersion = "" }},
		{name: "missing privacy timestamp", mutate: func(contact *Contact) { contact.PrivacyAcceptedAt = time.Time{} }},
		{name: "invalid state", mutate: func(contact *Contact) { contact.State = "unknown" }},
		{name: "missing created timestamp", mutate: func(contact *Contact) { contact.CreatedAt = time.Time{} }},
		{name: "updated before created", mutate: func(contact *Contact) { contact.UpdatedAt = contact.CreatedAt.Add(-time.Second) }},
		{name: "wrong review due timestamp", mutate: func(contact *Contact) { contact.ReviewDueAt = contact.ReviewDueAt.Add(time.Second) }},
		{name: "read without read timestamp", mutate: func(contact *Contact) { contact.ReadAt = nil }},
		{name: "read before creation", mutate: func(contact *Contact) { read := contact.CreatedAt.Add(-time.Second); contact.ReadAt = &read }},
		{name: "archived without archived timestamp", mutate: func(contact *Contact) { contact.State, contact.ArchivedAt = StateArchived, nil }},
		{name: "archived before read", mutate: func(contact *Contact) {
			archived := contact.ReadAt.Add(-time.Second)
			contact.State, contact.ArchivedAt = StateArchived, &archived
		}},
		{name: "deletion before creation", mutate: func(contact *Contact) { due := contact.CreatedAt.Add(-time.Second); contact.DeletionDueAt = &due }},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			contact := validContact()
			tt.mutate(&contact)
			if err := contact.Validate(); !errors.Is(err, ErrValidation) {
				t.Fatalf("Contact.Validate() error = %v, want ErrValidation", err)
			}
		})
	}
}

func TestStateTransitions(t *testing.T) {
	t.Parallel()

	states := []State{StateNew, StateRead, StateArchived}
	allowed := map[[2]State]bool{
		{StateNew, StateNew}:           true,
		{StateNew, StateRead}:          true,
		{StateRead, StateRead}:         true,
		{StateRead, StateArchived}:     true,
		{StateArchived, StateArchived}: true,
		{StateArchived, StateRead}:     true,
	}

	for _, from := range states {
		for _, to := range states {
			err := ValidateTransition(from, to)
			if allowed[[2]State{from, to}] && err != nil {
				t.Errorf("ValidateTransition(%q, %q) error = %v", from, to, err)
			}
			if !allowed[[2]State{from, to}] && !errors.Is(err, ErrInvalidTransition) {
				t.Errorf("ValidateTransition(%q, %q) error = %v, want ErrInvalidTransition", from, to, err)
			}
		}
	}
}

func validContact() Contact {
	created := time.Date(2024, time.February, 29, 10, 0, 0, 0, time.UTC)
	read := created.Add(time.Hour)
	return Contact{
		ID:                "contact-1",
		Name:              "Mario Rossi",
		Email:             "mario@example.test",
		Phone:             "+39 000 000000",
		Message:           "Messaggio sufficientemente lungo",
		ConsentVersion:    "privacy-v1",
		PrivacyAcceptedAt: created,
		State:             StateRead,
		CreatedAt:         created,
		UpdatedAt:         read,
		ReadAt:            &read,
		ReviewDueAt:       created.AddDate(2, 0, 0),
		ETag:              "1",
	}
}
