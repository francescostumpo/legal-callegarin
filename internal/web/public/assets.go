package public

import (
	"io/fs"
	"net/http"
	"path"
	"strings"
	"unicode"
)

func staticAssetHandler(files fs.FS) http.Handler {
	server := http.FileServerFS(files)
	return http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		if hasFingerprint(request.URL.Path) {
			response.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
		}
		server.ServeHTTP(response, request)
	})
}

func hasFingerprint(assetPath string) bool {
	name := path.Base(assetPath)
	stem := strings.TrimSuffix(name, path.Ext(name))
	separator := strings.LastIndexByte(stem, '-')
	if separator < 0 || len(stem)-separator-1 < 8 {
		return false
	}
	suffix := stem[separator+1:]
	allHex := true
	hasUpper := false
	hasLower := false
	for _, character := range suffix {
		allHex = allHex && (character >= '0' && character <= '9' || character >= 'a' && character <= 'f')
		hasUpper = hasUpper || unicode.IsUpper(character)
		hasLower = hasLower || unicode.IsLower(character)
	}
	return allHex || hasUpper && hasLower
}
