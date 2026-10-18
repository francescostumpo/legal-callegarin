package webassets

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"path"
	"strings"
	"sync"
)

type Cover struct {
	ID, Alt                      string
	CardAVIF, CardWebP           string
	LandscapeAVIF, LandscapeWebP string
}

type coverManifest struct {
	Assets []struct {
		ID          string `json:"id"`
		Alt         string `json:"alt"`
		Derivatives []struct {
			Variant  string `json:"variant"`
			Format   string `json:"format"`
			Filename string `json:"filename"`
		} `json:"derivatives"`
	} `json:"assets"`
}

var coverOnce sync.Once
var embeddedCovers []Cover
var coverErr error

func CoverCatalog() ([]Cover, error) {
	coverOnce.Do(func() {
		data, err := Files.ReadFile("covers/manifest.json")
		if err != nil {
			coverErr = err
			return
		}
		var manifest coverManifest
		if err = json.Unmarshal(data, &manifest); err != nil {
			coverErr = err
			return
		}
		seen := map[string]bool{}
		for _, asset := range manifest.Assets {
			cover := Cover{ID: asset.ID, Alt: asset.Alt}
			for _, derivative := range asset.Derivatives {
				content, readErr := Files.ReadFile("covers/" + derivative.Filename)
				if readErr != nil {
					coverErr = readErr
					return
				}
				digest := sha256.Sum256(content)
				extension := path.Ext(derivative.Filename)
				stem := strings.TrimSuffix(path.Base(derivative.Filename), extension)
				assetPath := fmt.Sprintf("/assets/covers/%s-%x%s", stem, digest[:6], extension)
				switch derivative.Variant + ":" + derivative.Format {
				case "card:avif":
					cover.CardAVIF = assetPath
				case "card:webp":
					cover.CardWebP = assetPath
				case "landscape:avif":
					cover.LandscapeAVIF = assetPath
				case "landscape:webp":
					cover.LandscapeWebP = assetPath
				}
			}
			if cover.ID == "" || cover.Alt == "" || cover.CardAVIF == "" || cover.CardWebP == "" || cover.LandscapeAVIF == "" || cover.LandscapeWebP == "" || seen[cover.ID] {
				coverErr = fmt.Errorf("invalid cover manifest entry %q", cover.ID)
				return
			}
			seen[cover.ID] = true
			embeddedCovers = append(embeddedCovers, cover)
		}
		if len(embeddedCovers) != 12 {
			coverErr = fmt.Errorf("cover manifest has %d entries, want 12", len(embeddedCovers))
		}
	})
	return append([]Cover(nil), embeddedCovers...), coverErr
}

func ValidCoverID(id string) bool {
	if id == "" {
		return true
	}
	covers, err := CoverCatalog()
	if err != nil {
		return false
	}
	for _, cover := range covers {
		if cover.ID == id {
			return true
		}
	}
	return false
}
