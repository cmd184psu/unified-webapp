package response

import (
	"encoding/json"
	"errors"
	"net/http"
)

// WriteJSON encodes v as JSON with the given HTTP status.
func WriteJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

// WriteError writes a JSON error envelope.
func WriteError(w http.ResponseWriter, status int, msg string) {
	WriteJSON(w, status, map[string]string{"error": msg})
}

// WriteDecodeError writes the JSON error envelope for a failed request-body
// decode. A body that overran a middleware.BodyLimit surfaces here as an
// *http.MaxBytesError, which maps to 413; any other decode failure
// (malformed JSON, unexpected EOF, wrong shape) maps to 400.
func WriteDecodeError(w http.ResponseWriter, err error) {
	var maxErr *http.MaxBytesError
	if errors.As(err, &maxErr) {
		WriteError(w, http.StatusRequestEntityTooLarge, "request body too large")
		return
	}
	WriteError(w, http.StatusBadRequest, "invalid request body")
}
