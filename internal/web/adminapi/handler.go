package adminapi

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"html/template"
	"io/fs"
	"mime"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/francescostumpo/legal-callegarin/internal/articles"
	"github.com/francescostumpo/legal-callegarin/internal/auth"
	"github.com/francescostumpo/legal-callegarin/internal/contacts"
	webmiddleware "github.com/francescostumpo/legal-callegarin/internal/web/middleware"
	publicweb "github.com/francescostumpo/legal-callegarin/internal/web/public"
)

const (
	loginBodyLimit    = 8 << 10
	loginTokenMaxAge  = 10 * time.Minute
	loginTokenSkew    = 5 * time.Second
	defaultLoginLimit = 5
	defaultMaxBuckets = 5000
)

type CredentialVerifier interface {
	Verify(username, password string) error
}

type SessionController interface {
	Rotate(context.Context, string) (string, auth.Session, error)
	Logout(context.Context, string) error
}

type Options struct {
	Credentials        CredentialVerifier
	ConfiguredUsername string
	Sessions           SessionController
	Contacts           contacts.ContactService
	Articles           articles.ArticleService
	Renderer           *publicweb.Renderer
	Assets             fs.FS
	SessionKey         []byte
	PublicBaseURL      string
	Now                func() time.Time
	TrustedProxyHops   int
	LoginCapacity      int
	MaxBuckets         int
}

type handler struct {
	credentials      CredentialVerifier
	sessions         SessionController
	contacts         contacts.ContactService
	articles         articles.ArticleService
	renderer         *publicweb.Renderer
	adminFiles       http.Handler
	index            []byte
	sessionKey       []byte
	formKey          []byte
	expectedOrigin   string
	now              func() time.Time
	trustedProxyHops int
	limiter          *loginLimiter
	template         *template.Template
	loginStylesheet  string
	loginFavicon     string
}

type loginView struct {
	Token      string
	Error      string
	Stylesheet string
	Favicon    string
}

func New(options Options) (http.Handler, error) {
	if options.Credentials == nil || options.Sessions == nil || options.Assets == nil || len(options.SessionKey) < 32 || normalizeUsername(options.ConfiguredUsername) == "" {
		return nil, errors.New("admin handler requires credentials, sessions, assets, and a session key")
	}
	base, err := url.Parse(options.PublicBaseURL)
	if err != nil || !validAdminOrigin(base) || base.User != nil || base.Path != "" || base.RawQuery != "" || base.Fragment != "" {
		return nil, errors.New("admin handler requires a valid public origin")
	}
	adminFS, err := fs.Sub(options.Assets, "admin/dist")
	if err != nil {
		return nil, fmt.Errorf("admin assets: %w", err)
	}
	index, err := fs.ReadFile(adminFS, "index.html")
	if err != nil {
		return nil, fmt.Errorf("admin index: %w", err)
	}
	if options.Now == nil {
		options.Now = time.Now
	}
	if options.LoginCapacity <= 0 {
		options.LoginCapacity = defaultLoginLimit
	}
	if options.MaxBuckets <= 0 {
		options.MaxBuckets = defaultMaxBuckets
	}
	loginTemplate, err := template.New("login").Parse(loginPage)
	if err != nil {
		return nil, fmt.Errorf("admin login template: %w", err)
	}
	loginStylesheet := ""
	loginFavicon := ""
	if options.Renderer != nil {
		loginStylesheet, err = options.Renderer.PublicAssetURL("site.css")
		if err != nil {
			return nil, fmt.Errorf("admin login stylesheet: %w", err)
		}
		loginFavicon, err = options.Renderer.PublicAssetURL("favicon.svg")
		if err != nil {
			return nil, fmt.Errorf("admin favicon: %w", err)
		}
		index, err = injectAdminFavicon(index, loginFavicon)
		if err != nil {
			return nil, fmt.Errorf("admin index favicon: %w", err)
		}
	}
	configuredUsernameKey := keyedValue(options.SessionKey, "callegarin/admin-login-username/v1", normalizeUsername(options.ConfiguredUsername))
	instance := &handler{
		credentials:      options.Credentials,
		sessions:         options.Sessions,
		contacts:         options.Contacts,
		articles:         options.Articles,
		renderer:         options.Renderer,
		adminFiles:       http.FileServer(http.FS(adminFS)),
		index:            index,
		sessionKey:       append([]byte(nil), options.SessionKey...),
		formKey:          deriveKey(options.SessionKey, "callegarin/admin-login-form/v1"),
		expectedOrigin:   base.Scheme + "://" + base.Host,
		now:              options.Now,
		trustedProxyHops: options.TrustedProxyHops,
		limiter:          newLoginLimiter(options.LoginCapacity, 3*time.Minute, time.Hour, options.MaxBuckets, configuredUsernameKey),
		template:         loginTemplate,
		loginStylesheet:  loginStylesheet,
		loginFavicon:     loginFavicon,
	}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /admin/login", instance.loginGET)
	mux.HandleFunc("POST /admin/login", instance.loginPOST)
	mux.HandleFunc("GET /api/admin/session", instance.sessionGET)
	mux.HandleFunc("DELETE /api/admin/session", instance.sessionDELETE)
	mux.HandleFunc("GET /api/admin/dashboard", instance.dashboardGET)
	mux.HandleFunc("GET /api/admin/contacts", instance.contactsGET)
	mux.HandleFunc("POST /api/admin/contacts/purge-due", instance.purgeDuePOST)
	mux.HandleFunc("GET /api/admin/contacts/{id}", instance.contactGET)
	mux.HandleFunc("POST /api/admin/contacts/{id}/{action}", instance.contactPOST)
	mux.HandleFunc("GET /api/admin/articles", instance.articlesGET)
	mux.HandleFunc("POST /api/admin/articles", instance.articleCreatePOST)
	mux.HandleFunc("GET /api/admin/articles/{id}", instance.articleGET)
	mux.HandleFunc("PUT /api/admin/articles/{id}/draft", instance.articleDraftPUT)
	mux.HandleFunc("POST /api/admin/articles/{id}/publish", instance.articlePublishPOST)
	mux.HandleFunc("POST /api/admin/articles/{id}/withdraw", instance.articleWithdrawPOST)
	mux.HandleFunc("GET /api/admin/covers", instance.coversGET)
	mux.HandleFunc("GET /admin/preview/articles/{id}", instance.articlePreviewGET)
	mux.HandleFunc("/api/admin", instance.apiNotFound)
	mux.HandleFunc("/api/admin/", instance.apiNotFound)
	mux.HandleFunc("GET /admin", instance.indexGET)
	mux.HandleFunc("GET /admin/", instance.adminGET)
	return mux, nil
}

func validAdminOrigin(origin *url.URL) bool {
	if origin.Host == "" {
		return false
	}
	if origin.Scheme == "https" {
		return true
	}
	host := origin.Hostname()
	return origin.Scheme == "http" && (host == "localhost" || host == "127.0.0.1" || host == "::1")
}

func (handler *handler) loginGET(response http.ResponseWriter, _ *http.Request) {
	handler.renderLogin(response, http.StatusOK, "")
}

func (handler *handler) loginPOST(response http.ResponseWriter, request *http.Request) {
	setPrivateAdminHeaders(response)
	mediaType, _, err := mime.ParseMediaType(request.Header.Get("Content-Type"))
	if err != nil || mediaType != "application/x-www-form-urlencoded" {
		handler.renderLogin(response, http.StatusUnsupportedMediaType, "Richiesta non valida")
		return
	}
	if request.Header.Get("Origin") != handler.expectedOrigin {
		handler.renderLogin(response, http.StatusForbidden, "Richiesta non valida")
		return
	}
	request.Body = http.MaxBytesReader(response, request.Body, loginBodyLimit)
	if err := request.ParseForm(); err != nil || !exactLoginForm(request.PostForm) {
		handler.renderLogin(response, http.StatusBadRequest, "Richiesta non valida")
		return
	}
	username := request.PostForm.Get("username")
	password := request.PostForm.Get("password")
	started := request.PostForm.Get("started")
	if len(username) > 128 || auth.ValidatePassword([]byte(password)) != nil || len(started) > 256 || !handler.validFormToken(started) {
		handler.renderLogin(response, http.StatusBadRequest, "Richiesta non valida")
		return
	}
	addressKey, err := loginAddressKey(request, handler.trustedProxyHops, handler.sessionKey)
	if err != nil {
		handler.renderLogin(response, http.StatusBadRequest, "Richiesta non valida")
		return
	}
	now := handler.now().UTC()
	if !handler.limiter.allowAddress(addressKey, now) {
		handler.renderLogin(response, http.StatusTooManyRequests, "Accesso temporaneamente non disponibile")
		return
	}
	usernameKey := keyedValue(handler.sessionKey, "callegarin/admin-login-username/v1", normalizeUsername(username))
	if !handler.limiter.allowUsername(usernameKey, now) {
		handler.renderLogin(response, http.StatusTooManyRequests, "Accesso temporaneamente non disponibile")
		return
	}
	if err := handler.credentials.Verify(username, password); err != nil {
		handler.renderLogin(response, http.StatusUnauthorized, "Credenziali non valide")
		return
	}
	prior := ""
	if cookie, err := request.Cookie(webmiddleware.SessionCookieName); err == nil {
		prior = cookie.Value
	}
	raw, _, err := handler.sessions.Rotate(request.Context(), prior)
	if err != nil {
		handler.renderLogin(response, http.StatusServiceUnavailable, "Accesso temporaneamente non disponibile")
		return
	}
	webmiddleware.SetSessionCookie(response, raw, now)
	http.Redirect(response, request, "/admin", http.StatusSeeOther)
}

func (handler *handler) sessionGET(response http.ResponseWriter, request *http.Request) {
	setPrivateAdminHeaders(response)
	principal, ok := webmiddleware.PrincipalFromContext(request.Context())
	if !ok {
		writeJSONError(response, http.StatusUnauthorized, "authentication required")
		return
	}
	response.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(response).Encode(map[string]string{
		"username":  principal.Username,
		"csrfToken": webmiddleware.CSRFToken(principal.Token, handler.sessionKey),
	})
}

func (handler *handler) sessionDELETE(response http.ResponseWriter, request *http.Request) {
	setPrivateAdminHeaders(response)
	principal, ok := webmiddleware.PrincipalFromContext(request.Context())
	if !ok {
		writeJSONError(response, http.StatusUnauthorized, "authentication required")
		return
	}
	if err := handler.sessions.Logout(request.Context(), principal.Token); err != nil {
		writeJSONError(response, http.StatusServiceUnavailable, "logout temporarily unavailable")
		return
	}
	webmiddleware.ClearSessionCookie(response, handler.now())
	response.WriteHeader(http.StatusNoContent)
}

func (handler *handler) indexGET(response http.ResponseWriter, _ *http.Request) {
	serveAdminIndex(response, handler.index)
}

func (handler *handler) adminGET(response http.ResponseWriter, request *http.Request) {
	setPrivateAdminHeaders(response)
	if strings.HasPrefix(request.URL.Path, "/admin/assets/") {
		clone := request.Clone(request.Context())
		clone.URL.Path = strings.TrimPrefix(request.URL.Path, "/admin/")
		handler.adminFiles.ServeHTTP(response, clone)
		return
	}
	serveAdminIndex(response, handler.index)
}

func (handler *handler) renderLogin(response http.ResponseWriter, status int, message string) {
	setPrivateAdminHeaders(response)
	response.Header().Set("Content-Type", "text/html; charset=utf-8")
	response.WriteHeader(status)
	_ = handler.template.Execute(response, loginView{Token: handler.newFormToken(), Error: message, Stylesheet: handler.loginStylesheet, Favicon: handler.loginFavicon})
}

func injectAdminFavicon(index []byte, faviconURL string) ([]byte, error) {
	headEnd := bytes.LastIndex(bytes.ToLower(index), []byte("</head>"))
	if headEnd < 0 {
		return nil, errors.New("admin index has no </head> insertion point")
	}
	linkTemplate, err := template.New("admin favicon").Parse(`<link rel="icon" type="image/svg+xml" href="{{.}}">`)
	if err != nil {
		return nil, err
	}
	var link bytes.Buffer
	if err := linkTemplate.Execute(&link, faviconURL); err != nil {
		return nil, err
	}
	result := make([]byte, 0, len(index)+link.Len())
	result = append(result, index[:headEnd]...)
	result = append(result, link.Bytes()...)
	result = append(result, index[headEnd:]...)
	return result, nil
}

func serveAdminIndex(response http.ResponseWriter, index []byte) {
	setPrivateAdminHeaders(response)
	response.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = response.Write(index)
}

func exactLoginForm(values url.Values) bool {
	if len(values) != 3 {
		return false
	}
	for _, field := range []string{"username", "password", "started"} {
		if len(values[field]) != 1 {
			return false
		}
	}
	return true
}

func (handler *handler) newFormToken() string {
	payload := fmt.Sprintf("%d", handler.now().UTC().Unix())
	return payload + "." + sign(handler.formKey, payload)
}

func (handler *handler) validFormToken(token string) bool {
	payload, signature, ok := strings.Cut(token, ".")
	if !ok || payload == "" || signature == "" || strings.Contains(signature, ".") {
		return false
	}
	var seconds int64
	if _, err := fmt.Sscanf(payload, "%d", &seconds); err != nil || payload != fmt.Sprintf("%d", seconds) {
		return false
	}
	expected := sign(handler.formKey, payload)
	if !hmac.Equal([]byte(signature), []byte(expected)) {
		return false
	}
	age := handler.now().UTC().Sub(time.Unix(seconds, 0).UTC())
	return age >= -loginTokenSkew && age <= loginTokenMaxAge
}

func deriveKey(master []byte, label string) []byte {
	mac := hmac.New(sha256.New, master)
	_, _ = mac.Write([]byte(label))
	return mac.Sum(nil)
}

func sign(key []byte, payload string) string {
	mac := hmac.New(sha256.New, key)
	_, _ = mac.Write([]byte(payload))
	return base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}

func normalizeUsername(username string) string {
	return strings.ToLower(strings.TrimSpace(username))
}

func keyedValue(key []byte, domain, value string) string {
	mac := hmac.New(sha256.New, key)
	_, _ = mac.Write([]byte(domain))
	_, _ = mac.Write([]byte{0})
	_, _ = mac.Write([]byte(value))
	return base64.RawURLEncoding.EncodeToString(mac.Sum(nil)[:16])
}

func writeJSONError(response http.ResponseWriter, status int, message string) {
	code := genericAPIErrorCode(status)
	_ = message
	webmiddleware.WriteAPIErrorResponse(response, status, code, safeAPIMessage(code), nil)
}

func genericAPIErrorCode(status int) string {
	switch status {
	case http.StatusBadRequest:
		return "invalid_request"
	case http.StatusUnauthorized:
		return "authentication_required"
	case http.StatusForbidden:
		return "request_rejected"
	case http.StatusNotFound:
		return "not_found"
	case http.StatusConflict:
		return "conflict"
	case http.StatusUnprocessableEntity:
		return "validation_failed"
	case http.StatusTooManyRequests:
		return "rate_limited"
	default:
		return "service_unavailable"
	}
}

func safeAPIMessage(code string) string {
	switch code {
	case "authentication_required":
		return "Autenticazione richiesta."
	case "request_rejected":
		return "Richiesta rifiutata."
	case "article_not_found":
		return "Articolo non trovato."
	case "article_validation", "invalid_json", "invalid_query", "invalid_status", "invalid_request", "validation_failed":
		return "Controlla i dati inviati."
	case "slug_taken":
		return "Questo indirizzo è già utilizzato da un altro articolo."
	case "article_conflict", "conflict":
		return "L’articolo è stato modificato. Ricarica i dati prima di riprovare."
	case "invalid_transition":
		return "Questa operazione non è disponibile nello stato corrente."
	case "commit_unknown":
		return "L’esito del salvataggio non è certo. Ricarica prima di decidere come procedere."
	case "request_too_large":
		return "La richiesta è troppo grande."
	case "unsupported_media_type":
		return "Il formato della richiesta non è supportato."
	case "if_match_required":
		return "Ricarica i dati prima di riprovare."
	case "not_found":
		return "Risorsa non trovata."
	case "rate_limited":
		return "Troppe richieste. Attendi prima di riprovare."
	default:
		return "Servizio temporaneamente non disponibile."
	}
}

func setPrivateAdminHeaders(response http.ResponseWriter) {
	response.Header().Set("Cache-Control", "no-store")
	response.Header().Set("X-Robots-Tag", "noindex, nofollow")
}

const loginPage = `<!doctype html>
<html lang="it"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1">
<title>Accesso amministrazione</title>{{if .Stylesheet}}<link rel="stylesheet" href="{{.Stylesheet}}">{{end}}{{if .Favicon}}<link rel="icon" type="image/svg+xml" href="{{.Favicon}}">{{end}}</head><body><main class="section shell reading-column"><h1>Accesso amministrazione</h1>
{{if .Error}}<p role="alert">{{.Error}}</p>{{end}}
<form method="post" action="/admin/login"><div class="contact-form">
<input type="hidden" name="started" value="{{.Token}}">
<label class="form-field">Nome utente <input name="username" autocomplete="username" maxlength="128" required></label>
<label class="form-field">Password <input type="password" name="password" autocomplete="current-password" maxlength="1024" required></label>
<button class="button button--primary" type="submit">Accedi</button></div></form></main></body></html>`
