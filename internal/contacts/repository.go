package contacts

import "context"

type ListOptions struct {
	State           *State
	Query           string
	Cursor          string
	Limit           int
	RetentionReview bool
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
