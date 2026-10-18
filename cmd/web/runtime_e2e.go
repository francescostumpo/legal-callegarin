//go:build e2e

package main

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"net"
	"net/url"
	"strings"
	"time"

	"github.com/francescostumpo/legal-callegarin/internal/articles"
	"github.com/francescostumpo/legal-callegarin/internal/auth"
	"github.com/francescostumpo/legal-callegarin/internal/contacts"
	storagebundle "github.com/francescostumpo/legal-callegarin/internal/storage"
)

const (
	e2ePublishedArticleSlug  = "affidamento-condiviso-guida-e2e"
	e2ePublishedArticleTitle = "Affidamento condiviso: guida essenziale"
)

var (
	errE2EAlreadySeeded = errors.New("E2E runtime data is already seeded")
	e2eFixedTime        = time.Date(2026, time.September, 11, 8, 42, 3, 0, time.UTC)
)

func prepareRuntimeEnvironment(getenv func(string) string) (func(string) string, error) {
	if getenv == nil {
		return nil, errors.New("E2E environment reader is required")
	}
	if getenv("APP_ENV") != "test" {
		return nil, errors.New("E2E startup requires APP_ENV=test")
	}
	if getenv("STORAGE_MODE") != "memory" {
		return nil, errors.New("E2E startup requires explicit STORAGE_MODE=memory")
	}
	if err := validateE2ELoopbackOrigin(getenv("HTTP_ADDRESS"), getenv("PUBLIC_BASE_URL")); err != nil {
		return nil, err
	}
	if err := validateE2ERunSecret(getenv("E2E_RUN_SECRET")); err != nil {
		return nil, err
	}

	username := getenv("E2E_ADMIN_USERNAME")
	password := []byte(getenv("E2E_ADMIN_PASSWORD"))
	defer zeroBytes(password)
	if username == "" || strings.TrimSpace(username) != username || len(username) > 128 {
		return nil, errors.New("E2E admin username is required and must be canonical")
	}
	if err := auth.ValidatePassword(password); err != nil {
		return nil, errors.New("E2E admin password is required and must be valid")
	}
	sessionKey := getenv("E2E_SESSION_KEY_BASE64")
	if err := validateE2ESessionKey(sessionKey); err != nil {
		return nil, err
	}
	phc, err := auth.HashPassword(password, rand.Reader)
	if err != nil {
		return nil, errors.New("derive E2E admin credentials")
	}

	overrides := map[string]string{
		"ADMIN_USERNAME":      username,
		"ADMIN_PASSWORD_HASH": phc,
		"SESSION_KEY_BASE64":  sessionKey,
	}
	return func(key string) string {
		if value, ok := overrides[key]; ok {
			return value
		}
		return getenv(key)
	}, nil
}

func validateE2ELoopbackOrigin(address, baseURL string) error {
	host, port, err := net.SplitHostPort(address)
	if err != nil || port == "" {
		return errors.New("E2E HTTP_ADDRESS must be an explicit loopback host and port")
	}
	ip := net.ParseIP(host)
	if ip == nil || !ip.IsLoopback() {
		return errors.New("E2E HTTP_ADDRESS must use a loopback IP")
	}
	parsed, err := url.Parse(baseURL)
	if err != nil || parsed.Scheme != "https" || parsed.Host != address || parsed.User != nil || (parsed.Path != "" && parsed.Path != "/") || parsed.RawQuery != "" || parsed.Fragment != "" {
		return errors.New("E2E PUBLIC_BASE_URL must be the matching loopback HTTPS origin")
	}
	return nil
}

func validateE2ERunSecret(encoded string) error {
	secret, err := base64.RawURLEncoding.Strict().DecodeString(encoded)
	if err != nil || len(secret) < 32 || base64.RawURLEncoding.EncodeToString(secret) != encoded {
		return errors.New("E2E_RUN_SECRET must be canonical base64url for at least 32 random bytes")
	}
	unique := make(map[byte]struct{}, len(secret))
	for _, value := range secret {
		unique[value] = struct{}{}
	}
	if len(unique) < 16 {
		return errors.New("E2E_RUN_SECRET does not have sufficient diversity")
	}
	return nil
}

func validateE2ESessionKey(encoded string) error {
	key, err := base64.StdEncoding.Strict().DecodeString(encoded)
	if err != nil || len(key) < 32 || base64.StdEncoding.EncodeToString(key) != encoded {
		return errors.New("E2E session key must be canonical base64 for at least 32 bytes")
	}
	return nil
}

func storageNow() time.Time { return e2eFixedTime }

type e2eClock struct{}

func (e2eClock) Now() time.Time { return e2eFixedTime }

type sequenceIDs struct {
	prefix string
	next   int
}

func (ids *sequenceIDs) NewID() string {
	ids.next++
	prefix := ids.prefix
	if prefix == "" {
		prefix = "e2e"
	}
	return fmt.Sprintf("%s-%02d", prefix, ids.next)
}

func seedRuntimeData(ctx context.Context, bundle *storagebundle.Bundle) error {
	if bundle == nil || bundle.Articles == nil || bundle.Bodies == nil || bundle.Contacts == nil {
		return errors.New("complete E2E memory storage bundle is required")
	}
	if _, err := bundle.Articles.GetPublishedBySlug(ctx, e2ePublishedArticleSlug); err == nil {
		return errE2EAlreadySeeded
	} else if !errors.Is(err, articles.ErrNotFound) {
		return fmt.Errorf("inspect E2E article fixtures: %w", err)
	}

	articleIDs := &sequenceIDs{prefix: "e2e-article"}
	articleService := articles.NewService(bundle.Articles, bundle.Bodies, e2eClock{}, articleIDs)
	published, err := articleService.CreateDraft(ctx, e2eArticleInput(
		e2ePublishedArticleSlug,
		e2ePublishedArticleTitle,
		"Una sintesi orientativa e interamente sintetica per il collaudo automatico.",
		"Questo contenuto sintetico verifica la pubblicazione dell'articolo.",
	))
	if err != nil {
		return fmt.Errorf("create published E2E article draft: %w", err)
	}
	if _, err := articleService.Publish(ctx, published.ID, published.ETag); err != nil {
		return fmt.Errorf("publish E2E article: %w", err)
	}
	if _, err := articleService.CreateDraft(ctx, e2eArticleInput(
		"bozza-riservata-e2e",
		"Bozza riservata per il collaudo",
		"Una bozza sintetica non pubblicata usata esclusivamente dal collaudo automatico.",
		"Questo contenuto sintetico deve restare in stato di bozza.",
	)); err != nil {
		return fmt.Errorf("create draft E2E article: %w", err)
	}

	contactIDs := &sequenceIDs{prefix: "e2e-contact"}
	contactService := contacts.NewService(bundle.Contacts, e2eClock{}, contactIDs)
	contactFixtures := []contacts.Submission{
		{Name: "Persona Nuova E2E", Email: "nuova@example.test", Phone: "+39 000 0000001", Message: "Richiesta sintetica nuova per il collaudo automatico.", ConsentVersion: "e2e-privacy-v1"},
		{Name: "Persona Letta E2E", Email: "letta@example.test", Phone: "+39 000 0000002", Message: "Richiesta sintetica letta per il collaudo automatico.", ConsentVersion: "e2e-privacy-v1"},
		{Name: "Persona Archiviata E2E", Email: "archiviata@example.test", Phone: "+39 000 0000003", Message: "Richiesta sintetica archiviata per il collaudo automatico.", ConsentVersion: "e2e-privacy-v1"},
	}
	created := make([]contacts.Contact, 0, len(contactFixtures))
	for _, fixture := range contactFixtures {
		contact, err := contactService.Submit(ctx, fixture)
		if err != nil {
			return fmt.Errorf("create E2E contact fixture: %w", err)
		}
		created = append(created, contact)
	}
	if _, err := contactService.Open(ctx, created[1].ID, created[1].ETag); err != nil {
		return fmt.Errorf("open E2E contact fixture: %w", err)
	}
	read, err := contactService.Open(ctx, created[2].ID, created[2].ETag)
	if err != nil {
		return fmt.Errorf("read archived E2E contact fixture: %w", err)
	}
	if _, err := contactService.Archive(ctx, read.ID, read.ETag); err != nil {
		return fmt.Errorf("archive E2E contact fixture: %w", err)
	}
	return nil
}

func e2eArticleInput(slug, title, summary, body string) articles.DraftInput {
	document := fmt.Sprintf(`{"type":"doc","content":[{"type":"paragraph","content":[{"type":"text","text":%q}]}]}`, body)
	return articles.DraftInput{
		Slug: slug, Title: title, Summary: summary, Area: "famiglia-e-persone", CoverID: "article-notebook",
		Body: articles.Body{SchemaVersion: 1, Document: []byte(document)},
	}
}

func zeroBytes(value []byte) {
	for index := range value {
		value[index] = 0
	}
}
