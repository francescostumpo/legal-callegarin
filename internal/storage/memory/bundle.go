package memory

import (
	"context"
	"time"

	"github.com/francescostumpo/legal-callegarin/internal/storage"
)

func NewBundle(now func() time.Time) *storage.Bundle {
	return &storage.Bundle{
		Articles: NewArticleMetadataRepository(), Bodies: NewArticleBodyStore(now),
		Contacts: NewContactRepository(now), Sessions: NewSessionRepository(), Readiness: memoryReadiness{},
	}
}

type memoryReadiness struct{}

func (memoryReadiness) Ready(ctx context.Context) error { return ctx.Err() }
