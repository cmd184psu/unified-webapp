// Package schema extracts and validates the structured JSON object an agent
// returns as its final message.
package schema

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"unicode"
)

// ErrNoObject means the text holds no balanced JSON object.
var ErrNoObject = errors.New("schema: no JSON object found")

// ValidationError names the offending field.
type ValidationError struct {
	Field string
	Msg   string
}

func (e *ValidationError) Error() string {
	return fmt.Sprintf("%s: %s", e.Field, e.Msg)
}

type validator interface{ Validate() error }

// ExtractFinal returns the last top-level balanced JSON object in text,
// whether bare or inside a markdown fence. Every role prompt asks for the
// answer as the final message content, so when a reply quotes an example
// object before its real answer, the last one wins. Candidates that are not
// valid JSON (prose braces) are skipped. Braces inside strings are ignored.
func ExtractFinal(text string) (json.RawMessage, error) {
	var last []byte
	for i := 0; i < len(text); i++ {
		if text[i] != '{' {
			continue
		}
		end := balancedEnd(text, i)
		if end < 0 {
			continue
		}
		if cand := []byte(text[i:end]); json.Valid(cand) {
			last = cand
			i = end - 1
		}
	}
	if last == nil {
		return nil, ErrNoObject
	}
	return json.RawMessage(last), nil
}

// balancedEnd returns the index after the object starting at start, or -1.
func balancedEnd(s string, start int) int {
	depth := 0
	inStr, esc := false, false
	for i := start; i < len(s); i++ {
		c := s[i]
		switch {
		case inStr && esc:
			esc = false
		case inStr && c == '\\':
			esc = true
		case inStr && c == '"':
			inStr = false
		case inStr:
		case c == '"':
			inStr = true
		case c == '{':
			depth++
		case c == '}':
			depth--
			if depth == 0 {
				return i + 1
			}
		}
	}
	return -1
}

// Parse decodes raw into T, rejecting unknown fields and trailing data, then
// runs T's Validate method (declared on the pointer receiver).
func Parse[T any](raw json.RawMessage) (T, error) {
	var v T
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&v); err != nil {
		return v, fmt.Errorf("schema: %w", err)
	}
	if _, err := dec.Token(); err != io.EOF {
		return v, errors.New("schema: trailing data after JSON object")
	}
	if val, ok := any(&v).(validator); ok {
		if err := val.Validate(); err != nil {
			return v, err
		}
	}
	return v, nil
}

// ParsePlan is Parse[PlanOut] with the deliberate-mode check (RP5) when
// deliberate is set; its error goes to the same corrective follow-up as any
// other invalid reply.
func ParsePlan(raw json.RawMessage, deliberate bool) (PlanOut, error) {
	p, err := Parse[PlanOut](raw)
	if err != nil || !deliberate {
		return p, err
	}
	return p, p.ValidateDeliberate()
}

// CorrectiveMessage builds the single follow-up sent to the same model after
// its reply failed to parse or validate.
func CorrectiveMessage(err error) string {
	return "Your previous reply could not be accepted: " + err.Error() + "\n\n" +
		"Reply again with exactly one JSON object that matches the schema in your instructions. " +
		"Output only the JSON object: no markdown fence, no commentary before or after it."
}

// Incoherent reports reply text that is not language at all: empty, a handful
// of words, mostly symbols, or a loop over a few words. A model that ends a
// finished job with a prose summary instead of the JSON object is not
// incoherent; that reply is worth a corrective follow-up (H14).
func Incoherent(text string) bool {
	words := strings.Fields(text)
	if len(words) < 8 {
		return true
	}
	letters, others := 0, 0
	for _, r := range text {
		switch {
		case unicode.IsLetter(r):
			letters++
		case !unicode.IsSpace(r) && !unicode.IsDigit(r):
			others++
		}
	}
	if letters < others {
		return true
	}
	distinct := map[string]bool{}
	for _, w := range words {
		distinct[strings.ToLower(w)] = true
	}
	return len(words) >= 20 && len(distinct)*5 < len(words)
}
