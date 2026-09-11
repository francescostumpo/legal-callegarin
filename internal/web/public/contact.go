package public

import (
	"bytes"
	"context"
	"errors"
	"io"
	"log/slog"
	"mime"
	"net/http"
	"net/mail"
	"net/url"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/francescostumpo/legal-callegarin/internal/contacts"
)

const (
	contactConsentVersion     = "privacy-v1-2026-09-11"
	contactMaxBodyBytes       = 16 << 10
	contactRateCapacity       = 5
	contactRateRefillInterval = time.Minute
	contactRateIdleExpiry     = 15 * time.Minute
	contactRateMaxEntries     = 2048
)

var errContactServicePanic = errors.New("contact service panic")

type contactHandler struct {
	renderer     *Renderer
	service      contacts.ContactService
	signer       *contactSigner
	now          func() time.Time
	logger       *slog.Logger
	trustedProxy bool
	limiter      *contactRateLimiter
}

type contactPageData struct {
	PageData
	Form           contactFormValues
	ErrorSummary   []contactFieldError
	NameError      string
	EmailError     string
	PhoneError     string
	MessageError   string
	PrivacyError   string
	FormToken      string
	Success        bool
	StorageFailure bool
	GlobalError    string
}

type contactFormValues struct {
	Name    string
	Email   string
	Phone   string
	Message string
	Privacy bool
}

type contactFieldError struct {
	Field   string
	Message string
}

func newContactHandler(renderer *Renderer, service contacts.ContactService, signer *contactSigner, now func() time.Time, logger *slog.Logger, trustedProxy bool) *contactHandler {
	if logger == nil {
		logger = slog.New(slog.NewTextHandler(io.Discard, nil))
	}
	return &contactHandler{
		renderer: renderer, service: service, signer: signer, now: now, logger: logger, trustedProxy: trustedProxy,
		limiter: newContactRateLimiter(contactRateCapacity, contactRateRefillInterval, contactRateIdleExpiry, contactRateMaxEntries),
	}
}

func (handler *contactHandler) get(response http.ResponseWriter, request *http.Request) {
	now := handler.now()
	data := handler.pageData(contactFormValues{})
	data.FormToken = handler.signer.newFormToken(now)
	data.Success = handler.signer.validSuccessToken(request.URL.Query().Get("esito"), now)
	handler.writePage(response, http.StatusOK, data)
}

func (handler *contactHandler) post(response http.ResponseWriter, request *http.Request) {
	if !handler.sameOrigin(request) {
		handler.writeSecurityError(response, http.StatusForbidden, "La richiesta non può essere verificata. Ricarica la pagina e riprova.")
		return
	}
	clientKey, err := handler.signer.clientKey(request, handler.trustedProxy)
	if err != nil {
		handler.writeSecurityError(response, http.StatusBadRequest, "La richiesta non può essere verificata. Ricarica la pagina e riprova.")
		return
	}
	if !handler.limiter.allow(clientKey, handler.now()) {
		handler.writeSecurityError(response, http.StatusTooManyRequests, "Sono state inviate troppe richieste. Attendi un minuto e riprova.")
		return
	}

	request.Body = http.MaxBytesReader(response, request.Body, contactMaxBodyBytes)
	mediaType, _, contentTypeErr := mime.ParseMediaType(request.Header.Get("Content-Type"))
	if contentTypeErr != nil || mediaType != "application/x-www-form-urlencoded" {
		handler.writeSecurityError(response, http.StatusBadRequest, "Formato della richiesta non valido. Ricarica la pagina e riprova.")
		return
	}
	if err := request.ParseForm(); err != nil {
		var maxBytesError *http.MaxBytesError
		if errors.As(err, &maxBytesError) {
			handler.writeSecurityError(response, http.StatusRequestEntityTooLarge, "La richiesta è troppo grande. Riduci il messaggio e riprova.")
			return
		}
		handler.writeSecurityError(response, http.StatusBadRequest, "Formato della richiesta non valido. Ricarica la pagina e riprova.")
		return
	}

	values, validShape := contactValues(request.PostForm)
	if !validShape {
		handler.writeSecurityError(response, http.StatusBadRequest, "Formato della richiesta non valido. Ricarica la pagina e riprova.")
		return
	}
	if request.PostForm.Get("website") != "" {
		handler.writeSecurityError(response, http.StatusBadRequest, "La richiesta non può essere verificata. Ricarica la pagina e riprova.")
		return
	}
	if !handler.signer.validFormToken(request.PostForm.Get("started"), handler.now()) {
		handler.writeSecurityError(response, http.StatusBadRequest, "La richiesta è scaduta o non valida. Ricarica la pagina e riprova.")
		return
	}

	fieldErrors := validateContactValues(values)
	if len(fieldErrors) != 0 {
		data := handler.pageData(values)
		data.ErrorSummary = orderedContactErrors(fieldErrors)
		data.NameError = fieldErrors["name"]
		data.EmailError = fieldErrors["email"]
		data.PhoneError = fieldErrors["phone"]
		data.MessageError = fieldErrors["message"]
		data.PrivacyError = fieldErrors["privacy"]
		data.FormToken = handler.signer.newFormToken(handler.now())
		handler.writePage(response, http.StatusUnprocessableEntity, data)
		return
	}

	submission := contacts.Submission{
		Name: values.Name, Email: values.Email, Phone: values.Phone, Message: values.Message,
		ConsentVersion: contactConsentVersion,
	}
	_, err = safelySubmitContact(request.Context(), handler.service, submission)
	if err != nil {
		handler.logger.Error("contact submission unavailable", "event", "contact_submit", "outcome", "storage_error")
		data := handler.pageData(contactFormValues{})
		data.StorageFailure = true
		data.FormToken = handler.signer.newFormToken(handler.now())
		handler.writePage(response, http.StatusServiceUnavailable, data)
		return
	}
	handler.logger.Info("contact submission accepted", "event", "contact_submit", "outcome", "accepted")
	success := handler.signer.newSuccessToken(handler.now())
	http.Redirect(response, request, "/contatti?esito="+url.QueryEscape(success), http.StatusSeeOther)
}

func safelySubmitContact(ctx context.Context, service contacts.ContactService, submission contacts.Submission) (contact contacts.Contact, err error) {
	defer func() {
		if recover() != nil {
			contact = contacts.Contact{}
			err = errContactServicePanic
		}
	}()
	return service.Submit(ctx, submission)
}

func (handler *contactHandler) sameOrigin(request *http.Request) bool {
	base, err := url.Parse(handler.renderer.baseURL)
	if err != nil || request.Host != base.Host {
		return false
	}
	origin, err := url.Parse(request.Header.Get("Origin"))
	return err == nil && origin.Scheme == base.Scheme && origin.Host == base.Host && origin.User == nil && origin.Path == "" && origin.RawQuery == "" && origin.Fragment == ""
}

func contactValues(form url.Values) (contactFormValues, bool) {
	allowed := map[string]bool{"name": true, "email": true, "phone": true, "message": true, "privacy": true, "website": true, "started": true}
	for key, values := range form {
		if !allowed[key] || len(values) != 1 {
			return contactFormValues{}, false
		}
	}
	for _, required := range []string{"name", "email", "phone", "message", "website", "started"} {
		if _, exists := form[required]; !exists {
			return contactFormValues{}, false
		}
	}
	return contactFormValues{
		Name: form.Get("name"), Email: form.Get("email"), Phone: form.Get("phone"), Message: form.Get("message"),
		Privacy: form.Get("privacy") == "accepted",
	}, true
}

func validateContactValues(values contactFormValues) map[string]string {
	errorsByField := make(map[string]string)
	name := strings.TrimSpace(values.Name)
	if length := utf8.RuneCountInString(name); length < 2 || length > 120 {
		errorsByField["name"] = "Inserisci un nome tra 2 e 120 caratteri."
	}
	email := strings.TrimSpace(values.Email)
	parsedEmail, err := mail.ParseAddress(email)
	if len(email) > 254 || err != nil || parsedEmail.Address != email {
		errorsByField["email"] = "Inserisci un indirizzo email valido."
	}
	if utf8.RuneCountInString(strings.TrimSpace(values.Phone)) > 40 {
		errorsByField["phone"] = "Il telefono può contenere al massimo 40 caratteri."
	}
	if length := utf8.RuneCountInString(strings.TrimSpace(values.Message)); length < 20 || length > 2000 {
		errorsByField["message"] = "Inserisci un messaggio tra 20 e 2000 caratteri."
	}
	if !values.Privacy {
		errorsByField["privacy"] = "Conferma di aver letto l’informativa privacy."
	}
	return errorsByField
}

func orderedContactErrors(errorsByField map[string]string) []contactFieldError {
	ordered := make([]contactFieldError, 0, len(errorsByField))
	for _, field := range []string{"name", "email", "phone", "message", "privacy"} {
		if message := errorsByField[field]; message != "" {
			ordered = append(ordered, contactFieldError{Field: field, Message: message})
		}
	}
	return ordered
}

func (handler *contactHandler) pageData(values contactFormValues) contactPageData {
	page := handler.renderer.pages["/contatti"]
	page.CanonicalURL = handler.renderer.baseURL + page.Path
	handler.renderer.applyPageSEO(&page, "website", page.HeroImage, nil)
	return contactPageData{PageData: page, Form: values}
}

func (handler *contactHandler) writeSecurityError(response http.ResponseWriter, status int, message string) {
	data := handler.pageData(contactFormValues{})
	data.GlobalError = message
	data.FormToken = handler.signer.newFormToken(handler.now())
	handler.writePage(response, status, data)
}

func (handler *contactHandler) writePage(response http.ResponseWriter, status int, data contactPageData) {
	var output bytes.Buffer
	if err := handler.renderer.contact.ExecuteTemplate(&output, "base", data); err != nil {
		writePublicError(response, http.StatusServiceUnavailable, "content temporarily unavailable")
		return
	}
	response.Header().Set("Cache-Control", "no-store")
	if status >= http.StatusBadRequest {
		response.Header().Set("X-Robots-Tag", "noindex, nofollow")
	}
	response.Header().Set("Content-Type", "text/html; charset=utf-8")
	response.WriteHeader(status)
	_, _ = response.Write(output.Bytes())
}
