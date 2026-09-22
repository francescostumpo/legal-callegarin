package azure

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	webapp "github.com/francescostumpo/legal-callegarin/internal/app"
	"github.com/francescostumpo/legal-callegarin/internal/config"
	"github.com/francescostumpo/legal-callegarin/internal/webassets"
)

func TestAzureClientOptionsDisableSDKRetryAndSetApplicationID(t *testing.T) {
	options := azureClientOptions()
	if options.Retry.MaxRetries != -1 || options.Telemetry.ApplicationID != applicationID {
		t.Fatalf("azureClientOptions() = %#v", options)
	}
}

func TestConnectionStringFactoryDoesNotEchoSecret(t *testing.T) {
	secret := "must-not-appear"
	_, err := OpenFromConnectionString(context.Background(), "invalid="+secret, DefaultResourceNames(), time.Now)
	if err == nil || strings.Contains(err.Error(), secret) {
		t.Fatalf("OpenFromConnectionString() error = %q", err)
	}
}

func TestDefaultResourceNames(t *testing.T) {
	want := ResourceNames{ArticlesTable: "articles", ContactsTable: "contacts", SessionsTable: "sessions", BodiesContainer: "article-bodies"}
	if got := DefaultResourceNames(); got != want {
		t.Fatalf("DefaultResourceNames() = %#v, want %#v", got, want)
	}
}

func TestOpenConstructsProductionBundleWithoutProvisioningRequests(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	bundle, err := Open(ctx, "https://example.blob.core.windows.net", DefaultResourceNames(), time.Now)
	if err != nil || bundle == nil {
		t.Fatalf("Open() = %#v, %v", bundle, err)
	}
}

func TestOpenRejectsUnknownArticleSchemaModeBeforeCreatingClients(t *testing.T) {
	t.Parallel()

	if _, err := OpenWithArticleSchemaMode(context.Background(), "https://example.blob.core.windows.net", DefaultResourceNames(), ArticleSchemaMode("automatic"), time.Now); err == nil || !strings.Contains(err.Error(), "article schema mode") {
		t.Fatalf("OpenWithArticleSchemaMode() error = %v", err)
	}
}

func TestAzureDependencyOutageDoesNotPreventStartupOrLiveness(t *testing.T) {
	bundle, err := Open(context.Background(), "https://example.blob.core.windows.net", DefaultResourceNames(), time.Now)
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	bundle.Readiness = &azureReadiness{tables: []tableDriver{&recordingTable{err: ErrTransient}}, blobs: newMemoryBlobDriver()}
	handler, err := webapp.New(webapp.Options{Config: config.Config{StorageMode: "azure", PublicBaseURL: "https://studio.example.test"}, Assets: webassets.Files, Storage: bundle, SessionSigningKey: []byte("0123456789abcdef0123456789abcdef")})
	if err != nil {
		t.Fatalf("app.New() error = %v", err)
	}
	for path, want := range map[string]int{"/health/live": http.StatusOK, "/health/ready": http.StatusServiceUnavailable} {
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, path, nil))
		if response.Code != want {
			t.Fatalf("GET %s = %d, want %d", path, response.Code, want)
		}
	}
}
