package config

import (
	"encoding/base64"
	"errors"
	"fmt"
	"strconv"

	"github.com/francescostumpo/legal-callegarin/internal/storage/azureurl"
)

const (
	developmentEnvironment = "development"
	testEnvironment        = "test"
	productionEnvironment  = "production"

	defaultHTTPAddress       = ":8080"
	developmentPublicBaseURL = "http://localhost:8080"
	developmentSessionKey    = "development-session-key-32-bytes"
	minimumSessionKeyLength  = 32

	ArticleStorageSchemaCompat  = "compat"
	ArticleStorageSchemaMigrate = "migrate"
	ArticleStorageSchemaRepair  = "repair"
)

type Config struct {
	Environment, HTTPAddress, PublicBaseURL, StorageMode string
	ArticleStorageSchemaMode                             string
	AzureStorageAccountURL, AzureStorageConnectionString string
	AdminUsername, AdminPasswordHash                     string
	SessionKey                                           []byte
	TrustedProxyHops                                     int
}

func Load(getenv func(string) string) (Config, error) {
	if getenv == nil {
		return Config{}, errors.New("getenv must not be nil")
	}

	cfg := Config{
		Environment:                  getenv("APP_ENV"),
		HTTPAddress:                  getenv("HTTP_ADDRESS"),
		PublicBaseURL:                getenv("PUBLIC_BASE_URL"),
		StorageMode:                  getenv("STORAGE_MODE"),
		ArticleStorageSchemaMode:     getenv("ARTICLE_STORAGE_SCHEMA_MODE"),
		AzureStorageAccountURL:       getenv("AZURE_STORAGE_ACCOUNT_URL"),
		AzureStorageConnectionString: getenv("AZURE_STORAGE_CONNECTION_STRING"),
		AdminUsername:                getenv("ADMIN_USERNAME"),
		AdminPasswordHash:            getenv("ADMIN_PASSWORD_HASH"),
	}
	if getenv("AZURE_ACCOUNT_URL") != "" {
		return Config{}, errors.New("AZURE_ACCOUNT_URL has been renamed to AZURE_STORAGE_ACCOUNT_URL")
	}

	if getenv("TRUSTED_PROXY") != "" {
		return Config{}, errors.New("TRUSTED_PROXY is obsolete; use TRUSTED_PROXY_HOPS")
	}
	trustedProxyHops, err := loadTrustedProxyHops(getenv("TRUSTED_PROXY_HOPS"))
	if err != nil {
		return Config{}, err
	}
	cfg.TrustedProxyHops = trustedProxyHops

	if err := applyDefaults(&cfg); err != nil {
		return Config{}, err
	}
	if err := validate(cfg); err != nil {
		return Config{}, err
	}

	sessionKey, err := loadSessionKey(cfg.Environment, getenv("SESSION_KEY_BASE64"))
	if err != nil {
		return Config{}, err
	}
	cfg.SessionKey = sessionKey

	return cfg, nil
}

func applyDefaults(cfg *Config) error {
	switch cfg.Environment {
	case developmentEnvironment:
		if cfg.PublicBaseURL == "" {
			cfg.PublicBaseURL = developmentPublicBaseURL
		}
		fallthrough
	case testEnvironment:
		if cfg.HTTPAddress == "" {
			cfg.HTTPAddress = defaultHTTPAddress
		}
		if cfg.StorageMode == "" {
			cfg.StorageMode = "memory"
		}
	case productionEnvironment:
		if cfg.HTTPAddress == "" {
			cfg.HTTPAddress = defaultHTTPAddress
		}
	default:
		return fmt.Errorf("APP_ENV must be one of development, test, or production")
	}
	if cfg.StorageMode == "azure" && cfg.ArticleStorageSchemaMode == "" {
		cfg.ArticleStorageSchemaMode = ArticleStorageSchemaCompat
	}

	return nil
}

func validate(cfg Config) error {
	if cfg.StorageMode != "memory" && cfg.StorageMode != "azure" {
		return errors.New("STORAGE_MODE must be memory or azure")
	}
	if cfg.StorageMode != "azure" && cfg.ArticleStorageSchemaMode != "" {
		return errors.New("ARTICLE_STORAGE_SCHEMA_MODE is only allowed when STORAGE_MODE=azure")
	}
	if cfg.StorageMode == "azure" {
		switch cfg.ArticleStorageSchemaMode {
		case ArticleStorageSchemaCompat, ArticleStorageSchemaMigrate, ArticleStorageSchemaRepair:
		default:
			return errors.New("ARTICLE_STORAGE_SCHEMA_MODE must be compat, migrate, or repair")
		}
	}
	if cfg.Environment == productionEnvironment {
		if cfg.TrustedProxyHops != 1 {
			return errors.New("TRUSTED_PROXY_HOPS must be exactly 1 in production")
		}
		if cfg.StorageMode == "memory" {
			return errors.New("STORAGE_MODE=memory is not allowed in production")
		}
		if cfg.PublicBaseURL == "" {
			return errors.New("PUBLIC_BASE_URL is required in production")
		}
		if cfg.AdminUsername == "" {
			return errors.New("ADMIN_USERNAME is required in production")
		}
		if cfg.AdminPasswordHash == "" {
			return errors.New("ADMIN_PASSWORD_HASH is required in production")
		}
	}
	if cfg.StorageMode == "azure" {
		if cfg.Environment == productionEnvironment && cfg.AzureStorageConnectionString != "" {
			return errors.New("AZURE_STORAGE_CONNECTION_STRING is not allowed in production")
		}
		if cfg.AzureStorageAccountURL != "" && cfg.AzureStorageConnectionString != "" {
			return errors.New("set only one of AZURE_STORAGE_ACCOUNT_URL or AZURE_STORAGE_CONNECTION_STRING")
		}
		if cfg.AzureStorageAccountURL == "" && cfg.AzureStorageConnectionString == "" {
			return errors.New("AZURE_STORAGE_ACCOUNT_URL is required when STORAGE_MODE=azure without a development connection string")
		}
		if cfg.AzureStorageAccountURL != "" {
			if _, _, err := azureurl.Endpoints(cfg.AzureStorageAccountURL); err != nil {
				return errors.New("AZURE_STORAGE_ACCOUNT_URL must be a canonical HTTPS blob service origin")
			}
		}
	}

	return nil
}

func loadSessionKey(environment, encoded string) ([]byte, error) {
	if encoded == "" {
		if environment == developmentEnvironment {
			return []byte(developmentSessionKey), nil
		}
		return nil, errors.New("SESSION_KEY_BASE64 is required outside development")
	}

	key, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		return nil, fmt.Errorf("decode SESSION_KEY_BASE64: %w", err)
	}
	if len(key) < minimumSessionKeyLength {
		return nil, fmt.Errorf("SESSION_KEY_BASE64 must decode to at least %d bytes", minimumSessionKeyLength)
	}

	return key, nil
}

func loadTrustedProxyHops(raw string) (int, error) {
	if raw == "" {
		return 0, nil
	}
	hops, err := strconv.Atoi(raw)
	if err != nil || hops < 0 || hops > 3 || strconv.Itoa(hops) != raw {
		return 0, errors.New("TRUSTED_PROXY_HOPS must be a canonical integer from 0 to 3")
	}
	return hops, nil
}
