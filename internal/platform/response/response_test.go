package response

import (
	"encoding/json"
	"errors"
	"fmt"
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

func TestWriteDecodeError(t *testing.T) {
	cases := []struct {
		name       string
		err        error
		wantStatus int
	}{
		{"MaxBytesError maps to 413", &http.MaxBytesError{Limit: 1024}, http.StatusRequestEntityTooLarge},
		{"wrapped MaxBytesError still maps to 413", fmt.Errorf("decode: %w", &http.MaxBytesError{Limit: 1024}), http.StatusRequestEntityTooLarge},
		{"generic decode error maps to 400", errors.New("unexpected EOF"), http.StatusBadRequest},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			w := httptest.NewRecorder()
			WriteDecodeError(w, c.err)

			if w.Code != c.wantStatus {
				t.Fatalf("status = %d, want %d", w.Code, c.wantStatus)
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
			if envelope["error"] == "" {
				t.Error("envelope[error] is empty")
			}
		})
	}
}
