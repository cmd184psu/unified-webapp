package api

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"

	"cmd184psu/unified-webapp/internal/agentflow/store"
	"cmd184psu/unified-webapp/internal/platform/response"
)

// errorBody is the JSON envelope of every failed call. Field is set when one
// request field is at fault (SV1).
type errorBody struct {
	Error string `json:"error"`
	Field string `json:"field,omitempty"`
}

// writeErr answers err with the status store.HTTPStatus maps it to: a
// store.FieldError becomes 400 {"error","field"}, ErrNotFound 404, ErrConflict
// 409 and anything else 500.
func writeErr(w http.ResponseWriter, err error) {
	body := errorBody{Error: err.Error()}
	var fe *store.FieldError
	if errors.As(err, &fe) {
		body.Error, body.Field = fe.Msg, fe.Field
	}
	writeJSON(w, store.HTTPStatus(err), body)
}

// badRequest answers 400 naming the offending field.
func badRequest(w http.ResponseWriter, field, msg string) {
	writeJSON(w, http.StatusBadRequest, errorBody{Error: msg, Field: field})
}

// decodeBody reads the request body into v, rejecting unknown fields. It
// writes the error response and returns false on failure. With optional set an
// empty body leaves v untouched.
func decodeBody(w http.ResponseWriter, r *http.Request, v any, optional bool) bool {
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	err := dec.Decode(v)
	if err == nil {
		if _, more := dec.Token(); more != io.EOF {
			err = errors.New("unexpected data after the JSON value")
		}
	}
	switch {
	case err == nil:
		return true
	case errors.Is(err, io.EOF) && optional:
		return true
	}
	writeDecodeErr(w, err)
	return false
}

// writeDecodeErr answers a body that did not decode: 413 past the body limit,
// else 400 naming the field when the decoder knows it.
func writeDecodeErr(w http.ResponseWriter, err error) {
	var maxErr *http.MaxBytesError
	if errors.As(err, &maxErr) {
		response.WriteError(w, http.StatusRequestEntityTooLarge, "request body too large")
		return
	}
	body := errorBody{Error: "invalid request body: " + err.Error(), Field: "body"}
	var te *json.UnmarshalTypeError
	var se *json.SyntaxError
	switch {
	case errors.As(err, &te):
		if te.Field != "" {
			body.Field = te.Field
		}
	case errors.As(err, &se):
		body.Field = "body"
	case strings.HasPrefix(err.Error(), "json: unknown field "):
		body.Field = strings.Trim(strings.TrimPrefix(err.Error(), "json: unknown field "), `"`)
	}
	writeJSON(w, http.StatusBadRequest, body)
}

// pathID parses a numeric path segment; it writes the 400 itself on failure.
func pathID(w http.ResponseWriter, r *http.Request, name string) (int64, bool) {
	n, err := strconv.ParseInt(r.PathValue(name), 10, 64)
	if err != nil || n <= 0 {
		badRequest(w, name, name+" must be a positive integer")
		return 0, false
	}
	return n, true
}
