package public

import (
	"bytes"
	"context"
	"encoding/json"
	"encoding/xml"
	"fmt"
	"html/template"
	"net/http"
	"sort"
	"time"

	"github.com/francescostumpo/legal-callegarin/internal/articles"
)

type schemaGraph struct {
	Context string       `json:"@context"`
	Graph   []schemaNode `json:"@graph"`
}

type schemaNode struct {
	Type             string     `json:"@type"`
	ID               string     `json:"@id,omitempty"`
	Name             string     `json:"name,omitempty"`
	URL              string     `json:"url,omitempty"`
	Headline         string     `json:"headline,omitempty"`
	Description      string     `json:"description,omitempty"`
	Image            string     `json:"image,omitempty"`
	DatePublished    string     `json:"datePublished,omitempty"`
	DateModified     string     `json:"dateModified,omitempty"`
	Author           *schemaRef `json:"author,omitempty"`
	MainEntityOfPage string     `json:"mainEntityOfPage,omitempty"`
}

type schemaRef struct {
	ID string `json:"@id"`
}

func (renderer *Renderer) applyPageSEO(page *PageData, graphType string, image EditorialImage, article *articles.Article) {
	page.OpenGraphType = graphType
	page.OpenGraphURL = page.CanonicalURL
	page.OpenGraphImageURL = renderer.baseURL + "/assets/covers/" + image.LandscapeWebP
	personID := renderer.baseURL + "/#person"
	graph := schemaGraph{Context: "https://schema.org", Graph: []schemaNode{
		{Type: "LegalService", ID: renderer.baseURL + "/#legal-service", Name: page.SiteName, URL: renderer.baseURL},
		{Type: "Person", ID: personID, Name: "Alessandro Callegarin"},
	}}
	if article != nil {
		publishedAt := article.CreatedAt
		if article.FirstPublishedAt != nil {
			publishedAt = *article.FirstPublishedAt
		}
		updatedAt := article.UpdatedAt
		if article.LastPublishedAt != nil {
			updatedAt = *article.LastPublishedAt
		}
		graph.Graph = append(graph.Graph, schemaNode{
			Type: "Article", ID: page.CanonicalURL + "#article", Headline: article.Title,
			Description: article.Summary, Image: page.OpenGraphImageURL,
			DatePublished: publishedAt.Format(time.RFC3339), DateModified: updatedAt.Format(time.RFC3339),
			Author: &schemaRef{ID: personID}, MainEntityOfPage: page.CanonicalURL,
		})
	}
	encoded, err := json.Marshal(graph)
	if err != nil {
		panic(fmt.Sprintf("marshal fixed SEO graph: %v", err))
	}
	page.StructuredData = template.JS(encoded) // #nosec G203 -- json.Marshal escapes script-significant user content.
}

func (renderer *Renderer) sitemapHandler() http.HandlerFunc {
	return func(response http.ResponseWriter, request *http.Request) {
		body, err := renderer.cache.GetOrFill(request.Context(), cacheKeySitemap, renderer.renderSitemap)
		if err != nil {
			writePublicError(response, http.StatusServiceUnavailable, "content temporarily unavailable")
			return
		}
		writeRevalidating(response, request, body, "application/xml; charset=utf-8")
	}
}

func (renderer *Renderer) renderSitemap(ctx context.Context) ([]byte, error) {
	locations := make([]sitemapURL, 0, len(renderer.pages)+8)
	paths := make([]string, 0, len(renderer.pages))
	for path := range renderer.pages {
		paths = append(paths, path)
	}
	sort.Strings(paths)
	for _, path := range paths {
		locations = append(locations, sitemapURL{Location: renderer.baseURL + path})
	}
	cursor := ""
	for {
		page, err := renderer.articles.ListPublished(ctx, articles.ListOptions{Cursor: cursor, Limit: 100})
		if err != nil {
			return nil, err
		}
		for _, article := range page.Items {
			entry := sitemapURL{Location: renderer.baseURL + "/sentenze-e-riflessioni/" + article.Slug}
			if article.LastPublishedAt != nil {
				entry.LastModified = article.LastPublishedAt.Format(time.DateOnly)
			}
			locations = append(locations, entry)
		}
		if page.NextCursor == "" {
			break
		}
		cursor = page.NextCursor
	}
	document := sitemapDocument{Namespace: "http://www.sitemaps.org/schemas/sitemap/0.9", URLs: locations}
	var output bytes.Buffer
	output.WriteString(xml.Header)
	if err := xml.NewEncoder(&output).Encode(document); err != nil {
		return nil, fmt.Errorf("encode sitemap: %w", err)
	}
	return output.Bytes(), nil
}

func (renderer *Renderer) robotsHandler() http.HandlerFunc {
	body := []byte("User-agent: *\nDisallow: /admin\nDisallow: /admin/\nDisallow: /admin/preview\nDisallow: /admin/preview/\nDisallow: /api/admin\nDisallow: /api/admin/\nSitemap: " + renderer.baseURL + "/sitemap.xml\n")
	return func(response http.ResponseWriter, request *http.Request) {
		writeRevalidating(response, request, body, "text/plain; charset=utf-8")
	}
}

func writeRevalidating(response http.ResponseWriter, request *http.Request, body []byte, contentType string) {
	if setRevalidationHeaders(response, request, body) {
		response.WriteHeader(http.StatusNotModified)
		return
	}
	response.Header().Set("Content-Type", contentType)
	response.WriteHeader(http.StatusOK)
	_, _ = response.Write(body)
}

type sitemapDocument struct {
	XMLName   xml.Name     `xml:"urlset"`
	Namespace string       `xml:"xmlns,attr"`
	URLs      []sitemapURL `xml:"url"`
}

type sitemapURL struct {
	Location     string `xml:"loc"`
	LastModified string `xml:"lastmod,omitempty"`
}
