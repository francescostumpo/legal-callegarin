package config

import (
	"encoding/base64"
	"errors"
	"fmt"
)

const (
	developmentEnvironment = "development"
	testEnvironment        = "test"
	productionEnvironment  = "production"

	defaultHTTPAddress       = ":8080"
	developmentPublicBaseURL = "http://localhost:8080"
	developmentSessionKey    = "development-session-key-32-bytes"
	minimumSessionKeyLength  = 32
)

type Config struct {
	Environment, HTTPAddress, PublicBaseURL, StorageMode, AzureAccountURL string
	AdminUsername, AdminPasswordHash                                      string
	SessionKey                                                            []byte
	TrustedProxy                                                          bool
}

func Load(getenv func(string) string) (Config, error) {
	if getenv == nil {
		return Config{}, errors.New("getenv must not be nil")
	}

	cfg := Config{
		Environment:       getenv("APP_ENV"),
		HTTPAddress:       getenv("HTTP_ADDRESS"),
		PublicBaseURL:     getenv("PUBLIC_BASE_URL"),
		StorageMode:       getenv("STORAGE_MODE"),
		AzureAccountURL:   getenv("AZURE_ACCOUNT_URL"),
		AdminUsername:     getenv("ADMIN_USERNAME"),
		AdminPasswordHash: getenv("ADMIN_PASSWORD_HASH"),
	}

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

	trustedProxy, err := loadTrustedProxy(getenv("TRUSTED_PROXY"))
	if err != nil {
		return Config{}, err
	}
	cfg.TrustedProxy = trustedProxy

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

	return nil
}

func validate(cfg Config) error {
	if cfg.StorageMode != "memory" && cfg.StorageMode != "azure" {
		return errors.New("STORAGE_MODE must be memory or azure")
	}
	if cfg.Environment == productionEnvironment {
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
	if cfg.StorageMode == "azure" && cfg.AzureAccountURL == "" {
		return errors.New("AZURE_ACCOUNT_URL is required when STORAGE_MODE=azure")
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

func loadTrustedProxy(raw string) (bool, error) {
	switch raw {
	case "", "false":
		return false, nil
	case "true":
		return true, nil
	default:
		return false, errors.New("TRUSTED_PROXY must be true or false")
	}
}
