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
	"github.com/francescostumpo/legal-callegarin/internal/auth"
	"github.com/francescostumpo/legal-callegarin/internal/config"
	"github.com/francescostumpo/legal-callegarin/internal/contacts"
	storagebundle "github.com/francescostumpo/legal-callegarin/internal/storage"
	"github.com/francescostumpo/legal-callegarin/internal/storage/memory"
	"github.com/francescostumpo/legal-callegarin/internal/web/adminapi"
	webmiddleware "github.com/francescostumpo/legal-callegarin/internal/web/middleware"
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
	TrustedProxyHops  int
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
	adminConfigured := options.Config.AdminUsername != "" || options.Config.AdminPasswordHash != ""
	if adminConfigured && (options.Config.AdminUsername == "" || options.Config.AdminPasswordHash == "") {
		return nil, errors.New("admin username and password hash must be configured together")
	}
	var credentials *auth.Credentials
	if adminConfigured {
		var err error
		credentials, err = auth.ParseCredentials(options.Config.AdminUsername, options.Config.AdminPasswordHash)
		if err != nil {
			return nil, fmt.Errorf("initialize admin credentials: %w", err)
		}
	}
	events := options.ArticleEvents
	if events == nil && bundle != nil && bundle.Articles != nil && bundle.Bodies != nil {
		events = publicweb.NewArticleEventSink()
	}
	articleReader := options.Articles
	var articleService articles.ArticleService
	if configured, ok := options.Articles.(articles.ArticleService); ok {
		articleService = configured
	}
	if bundle != nil && bundle.Articles != nil && bundle.Bodies != nil {
		articleService = articles.NewService(bundle.Articles, bundle.Bodies, appClock{now: clock}, secureArticleIDs{}, events)
		if articleReader == nil {
			articleReader = articleService
		}
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
			publicweb.WithContactTrustedProxyHops(effectiveTrustedProxyHops(options)),
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
		_, _ = response.Write([]byte("ok\n"))
	})
	mux.HandleFunc("GET /health/ready", func(response http.ResponseWriter, request *http.Request) {
		response.Header().Set("Content-Type", "text/plain; charset=utf-8")
		if bundle != nil && bundle.Readiness != nil {
			ctx, cancel := context.WithTimeout(request.Context(), 2*time.Second)
			defer cancel()
			if err := bundle.Readiness.Ready(ctx); err != nil {
				response.WriteHeader(http.StatusServiceUnavailable)
				_, _ = response.Write([]byte("unavailable\n"))
				return
			}
		}
		response.WriteHeader(http.StatusOK)
		_, _ = response.Write([]byte("ok\n"))
	})
	if err := publicweb.RegisterRoutes(mux, renderer, options.Assets); err != nil {
		return nil, fmt.Errorf("register public routes: %w", err)
	}
	signingKey := options.SessionSigningKey
	if len(signingKey) == 0 {
		signingKey = options.Config.SessionKey
	}
	var authenticator webmiddleware.Authenticator
	if adminConfigured {
		if bundle == nil || bundle.Sessions == nil {
			return nil, errors.New("admin authentication requires a session repository")
		}
		sessions, sessionErr := auth.NewSessionService(bundle.Sessions, options.Config.AdminUsername, auth.CredentialVersion(options.Config.AdminPasswordHash), rand.Reader, clock)
		if sessionErr != nil {
			return nil, fmt.Errorf("initialize admin sessions: %w", sessionErr)
		}
		authenticator = sessions
		adminHandler, adminErr := adminapi.New(adminapi.Options{
			Credentials:        credentials,
			ConfiguredUsername: options.Config.AdminUsername,
			Sessions:           sessions,
			Contacts:           contactService,
			Articles:           articleService,
			Renderer:           renderer,
			Assets:             options.Assets,
			SessionKey:         signingKey,
			PublicBaseURL:      options.Config.PublicBaseURL,
			Now:                clock,
			TrustedProxyHops:   effectiveTrustedProxyHops(options),
		})
		if adminErr != nil {
			return nil, fmt.Errorf("initialize admin handler: %w", adminErr)
		}
		mux.Handle("GET /admin", adminHandler)
		mux.Handle("GET /admin/", adminHandler)
		mux.Handle("POST /admin/login", adminHandler)
		for _, method := range []string{http.MethodGet, http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete, http.MethodOptions} {
			mux.Handle(method+" /api/admin", adminHandler)
			mux.Handle(method+" /api/admin/", adminHandler)
		}
	}
	handlerTimeout := time.Duration(0)
	if options.Config.Environment == "production" {
		handlerTimeout = 25 * time.Second
	}
	secured, err := webmiddleware.New(mux, webmiddleware.Options{
		Authenticator:    authenticator,
		SessionKey:       signingKey,
		PublicBaseURL:    options.Config.PublicBaseURL,
		Logger:           options.Logger,
		Now:              clock,
		Environment:      options.Config.Environment,
		TrustedProxyHops: effectiveTrustedProxyHops(options),
		HandlerTimeout:   handlerTimeout,
		ErrorRenderer:    renderer.WriteError,
	})
	if err != nil {
		return nil, fmt.Errorf("initialize HTTP security middleware: %w", err)
	}
	return secured, nil
}

func effectiveTrustedProxyHops(options Options) int {
	if options.Config.Environment != "production" {
		return 0
	}
	if options.TrustedProxyHops > 0 {
		return options.TrustedProxyHops
	}
	return options.Config.TrustedProxyHops
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
