package azure

import (
	"context"
	"strings"
	"testing"
	"time"
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
