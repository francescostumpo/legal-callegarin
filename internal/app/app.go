package app

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"net/http"
	"time"

	"github.com/francescostumpo/legal-callegarin/internal/articles"
	"github.com/francescostumpo/legal-callegarin/internal/config"
	"github.com/francescostumpo/legal-callegarin/internal/contacts"
	storagebundle "github.com/francescostumpo/legal-callegarin/internal/storage"
	"github.com/francescostumpo/legal-callegarin/internal/storage/memory"
	publicweb "github.com/francescostumpo/legal-callegarin/internal/web/public"
)

type Options struct {
	Config            config.Config
	Assets            fs.FS
	Logger            *slog.Logger
	Articles          publicweb.ArticleReader
	ArticleEvents     *publicweb.ArticleEventSink
	ContactService    contacts.ContactService
	SessionSigningKey []byte
	RateClock         func() time.Time
	TrustedProxy      bool
	Storage           *storagebundle.Bundle
}

func New(options Options) (http.Handler, error) {
	clock := options.RateClock
	if clock == nil {
		clock = time.Now
	}
	bundle := options.Storage
	if bundle == nil && options.Config.StorageMode == "memory" {
		bundle = memory.NewBundle(clock)
	}
	if options.Config.StorageMode == "azure" && !bundle.Complete() {
		return nil, errors.New("azure mode requires an initialized coherent storage bundle")
	}
	events := options.ArticleEvents
	if events == nil && bundle != nil && bundle.Articles != nil && bundle.Bodies != nil {
		events = publicweb.NewArticleEventSink()
	}
	articleReader := options.Articles
	if articleReader == nil && bundle != nil && bundle.Articles != nil && bundle.Bodies != nil {
		articleReader = articles.NewService(bundle.Articles, bundle.Bodies, appClock{now: clock}, secureArticleIDs{}, events)
	}
	rendererOptions := make([]publicweb.RendererOption, 0, 6)
	if articleReader != nil {
		rendererOptions = append(rendererOptions, publicweb.WithArticleReader(articleReader))
	}
	if events != nil {
		rendererOptions = append(rendererOptions, publicweb.WithArticleEventSink(events))
	}
	contactService := options.ContactService
	if contactService == nil && bundle != nil && bundle.Contacts != nil {
		contactService = contacts.NewService(bundle.Contacts, appClock{now: clock}, secureContactIDs{})
	}
	if contactService != nil {
		signingKey := options.SessionSigningKey
		if len(signingKey) == 0 {
			signingKey = options.Config.SessionKey
		}
		rendererOptions = append(rendererOptions,
			publicweb.WithContactService(contactService, signingKey),
			publicweb.WithContactClock(clock),
			publicweb.WithContactLogger(options.Logger),
			publicweb.WithContactTrustedProxy(options.TrustedProxy || options.Config.TrustedProxy),
		)
	}
	renderer, err := publicweb.NewRenderer(options.Assets, options.Config.PublicBaseURL, rendererOptions...)
	if err != nil {
		return nil, fmt.Errorf("initialize public renderer: %w", err)
	}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /health/live", func(response http.ResponseWriter, _ *http.Request) {
		response.Header().Set("Content-Type", "text/plain; charset=utf-8")
		response.WriteHeader(http.StatusOK)
	})
	mux.HandleFunc("GET /health/ready", func(response http.ResponseWriter, request *http.Request) {
		response.Header().Set("Content-Type", "text/plain; charset=utf-8")
		if bundle != nil && bundle.Readiness != nil {
			ctx, cancel := context.WithTimeout(request.Context(), 2*time.Second)
			defer cancel()
			if err := bundle.Readiness.Ready(ctx); err != nil {
				response.WriteHeader(http.StatusServiceUnavailable)
				return
			}
		}
		response.WriteHeader(http.StatusOK)
	})
	if err := publicweb.RegisterRoutes(mux, renderer, options.Assets); err != nil {
		return nil, fmt.Errorf("register public routes: %w", err)
	}

	return mux, nil
}

type appClock struct {
	now func() time.Time
}

func (clock appClock) Now() time.Time { return clock.now() }

type secureContactIDs struct{}

func (secureContactIDs) NewID() string {
	random := make([]byte, 16)
	if _, err := rand.Read(random); err != nil {
		panic("generate contact identifier")
	}
	return base64.RawURLEncoding.EncodeToString(random)
}

type secureArticleIDs struct{}

func (secureArticleIDs) NewID() string {
	random := make([]byte, 16)
	if _, err := rand.Read(random); err != nil {
		panic("generate article identifier")
	}
	return base64.RawURLEncoding.EncodeToString(random)
}
