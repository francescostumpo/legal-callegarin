package articles

import "context"

type MetadataRepository interface {
	Create(context.Context, Article) (Article, error)
	Get(context.Context, string) (Article, error)
	GetBySlug(context.Context, string) (Article, error)
	GetPublishedBySlug(context.Context, string) (Article, error)
	List(context.Context, ListOptions) (ArticlePage, error)
	Update(context.Context, Article, string) (Article, error)
}

type ListOptions struct {
	Status *Status
	Cursor string
	Limit  int
}

type ArticlePage struct {
	Items      []Article
	NextCursor string
	PageNumber int
}

type BodyStore interface {
	Put(context.Context, string, Body) (BodyRef, error)
	Get(context.Context, BodyRef) (Body, error)
	Delete(context.Context, BodyRef) error
}
