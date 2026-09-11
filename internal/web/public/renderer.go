package public

import (
	"bytes"
	"encoding/json"
	"fmt"
	"html/template"
	"io/fs"
	"net/http"
	"strings"
)

type Renderer struct {
	baseURL  string
	home     *template.Template
	standard *template.Template
	pages    map[string]PageData
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

func NewRenderer(files fs.FS, publicBaseURL string) (*Renderer, error) {
	if files == nil {
		return nil, fmt.Errorf("public assets filesystem is required")
	}
	home, err := parsePageTemplate(files, "templates/pages/home.html")
	if err != nil {
		return nil, fmt.Errorf("parse home templates: %w", err)
	}
	standard, err := parsePageTemplate(files, "templates/pages/standard.html")
	if err != nil {
		return nil, fmt.Errorf("parse standard templates: %w", err)
	}
	images, err := loadEditorialImages(files)
	if err != nil {
		return nil, err
	}

	return &Renderer{
		baseURL:  strings.TrimRight(publicBaseURL, "/"),
		home:     home,
		standard: standard,
		pages:    pageCatalog(images),
	}, nil
}

func RegisterRoutes(mux *http.ServeMux, renderer *Renderer, files fs.FS) error {
	if mux == nil || renderer == nil {
		return fmt.Errorf("public mux and renderer are required")
	}
	publicFiles, err := fs.Sub(files, "public")
	if err != nil {
		return fmt.Errorf("open public assets: %w", err)
	}
	coverFiles, err := fs.Sub(files, "covers")
	if err != nil {
		return fmt.Errorf("open editorial assets: %w", err)
	}
	mux.Handle("GET /assets/covers/", http.StripPrefix("/assets/covers/", http.FileServerFS(coverFiles)))
	mux.Handle("GET /assets/", http.StripPrefix("/assets/", http.FileServerFS(publicFiles)))

	for path := range renderer.pages {
		pattern := "GET " + path
		if path == "/" {
			pattern = "GET /{$}"
		}
		mux.HandleFunc(pattern, renderer.pageHandler(path))
	}
	return nil
}

func (renderer *Renderer) pageHandler(path string) http.HandlerFunc {
	return func(response http.ResponseWriter, _ *http.Request) {
		page := renderer.pages[path]
		page.CanonicalURL = renderer.baseURL + page.Path
		selected := renderer.standard
		if page.Kind == pageKindHome {
			selected = renderer.home
		}
		var output bytes.Buffer
		if err := selected.ExecuteTemplate(&output, "base", page); err != nil {
			http.Error(response, "rendering unavailable", http.StatusInternalServerError)
			return
		}
		response.Header().Set("Content-Type", "text/html; charset=utf-8")
		response.WriteHeader(http.StatusOK)
		_, _ = response.Write(output.Bytes())
	}
}

func parsePageTemplate(files fs.FS, pageTemplate string) (*template.Template, error) {
	return template.New("public").Option("missingkey=error").ParseFS(
		files,
		"templates/layouts/base.html",
		"templates/partials/header.html",
		"templates/partials/footer.html",
		"templates/partials/picture.html",
		pageTemplate,
	)
}

func loadEditorialImages(files fs.FS) (map[string]EditorialImage, error) {
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
			switch derivative.Variant + ":" + derivative.Format {
			case "landscape:avif":
				image.LandscapeAVIF = derivative.Filename
			case "landscape:webp":
				image.LandscapeWebP = derivative.Filename
			case "card:avif":
				image.CardAVIF = derivative.Filename
			case "card:webp":
				image.CardWebP = derivative.Filename
			}
		}
		images[asset.ID] = image
	}
	return images, nil
}
