package articles

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"
)

func TestNormalizeSlug(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		input   string
		want    string
		wantErr bool
	}{
		{name: "lowercase and collapse separators", input: "  Diritto   DI---Famiglia  ", want: "diritto-di-famiglia"},
		{name: "minimum length", input: "A-B", want: "a-b"},
		{name: "maximum length", input: strings.Repeat("a", 100), want: strings.Repeat("a", 100)},
		{name: "too short", input: "ab", wantErr: true},
		{name: "too long", input: strings.Repeat("a", 101), wantErr: true},
		{name: "non ASCII", input: "legalità", wantErr: true},
		{name: "punctuation", input: "diritto/civile", wantErr: true},
		{name: "underscore", input: "diritto_civile", wantErr: true},
		{name: "only separators", input: " --  ", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, err := NormalizeSlug(tt.input)
			if tt.wantErr {
				if !errors.Is(err, ErrValidation) {
					t.Fatalf("NormalizeSlug() error = %v, want ErrValidation", err)
				}
				return
			}
			if err != nil {
				t.Fatalf("NormalizeSlug() error = %v", err)
			}
			if got != tt.want {
				t.Fatalf("NormalizeSlug() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestArticleValidate(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		mutate func(*Article)
	}{
		{name: "missing ID", mutate: func(article *Article) { article.ID = "" }},
		{name: "unnormalized slug", mutate: func(article *Article) { article.Slug = "Valid-Slug" }},
		{name: "short title", mutate: func(article *Article) { article.Title = "1234" }},
		{name: "long title", mutate: func(article *Article) { article.Title = strings.Repeat("a", 161) }},
		{name: "short summary", mutate: func(article *Article) { article.Summary = strings.Repeat("a", 19) }},
		{name: "long summary", mutate: func(article *Article) { article.Summary = strings.Repeat("a", 321) }},
		{name: "missing area", mutate: func(article *Article) { article.Area = "" }},
		{name: "missing cover", mutate: func(article *Article) { article.CoverID = "" }},
		{name: "invalid status", mutate: func(article *Article) { article.Status = "unknown" }},
		{name: "missing created timestamp", mutate: func(article *Article) { article.CreatedAt = time.Time{} }},
		{name: "updated before created", mutate: func(article *Article) { article.UpdatedAt = article.CreatedAt.Add(-time.Second) }},
		{name: "published without body", mutate: func(article *Article) { article.Status, article.PublishedBody = StatusPublished, nil }},
		{name: "published without first timestamp", mutate: func(article *Article) { article.Status, article.FirstPublishedAt = StatusPublished, nil }},
		{name: "withdrawn without published body", mutate: func(article *Article) { article.Status, article.PublishedBody = StatusWithdrawn, nil }},
		{
			name: "first publication after update",
			mutate: func(article *Article) {
				publishedAt := article.UpdatedAt.Add(time.Second)
				article.FirstPublishedAt = &publishedAt
				article.LastPublishedAt = &publishedAt
			},
		},
		{
			name: "last publication after update",
			mutate: func(article *Article) {
				lastPublishedAt := article.UpdatedAt.Add(time.Second)
				article.LastPublishedAt = &lastPublishedAt
			},
		},
	}

	valid := validArticle()
	if err := valid.Validate(); err != nil {
		t.Fatalf("valid Article.Validate() error = %v", err)
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			article := validArticle()
			tt.mutate(&article)
			if err := article.Validate(); !errors.Is(err, ErrValidation) {
				t.Fatalf("Article.Validate() error = %v, want ErrValidation", err)
			}
		})
	}
}

func TestBodyValidate(t *testing.T) {
	t.Parallel()

	valid := Body{SchemaVersion: 1, Document: []byte(`{"type":"doc","content":[]}`), HTML: "<p>contenuto</p>", PlainText: "contenuto"}
	if err := valid.Validate(); err != nil {
		t.Fatalf("valid Body.Validate() error = %v", err)
	}

	tests := []struct {
		name   string
		mutate func(*Body)
	}{
		{name: "invalid schema", mutate: func(body *Body) { body.SchemaVersion = 0 }},
		{name: "invalid JSON", mutate: func(body *Body) { body.Document = []byte(`{"type":`) }},
		{name: "oversized document", mutate: func(body *Body) { body.Document = []byte(`"` + strings.Repeat("a", 512*1024) + `"`) }},
		{name: "document too deep", mutate: func(body *Body) { body.Document = nestedDocument(t, 33) }},
		{name: "too many document nodes", mutate: func(body *Body) { body.Document = documentWithNodes(t, 5001) }},
		{name: "link too long", mutate: func(body *Body) { body.Document = documentWithLink(t, strings.Repeat("a", 2049)) }},
		{name: "missing HTML", mutate: func(body *Body) { body.HTML = "" }},
		{name: "missing plain text", mutate: func(body *Body) { body.PlainText = "" }},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			body := valid
			tt.mutate(&body)
			if err := body.Validate(); !errors.Is(err, ErrValidation) {
				t.Fatalf("Body.Validate() error = %v, want ErrValidation", err)
			}
		})
	}
}

func nestedDocument(t *testing.T, depth int) []byte {
	t.Helper()
	var value any = map[string]any{"type": "text"}
	for range depth - 1 {
		value = map[string]any{"type": "node", "content": []any{value}}
	}
	encoded, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return encoded
}

func documentWithNodes(t *testing.T, count int) []byte {
	t.Helper()
	nodes := make([]any, count-1)
	for index := range nodes {
		nodes[index] = map[string]any{"type": "paragraph"}
	}
	encoded, err := json.Marshal(map[string]any{"type": "doc", "content": nodes})
	if err != nil {
		t.Fatal(err)
	}
	return encoded
}

func documentWithLink(t *testing.T, href string) []byte {
	t.Helper()
	encoded, err := json.Marshal(map[string]any{"type": "text", "attrs": map[string]any{"href": href}})
	if err != nil {
		t.Fatal(err)
	}
	return encoded
}

func TestStatusTransitions(t *testing.T) {
	t.Parallel()

	statuses := []Status{StatusDraft, StatusPublished, StatusWithdrawn}
	allowed := map[[2]Status]bool{
		{StatusDraft, StatusDraft}:         true,
		{StatusDraft, StatusPublished}:     true,
		{StatusPublished, StatusPublished}: true,
		{StatusPublished, StatusWithdrawn}: true,
		{StatusWithdrawn, StatusWithdrawn}: true,
		{StatusWithdrawn, StatusPublished}: true,
	}

	for _, from := range statuses {
		for _, to := range statuses {
			err := ValidateTransition(from, to)
			if allowed[[2]Status{from, to}] && err != nil {
				t.Errorf("ValidateTransition(%q, %q) error = %v", from, to, err)
			}
			if !allowed[[2]Status{from, to}] && !errors.Is(err, ErrInvalidTransition) {
				t.Errorf("ValidateTransition(%q, %q) error = %v, want ErrInvalidTransition", from, to, err)
			}
		}
	}
}

func validArticle() Article {
	created := time.Date(2026, time.September, 11, 10, 0, 0, 0, time.UTC)
	firstPublished := created.Add(time.Hour)
	lastPublished := firstPublished
	ref := &BodyRef{BlobName: "article-1", Version: "1", SavedAt: created}
	return Article{
		ID:               "article-1",
		Slug:             "diritto-civile",
		Title:            "Titolo valido",
		Summary:          "Sommario sufficientemente lungo",
		Area:             "obbligazioni",
		CoverID:          "cover-1",
		Status:           StatusPublished,
		DraftBody:        ref,
		PublishedBody:    ref,
		FirstPublishedAt: &firstPublished,
		LastPublishedAt:  &lastPublished,
		CreatedAt:        created,
		UpdatedAt:        lastPublished,
		ETag:             "1",
	}
}
