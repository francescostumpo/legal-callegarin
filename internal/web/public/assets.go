package public

import (
	"bytes"
	"crypto/sha256"
	"fmt"
	"io/fs"
	"mime"
	"net/http"
	"path"
	"sort"
	"strings"
	"time"
)

const immutableAssetCacheControl = "public, max-age=31536000, immutable"

type assetCatalog struct {
	files      fs.FS
	assets     map[string]staticAsset
	publicURLs map[string]string
	coverNames map[string]string
}

type staticAsset struct {
	name       string
	sourcePath string
	content    []byte
}

func newAssetCatalog(files fs.FS) (*assetCatalog, error) {
	catalog := &assetCatalog{
		files: files, assets: make(map[string]staticAsset), publicURLs: make(map[string]string), coverNames: make(map[string]string),
	}
	var styles []string
	err := fs.WalkDir(files, "public", func(filename string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			return nil
		}
		logical := strings.TrimPrefix(filename, "public/")
		if path.Ext(logical) == ".css" {
			styles = append(styles, logical)
			return nil
		}
		content, err := fs.ReadFile(files, filename)
		if err != nil {
			return err
		}
		catalog.addPublic(logical, content, filename)
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("load public assets: %w", err)
	}

	logicalAssets := make([]string, 0, len(catalog.publicURLs))
	for logical := range catalog.publicURLs {
		logicalAssets = append(logicalAssets, logical)
	}
	sort.Slice(logicalAssets, func(left, right int) bool { return len(logicalAssets[left]) > len(logicalAssets[right]) })
	for _, logical := range styles {
		content, err := fs.ReadFile(files, "public/"+logical)
		if err != nil {
			return nil, fmt.Errorf("read stylesheet %q: %w", logical, err)
		}
		rewritten := string(content)
		for _, dependency := range logicalAssets {
			rewritten = strings.ReplaceAll(rewritten, "/assets/"+dependency, catalog.publicURLs[dependency])
		}
		catalog.addPublic(logical, []byte(rewritten), "")
	}

	err = fs.WalkDir(files, "covers", func(filename string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() || filename == "covers/manifest.json" {
			return nil
		}
		content, err := fs.ReadFile(files, filename)
		if err != nil {
			return err
		}
		logical := strings.TrimPrefix(filename, "covers/")
		fingerprinted := fingerprintedName(logical, content)
		catalog.coverNames[logical] = fingerprinted
		assetURL := "/assets/covers/" + fingerprinted
		catalog.assets[assetURL] = staticAsset{name: fingerprinted, sourcePath: filename}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("load editorial assets: %w", err)
	}
	return catalog, nil
}

func (catalog *assetCatalog) addPublic(logical string, content []byte, sourcePath string) {
	fingerprinted := fingerprintedName(logical, content)
	assetURL := "/assets/" + fingerprinted
	catalog.publicURLs[logical] = assetURL
	asset := staticAsset{name: fingerprinted, sourcePath: sourcePath}
	if sourcePath == "" {
		asset.content = append([]byte(nil), content...)
	}
	catalog.assets[assetURL] = asset
}

func (catalog *assetCatalog) publicURL(logical string) (string, error) {
	assetURL, exists := catalog.publicURLs[logical]
	if !exists {
		return "", fmt.Errorf("public asset %q is not catalogued", logical)
	}
	return assetURL, nil
}

func (catalog *assetCatalog) coverName(logical string) (string, error) {
	name, exists := catalog.coverNames[logical]
	if !exists {
		return "", fmt.Errorf("editorial asset %q is not catalogued", logical)
	}
	return name, nil
}

func (catalog *assetCatalog) ServeHTTP(response http.ResponseWriter, request *http.Request) {
	asset, exists := catalog.assets[request.URL.Path]
	if !exists {
		writePublicError(response, http.StatusNotFound, "asset not found")
		return
	}
	response.Header().Set("Cache-Control", immutableAssetCacheControl)
	if contentType := mime.TypeByExtension(path.Ext(asset.name)); contentType != "" {
		response.Header().Set("Content-Type", contentType)
	}
	if asset.sourcePath != "" {
		http.ServeFileFS(response, request, catalog.files, asset.sourcePath)
		return
	}
	http.ServeContent(response, request, asset.name, time.Time{}, bytes.NewReader(asset.content))
}

func fingerprintedName(logical string, content []byte) string {
	digest := sha256.Sum256(content)
	extension := path.Ext(logical)
	stem := strings.TrimSuffix(path.Base(logical), extension)
	name := fmt.Sprintf("%s-%x%s", stem, digest[:6], extension)
	if directory := path.Dir(logical); directory != "." {
		return directory + "/" + name
	}
	return name
}
