package adminapi

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/francescostumpo/legal-callegarin/internal/contacts"
)

type dashboardDTO struct {
	New               int `json:"new"`
	Read              int `json:"read"`
	Archived          int `json:"archived"`
	DeletionScheduled int `json:"deletionScheduled"`
	RetentionReview   int `json:"retentionReview"`
	Purged            int `json:"purged"`
}

type purgeDTO struct {
	Purged int `json:"purged"`
}

type contactSummaryDTO struct {
	ID            string         `json:"id"`
	Name          string         `json:"name"`
	Email         string         `json:"email"`
	State         contacts.State `json:"state"`
	CreatedAt     time.Time      `json:"createdAt"`
	UpdatedAt     time.Time      `json:"updatedAt"`
	ReviewDueAt   time.Time      `json:"reviewDueAt"`
	DeletionDueAt *time.Time     `json:"deletionDueAt,omitempty"`
}

type contactDetailDTO struct {
	ID                string         `json:"id"`
	Name              string         `json:"name"`
	Email             string         `json:"email"`
	Phone             string         `json:"phone,omitempty"`
	Message           string         `json:"message"`
	ConsentVersion    string         `json:"consentVersion"`
	PrivacyAcceptedAt time.Time      `json:"privacyAcceptedAt"`
	State             contacts.State `json:"state"`
	CreatedAt         time.Time      `json:"createdAt"`
	UpdatedAt         time.Time      `json:"updatedAt"`
	ReadAt            *time.Time     `json:"readAt,omitempty"`
	ArchivedAt        *time.Time     `json:"archivedAt,omitempty"`
	ReviewDueAt       time.Time      `json:"reviewDueAt"`
	DeletionDueAt     *time.Time     `json:"deletionDueAt,omitempty"`
}

type contactPageDTO struct {
	Items      []contactSummaryDTO `json:"items"`
	NextCursor string              `json:"nextCursor"`
}

func (handler *handler) dashboardGET(response http.ResponseWriter, request *http.Request) {
	if handler.contacts == nil {
		writeJSONError(response, http.StatusServiceUnavailable, "contacts temporarily unavailable")
		return
	}
	summary, err := handler.contacts.Dashboard(request.Context())
	if err != nil {
		writeContactServiceError(response, err)
		return
	}
	writeJSON(response, http.StatusOK, dashboardDTO{
		New: summary.New, Read: summary.Read, Archived: summary.Archived,
		DeletionScheduled: summary.DeletionScheduled, RetentionReview: summary.RetentionReview, Purged: summary.Purged,
	})
}

func (handler *handler) purgeDuePOST(response http.ResponseWriter, request *http.Request) {
	if handler.contacts == nil {
		writeJSONError(response, http.StatusServiceUnavailable, "contacts temporarily unavailable")
		return
	}
	purged, err := handler.contacts.PurgeDue(request.Context(), handler.now().UTC())
	if err != nil {
		writeContactServiceError(response, err)
		return
	}
	writeJSON(response, http.StatusOK, purgeDTO{Purged: purged})
}

func (handler *handler) contactsGET(response http.ResponseWriter, request *http.Request) {
	if handler.contacts == nil {
		writeJSONError(response, http.StatusServiceUnavailable, "contacts temporarily unavailable")
		return
	}
	options, err := parseContactListOptions(request.URL.Query())
	if err != nil {
		writeJSONError(response, http.StatusBadRequest, "invalid contact query")
		return
	}
	page, err := handler.contacts.List(request.Context(), options)
	if err != nil {
		writeContactServiceError(response, err)
		return
	}
	items := make([]contactSummaryDTO, 0, len(page.Items))
	for _, contact := range page.Items {
		items = append(items, contactSummary(contact))
	}
	writeJSON(response, http.StatusOK, contactPageDTO{Items: items, NextCursor: page.NextCursor})
}

func (handler *handler) contactGET(response http.ResponseWriter, request *http.Request) {
	if handler.contacts == nil {
		writeJSONError(response, http.StatusServiceUnavailable, "contacts temporarily unavailable")
		return
	}
	contact, err := handler.contacts.Get(request.Context(), request.PathValue("id"))
	if err != nil {
		writeContactServiceError(response, err)
		return
	}
	writeContact(response, contact)
}

func (handler *handler) contactPOST(response http.ResponseWriter, request *http.Request) {
	if handler.contacts == nil {
		writeJSONError(response, http.StatusServiceUnavailable, "contacts temporarily unavailable")
		return
	}
	action := request.PathValue("action")
	if !validContactAction(action) {
		handler.apiNotFound(response, request)
		return
	}
	expectedETag, status, err := parseIfMatch(request.Header.Get("If-Match"))
	if err != nil {
		writeJSONError(response, status, "valid If-Match header required")
		return
	}
	id := request.PathValue("id")
	var updated contacts.Contact
	switch action {
	case "read":
		updated, err = handler.contacts.Open(request.Context(), id, expectedETag)
	case "archive":
		updated, err = handler.contacts.Archive(request.Context(), id, expectedETag)
	case "restore":
		updated, err = handler.contacts.Restore(request.Context(), id, expectedETag)
	case "schedule-deletion":
		updated, err = handler.contacts.ScheduleDeletion(request.Context(), id, expectedETag)
	case "cancel-deletion":
		updated, err = handler.contacts.CancelDeletion(request.Context(), id, expectedETag)
	}
	if err != nil {
		writeContactServiceError(response, err)
		return
	}
	writeContact(response, updated)
}

func (handler *handler) apiNotFound(response http.ResponseWriter, _ *http.Request) {
	writeJSONError(response, http.StatusNotFound, "admin API route not found")
}

func parseContactListOptions(values url.Values) (contacts.ListOptions, error) {
	allowed := map[string]bool{"q": true, "state": true, "cursor": true, "retentionReview": true, "deletionScheduled": true}
	for key := range values {
		if !allowed[key] || len(values[key]) != 1 {
			return contacts.ListOptions{}, contacts.ErrValidation
		}
	}
	query, ok := singleQueryValue(values, "q")
	if !ok {
		return contacts.ListOptions{}, contacts.ErrValidation
	}
	cursor, ok := singleQueryValue(values, "cursor")
	if !ok {
		return contacts.ListOptions{}, contacts.ErrValidation
	}
	options := contacts.ListOptions{Query: query, Cursor: cursor, Limit: 25}
	state, ok := singleQueryValue(values, "state")
	if !ok {
		return contacts.ListOptions{}, contacts.ErrValidation
	}
	if state != "" {
		parsed := contacts.State(state)
		if parsed != contacts.StateNew && parsed != contacts.StateRead && parsed != contacts.StateArchived {
			return contacts.ListOptions{}, contacts.ErrValidation
		}
		options.State = &parsed
	}
	options.RetentionReview, ok = parseQueryBool(values, "retentionReview")
	if !ok {
		return contacts.ListOptions{}, contacts.ErrValidation
	}
	options.DeletionScheduled, ok = parseQueryBool(values, "deletionScheduled")
	if !ok {
		return contacts.ListOptions{}, contacts.ErrValidation
	}
	if err := options.Validate(); err != nil {
		return contacts.ListOptions{}, err
	}
	return options, nil
}

func singleQueryValue(values url.Values, key string) (string, bool) {
	items, exists := values[key]
	if !exists {
		return "", true
	}
	if len(items) != 1 {
		return "", false
	}
	return items[0], true
}

func parseQueryBool(values url.Values, key string) (bool, bool) {
	value, ok := singleQueryValue(values, key)
	if !ok {
		return false, false
	}
	switch value {
	case "", "false":
		return false, true
	case "true":
		return true, true
	default:
		return false, false
	}
}

func validContactAction(action string) bool {
	switch action {
	case "read", "archive", "restore", "schedule-deletion", "cancel-deletion":
		return true
	default:
		return false
	}
}

func parseIfMatch(value string) (string, int, error) {
	if value == "" {
		return "", http.StatusPreconditionRequired, errors.New("If-Match is required")
	}
	if len(value) < 3 || len(value) > 1024 || value[0] != '"' || value[len(value)-1] != '"' || strings.Contains(value[1:len(value)-1], `"`) {
		return "", http.StatusBadRequest, errors.New("If-Match is malformed")
	}
	encoded := value[1 : len(value)-1]
	decoded, err := base64.RawURLEncoding.DecodeString(encoded)
	if err != nil || len(decoded) == 0 || base64.RawURLEncoding.EncodeToString(decoded) != encoded {
		return "", http.StatusBadRequest, errors.New("If-Match is malformed")
	}
	return string(decoded), 0, nil
}

func formatETag(value string) string {
	return `"` + base64.RawURLEncoding.EncodeToString([]byte(value)) + `"`
}

func contactSummary(contact contacts.Contact) contactSummaryDTO {
	return contactSummaryDTO{
		ID: contact.ID, Name: contact.Name, Email: contact.Email, State: contact.State,
		CreatedAt: contact.CreatedAt, UpdatedAt: contact.UpdatedAt, ReviewDueAt: contact.ReviewDueAt, DeletionDueAt: contact.DeletionDueAt,
	}
}

func contactDetail(contact contacts.Contact) contactDetailDTO {
	return contactDetailDTO{
		ID: contact.ID, Name: contact.Name, Email: contact.Email, Phone: contact.Phone, Message: contact.Message,
		ConsentVersion: contact.ConsentVersion, PrivacyAcceptedAt: contact.PrivacyAcceptedAt, State: contact.State,
		CreatedAt: contact.CreatedAt, UpdatedAt: contact.UpdatedAt, ReadAt: contact.ReadAt, ArchivedAt: contact.ArchivedAt,
		ReviewDueAt: contact.ReviewDueAt, DeletionDueAt: contact.DeletionDueAt,
	}
}

func writeContact(response http.ResponseWriter, contact contacts.Contact) {
	if contact.ETag == "" {
		writeJSONError(response, http.StatusServiceUnavailable, "contact temporarily unavailable")
		return
	}
	response.Header().Set("ETag", formatETag(contact.ETag))
	writeJSON(response, http.StatusOK, contactDetail(contact))
}

func writeContactServiceError(response http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, contacts.ErrNotFound):
		writeJSONError(response, http.StatusNotFound, "contact not found")
	case errors.Is(err, contacts.ErrConflict), errors.Is(err, contacts.ErrInvalidTransition):
		writeJSONError(response, http.StatusConflict, "contact changed; reload and retry")
	case errors.Is(err, contacts.ErrValidation):
		writeJSONError(response, http.StatusBadRequest, "invalid contact request")
	default:
		writeJSONError(response, http.StatusServiceUnavailable, "contacts temporarily unavailable")
	}
}

func writeJSON(response http.ResponseWriter, status int, value any) {
	setPrivateAdminHeaders(response)
	response.Header().Set("Content-Type", "application/json; charset=utf-8")
	response.WriteHeader(status)
	_ = json.NewEncoder(response).Encode(value)
}
