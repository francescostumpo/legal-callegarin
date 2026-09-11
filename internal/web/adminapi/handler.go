package adminapi

import (
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

	"github.com/francescostumpo/legal-callegarin/internal/auth"
	webmiddleware "github.com/francescostumpo/legal-callegarin/internal/web/middleware"
)

const (
	loginBodyLimit    = 8 << 10
	loginTokenMaxAge  = 10 * time.Minute
	loginTokenSkew    = 5 * time.Second
	defaultLoginLimit = 5
	defaultMaxBuckets = 2048
)

type CredentialVerifier interface {
	Verify(username, password string) error
}

type SessionController interface {
	Rotate(context.Context, string) (string, auth.Session, error)
	Logout(context.Context, string) error
}

type Options struct {
	Credentials   CredentialVerifier
	Sessions      SessionController
	Assets        fs.FS
	SessionKey    []byte
	PublicBaseURL string
	Now           func() time.Time
	TrustedProxy  bool
	LoginCapacity int
	MaxBuckets    int
}

type handler struct {
	credentials    CredentialVerifier
	sessions       SessionController
	adminFiles     http.Handler
	index          []byte
	sessionKey     []byte
	formKey        []byte
	expectedOrigin string
	now            func() time.Time
	trustedProxy   bool
	limiter        *loginLimiter
	template       *template.Template
}

type loginView struct {
	Token string
	Error string
}

func New(options Options) (http.Handler, error) {
	if options.Credentials == nil || options.Sessions == nil || options.Assets == nil || len(options.SessionKey) < 32 {
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
	instance := &handler{
		credentials:    options.Credentials,
		sessions:       options.Sessions,
		adminFiles:     http.FileServer(http.FS(adminFS)),
		index:          index,
		sessionKey:     append([]byte(nil), options.SessionKey...),
		formKey:        deriveKey(options.SessionKey, "callegarin/admin-login-form/v1"),
		expectedOrigin: base.Scheme + "://" + base.Host,
		now:            options.Now,
		trustedProxy:   options.TrustedProxy,
		limiter:        newLoginLimiter(options.LoginCapacity, time.Minute, time.Hour, options.MaxBuckets),
		template:       loginTemplate,
	}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /admin/login", instance.loginGET)
	mux.HandleFunc("POST /admin/login", instance.loginPOST)
	mux.HandleFunc("GET /api/admin/session", instance.sessionGET)
	mux.HandleFunc("DELETE /api/admin/session", instance.sessionDELETE)
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
	if len(username) > 128 || len(password) == 0 || len(password) > 1024 || len(started) > 256 || !handler.validFormToken(started) {
		handler.renderLogin(response, http.StatusBadRequest, "Richiesta non valida")
		return
	}
	addressKey, err := loginAddressKey(request, handler.trustedProxy, handler.sessionKey)
	if err != nil {
		handler.renderLogin(response, http.StatusBadRequest, "Richiesta non valida")
		return
	}
	usernameKey := keyedValue(handler.sessionKey, "callegarin/admin-login-username/v1", normalizeUsername(username))
	now := handler.now().UTC()
	addressAllowed := handler.limiter.allowAddress(addressKey, now)
	usernameAllowed := handler.limiter.allowUsername(usernameKey, now)
	if !addressAllowed || !usernameAllowed {
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
	response.Header().Set("Cache-Control", "no-store")
	http.Redirect(response, request, "/admin", http.StatusSeeOther)
}

func (handler *handler) sessionGET(response http.ResponseWriter, request *http.Request) {
	principal, ok := webmiddleware.PrincipalFromContext(request.Context())
	if !ok {
		writeJSONError(response, http.StatusUnauthorized, "authentication required")
		return
	}
	response.Header().Set("Cache-Control", "no-store")
	response.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(response).Encode(map[string]string{
		"username":  principal.Username,
		"csrfToken": webmiddleware.CSRFToken(principal.Token, handler.sessionKey),
	})
}

func (handler *handler) sessionDELETE(response http.ResponseWriter, request *http.Request) {
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
	response.Header().Set("Cache-Control", "no-store")
	response.WriteHeader(http.StatusNoContent)
}

func (handler *handler) indexGET(response http.ResponseWriter, _ *http.Request) {
	serveAdminIndex(response, handler.index)
}

func (handler *handler) adminGET(response http.ResponseWriter, request *http.Request) {
	response.Header().Set("Cache-Control", "no-store")
	response.Header().Set("X-Robots-Tag", "noindex, nofollow")
	if strings.HasPrefix(request.URL.Path, "/admin/assets/") {
		clone := request.Clone(request.Context())
		clone.URL.Path = strings.TrimPrefix(request.URL.Path, "/admin/")
		handler.adminFiles.ServeHTTP(response, clone)
		return
	}
	serveAdminIndex(response, handler.index)
}

func (handler *handler) renderLogin(response http.ResponseWriter, status int, message string) {
	response.Header().Set("Cache-Control", "no-store")
	response.Header().Set("X-Robots-Tag", "noindex, nofollow")
	response.Header().Set("Content-Type", "text/html; charset=utf-8")
	response.WriteHeader(status)
	_ = handler.template.Execute(response, loginView{Token: handler.newFormToken(), Error: message})
}

func serveAdminIndex(response http.ResponseWriter, index []byte) {
	response.Header().Set("Cache-Control", "no-store")
	response.Header().Set("X-Robots-Tag", "noindex, nofollow")
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
	response.Header().Set("Cache-Control", "no-store")
	response.Header().Set("Content-Type", "application/json; charset=utf-8")
	response.WriteHeader(status)
	_ = json.NewEncoder(response).Encode(map[string]string{"error": message})
}

const loginPage = `<!doctype html>
<html lang="it"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1">
<title>Accesso amministrazione</title></head><body><main><h1>Accesso amministrazione</h1>
{{if .Error}}<p role="alert">{{.Error}}</p>{{end}}
<form method="post" action="/admin/login">
<input type="hidden" name="started" value="{{.Token}}">
<label>Nome utente <input name="username" autocomplete="username" maxlength="128" required></label>
<label>Password <input type="password" name="password" autocomplete="current-password" maxlength="1024" required></label>
<button type="submit">Accedi</button></form></main></body></html>`
