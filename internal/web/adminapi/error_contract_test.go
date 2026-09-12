package adminapi

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"
)

func TestAdminAPIErrorsUseOneNestedContract(t *testing.T) {
	response := httptest.NewRecorder()
	response.Header().Set("X-Request-ID", "request-contract")
	writeJSONCode(response, http.StatusConflict, "article_conflict", "internal detail must not be exposed")

	var payload map[string]any
	if err := json.Unmarshal(response.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if keys := reflect.ValueOf(payload).MapKeys(); len(keys) != 1 || keys[0].String() != "error" {
		t.Fatalf("top-level payload = %#v", payload)
	}
	errorObject, ok := payload["error"].(map[string]any)
	if !ok {
		t.Fatalf("error object = %#v", payload["error"])
	}
	if errorObject["code"] != "article_conflict" || errorObject["requestId"] != "request-contract" || errorObject["message"] != "L’articolo è stato modificato. Ricarica i dati prima di riprovare." {
		t.Fatalf("error object = %#v", errorObject)
	}
	fields, ok := errorObject["fields"].(map[string]any)
	if !ok || len(fields) != 0 {
		t.Fatalf("fields = %#v", errorObject["fields"])
	}
	if response.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("Cache-Control = %q", response.Header().Get("Cache-Control"))
	}
}
