package public

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"html/template"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/francescostumpo/legal-callegarin/internal/articles"
)

const articlePageSize = 6

const (
	cacheKeyHome      = "home"
	cacheKeySitemap   = "sitemap"
	cachePrefixIndex  = "articles:index:"
	cachePrefixDetail = "articles:detail:"
	cachePrefixArea   = "area:"
)

type ArticleReader interface {
	ListPublished(context.Context, articles.ListOptions) (articles.ArticlePage, error)
	GetPublished(context.Context, string) (articles.ArticleWithBody, error)
}

type ArticleCard struct {
	Title     string
	Summary   string
	Href      string
	AreaTitle string
	Image     EditorialImage
}

type ArticleIndexPageData struct {
	PageData
	Articles []ArticleCard
	NextURL  string
}

type ArticlePageData struct {
	PageData
	ArticleID      string
	Author         string
	PublishedISO   string
	PublishedLabel string
	UpdatedISO     string
	UpdatedLabel   string
	AreaTitle      string
	AreaHref       string
	Preview        bool
	SanitizedBody  template.HTML
}

func (renderer *Renderer) NewArticlePageData(content articles.ArticleWithBody, preview bool) (ArticlePageData, error) {
	article := content.Article
	image := renderer.editorialImage(article.CoverID)
	areaTitle, areaHref := practiceArea(article.Area)
	publishedAt := article.CreatedAt
	if article.FirstPublishedAt != nil {
		publishedAt = *article.FirstPublishedAt
	}
	updatedAt := article.UpdatedAt
	if article.LastPublishedAt != nil {
		updatedAt = *article.LastPublishedAt
	}
	path := "/sentenze-e-riflessioni/" + article.Slug
	page := PageData{
		SiteName: "Studio Legale Alessandro Callegarin", Title: article.Title,
		Description: article.Summary, CanonicalURL: renderer.baseURL + path, Path: path,
		Kind: pageKindArticle, Navigation: navigationFor("/sentenze-e-riflessioni"), HeroImage: image,
	}
	if preview {
		page.Robots = "noindex, nofollow"
	}
	renderer.applyPageSEO(&page, "article", image, &article)

	// Invariant: Body.HTML is the allow-list-sanitized representation persisted
	// by the article service. This final template boundary is the only place it
	// is promoted from an ordinary string to trusted HTML.
	trustedBody := template.HTML(content.Body.HTML) // #nosec G203 -- invariant documented above.
	return ArticlePageData{
		PageData: page, ArticleID: article.ID, Author: "Alessandro Callegarin",
		PublishedISO: publishedAt.Format(time.DateOnly), PublishedLabel: formatItalianDate(publishedAt),
		UpdatedISO: updatedAt.Format(time.DateOnly), UpdatedLabel: formatItalianDate(updatedAt),
		AreaTitle: areaTitle, AreaHref: areaHref, Preview: preview, SanitizedBody: trustedBody,
	}, nil
}

func (renderer *Renderer) RenderArticle(writer io.Writer, data ArticlePageData) error {
	return renderer.article.ExecuteTemplate(writer, "base", data)
}

func (renderer *Renderer) articleIndexHandler() http.HandlerFunc {
	return func(response http.ResponseWriter, request *http.Request) {
		cursor := request.URL.Query().Get("cursor")
		body, err := renderer.cache.GetOrFill(request.Context(), cachePrefixIndex+cursor, func(ctx context.Context) ([]byte, error) {
			return renderer.renderArticleIndex(ctx, cursor)
		})
		if err != nil {
			writeArticleReadError(response, err)
			return
		}
		writeRevalidatingHTML(response, request, body)
	}
}

func (renderer *Renderer) renderArticleIndex(ctx context.Context, cursor string) ([]byte, error) {
	page, err := renderer.articles.ListPublished(ctx, articles.ListOptions{Cursor: cursor, Limit: articlePageSize})
	if err != nil {
		return nil, err
	}
	if page.PageNumber < 1 || cursor == "" && page.PageNumber != 1 || cursor != "" && page.PageNumber < 2 {
		return nil, fmt.Errorf("%w: article page number does not match cursor", articles.ErrValidation)
	}
	base := renderer.pages["/sentenze-e-riflessioni"]
	base.CanonicalURL = renderer.baseURL + base.Path
	if page.PageNumber > 1 {
		base.CanonicalURL += "?cursor=" + url.QueryEscape(cursor)
		base.Title += fmt.Sprintf(" — pagina %d", page.PageNumber)
		base.Description += fmt.Sprintf(" Pagina %d della raccolta.", page.PageNumber)
	}
	base.Sections = nil
	renderer.applyPageSEO(&base, "website", base.HeroImage, nil)
	data := ArticleIndexPageData{PageData: base, Articles: make([]ArticleCard, 0, len(page.Items))}
	for _, article := range page.Items {
		areaTitle, _ := practiceArea(article.Area)
		data.Articles = append(data.Articles, ArticleCard{
			Title: article.Title, Summary: article.Summary,
			Href:      "/sentenze-e-riflessioni/" + article.Slug,
			AreaTitle: areaTitle, Image: renderer.editorialImage(article.CoverID),
		})
	}
	if page.NextCursor != "" {
		data.NextURL = "/sentenze-e-riflessioni?cursor=" + url.QueryEscape(page.NextCursor)
	}
	var output bytes.Buffer
	if err := renderer.articleIndex.ExecuteTemplate(&output, "base", data); err != nil {
		return nil, fmt.Errorf("render article index: %w", err)
	}
	return output.Bytes(), nil
}

func (renderer *Renderer) articleDetailHandler() http.HandlerFunc {
	return func(response http.ResponseWriter, request *http.Request) {
		slug := request.PathValue("slug")
		body, err := renderer.cache.GetOrFill(request.Context(), cachePrefixDetail+slug, func(ctx context.Context) ([]byte, error) {
			return renderer.renderPublishedArticle(ctx, slug)
		})
		if err != nil {
			var redirect canonicalArticleRedirect
			if errors.As(err, &redirect) {
				http.Redirect(response, request, "/sentenze-e-riflessioni/"+redirect.slug, http.StatusPermanentRedirect)
				return
			}
			writeArticleReadError(response, err)
			return
		}
		writeRevalidatingHTML(response, request, body)
	}
}

func (renderer *Renderer) renderPublishedArticle(ctx context.Context, slug string) ([]byte, error) {
	content, err := renderer.articles.GetPublished(ctx, slug)
	if err != nil {
		return nil, err
	}
	if content.Article.Slug != slug {
		return nil, canonicalArticleRedirect{slug: content.Article.Slug}
	}
	data, err := renderer.NewArticlePageData(content, false)
	if err != nil {
		return nil, err
	}
	var output bytes.Buffer
	if err := renderer.RenderArticle(&output, data); err != nil {
		return nil, fmt.Errorf("render article: %w", err)
	}
	return output.Bytes(), nil
}

func (renderer *Renderer) editorialImage(id string) EditorialImage {
	if image, exists := renderer.images[id]; exists {
		return image
	}
	return renderer.images["article-notebook"]
}

func writeArticleReadError(response http.ResponseWriter, err error) {
	if errors.Is(err, articles.ErrNotFound) || errors.Is(err, articles.ErrValidation) {
		writePublicError(response, http.StatusNotFound, "page not found")
		return
	}
	writePublicError(response, http.StatusServiceUnavailable, "content temporarily unavailable")
}

func writeRevalidatingHTML(response http.ResponseWriter, request *http.Request, body []byte) {
	writeRevalidating(response, request, body, "text/html; charset=utf-8")
}

func setRevalidationHeaders(response http.ResponseWriter, request *http.Request, body []byte) bool {
	digest := sha256.Sum256(body)
	etag := `"` + fmt.Sprintf("%x", digest[:]) + `"`
	response.Header().Set("ETag", etag)
	response.Header().Set("Cache-Control", "public, max-age=0, must-revalidate")
	return etagMatches(request.Header.Get("If-None-Match"), etag)
}

func etagMatches(header, etag string) bool {
	for _, candidate := range strings.Split(header, ",") {
		candidate = strings.TrimSpace(candidate)
		if candidate == "*" || candidate == etag || strings.TrimPrefix(candidate, "W/") == etag {
			return true
		}
	}
	return false
}

type canonicalArticleRedirect struct{ slug string }

func (redirect canonicalArticleRedirect) Error() string {
	return "canonical article slug: " + strconv.Quote(redirect.slug)
}

func (renderer *Renderer) PublicArticleChanged(_ context.Context, before, after articles.Article) {
	renderer.cache.Invalidate(cacheKeyHome, cacheKeySitemap)
	renderer.cache.InvalidatePrefix(cachePrefixIndex)
	for _, article := range []articles.Article{before, after} {
		if article.Published == nil {
			continue
		}
		renderer.cache.Invalidate(cachePrefixDetail+article.Published.Slug, cachePrefixArea+article.Published.Area)
		for _, slug := range article.Published.HistoricalSlugs {
			renderer.cache.Invalidate(cachePrefixDetail + slug)
		}
	}
}

var _ articles.ArticleEvents = (*Renderer)(nil)

func (renderer *Renderer) latestArticleCards(ctx context.Context, area string, limit int) ([]ArticleCard, error) {
	cards := make([]ArticleCard, 0, limit)
	cursor := ""
	for {
		page, err := renderer.articles.ListPublished(ctx, articles.ListOptions{Cursor: cursor, Limit: 100})
		if err != nil {
			return nil, err
		}
		for _, article := range page.Items {
			if area != "" && article.Area != area {
				continue
			}
			areaTitle, _ := practiceArea(article.Area)
			cards = append(cards, ArticleCard{
				Title: article.Title, Summary: article.Summary,
				Href:      "/sentenze-e-riflessioni/" + article.Slug,
				AreaTitle: areaTitle, Image: renderer.editorialImage(article.CoverID),
			})
			if len(cards) == limit {
				return cards, nil
			}
		}
		if page.NextCursor == "" {
			return cards, nil
		}
		cursor = page.NextCursor
	}
}

func practiceArea(slug string) (string, string) {
	areas := map[string]string{
		"famiglia-e-persone": "Famiglia e persone", "successioni-e-donazioni": "Successioni e donazioni",
		"obbligazioni-e-contratti": "Obbligazioni e contratti", "recupero-crediti": "Recupero crediti",
		"risarcimento-danni": "Risarcimento danni", "diritti-reali": "Diritti reali",
		"diritto-penale": "Diritto penale", "diritto-tributario": "Diritto tributario",
	}
	title, exists := areas[slug]
	if !exists {
		return "Aree di attività", "/aree-di-attivita"
	}
	return title, "/aree-di-attivita/" + slug
}

func formatItalianDate(value time.Time) string {
	months := [...]string{"gennaio", "febbraio", "marzo", "aprile", "maggio", "giugno", "luglio", "agosto", "settembre", "ottobre", "novembre", "dicembre"}
	return fmt.Sprintf("%d %s %d", value.Day(), months[value.Month()-1], value.Year())
}

type emptyArticleReader struct{}

func (emptyArticleReader) ListPublished(context.Context, articles.ListOptions) (articles.ArticlePage, error) {
	return articles.ArticlePage{PageNumber: 1}, nil
}

func (emptyArticleReader) GetPublished(context.Context, string) (articles.ArticleWithBody, error) {
	return articles.ArticleWithBody{}, articles.ErrNotFound
}
