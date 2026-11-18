package schema

import (
	"strings"
	"testing"
)

// H14: only a reply that is really incoherent is given up on at once; a
// coherent prose summary that merely lacks the JSON object gets the follow-up.
func TestIncoherent(t *testing.T) {
	loop := strings.Repeat("the the the okay okay ", 20)
	cases := []struct {
		name, text string
		want       bool
	}{
		{"empty", "  \n", true},
		{"a handful of words", "Sorry, I can't do that.", true},
		{"mostly symbols", strings.Repeat("#@!%^&*()_+=- ", 12), true},
		{"repetition loop", loop, true},
		{"prose summary", "All tests are passing successfully. I added the divide handler in main.go, returned HTTP 400 for a zero divisor, and wrote tests for the add, subtract and divide endpoints in main_test.go.", false},
	}
	for _, c := range cases {
		if got := Incoherent(c.text); got != c.want {
			t.Errorf("%s: Incoherent = %v, want %v", c.name, got, c.want)
		}
	}
}
