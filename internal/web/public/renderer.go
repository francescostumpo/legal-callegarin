package public

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"html/template"
	"io/fs"
	"net/http"
	"net/url"
	"strings"
	"time"
)

type Renderer struct {
	baseURL      string
	home         *template.Template
	standard     *template.Template
	articleIndex *template.Template
	article      *template.Template
	pages        map[string]PageData
	images       map[string]EditorialImage
	articles     ArticleReader
	cache        *htmlCache
	assets       *assetCatalog
}

type rendererOptions struct {
	articles ArticleReader
	now      func() time.Time
	events   *ArticleEventSink
}

type RendererOption func(*rendererOptions)

func WithArticleReader(reader ArticleReader) RendererOption {
	return func(options *rendererOptions) { options.articles = reader }
}

func WithArticleEventSink(events *ArticleEventSink) RendererOption {
	return func(options *rendererOptions) { options.events = events }
}

type assetManifest struct {
	Assets []manifestAsset `json:"assets"`
}

type manifestAsset struct {
	ID          string               `json:"id"`
	Alt         string               `json:"alt"`
	Derivatives []manifestDerivative `json:"derivatives"`
}

type manifestDerivative struct {
	Variant  string `json:"variant"`
	Format   string `json:"format"`
	Filename string `json:"filename"`
}

func NewRenderer(files fs.FS, publicBaseURL string, optionFunctions ...RendererOption) (*Renderer, error) {
	if files == nil {
		return nil, fmt.Errorf("public assets filesystem is required")
	}
	baseURL, err := normalizePublicBaseURL(publicBaseURL)
	if err != nil {
		return nil, err
	}
	assets, err := newAssetCatalog(files)
	if err != nil {
		return nil, err
	}
	home, err := parsePageTemplate(files, assets, "templates/pages/home.html")
	if err != nil {
		return nil, fmt.Errorf("parse home templates: %w", err)
	}
	standard, err := parsePageTemplate(files, assets, "templates/pages/standard.html")
	if err != nil {
		return nil, fmt.Errorf("parse standard templates: %w", err)
	}
	articleIndex, err := parsePageTemplate(files, assets, "templates/pages/articles.html")
	if err != nil {
		return nil, fmt.Errorf("parse article index templates: %w", err)
	}
	article, err := parsePageTemplate(files, assets, "templates/pages/article.html")
	if err != nil {
		return nil, fmt.Errorf("parse article templates: %w", err)
	}
	images, err := loadEditorialImages(files, assets)
	if err != nil {
		return nil, err
	}

	options := rendererOptions{articles: emptyArticleReader{}, now: time.Now}
	for _, apply := range optionFunctions {
		if apply != nil {
			apply(&options)
		}
	}
	if options.articles == nil {
		options.articles = emptyArticleReader{}
	}
	renderer := &Renderer{
		baseURL:      baseURL,
		home:         home,
		standard:     standard,
		articleIndex: articleIndex,
		article:      article,
		pages:        pageCatalog(images), images: images, articles: options.articles,
		cache:  newHTMLCache(options.now, publicCacheMaxEntries, publicCacheMaxBytes, publicCacheTTL),
		assets: assets,
	}
	options.events.subscribe(renderer)
	return renderer, nil
}

func RegisterRoutes(mux *http.ServeMux, renderer *Renderer, _ fs.FS) error {
	if mux == nil || renderer == nil {
		return fmt.Errorf("public mux and renderer are required")
	}
	mux.Handle("GET /assets/", renderer.assets)
	mux.HandleFunc("GET /sitemap.xml", renderer.sitemapHandler())
	mux.HandleFunc("GET /robots.txt", renderer.robotsHandler())

	for path := range renderer.pages {
		pattern := "GET " + path
		if path == "/" {
			pattern = "GET /{$}"
		}
		if path == "/sentenze-e-riflessioni" {
			mux.HandleFunc(pattern, renderer.articleIndexHandler())
		} else {
			mux.HandleFunc(pattern, renderer.pageHandler(path))
		}
	}
	mux.HandleFunc("GET /sentenze-e-riflessioni/{slug}", renderer.articleDetailHandler())
	mux.HandleFunc("GET /", func(response http.ResponseWriter, _ *http.Request) {
		writePublicError(response, http.StatusNotFound, "page not found")
	})
	return nil
}

func (renderer *Renderer) pageHandler(path string) http.HandlerFunc {
	return func(response http.ResponseWriter, request *http.Request) {
		key := "static:" + path
		if path == "/" {
			key = cacheKeyHome
		} else if strings.HasPrefix(path, "/aree-di-attivita/") {
			key = cachePrefixArea + strings.TrimPrefix(path, "/aree-di-attivita/")
		}
		body, err := renderer.cache.GetOrFill(request.Context(), key, func(ctx context.Context) ([]byte, error) {
			return renderer.renderPage(ctx, path)
		})
		if err != nil {
			writePublicError(response, http.StatusServiceUnavailable, "content temporarily unavailable")
			return
		}
		writeRevalidatingHTML(response, request, body)
	}
}

func (renderer *Renderer) renderPage(ctx context.Context, path string) ([]byte, error) {
	page := renderer.pages[path]
	page.CanonicalURL = renderer.baseURL + page.Path
	if page.Kind == pageKindHome {
		cards, err := renderer.latestArticleCards(ctx, "", 3)
		if err != nil {
			return nil, err
		}
		page.Articles = cards
	} else if page.Kind == pageKindArea {
		area := strings.TrimPrefix(page.Path, "/aree-di-attivita/")
		cards, err := renderer.latestArticleCards(ctx, area, 3)
		if err != nil {
			return nil, err
		}
		page.Articles = cards
	}
	renderer.applyPageSEO(&page, "website", page.HeroImage, nil)
	selected := renderer.standard
	if page.Kind == pageKindHome {
		selected = renderer.home
	}
	var output bytes.Buffer
	if err := selected.ExecuteTemplate(&output, "base", page); err != nil {
		return nil, fmt.Errorf("render public page: %w", err)
	}
	return output.Bytes(), nil
}

func normalizePublicBaseURL(raw string) (string, error) {
	if strings.TrimSpace(raw) != raw || raw == "" {
		return "", fmt.Errorf("PUBLIC_BASE_URL must be an absolute HTTP(S) origin")
	}
	parsed, err := url.Parse(raw)
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" || (parsed.Path != "" && parsed.Path != "/") {
		return "", fmt.Errorf("PUBLIC_BASE_URL must be an absolute HTTP(S) origin")
	}
	return strings.TrimRight(raw, "/"), nil
}

func parsePageTemplate(files fs.FS, assets *assetCatalog, pageTemplate string) (*template.Template, error) {
	return template.New("public").Funcs(template.FuncMap{"assetURL": assets.publicURL}).Option("missingkey=error").ParseFS(
		files,
		"templates/layouts/base.html",
		"templates/partials/header.html",
		"templates/partials/footer.html",
		"templates/partials/picture.html",
		pageTemplate,
	)
}

func loadEditorialImages(files fs.FS, assets *assetCatalog) (map[string]EditorialImage, error) {
	encoded, err := fs.ReadFile(files, "covers/manifest.json")
	if err != nil {
		return nil, fmt.Errorf("read editorial asset manifest: %w", err)
	}
	var manifest assetManifest
	if err := json.Unmarshal(encoded, &manifest); err != nil {
		return nil, fmt.Errorf("decode editorial asset manifest: %w", err)
	}
	images := make(map[string]EditorialImage, len(manifest.Assets))
	for _, asset := range manifest.Assets {
		image := EditorialImage{ID: asset.ID, Alt: asset.Alt}
		for _, derivative := range asset.Derivatives {
			fingerprinted, err := assets.coverName(derivative.Filename)
			if err != nil {
				return nil, err
			}
			switch derivative.Variant + ":" + derivative.Format {
			case "landscape:avif":
				image.LandscapeAVIF = fingerprinted
			case "landscape:webp":
				image.LandscapeWebP = fingerprinted
			case "card:avif":
				image.CardAVIF = fingerprinted
			case "card:webp":
				image.CardWebP = fingerprinted
			}
		}
		images[asset.ID] = image
	}
	return images, nil
}
