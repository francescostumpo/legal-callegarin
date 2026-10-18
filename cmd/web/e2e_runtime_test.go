//go:build e2e

package main

import (
	"context"
	"encoding/base64"
	"errors"
	"strings"
	"testing"

	"github.com/francescostumpo/legal-callegarin/internal/articles"
	"github.com/francescostumpo/legal-callegarin/internal/auth"
	"github.com/francescostumpo/legal-callegarin/internal/config"
	"github.com/francescostumpo/legal-callegarin/internal/contacts"
	memorystorage "github.com/francescostumpo/legal-callegarin/internal/storage/memory"
)

func TestPrepareRuntimeEnvironmentFailsClosed(t *testing.T) {
	t.Parallel()

	valid := validE2EEnvironment()
	tests := []struct {
		name  string
		key   string
		value string
	}{
		{name: "wrong application environment", key: "APP_ENV", value: "production"},
		{name: "implicit storage mode", key: "STORAGE_MODE", value: ""},
		{name: "azure storage", key: "STORAGE_MODE", value: "azure"},
		{name: "wildcard address", key: "HTTP_ADDRESS", value: ":4173"},
		{name: "non-loopback address", key: "HTTP_ADDRESS", value: "0.0.0.0:4173"},
		{name: "insecure origin", key: "PUBLIC_BASE_URL", value: "http://127.0.0.1:4173"},
		{name: "mismatched origin", key: "PUBLIC_BASE_URL", value: "http://127.0.0.1:4174"},
		{name: "missing run secret", key: "E2E_RUN_SECRET", value: ""},
		{name: "weak run secret", key: "E2E_RUN_SECRET", value: base64.RawURLEncoding.EncodeToString([]byte("too-short"))},
		{name: "missing username", key: "E2E_ADMIN_USERNAME", value: ""},
		{name: "missing password", key: "E2E_ADMIN_PASSWORD", value: ""},
		{name: "missing session key", key: "E2E_SESSION_KEY_BASE64", value: ""},
	}
	for _, testCase := range tests {
		testCase := testCase
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			environment := cloneEnvironment(valid)
			environment[testCase.key] = testCase.value
			if _, err := prepareRuntimeEnvironment(mapEnvironment(environment)); err == nil {
				t.Fatal("prepareRuntimeEnvironment() succeeded, want fail-closed error")
			}
		})
	}
}

func TestPrepareRuntimeEnvironmentDerivesEphemeralCredentials(t *testing.T) {
	t.Parallel()

	environment := validE2EEnvironment()
	environment["ADMIN_USERNAME"] = "must-not-win"
	environment["ADMIN_PASSWORD_HASH"] = "must-not-win"
	environment["SESSION_KEY_BASE64"] = "must-not-win"
	prepared, err := prepareRuntimeEnvironment(mapEnvironment(environment))
	if err != nil {
		t.Fatal(err)
	}
	if got := prepared("ADMIN_USERNAME"); got != environment["E2E_ADMIN_USERNAME"] {
		t.Fatalf("ADMIN_USERNAME = %q", got)
	}
	if got := prepared("SESSION_KEY_BASE64"); got != environment["E2E_SESSION_KEY_BASE64"] {
		t.Fatal("SESSION_KEY_BASE64 was not supplied from the E2E-only environment")
	}
	phc := prepared("ADMIN_PASSWORD_HASH")
	if phc == "" || strings.Contains(phc, environment["E2E_ADMIN_PASSWORD"]) {
		t.Fatal("ADMIN_PASSWORD_HASH is missing or contains the plaintext password")
	}
	credentials, err := auth.ParseCredentials(environment["E2E_ADMIN_USERNAME"], phc)
	if err != nil {
		t.Fatal(err)
	}
	if err := credentials.Verify(environment["E2E_ADMIN_USERNAME"], environment["E2E_ADMIN_PASSWORD"]); err != nil {
		t.Fatalf("derived credentials do not verify: %v", err)
	}
	cfg, err := config.Load(prepared)
	if err != nil {
		t.Fatalf("normal config loader rejected prepared environment: %v", err)
	}
	if cfg.AdminUsername != environment["E2E_ADMIN_USERNAME"] || cfg.AdminPasswordHash != phc || len(cfg.SessionKey) != 32 {
		t.Fatal("normal config loader did not receive the ephemeral E2E credentials")
	}
}

func TestSeedRuntimeDataUsesServicesAndProvidesStableFixtures(t *testing.T) {
	t.Parallel()

	bundle := memorystorage.NewBundle(storageNow)
	if err := seedRuntimeData(context.Background(), bundle); err != nil {
		t.Fatal(err)
	}
	articleService := articles.NewService(bundle.Articles, bundle.Bodies, e2eClock{}, &sequenceIDs{})
	published, err := articleService.GetPublished(context.Background(), e2ePublishedArticleSlug)
	if err != nil {
		t.Fatalf("get published fixture: %v", err)
	}
	if published.Article.Title != e2ePublishedArticleTitle || !strings.Contains(published.Body.PlainText, "contenuto sintetico") {
		t.Fatalf("published fixture = %#v body=%q", published.Article, published.Body.PlainText)
	}
	draftStatus := articles.StatusDraft
	drafts, err := bundle.Articles.List(context.Background(), articles.ListOptions{Status: &draftStatus, Limit: 20})
	if err != nil || len(drafts.Items) != 1 {
		t.Fatalf("draft fixtures = %d, err = %v", len(drafts.Items), err)
	}

	contactsPage, err := bundle.Contacts.List(context.Background(), contacts.ListOptions{Limit: 20})
	if err != nil {
		t.Fatal(err)
	}
	states := map[contacts.State]int{}
	for _, contact := range contactsPage.Items {
		states[contact.State]++
		if !strings.HasSuffix(contact.Email, ".test") {
			t.Fatalf("non-synthetic contact email %q", contact.Email)
		}
	}
	for _, state := range []contacts.State{contacts.StateNew, contacts.StateRead, contacts.StateArchived} {
		if states[state] != 1 {
			t.Fatalf("state %q fixtures = %d, want 1", state, states[state])
		}
	}
	if err := seedRuntimeData(context.Background(), bundle); !errors.Is(err, errE2EAlreadySeeded) {
		t.Fatalf("second seed error = %v, want %v", err, errE2EAlreadySeeded)
	}
}

func validE2EEnvironment() map[string]string {
	return map[string]string{
		"APP_ENV":                "test",
		"STORAGE_MODE":           "memory",
		"HTTP_ADDRESS":           "127.0.0.1:4173",
		"PUBLIC_BASE_URL":        "https://127.0.0.1:4173",
		"E2E_RUN_SECRET":         base64.RawURLEncoding.EncodeToString([]byte("0123456789abcdef0123456789abcdef")),
		"E2E_ADMIN_USERNAME":     "e2e-admin",
		"E2E_ADMIN_PASSWORD":     "valid random e2e password",
		"E2E_SESSION_KEY_BASE64": base64.StdEncoding.EncodeToString([]byte("abcdef0123456789abcdef0123456789")),
	}
}

func mapEnvironment(values map[string]string) func(string) string {
	return func(key string) string { return values[key] }
}

func cloneEnvironment(source map[string]string) map[string]string {
	clone := make(map[string]string, len(source))
	for key, value := range source {
		clone[key] = value
	}
	return clone
}
