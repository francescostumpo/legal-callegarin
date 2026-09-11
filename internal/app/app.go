package app

import (
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"io/fs"
	"log/slog"
	"net/http"
	"time"

	"github.com/francescostumpo/legal-callegarin/internal/config"
	"github.com/francescostumpo/legal-callegarin/internal/contacts"
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
}

func New(options Options) (http.Handler, error) {
	rendererOptions := make([]publicweb.RendererOption, 0, 6)
	if options.Articles != nil {
		rendererOptions = append(rendererOptions, publicweb.WithArticleReader(options.Articles))
	}
	if options.ArticleEvents != nil {
		rendererOptions = append(rendererOptions, publicweb.WithArticleEventSink(options.ArticleEvents))
	}
	clock := options.RateClock
	if clock == nil {
		clock = time.Now
	}
	contactService := options.ContactService
	if contactService == nil && options.Config.StorageMode == "memory" {
		contactService = contacts.NewService(memory.NewContactRepository(clock), appClock{now: clock}, secureContactIDs{})
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
