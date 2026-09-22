package storage

import (
	"context"

	"github.com/francescostumpo/legal-callegarin/internal/articles"
	"github.com/francescostumpo/legal-callegarin/internal/auth"
	"github.com/francescostumpo/legal-callegarin/internal/contacts"
)

type Readiness interface{ Ready(context.Context) error }

type Bundle struct {
	Articles  articles.MetadataRepository
	Bodies    articles.BodyStore
	Contacts  contacts.Repository
	Sessions  auth.SessionRepository
	Readiness Readiness
}

func (bundle *Bundle) Complete() bool {
	return bundle != nil && bundle.Articles != nil && bundle.Bodies != nil && bundle.Contacts != nil && bundle.Sessions != nil && bundle.Readiness != nil
}
