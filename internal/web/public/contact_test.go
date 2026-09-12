package public

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/francescostumpo/legal-callegarin/internal/contacts"
	"github.com/francescostumpo/legal-callegarin/internal/storage/memory"
	"github.com/francescostumpo/legal-callegarin/internal/webassets"
)

var contactTestKey = []byte("0123456789abcdef0123456789abcdef")

func TestContactFormValidSubmissionPersistsAndUsesCookieFreePRG(t *testing.T) {
	t.Parallel()

	clock := &contactTestClock{now: time.Date(2026, 9, 11, 12, 0, 0, 0, time.UTC)}
	repository := memory.NewContactRepository(clock.Now)
	service := contacts.NewService(repository, clock, &contactTestIDs{})
	handler := newContactTestHandler(t, service, clock, nil, false)

	form := validContactForm(t, clock.now)
	clock.now = clock.now.Add(contactMinimumCompletionTime)
	response := submitContactForm(handler, form, "studio.example.test", "https://studio.example.test", "192.0.2.45:4000")
	if response.Code != http.StatusSeeOther {
		t.Fatalf("POST status = %d, want 303; body=%q", response.Code, response.Body.String())
	}
	if cookie := response.Header().Get("Set-Cookie"); cookie != "" {
		t.Fatalf("POST Set-Cookie = %q, want empty", cookie)
	}
	location := response.Header().Get("Location")
	if !strings.HasPrefix(location, "/contatti?esito=") {
		t.Fatalf("Location = %q, want signed success query", location)
	}

	page, err := repository.List(context.Background(), contacts.ListOptions{})
	if err != nil || len(page.Items) != 1 {
		t.Fatalf("stored contacts = %#v, error = %v", page, err)
	}
	stored := page.Items[0]
	if stored.Name != "Mario Rossi" || stored.Email != "mario@example.test" || stored.Phone != "+39 000 000000" || stored.Message != "Una richiesta sufficientemente dettagliata." {
		t.Fatalf("stored visible fields = %#v", stored)
	}
	if stored.ConsentVersion != contactConsentVersion || !stored.PrivacyAcceptedAt.Equal(clock.now) || stored.State != contacts.StateNew || !stored.CreatedAt.Equal(clock.now) || !stored.UpdatedAt.Equal(clock.now) || !stored.ReviewDueAt.Equal(clock.now.AddDate(2, 0, 0)) {
		t.Fatalf("stored server fields = %#v", stored)
	}

	get := httptest.NewRecorder()
	handler.ServeHTTP(get, httptest.NewRequest(http.MethodGet, "https://studio.example.test"+location, nil))
	if get.Code != http.StatusOK || !strings.Contains(get.Body.String(), "Richiesta ricevuta") {
		t.Fatalf("PRG GET = status %d, body %q", get.Code, get.Body.String())
	}
	if get.Header().Get("Set-Cookie") != "" {
		t.Fatalf("PRG GET set cookie %q", get.Header().Get("Set-Cookie"))
	}
	handler.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "https://studio.example.test"+location, nil))
	page, err = repository.List(context.Background(), contacts.ListOptions{})
	if err != nil || len(page.Items) != 1 {
		t.Fatalf("refresh duplicated contact: %#v, %v", page, err)
	}
}

func TestContactFormValidationIsAccessibleAndPreservesOnlyVisibleEscapedInput(t *testing.T) {
	t.Parallel()

	clock := &contactTestClock{now: time.Date(2026, 9, 11, 12, 0, 10, 0, time.UTC)}
	service := &contactRecordingService{}
	handler := newContactTestHandler(t, service, clock, nil, false)
	oldToken := contactFormToken(t, clock.now.Add(-contactMinimumCompletionTime))
	form := validContactForm(t, clock.now.Add(-contactMinimumCompletionTime))
	form.Set("name", `<script>alert("name-secret")</script>`)
	form.Set("email", "indirizzo-non-valido")
	form.Del("privacy")
	form.Set("started", oldToken)
	form.Set("website", "honeypot-secret")

	// A honeypot hit is intentionally generic and must not echo any input.
	honeypot := submitContactForm(handler, form, "studio.example.test", "https://studio.example.test", "192.0.2.46:4000")
	if honeypot.Code != http.StatusBadRequest || strings.Contains(honeypot.Body.String(), "name-secret") || strings.Contains(honeypot.Body.String(), "honeypot-secret") || strings.Contains(honeypot.Body.String(), oldToken) {
		t.Fatalf("honeypot response leaked input: status %d body %q", honeypot.Code, honeypot.Body.String())
	}

	form.Set("website", "")
	response := submitContactForm(handler, form, "studio.example.test", "https://studio.example.test", "192.0.2.47:4000")
	body := response.Body.String()
	if response.Code != http.StatusUnprocessableEntity {
		t.Fatalf("validation status = %d, want 422; body=%q", response.Code, body)
	}
	for _, fragment := range []string{
		`id="error-summary"`, `role="alert"`, `tabindex="-1"`,
		`href="#email"`, `href="#privacy"`,
		`aria-invalid="true"`, `aria-describedby="email-error"`,
		`&lt;script&gt;alert`, `value="indirizzo-non-valido"`,
	} {
		if !strings.Contains(body, fragment) {
			t.Errorf("validation response lacks %q", fragment)
		}
	}
	if strings.Contains(body, `<script>alert`) || strings.Contains(body, oldToken) || strings.Contains(body, "honeypot-secret") {
		t.Fatalf("validation response contains unsafe or hidden prior value: %q", body)
	}
	if service.calls != 0 {
		t.Fatalf("Submit calls = %d, want 0", service.calls)
	}
	assertPrivateErrorHeaders(t, response)
}

func TestContactFormRejectsEveryVisibleFieldLimitConsentAndEmail(t *testing.T) {
	t.Parallel()

	clock := &contactTestClock{now: time.Date(2026, 9, 11, 12, 0, 10, 0, time.UTC)}
	tests := []struct {
		name  string
		field string
		value string
	}{
		{name: "name too short", field: "name", value: "A"},
		{name: "name too long", field: "name", value: strings.Repeat("è", 121)},
		{name: "email malformed", field: "email", value: "not-an-email"},
		{name: "email too long", field: "email", value: strings.Repeat("a", 250) + "@x.it"},
		{name: "phone too long", field: "phone", value: strings.Repeat("1", 41)},
		{name: "message too short", field: "message", value: strings.Repeat("a", 19)},
		{name: "message too long", field: "message", value: strings.Repeat("è", 2001)},
		{name: "consent absent", field: "privacy", value: ""},
		{name: "consent forged", field: "privacy", value: "yes"},
	}
	for index, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			service := &contactRecordingService{}
			handler := newContactTestHandler(t, service, clock, nil, false)
			form := validContactForm(t, clock.now.Add(-contactMinimumCompletionTime))
			if tt.value == "" {
				form.Del(tt.field)
			} else {
				form.Set(tt.field, tt.value)
			}
			response := submitContactForm(handler, form, "studio.example.test", "https://studio.example.test", fmt.Sprintf("192.0.2.%d:4000", 60+index))
			if response.Code != http.StatusUnprocessableEntity {
				t.Fatalf("status = %d, want 422; body=%q", response.Code, response.Body.String())
			}
			if service.calls != 0 {
				t.Fatalf("Submit calls = %d, want 0", service.calls)
			}
			assertPrivateErrorHeaders(t, response)
		})
	}
}

func TestContactFormRejectsTimestampAbuseCrossOriginAndOversizedBody(t *testing.T) {
	t.Parallel()

	clock := &contactTestClock{now: time.Date(2026, 9, 11, 12, 0, 10, 0, time.UTC)}
	tests := []struct {
		name   string
		formAt time.Time
		mutate func(url.Values)
		host   string
		origin string
		status int
	}{
		{name: "too fast", formAt: clock.now, status: http.StatusBadRequest},
		{name: "expired", formAt: clock.now.Add(-contactFormTokenMaxAge - time.Second), status: http.StatusBadRequest},
		{name: "future", formAt: clock.now.Add(contactTokenFutureSkew + time.Second), status: http.StatusBadRequest},
		{name: "bad signature", formAt: clock.now.Add(-contactMinimumCompletionTime), mutate: func(form url.Values) { form.Set("started", mutateLastTokenByte(form.Get("started"))) }, status: http.StatusBadRequest},
		{name: "cross origin", formAt: clock.now.Add(-contactMinimumCompletionTime), origin: "https://evil.example", status: http.StatusForbidden},
		{name: "wrong host", formAt: clock.now.Add(-contactMinimumCompletionTime), host: "evil.example", status: http.StatusForbidden},
	}
	for index, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			service := &contactRecordingService{}
			handler := newContactTestHandler(t, service, clock, nil, false)
			form := validContactForm(t, tc.formAt)
			if tc.mutate != nil {
				tc.mutate(form)
			}
			host, origin := tc.host, tc.origin
			if host == "" {
				host = "studio.example.test"
			}
			if origin == "" {
				origin = "https://studio.example.test"
			}
			response := submitContactForm(handler, form, host, origin, fmt.Sprintf("198.51.100.%d:4000", 10+index))
			if response.Code != tc.status {
				t.Fatalf("status = %d, want %d; body=%q", response.Code, tc.status, response.Body.String())
			}
			if service.calls != 0 {
				t.Fatalf("Submit calls = %d, want 0", service.calls)
			}
			assertPrivateErrorHeaders(t, response)
		})
	}

	service := &contactRecordingService{}
	handler := newContactTestHandler(t, service, clock, nil, false)
	body := strings.Repeat("x", contactMaxBodyBytes+1)
	request := httptest.NewRequest(http.MethodPost, "https://studio.example.test/contatti", strings.NewReader(body))
	request.Host = "studio.example.test"
	request.RemoteAddr = "203.0.113.90:4000"
	request.Header.Set("Origin", "https://studio.example.test")
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusRequestEntityTooLarge || service.calls != 0 {
		t.Fatalf("oversized response = status %d calls %d body %q", response.Code, service.calls, response.Body.String())
	}
	assertPrivateErrorHeaders(t, response)
}

func TestContactFormRateLimitExhaustionAndRecovery(t *testing.T) {
	t.Parallel()

	clock := &contactTestClock{now: time.Date(2026, 9, 11, 12, 0, 10, 0, time.UTC)}
	service := &contactRecordingService{}
	handler := newContactTestHandler(t, service, clock, nil, false)
	for index := 0; index < contactRateCapacity; index++ {
		form := validContactForm(t, clock.now.Add(-contactMinimumCompletionTime))
		form.Del("privacy")
		response := submitContactForm(handler, form, "studio.example.test", "https://studio.example.test", "203.0.113.44:4000")
		if response.Code != http.StatusUnprocessableEntity {
			t.Fatalf("attempt %d status = %d, want 422", index+1, response.Code)
		}
	}
	blocked := submitContactForm(handler, validContactForm(t, clock.now.Add(-contactMinimumCompletionTime)), "studio.example.test", "https://studio.example.test", "203.0.113.44:4000")
	if blocked.Code != http.StatusTooManyRequests {
		t.Fatalf("exhausted status = %d, want 429", blocked.Code)
	}
	assertPrivateErrorHeaders(t, blocked)
	clock.now = clock.now.Add(contactRateRefillInterval)
	recovered := submitContactForm(handler, validContactForm(t, clock.now.Add(-contactMinimumCompletionTime)), "studio.example.test", "https://studio.example.test", "203.0.113.44:4000")
	if recovered.Code != http.StatusSeeOther {
		t.Fatalf("recovered status = %d, want 303; body=%q", recovered.Code, recovered.Body.String())
	}
}

func TestContactFormStorageFailureAndPanicDoNotLeakOrClaimSuccess(t *testing.T) {
	t.Parallel()

	secrets := []string{"Nome Segretissimo", "persona-segreta@example.test", "+39 123 SECRET", "messaggio segretissimo abbastanza lungo"}
	for _, mode := range []string{"error", "panic"} {
		t.Run(mode, func(t *testing.T) {
			clock := &contactTestClock{now: time.Date(2026, 9, 11, 12, 0, 10, 0, time.UTC)}
			var logs bytes.Buffer
			logger := slog.New(slog.NewJSONHandler(&logs, nil))
			service := &contactRecordingService{}
			if mode == "error" {
				service.err = errors.New(strings.Join(secrets, " "))
			} else {
				service.panicValue = strings.Join(secrets, " ")
			}
			handler := newContactTestHandler(t, service, clock, logger, false)
			form := validContactForm(t, clock.now.Add(-contactMinimumCompletionTime))
			form.Set("name", secrets[0])
			form.Set("email", secrets[1])
			form.Set("phone", secrets[2])
			form.Set("message", secrets[3])
			response := submitContactForm(handler, form, "studio.example.test", "https://studio.example.test", "203.0.113.110:4000")
			if response.Code != http.StatusServiceUnavailable || response.Header().Get("Location") != "" || strings.Contains(response.Body.String(), "Richiesta ricevuta") {
				t.Fatalf("storage failure response = status %d Location %q body %q", response.Code, response.Header().Get("Location"), response.Body.String())
			}
			for _, fragment := range []string{"Riprova", "DATO DA CONFERMARE"} {
				if !strings.Contains(response.Body.String(), fragment) {
					t.Errorf("storage failure body lacks %q", fragment)
				}
			}
			for _, secret := range secrets {
				if strings.Contains(logs.String(), secret) {
					t.Fatalf("logs leaked %q: %s", secret, logs.String())
				}
				if strings.Contains(response.Body.String(), secret) {
					t.Fatalf("error response leaked %q", secret)
				}
			}
			if strings.Contains(logs.String(), "203.0.113.110") {
				t.Fatalf("logs contain raw client IP: %s", logs.String())
			}
			assertPrivateErrorHeaders(t, response)
		})
	}
}

func TestContactJSONNegotiationKeepsPIIOffFailureResponsesAndReturnsSafeFields(t *testing.T) {
	clock := &contactTestClock{now: time.Date(2026, 9, 11, 12, 0, 10, 0, time.UTC)}
	service := &contactRecordingService{err: errors.New("storage contains persona-segreta@example.test")}
	handler := newContactTestHandler(t, service, clock, nil, false)
	form := validContactForm(t, clock.now.Add(-contactMinimumCompletionTime))
	form.Set("name", "Nome Segreto")
	form.Set("email", "persona-segreta@example.test")
	response := submitContactJSON(handler, form, "192.0.2.80:1234")
	if response.Code != http.StatusServiceUnavailable || response.Header().Get("Content-Type") != "application/json; charset=utf-8" {
		t.Fatalf("storage JSON = status %d headers %#v body %q", response.Code, response.Header(), response.Body.String())
	}
	if !strings.Contains(response.Body.String(), `"code":"contact_unavailable"`) || !strings.Contains(response.Body.String(), `"fields":{}`) || strings.Contains(response.Body.String(), "Nome Segreto") || strings.Contains(response.Body.String(), "persona-segreta@example.test") {
		t.Fatalf("storage JSON body = %q", response.Body.String())
	}

	service.err = fmt.Errorf("%w: persona-segreta@example.test", contacts.ErrCommitUnknown)
	response = submitContactJSON(handler, form, "192.0.2.83:1234")
	if response.Code != http.StatusConflict || !strings.Contains(response.Body.String(), `"code":"contact_commit_unknown"`) || strings.Contains(response.Body.String(), "persona-segreta@example.test") {
		t.Fatalf("unknown-commit JSON = status %d body %q", response.Code, response.Body.String())
	}

	service.err = nil
	invalid := validContactForm(t, clock.now.Add(-contactMinimumCompletionTime))
	invalid.Set("email", "invalid")
	response = submitContactJSON(handler, invalid, "192.0.2.81:1234")
	if response.Code != http.StatusUnprocessableEntity || !strings.Contains(response.Body.String(), `"code":"contact_validation"`) || !strings.Contains(response.Body.String(), `"email":"Inserisci un indirizzo email valido."`) || strings.Contains(response.Body.String(), `"name":"Mario Rossi"`) {
		t.Fatalf("validation JSON = status %d body %q", response.Code, response.Body.String())
	}

	response = submitContactJSON(handler, validContactForm(t, clock.now.Add(-contactMinimumCompletionTime)), "192.0.2.82:1234")
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"redirect":"/contatti?esito=`) || response.Header().Get("Set-Cookie") != "" {
		t.Fatalf("success JSON = status %d headers %#v body %q", response.Code, response.Header(), response.Body.String())
	}
}

func TestContactJSONRateLimitUsesSafeNestedError(t *testing.T) {
	clock := &contactTestClock{now: time.Date(2026, 9, 11, 12, 0, 10, 0, time.UTC)}
	handler := newContactTestHandler(t, &contactRecordingService{}, clock, nil, false)
	var response *httptest.ResponseRecorder
	for attempt := 0; attempt <= contactRateCapacity; attempt++ {
		response = submitContactJSON(handler, validContactForm(t, clock.now.Add(-contactMinimumCompletionTime)), "192.0.2.90:1234")
	}
	if response.Code != http.StatusTooManyRequests || !strings.Contains(response.Body.String(), `"code":"contact_rate_limited"`) || !strings.Contains(response.Body.String(), `"fields":{}`) {
		t.Fatalf("rate-limit JSON = status %d body %q", response.Code, response.Body.String())
	}
}

func TestContactFormUnknownCommitWarnsAgainstDuplicateSubmissionWithoutLeakingData(t *testing.T) {
	t.Parallel()

	secrets := []string{"Nome Incerto", "incerto@example.test", "+39 555 SEGRETO", "messaggio incerto sufficientemente lungo"}
	clock := &contactTestClock{now: time.Date(2026, 9, 11, 12, 0, 10, 0, time.UTC)}
	var logs bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&logs, nil))
	service := &contactRecordingService{err: fmt.Errorf("%w: %s", contacts.ErrCommitUnknown, strings.Join(secrets, " "))}
	handler := newContactTestHandler(t, service, clock, logger, false)
	form := validContactForm(t, clock.now.Add(-contactMinimumCompletionTime))
	form.Set("name", secrets[0])
	form.Set("email", secrets[1])
	form.Set("phone", secrets[2])
	form.Set("message", secrets[3])
	response := submitContactForm(handler, form, "studio.example.test", "https://studio.example.test", "203.0.113.111:4000")
	body := response.Body.String()

	if response.Code != http.StatusServiceUnavailable || response.Header().Get("Location") != "" || response.Header().Get("Set-Cookie") != "" {
		t.Fatalf("unknown commit response = status %d Location %q Set-Cookie %q", response.Code, response.Header().Get("Location"), response.Header().Get("Set-Cookie"))
	}
	for _, fragment := range []string{"Non è possibile confermare la ricezione", "non inviare di nuovo", "recapiti diretti", "DATO DA CONFERMARE"} {
		if !strings.Contains(body, fragment) {
			t.Errorf("unknown commit body lacks %q", fragment)
		}
	}
	if strings.Contains(strings.ToLower(body), "riprova") || strings.Contains(body, "<form") || strings.Contains(body, "Richiesta ricevuta") {
		t.Fatalf("unknown commit response offers an unsafe retry/success path: %q", body)
	}
	for _, secret := range secrets {
		if strings.Contains(body, secret) || strings.Contains(logs.String(), secret) {
			t.Fatalf("unknown commit path leaked %q", secret)
		}
	}
	if !strings.Contains(logs.String(), `"outcome":"commit_unknown"`) || strings.Contains(logs.String(), "203.0.113.111") {
		t.Fatalf("unknown commit log = %s", logs.String())
	}
	assertPrivateErrorHeaders(t, response)
}

func TestContactFormSignedSuccessInvalidExpiredAndPrivacyRetentionCopy(t *testing.T) {
	t.Parallel()

	clock := &contactTestClock{now: time.Date(2026, 9, 11, 12, 0, 10, 0, time.UTC)}
	handler := newContactTestHandler(t, &contactRecordingService{}, clock, nil, false)
	signer, err := newContactSigner(contactTestKey)
	if err != nil {
		t.Fatalf("newContactSigner() error = %v", err)
	}
	tests := []struct {
		name  string
		token string
		want  bool
	}{
		{name: "valid", token: signer.newSuccessToken(clock.now), want: true},
		{name: "invalid", token: "invalid"},
		{name: "expired", token: signer.newSuccessToken(clock.now.Add(-contactSuccessTokenMaxAge - time.Second))},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "https://studio.example.test/contatti?esito="+url.QueryEscape(tt.token), nil))
			if got := strings.Contains(response.Body.String(), "Richiesta ricevuta"); got != tt.want {
				t.Fatalf("success visible = %t, want %t", got, tt.want)
			}
		})
	}

	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "https://studio.example.test/contatti", nil))
	body := response.Body.String()
	normalizedBody := strings.Join(strings.Fields(body), " ")
	for _, copy := range []string{
		"non costituisce conferimento di incarico",
		"non inviare documenti",
		"dati sensibili non necessari",
		"Titolare del trattamento",
		"finalità",
		"base giuridica",
		"destinatari e responsabili",
		"Azure nell’Unione europea (regione Italy North)",
		"accesso limitato",
		"24 mesi",
		"non comporta cancellazione automatica",
		"30 giorni",
		"diritti",
		"DATO DA CONFERMARE",
		"DA VALIDARE CON IL PROFESSIONISTA",
		`href="/privacy-cookie-policy"`,
	} {
		if !strings.Contains(normalizedBody, copy) {
			t.Errorf("contact/privacy copy lacks %q", copy)
		}
	}
	if strings.Contains(strings.ToLower(body), "cookie banner") || strings.Contains(strings.ToLower(body), "accetta i cookie") {
		t.Fatal("contact page contains a cookie banner")
	}
	if response.Header().Get("Set-Cookie") != "" {
		t.Fatalf("GET Set-Cookie = %q", response.Header().Get("Set-Cookie"))
	}
}

func validContactForm(t *testing.T, issued time.Time) url.Values {
	t.Helper()
	return url.Values{
		"name":    {" Mario Rossi "},
		"email":   {" mario@example.test "},
		"phone":   {" +39 000 000000 "},
		"message": {" Una richiesta sufficientemente dettagliata. "},
		"privacy": {"accepted"},
		"website": {""},
		"started": {contactFormToken(t, issued)},
	}
}

func contactFormToken(t *testing.T, issued time.Time) string {
	t.Helper()
	signer, err := newContactSigner(contactTestKey)
	if err != nil {
		t.Fatalf("newContactSigner() error = %v", err)
	}
	return signer.newFormToken(issued)
}

func newContactTestHandler(t *testing.T, service contacts.ContactService, clock *contactTestClock, logger *slog.Logger, trustedProxy bool) http.Handler {
	t.Helper()
	options := []RendererOption{
		WithContactService(service, contactTestKey),
		WithContactClock(clock.Now),
		WithContactTrustedProxyHops(map[bool]int{false: 0, true: 1}[trustedProxy]),
	}
	if logger != nil {
		options = append(options, WithContactLogger(logger))
	}
	renderer, err := NewRenderer(webassets.Files, "https://studio.example.test", options...)
	if err != nil {
		t.Fatalf("NewRenderer() error = %v", err)
	}
	mux := http.NewServeMux()
	if err := RegisterRoutes(mux, renderer, webassets.Files); err != nil {
		t.Fatalf("RegisterRoutes() error = %v", err)
	}
	return mux
}

func submitContactForm(handler http.Handler, form url.Values, host, origin, remoteAddress string) *httptest.ResponseRecorder {
	request := httptest.NewRequest(http.MethodPost, "https://"+host+"/contatti", strings.NewReader(form.Encode()))
	request.Host = host
	request.RemoteAddr = remoteAddress
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	request.Header.Set("Origin", origin)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	return response
}

func submitContactJSON(handler http.Handler, form url.Values, remoteAddress string) *httptest.ResponseRecorder {
	request := httptest.NewRequest(http.MethodPost, "https://studio.example.test/contatti", strings.NewReader(form.Encode()))
	request.RemoteAddr = remoteAddress
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	request.Header.Set("Origin", "https://studio.example.test")
	request.Header.Set("Accept", "application/json")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	return response
}

func assertPrivateErrorHeaders(t *testing.T, response *httptest.ResponseRecorder) {
	t.Helper()
	if got := response.Header().Get("Cache-Control"); got != "no-store" {
		t.Errorf("Cache-Control = %q, want no-store", got)
	}
	if got := response.Header().Get("X-Robots-Tag"); got != "noindex, nofollow" {
		t.Errorf("X-Robots-Tag = %q, want noindex, nofollow", got)
	}
}

type contactTestClock struct {
	now time.Time
}

func (clock *contactTestClock) Now() time.Time { return clock.now }

type contactTestIDs struct{ next int }

func (ids *contactTestIDs) NewID() string {
	ids.next++
	return fmt.Sprintf("contact-%d", ids.next)
}

type contactRecordingService struct {
	contacts.ContactService
	calls      int
	submission contacts.Submission
	err        error
	panicValue any
}

func (service *contactRecordingService) Submit(_ context.Context, submission contacts.Submission) (contacts.Contact, error) {
	service.calls++
	service.submission = submission
	if service.panicValue != nil {
		panic(service.panicValue)
	}
	return contacts.Contact{}, service.err
}
