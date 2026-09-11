package adminapi

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"time"

	"github.com/francescostumpo/legal-callegarin/internal/articles"
	"github.com/francescostumpo/legal-callegarin/internal/webassets"
)

const articleBodyLimit = 640 << 10

type articleBodyInput struct {
	SchemaVersion int             `json:"schemaVersion"`
	Document      json.RawMessage `json:"document"`
}
type articleInput struct {
	Slug    string           `json:"slug"`
	Title   string           `json:"title"`
	Summary string           `json:"summary"`
	Area    string           `json:"area"`
	CoverID string           `json:"coverId"`
	Body    articleBodyInput `json:"body"`
}
type articleSummaryDTO struct {
	ID        string          `json:"id"`
	Slug      string          `json:"slug"`
	Title     string          `json:"title"`
	Summary   string          `json:"summary"`
	Area      string          `json:"area"`
	CoverID   string          `json:"coverId"`
	Status    articles.Status `json:"status"`
	CreatedAt time.Time       `json:"createdAt"`
	UpdatedAt time.Time       `json:"updatedAt"`
}
type articleDetailDTO struct {
	articleSummaryDTO
	Body articleBodyInput `json:"body"`
}
type articlePageDTO struct {
	Items      []articleSummaryDTO `json:"items"`
	NextCursor string              `json:"nextCursor"`
}
type coverDTO struct {
	ID            string `json:"id"`
	Alt           string `json:"alt"`
	CardAVIF      string `json:"cardAvif"`
	CardWebP      string `json:"cardWebp"`
	LandscapeAVIF string `json:"landscapeAvif"`
	LandscapeWebP string `json:"landscapeWebp"`
}

func (handler *handler) articlesGET(w http.ResponseWriter, r *http.Request) {
	if handler.articles == nil {
		writeJSONCode(w, 503, "articles_unavailable", "articles temporarily unavailable")
		return
	}
	values := r.URL.Query()
	for key, v := range values {
		if (key != "status" && key != "cursor") || len(v) != 1 {
			writeJSONCode(w, 400, "invalid_query", "invalid article query")
			return
		}
	}
	opt := articles.ListOptions{Limit: 25}
	opt.Cursor = values.Get("cursor")
	if status := values.Get("status"); status != "" {
		parsed := articles.Status(status)
		if parsed != articles.StatusDraft && parsed != articles.StatusPublished && parsed != articles.StatusWithdrawn {
			writeJSONCode(w, 400, "invalid_status", "invalid article status")
			return
		}
		opt.Status = &parsed
	}
	page, err := handler.articles.List(r.Context(), opt)
	if err != nil {
		writeArticleError(w, err)
		return
	}
	items := make([]articleSummaryDTO, 0, len(page.Items))
	for _, a := range page.Items {
		items = append(items, articleSummary(a))
	}
	writeJSON(w, 200, articlePageDTO{items, page.NextCursor})
}
func (handler *handler) articleCreatePOST(w http.ResponseWriter, r *http.Request) {
	input, ok := decodeArticleInput(w, r)
	if !ok {
		return
	}
	if handler.articles == nil {
		writeJSONCode(w, 503, "articles_unavailable", "articles temporarily unavailable")
		return
	}
	a, err := handler.articles.CreateDraft(r.Context(), toDraft(input))
	if err != nil {
		writeArticleError(w, err)
		return
	}
	w.Header().Set("Location", "/api/admin/articles/"+url.PathEscape(a.ID))
	writeArticle(w, http.StatusCreated, a, input.Body)
}
func (handler *handler) articleGET(w http.ResponseWriter, r *http.Request) {
	if handler.articles == nil {
		writeJSONCode(w, 503, "articles_unavailable", "articles temporarily unavailable")
		return
	}
	a, err := handler.articles.Get(r.Context(), r.PathValue("id"))
	if err != nil {
		writeArticleError(w, err)
		return
	}
	preview, err := handler.articles.GetPreview(r.Context(), a.ID)
	if err != nil {
		writeArticleError(w, err)
		return
	}
	writeArticle(w, 200, a, articleBodyInput{preview.Body.SchemaVersion, preview.Body.Document})
}
func (handler *handler) articleDraftPUT(w http.ResponseWriter, r *http.Request) {
	if handler.articles == nil {
		writeJSONCode(w, 503, "articles_unavailable", "articles temporarily unavailable")
		return
	}
	etag, ok := requiredETag(w, r)
	if !ok {
		return
	}
	input, ok := decodeArticleInput(w, r)
	if !ok {
		return
	}
	a, err := handler.articles.SaveDraft(r.Context(), r.PathValue("id"), toDraft(input), etag)
	if err != nil {
		writeArticleError(w, err)
		return
	}
	writeArticle(w, 200, a, input.Body)
}
func (handler *handler) articlePublishPOST(w http.ResponseWriter, r *http.Request) {
	handler.articleTransition(w, r, true)
}
func (handler *handler) articleWithdrawPOST(w http.ResponseWriter, r *http.Request) {
	handler.articleTransition(w, r, false)
}
func (handler *handler) articleTransition(w http.ResponseWriter, r *http.Request, publish bool) {
	if handler.articles == nil {
		writeJSONCode(w, 503, "articles_unavailable", "articles temporarily unavailable")
		return
	}
	etag, ok := requiredETag(w, r)
	if !ok {
		return
	}
	var a articles.Article
	var err error
	if publish {
		a, err = handler.articles.Publish(r.Context(), r.PathValue("id"), etag)
	} else {
		a, err = handler.articles.Withdraw(r.Context(), r.PathValue("id"), etag)
	}
	if err != nil {
		writeArticleError(w, err)
		return
	}
	preview, err := handler.articles.GetPreview(r.Context(), a.ID)
	if err != nil {
		writeArticleError(w, err)
		return
	}
	writeArticle(w, 200, a, articleBodyInput{preview.Body.SchemaVersion, preview.Body.Document})
}
func (handler *handler) coversGET(w http.ResponseWriter, _ *http.Request) {
	covers, err := webassets.CoverCatalog()
	if err != nil {
		writeJSONCode(w, 503, "covers_unavailable", "covers temporarily unavailable")
		return
	}
	out := make([]coverDTO, 0, len(covers))
	for _, c := range covers {
		out = append(out, coverDTO{c.ID, c.Alt, c.CardAVIF, c.CardWebP, c.LandscapeAVIF, c.LandscapeWebP})
	}
	writeJSON(w, 200, out)
}
func (handler *handler) articlePreviewGET(w http.ResponseWriter, r *http.Request) {
	if handler.articles == nil || handler.renderer == nil {
		writeJSONCode(w, 503, "preview_unavailable", "preview temporarily unavailable")
		return
	}
	content, err := handler.articles.GetPreview(r.Context(), r.PathValue("id"))
	if err != nil {
		writeArticleError(w, err)
		return
	}
	data, err := handler.renderer.NewArticlePageData(content, true)
	if err != nil {
		writeArticleError(w, err)
		return
	}
	setPrivateAdminHeaders(w)
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := handler.renderer.RenderArticle(w, data); err != nil {
		return
	}
}

func decodeArticleInput(w http.ResponseWriter, r *http.Request) (articleInput, bool) {
	if r.Header.Get("Content-Type") != "application/json" {
		writeJSONCode(w, 415, "unsupported_media_type", "application/json required")
		return articleInput{}, false
	}
	r.Body = http.MaxBytesReader(w, r.Body, articleBodyLimit)
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	var input articleInput
	if err := decoder.Decode(&input); err != nil {
		writeJSONCode(w, 400, "invalid_json", "invalid JSON body")
		return articleInput{}, false
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		writeJSONCode(w, 400, "invalid_json", "one JSON value required")
		return articleInput{}, false
	}
	return input, true
}
func toDraft(i articleInput) articles.DraftInput {
	return articles.DraftInput{Slug: i.Slug, Title: i.Title, Summary: i.Summary, Area: i.Area, CoverID: i.CoverID, Body: articles.Body{SchemaVersion: i.Body.SchemaVersion, Document: i.Body.Document}}
}
func requiredETag(w http.ResponseWriter, r *http.Request) (string, bool) {
	etag, status, err := parseIfMatch(r.Header.Get("If-Match"))
	if err != nil {
		writeJSONCode(w, status, "if_match_required", "valid If-Match header required")
		return "", false
	}
	return etag, true
}
func articleSummary(a articles.Article) articleSummaryDTO {
	return articleSummaryDTO{a.ID, a.Slug, a.Title, a.Summary, a.Area, a.CoverID, a.Status, a.CreatedAt, a.UpdatedAt}
}
func writeArticle(w http.ResponseWriter, status int, a articles.Article, b articleBodyInput) {
	if a.ETag == "" {
		writeJSONCode(w, 503, "article_unavailable", "article temporarily unavailable")
		return
	}
	w.Header().Set("ETag", formatETag(a.ETag))
	writeJSON(w, status, articleDetailDTO{articleSummary(a), b})
}
func writeArticleError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, articles.ErrNotFound):
		writeJSONCode(w, 404, "article_not_found", "article not found")
	case errors.Is(err, articles.ErrValidation), errors.Is(err, articles.ErrBodyTooLarge):
		writeJSONCode(w, 422, "article_validation", "invalid article")
	case errors.Is(err, articles.ErrSlugTaken):
		writeJSONCode(w, 409, "slug_taken", "article slug is already taken")
	case errors.Is(err, articles.ErrConflict):
		writeJSONCode(w, 409, "article_conflict", "article changed; reload before retrying")
	case errors.Is(err, articles.ErrInvalidTransition):
		writeJSONCode(w, 409, "invalid_transition", "invalid article transition")
	case errors.Is(err, articles.ErrCommitUnknown):
		writeJSONCode(w, 503, "commit_unknown", "save outcome is unknown; reload before deciding what to do")
	default:
		writeJSONCode(w, 503, "articles_unavailable", "articles temporarily unavailable")
	}
}
func writeJSONCode(w http.ResponseWriter, status int, code, message string) {
	setPrivateAdminHeaders(w)
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": message, "code": code})
}
