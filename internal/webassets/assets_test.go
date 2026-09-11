package webassets_test

import (
	"encoding/json"
	"io/fs"
	"math"
	"os"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/francescostumpo/legal-callegarin/internal/webassets"
)

const maximumDerivativeSize = 300 * 1024

func TestAdminLightSurfacesPinAccessibleDarkText(t *testing.T) {
	t.Parallel()

	content, err := os.ReadFile("../../web/admin/src/admin.css")
	if err != nil {
		t.Fatalf("read admin stylesheet: %v", err)
	}
	for _, selector := range []string{"article-admin-card", "tiptap-surface"} {
		pattern := regexp.MustCompile(`(?s)\.` + selector + `\s*\{[^}]*background:\s*#fff;[^}]*color:\s*#1d1b19;`)
		if !pattern.Match(content) {
			t.Errorf(".%s must pin #1d1b19 text on its white surface", selector)
		}
	}
	if ratio := contrastRatio([3]float64{29, 27, 25}, [3]float64{255, 255, 255}); ratio < 4.5 {
		t.Fatalf("light-surface contrast ratio = %.2f, want at least 4.5", ratio)
	}
}

func contrastRatio(foreground, background [3]float64) float64 {
	luminance := func(rgb [3]float64) float64 {
		for i, value := range rgb {
			value /= 255
			if value <= 0.04045 {
				rgb[i] = value / 12.92
			} else {
				rgb[i] = math.Pow((value+0.055)/1.055, 2.4)
			}
		}
		return rgb[0]*0.2126 + rgb[1]*0.7152 + rgb[2]*0.0722
	}
	lighter := luminance(background)
	darker := luminance(foreground)
	return (lighter + 0.05) / (darker + 0.05)
}

type assetManifest struct {
	Version int              `json:"version"`
	Assets  []editorialAsset `json:"assets"`
}

type editorialAsset struct {
	ID          string       `json:"id"`
	Alt         string       `json:"alt"`
	UsageRole   string       `json:"usageRole"`
	Derivatives []derivative `json:"derivatives"`
}

type derivative struct {
	Variant     string `json:"variant"`
	Format      string `json:"format"`
	Filename    string `json:"filename"`
	Width       int    `json:"width"`
	Height      int    `json:"height"`
	AspectRatio string `json:"aspectRatio"`
}

func TestEditorialAssetManifest(t *testing.T) {
	t.Parallel()

	wantIDs := []string{
		"hero-architecture",
		"approach-library",
		"family-objects",
		"succession-seal",
		"contracts-pen",
		"debt-ledger",
		"damages-road",
		"property-key",
		"criminal-threshold",
		"tax-ledger",
		"article-notebook",
		"contact-entrance",
	}
	manifestBytes, err := fs.ReadFile(webassets.Files, "covers/manifest.json")
	if err != nil {
		t.Fatalf("read manifest: %v", err)
	}
	var manifest assetManifest
	if err := json.Unmarshal(manifestBytes, &manifest); err != nil {
		t.Fatalf("decode manifest: %v", err)
	}
	if manifest.Version != 1 {
		t.Fatalf("manifest version = %d, want 1", manifest.Version)
	}
	if len(manifest.Assets) != len(wantIDs) {
		t.Fatalf("asset count = %d, want %d", len(manifest.Assets), len(wantIDs))
	}

	wantFiles := make(map[string]bool, len(wantIDs)*4)
	seenIDs := make(map[string]bool, len(wantIDs))
	for _, asset := range manifest.Assets {
		if !slices.Contains(wantIDs, asset.ID) || seenIDs[asset.ID] {
			t.Fatalf("unexpected or duplicate asset ID %q", asset.ID)
		}
		seenIDs[asset.ID] = true
		if strings.TrimSpace(asset.Alt) == "" || strings.TrimSpace(asset.UsageRole) == "" {
			t.Fatalf("asset %q lacks alt text or usage role", asset.ID)
		}
		if len(asset.Derivatives) != 4 {
			t.Fatalf("asset %q derivative count = %d, want 4", asset.ID, len(asset.Derivatives))
		}
		for _, item := range asset.Derivatives {
			assertDerivativeContract(t, asset.ID, item)
			if wantFiles[item.Filename] {
				t.Fatalf("duplicate derivative filename %q", item.Filename)
			}
			wantFiles[item.Filename] = true
			content, err := fs.ReadFile(webassets.Files, "covers/"+item.Filename)
			if err != nil {
				t.Fatalf("read derivative %q: %v", item.Filename, err)
			}
			if len(content) == 0 || len(content) > maximumDerivativeSize {
				t.Fatalf("derivative %q size = %d, want 1..%d bytes", item.Filename, len(content), maximumDerivativeSize)
			}
			assertImageSignature(t, item, content)
		}
	}

	entries, err := fs.ReadDir(webassets.Files, "covers")
	if err != nil {
		t.Fatalf("read covers directory: %v", err)
	}
	for _, entry := range entries {
		if entry.Name() != "manifest.json" && !wantFiles[entry.Name()] {
			t.Fatalf("unlisted cover file %q", entry.Name())
		}
	}
}

func TestSelfHostedVariableFonts(t *testing.T) {
	t.Parallel()

	for _, filename := range []string{
		"fraunces-latin-variable.woff2",
		"source-sans-3-latin-variable.woff2",
	} {
		content, err := fs.ReadFile(webassets.Files, "public/fonts/"+filename)
		if err != nil {
			t.Fatalf("read font %q: %v", filename, err)
		}
		if len(content) < 4 || string(content[:4]) != "wOF2" {
			t.Fatalf("font %q is not WOFF2", filename)
		}
	}
	for _, filename := range []string{"OFL-Fraunces.txt", "OFL-Source-Sans-3.txt"} {
		content, err := fs.ReadFile(webassets.Files, "public/fonts/"+filename)
		if err != nil {
			t.Fatalf("read license %q: %v", filename, err)
		}
		if !strings.Contains(string(content), "SIL OPEN FONT LICENSE") {
			t.Fatalf("license %q does not contain the OFL heading", filename)
		}
	}
}

func TestPublicVisualSystemIsLocalAndAccessible(t *testing.T) {
	t.Parallel()

	cssBytes, err := fs.ReadFile(webassets.Files, "public/site.css")
	if err != nil {
		t.Fatalf("read site.css: %v", err)
	}
	css := string(cssBytes)
	for _, fragment := range []string{
		"fraunces-latin-variable.woff2",
		"source-sans-3-latin-variable.woff2",
		"--color-charcoal",
		"--color-ivory",
		"--color-copper",
		"clamp(",
		":focus-visible",
		"@media (min-width: 48rem)",
		"@media (min-width: 72rem)",
		"prefers-reduced-motion: reduce",
		"@media print",
	} {
		if !strings.Contains(css, fragment) {
			t.Fatalf("site.css lacks %q", fragment)
		}
	}
	if strings.Contains(css, "http://") || strings.Contains(css, "https://") {
		t.Fatal("site.css contains a remote runtime URL")
	}

	navigationBytes, err := fs.ReadFile(webassets.Files, "public/nav.js")
	if err != nil {
		t.Fatalf("read nav.js: %v", err)
	}
	navigation := string(navigationBytes)
	for _, fragment := range []string{"data-navigation-close", "Escape", ".focus()", "contains("} {
		if !strings.Contains(navigation, fragment) {
			t.Fatalf("nav.js lacks %q", fragment)
		}
	}
	if strings.Contains(navigation, "http://") || strings.Contains(navigation, "https://") {
		t.Fatal("nav.js contains a remote runtime URL")
	}
}

func TestStandaloneLinksMeetMinimumPointerTarget(t *testing.T) {
	t.Parallel()

	cssBytes, err := fs.ReadFile(webassets.Files, "public/site.css")
	if err != nil {
		t.Fatalf("read site.css: %v", err)
	}
	css := string(cssBytes)
	for _, selector := range []string{".area-card h3 a", ".site-footer nav a"} {
		assertCSSRuleContains(t, css, selector, "display: inline-flex", "min-height: 2.75rem", "align-items: center")
	}
}

func TestArticlePagesHaveReadableEditorialTypography(t *testing.T) {
	t.Parallel()

	cssBytes, err := fs.ReadFile(webassets.Files, "public/site.css")
	if err != nil {
		t.Fatalf("read site.css: %v", err)
	}
	css := string(cssBytes)
	for _, selector := range []string{".article-detail", ".article-byline", ".article-body", ".preview-ribbon"} {
		if !strings.Contains(css, selector+" {") {
			t.Fatalf("site.css lacks rule for %q", selector)
		}
	}
	assertCSSRuleContains(t, css, ".article-body", "max-width:", "font-size:", "line-height:")
}

func TestMobileCloseControlRequiresJavaScriptEnhancement(t *testing.T) {
	t.Parallel()

	cssBytes, err := fs.ReadFile(webassets.Files, "public/site.css")
	if err != nil {
		t.Fatalf("read site.css: %v", err)
	}
	css := string(cssBytes)
	assertCSSRuleContains(t, css, ".mobile-navigation__close", "display: none")
	assertCSSRuleContains(t, css, ".js-enabled .mobile-navigation__close", "display: inline-flex")

	navigationBytes, err := fs.ReadFile(webassets.Files, "public/nav.js")
	if err != nil {
		t.Fatalf("read nav.js: %v", err)
	}
	navigation := strings.TrimSpace(string(navigationBytes))
	marker := `document.documentElement.classList.add("js-enabled")`
	if !strings.HasPrefix(navigation, marker) {
		t.Fatalf("nav.js must apply the enhancement marker before controller setup; got %q", navigation[:min(len(navigation), 80)])
	}
}

func assertCSSRuleContains(t *testing.T, css, selector string, declarations ...string) {
	t.Helper()

	start := strings.Index(css, selector+" {")
	if start < 0 {
		t.Fatalf("site.css lacks rule for %q", selector)
	}
	end := strings.Index(css[start:], "}")
	if end < 0 {
		t.Fatalf("site.css rule for %q is not closed", selector)
	}
	rule := css[start : start+end]
	for _, declaration := range declarations {
		if !strings.Contains(rule, declaration) {
			t.Fatalf("site.css rule for %q lacks %q", selector, declaration)
		}
	}
}

func assertDerivativeContract(t *testing.T, assetID string, item derivative) {
	t.Helper()

	wantWidth, wantHeight, wantRatio := 1600, 900, "16:9"
	if item.Variant == "card" {
		wantWidth, wantHeight, wantRatio = 900, 1125, "4:5"
	} else if item.Variant != "landscape" {
		t.Fatalf("asset %q has unknown variant %q", assetID, item.Variant)
	}
	if item.Format != "avif" && item.Format != "webp" {
		t.Fatalf("asset %q has unsupported format %q", assetID, item.Format)
	}
	if item.Width != wantWidth || item.Height != wantHeight || item.AspectRatio != wantRatio {
		t.Fatalf("asset %q derivative %#v has wrong geometry", assetID, item)
	}
	wantFilename := assetID + "-" + item.Variant + "." + item.Format
	if item.Filename != wantFilename {
		t.Fatalf("asset %q derivative filename = %q, want %q", assetID, item.Filename, wantFilename)
	}
}

func assertImageSignature(t *testing.T, item derivative, content []byte) {
	t.Helper()

	if item.Format == "webp" && (len(content) < 12 || string(content[:4]) != "RIFF" || string(content[8:12]) != "WEBP") {
		t.Fatalf("derivative %q is not WebP", item.Filename)
	}
	if item.Format == "avif" && (len(content) < 12 || string(content[4:8]) != "ftyp") {
		t.Fatalf("derivative %q is not AVIF", item.Filename)
	}
}
