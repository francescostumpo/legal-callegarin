package config

import (
	"encoding/base64"
	"strings"
	"testing"
)

func TestLoadDefaults(t *testing.T) {
	t.Parallel()

	validSessionKey := base64.StdEncoding.EncodeToString([]byte("0123456789abcdef0123456789abcdef"))
	tests := []struct {
		name string
		env  map[string]string
		want Config
	}{
		{
			name: "development",
			env:  map[string]string{"APP_ENV": "development"},
			want: Config{
				Environment:   "development",
				HTTPAddress:   ":8080",
				PublicBaseURL: "http://localhost:8080",
				StorageMode:   "memory",
				SessionKey:    []byte("development-session-key-32-bytes"),
			},
		},
		{
			name: "test",
			env: map[string]string{
				"APP_ENV":            "test",
				"SESSION_KEY_BASE64": validSessionKey,
				"TRUSTED_PROXY":      "true",
			},
			want: Config{
				Environment:  "test",
				HTTPAddress:  ":8080",
				StorageMode:  "memory",
				SessionKey:   []byte("0123456789abcdef0123456789abcdef"),
				TrustedProxy: true,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, err := Load(mapGetenv(tt.env))
			if err != nil {
				t.Fatalf("Load() error = %v", err)
			}
			assertConfig(t, got, tt.want)
		})
	}
}

func TestLoadRejectsInvalidConfiguration(t *testing.T) {
	t.Parallel()

	validSessionKey := base64.StdEncoding.EncodeToString([]byte("0123456789abcdef0123456789abcdef"))
	validProduction := map[string]string{
		"APP_ENV":                   "production",
		"PUBLIC_BASE_URL":           "https://example.test",
		"STORAGE_MODE":              "azure",
		"AZURE_STORAGE_ACCOUNT_URL": "https://example.blob.core.windows.net",
		"ADMIN_USERNAME":            "admin",
		"ADMIN_PASSWORD_HASH":       "$argon2id$fixture",
		"SESSION_KEY_BASE64":        validSessionKey,
	}

	tests := []struct {
		name      string
		env       map[string]string
		wantError string
	}{
		{
			name:      "missing environment",
			env:       map[string]string{},
			wantError: "APP_ENV",
		},
		{
			name:      "invalid environment",
			env:       map[string]string{"APP_ENV": "staging"},
			wantError: "APP_ENV",
		},
		{
			name:      "production memory storage",
			env:       withOverride(validProduction, "STORAGE_MODE", "memory"),
			wantError: "STORAGE_MODE",
		},
		{
			name:      "missing production public base URL",
			env:       withOverride(validProduction, "PUBLIC_BASE_URL", ""),
			wantError: "PUBLIC_BASE_URL",
		},
		{
			name:      "missing Azure account URL",
			env:       withOverride(validProduction, "AZURE_STORAGE_ACCOUNT_URL", ""),
			wantError: "AZURE_STORAGE_ACCOUNT_URL",
		},
		{
			name:      "legacy Azure account URL is rejected",
			env:       withOverride(validProduction, "AZURE_ACCOUNT_URL", "https://legacy.table.core.windows.net"),
			wantError: "AZURE_ACCOUNT_URL has been renamed",
		},
		{
			name:      "non canonical Azure account URL",
			env:       withOverride(validProduction, "AZURE_STORAGE_ACCOUNT_URL", "https://example.table.core.windows.net/path"),
			wantError: "AZURE_STORAGE_ACCOUNT_URL",
		},
		{
			name:      "production connection string",
			env:       withOverride(validProduction, "AZURE_STORAGE_CONNECTION_STRING", "secret-production-connection-string"),
			wantError: "AZURE_STORAGE_CONNECTION_STRING is not allowed in production",
		},
		{
			name:      "missing admin username",
			env:       withOverride(validProduction, "ADMIN_USERNAME", ""),
			wantError: "ADMIN_USERNAME",
		},
		{
			name:      "missing admin password hash",
			env:       withOverride(validProduction, "ADMIN_PASSWORD_HASH", ""),
			wantError: "ADMIN_PASSWORD_HASH",
		},
		{
			name:      "missing production session key",
			env:       withOverride(validProduction, "SESSION_KEY_BASE64", ""),
			wantError: "SESSION_KEY_BASE64",
		},
		{
			name:      "session key is not base64",
			env:       withOverride(validProduction, "SESSION_KEY_BASE64", "not base64"),
			wantError: "SESSION_KEY_BASE64",
		},
		{
			name:      "session key is shorter than 32 bytes",
			env:       withOverride(validProduction, "SESSION_KEY_BASE64", base64.StdEncoding.EncodeToString([]byte("too short"))),
			wantError: "32 bytes",
		},
		{
			name:      "trusted proxy is not canonical boolean",
			env:       withOverride(validProduction, "TRUSTED_PROXY", "TRUE"),
			wantError: "TRUSTED_PROXY",
		},
		{
			name:      "invalid article storage schema mode",
			env:       withOverride(validProduction, "ARTICLE_STORAGE_SCHEMA_MODE", "automatic"),
			wantError: "ARTICLE_STORAGE_SCHEMA_MODE",
		},
		{
			name: "article storage schema mode with memory storage",
			env: map[string]string{
				"APP_ENV":                     "development",
				"ARTICLE_STORAGE_SCHEMA_MODE": "compat",
			},
			wantError: "ARTICLE_STORAGE_SCHEMA_MODE",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			_, err := Load(mapGetenv(tt.env))
			if err == nil {
				t.Fatal("Load() error = nil, want an error")
			}
			if !strings.Contains(err.Error(), tt.wantError) {
				t.Fatalf("Load() error = %q, want it to contain %q", err, tt.wantError)
			}
		})
	}
}

func TestLoadAllowsAzureConnectionStringOnlyOutsideProduction(t *testing.T) {
	t.Parallel()

	secret := "UseDevelopmentStorage=true"
	encodedKey := base64.StdEncoding.EncodeToString([]byte("0123456789abcdef0123456789abcdef"))
	for _, environment := range []string{"development", "test"} {
		t.Run(environment, func(t *testing.T) {
			env := map[string]string{
				"APP_ENV":                         environment,
				"STORAGE_MODE":                    "azure",
				"AZURE_STORAGE_CONNECTION_STRING": secret,
			}
			if environment == "test" {
				env["SESSION_KEY_BASE64"] = encodedKey
			}
			got, err := Load(mapGetenv(env))
			if err != nil {
				t.Fatalf("Load() error = %v", err)
			}
			if got.AzureStorageConnectionString != secret || got.AzureStorageAccountURL != "" || got.ArticleStorageSchemaMode != "compat" {
				t.Fatalf("Azure configuration = URL %q, connection string present %t", got.AzureStorageAccountURL, got.AzureStorageConnectionString != "")
			}
		})
	}
}

func TestLoadAcceptsExplicitArticleStorageSchemaModes(t *testing.T) {
	t.Parallel()

	validSessionKey := base64.StdEncoding.EncodeToString([]byte("0123456789abcdef0123456789abcdef"))
	for _, mode := range []string{"compat", "migrate", "repair"} {
		mode := mode
		t.Run(mode, func(t *testing.T) {
			t.Parallel()
			got, err := Load(mapGetenv(map[string]string{
				"APP_ENV":                     "production",
				"PUBLIC_BASE_URL":             "https://example.test",
				"STORAGE_MODE":                "azure",
				"ARTICLE_STORAGE_SCHEMA_MODE": mode,
				"AZURE_STORAGE_ACCOUNT_URL":   "https://example.blob.core.windows.net",
				"ADMIN_USERNAME":              "admin",
				"ADMIN_PASSWORD_HASH":         "$argon2id$fixture",
				"SESSION_KEY_BASE64":          validSessionKey,
			}))
			if err != nil || got.ArticleStorageSchemaMode != mode {
				t.Fatalf("Load() mode = %q, %v", got.ArticleStorageSchemaMode, err)
			}
		})
	}
}

func mapGetenv(values map[string]string) func(string) string {
	return func(key string) string { return values[key] }
}

func withOverride(values map[string]string, key, value string) map[string]string {
	clone := make(map[string]string, len(values))
	for currentKey, currentValue := range values {
		clone[currentKey] = currentValue
	}
	clone[key] = value
	return clone
}

func assertConfig(t *testing.T, got, want Config) {
	t.Helper()

	if got.Environment != want.Environment ||
		got.HTTPAddress != want.HTTPAddress ||
		got.PublicBaseURL != want.PublicBaseURL ||
		got.StorageMode != want.StorageMode ||
		got.AzureStorageAccountURL != want.AzureStorageAccountURL ||
		got.AzureStorageConnectionString != want.AzureStorageConnectionString ||
		got.ArticleStorageSchemaMode != want.ArticleStorageSchemaMode ||
		got.AdminUsername != want.AdminUsername ||
		got.AdminPasswordHash != want.AdminPasswordHash ||
		got.TrustedProxy != want.TrustedProxy ||
		string(got.SessionKey) != string(want.SessionKey) {
		t.Fatalf("Load() = %#v, want %#v", got, want)
	}
}
