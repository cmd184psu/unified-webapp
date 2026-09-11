package response

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestWriteJSON(t *testing.T) {
	w := httptest.NewRecorder()
	WriteJSON(w, http.StatusCreated, map[string]string{"msg": "saved"})

	if w.Code != http.StatusCreated {
		t.Errorf("status = %d, want %d", w.Code, http.StatusCreated)
	}
	if ct := w.Header().Get("Content-Type"); ct != "application/json" {
		t.Errorf("Content-Type = %q, want %q", ct, "application/json")
	}
	var body map[string]string
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode body: %v\nbody: %s", err, w.Body.String())
	}
	if body["msg"] != "saved" {
		t.Errorf("body[msg] = %q, want %q", body["msg"], "saved")
	}
}

func TestWriteError(t *testing.T) {
	w := httptest.NewRecorder()
	WriteError(w, http.StatusBadRequest, "invalid path")

	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", w.Code, http.StatusBadRequest)
	}
	if ct := w.Header().Get("Content-Type"); ct != "application/json" {
		t.Errorf("Content-Type = %q, want %q", ct, "application/json")
	}

	var envelope map[string]string
	if err := json.Unmarshal(w.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("decode body: %v\nbody: %s", err, w.Body.String())
	}
	if len(envelope) != 1 {
		t.Fatalf("envelope has %d keys, want exactly 1 (\"error\"): %v", len(envelope), envelope)
	}
	if envelope["error"] != "invalid path" {
		t.Errorf("envelope[error] = %q, want %q", envelope["error"], "invalid path")
	}
}
