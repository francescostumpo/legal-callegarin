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
		"APP_ENV":             "production",
		"PUBLIC_BASE_URL":     "https://example.test",
		"STORAGE_MODE":        "azure",
		"AZURE_ACCOUNT_URL":   "https://example.table.core.windows.net",
		"ADMIN_USERNAME":      "admin",
		"ADMIN_PASSWORD_HASH": "$argon2id$fixture",
		"SESSION_KEY_BASE64":  validSessionKey,
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
			env:       withOverride(validProduction, "AZURE_ACCOUNT_URL", ""),
			wantError: "AZURE_ACCOUNT_URL",
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
		got.AzureAccountURL != want.AzureAccountURL ||
		got.AdminUsername != want.AdminUsername ||
		got.AdminPasswordHash != want.AdminPasswordHash ||
		got.TrustedProxy != want.TrustedProxy ||
		string(got.SessionKey) != string(want.SessionKey) {
		t.Fatalf("Load() = %#v, want %#v", got, want)
	}
}
