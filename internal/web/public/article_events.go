package public

import (
	"context"
	"sync"

	"github.com/francescostumpo/legal-callegarin/internal/articles"
)

// ArticleEventSink breaks the construction cycle between an article service
// and the renderer whose cache must observe successful public changes.
type ArticleEventSink struct {
	mu        sync.RWMutex
	observers []articles.ArticleEvents
}

func NewArticleEventSink() *ArticleEventSink {
	return &ArticleEventSink{}
}

func (sink *ArticleEventSink) subscribe(observer articles.ArticleEvents) {
	if sink == nil || observer == nil {
		return
	}
	sink.mu.Lock()
	sink.observers = append(sink.observers, observer)
	sink.mu.Unlock()
}

func (sink *ArticleEventSink) PublicArticleChanged(ctx context.Context, before, after articles.Article) {
	if sink == nil {
		return
	}
	sink.mu.RLock()
	observers := append([]articles.ArticleEvents(nil), sink.observers...)
	sink.mu.RUnlock()
	for _, observer := range observers {
		observer.PublicArticleChanged(ctx, before, after)
	}
}

var _ articles.ArticleEvents = (*ArticleEventSink)(nil)
