package store

import (
	"errors"
	"net/http"
)

// Sentinels every store method returns (wrapped with %w) so callers branch
// with errors.Is, never on error text.
var (
	ErrNotFound = errors.New("not found")
	ErrConflict = errors.New("conflict")
	ErrInvalid  = errors.New("invalid")

	// ErrSchemaTooNew is returned by Open when the database was written by a
	// newer build than this one (its schema_version exceeds the latest
	// migration this code knows about).
	ErrSchemaTooNew = errors.New("database schema is newer than this build")
)

// FieldError is a validation failure naming the offending field. It matches
// ErrInvalid under errors.Is.
type FieldError struct {
	Field string
	Msg   string
}

func (e *FieldError) Error() string { return e.Field + ": " + e.Msg }

func (e *FieldError) Unwrap() error { return ErrInvalid }

// Invalid builds a *FieldError.
func Invalid(field, msg string) error { return &FieldError{Field: field, Msg: msg} }

// HTTPStatus maps a store error to an HTTP status code using errors.Is, so
// wrapping with fmt.Errorf("...: %w", err) never changes the mapping. nil maps
// to 200; unrecognised errors map to 500.
func HTTPStatus(err error) int {
	switch {
	case err == nil:
		return http.StatusOK
	case errors.Is(err, ErrNotFound):
		return http.StatusNotFound
	case errors.Is(err, ErrConflict):
		return http.StatusConflict
	case errors.Is(err, ErrInvalid):
		return http.StatusBadRequest
	default:
		return http.StatusInternalServerError
	}
}
