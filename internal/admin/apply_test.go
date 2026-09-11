package admin

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"golang.org/x/crypto/bcrypt"

	"cmd184psu/unified-webapp/internal/platform/auth"
	"cmd184psu/unified-webapp/internal/platform/config"
)

// bcryptHash bcrypt-hashes pin at the minimum cost (tests don't need real
// hardening, just a hash checkPIN's bcrypt.CompareHashAndPassword accepts).
func bcryptHash(t *testing.T, pin string) string {
	t.Helper()
	h, err := bcrypt.GenerateFromPassword([]byte(pin), bcrypt.MinCost)
	if err != nil {
		t.Fatalf("bcrypt.GenerateFromPassword: %v", err)
	}
	return string(h)
}

// writeApplyFixture writes content to a fresh temp dir and returns its path.
func writeApplyFixture(t *testing.T, content string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("writing fixture: %v", err)
	}
	return path
}

// applyTestHandler builds a Handler wired to a real *auth.Service (built via
// auth.FromConfig, exactly as boot does) and to configPath, so applyAuth
// exercises the real ValidatePolicy/BuildPolicy/SwapPolicy pipeline.
func applyTestHandler(t *testing.T, initial config.AuthConfig, knownModules []string, adminRouted bool, configPath string) *Handler {
	t.Helper()
	svc, err := auth.FromConfig(initial, knownModules, adminRouted)
	if err != nil {
		t.Fatalf("auth.FromConfig: %v", err)
	}
	return NewHandler(config.AdminConfig{}, initial, Deps{
		Service:      svc,
		ConfigPath:   configPath,
		KnownModules: knownModules,
		AdminRouted:  adminRouted,
	})
}

// --- AC-14: byte-range preservation ---

// fixtureWithAuth is a distinctively-formatted config file (odd spacing
// around a colon, unsorted/non-alphabetical keys, a trailing member after
// "auth") with a top-level "auth" member already present, to prove a valid
// save touches only that member's value bytes.
const fixtureWithAuth = `{
  "custom_a"   : 9999,
  "nested": {
    "list": [
      "a",
      "b"
    ]
  },
  "auth": {
    "modules": {
      "old": [
        "pin"
      ]
    }
  },
  "trailing": {
    "z": "keep me"
  }
}
`

// fixtureWithoutAuth is a distinctively-formatted config file with no
// top-level "auth" member at all, to prove a valid save inserts one before
// the final "}" with a deliberately-placed comma after the preceding
// member, touching nothing else.
const fixtureWithoutAuth = `{
  "custom_a"    : 1,
  "nested":   {
    "x": true
  },
  "trailing": "keep-me-too"
}
`

func TestApplyAuthReplacesExistingAuthMemberByteRangeOnly(t *testing.T) {
	original := []byte(fixtureWithAuth)
	loc, err := locateAuthMember(original)
	if err != nil {
		t.Fatalf("locateAuthMember(original): %v", err)
	}
	if !loc.found {
		t.Fatalf("locateAuthMember(original).found = false, want true")
	}

	newAuth := config.AuthConfig{
		Modules: map[string][]string{"todo": {"pin"}},
		PINs:    []config.NamedHash{{Name: "alice", Hash: bcryptHash(t, "1234")}},
	}
	expectedMarshaled, err := json.MarshalIndent(newAuth, loc.indent, "  ")
	if err != nil {
		t.Fatalf("MarshalIndent: %v", err)
	}

	path := writeApplyFixture(t, fixtureWithAuth)
	h := applyTestHandler(t, config.AuthConfig{}, []string{"todo"}, false, path)

	if err := h.applyAuth(newAuth); err != nil {
		t.Fatalf("applyAuth: %v", err)
	}

	newData, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading spliced config: %v", err)
	}

	prefixLen := int(loc.valueStart)
	suffixLen := len(original) - int(loc.valueEnd)

	if !bytes.Equal(newData[:prefixLen], original[:prefixLen]) {
		t.Errorf("bytes before the auth member's value changed:\n got: %q\nwant: %q", newData[:prefixLen], original[:prefixLen])
	}
	gotSuffix := newData[len(newData)-suffixLen:]
	wantSuffix := original[loc.valueEnd:]
	if !bytes.Equal(gotSuffix, wantSuffix) {
		t.Errorf("bytes after the auth member's value changed:\n got: %q\nwant: %q", gotSuffix, wantSuffix)
	}
	middle := newData[prefixLen : len(newData)-suffixLen]
	if !bytes.Equal(middle, expectedMarshaled) {
		t.Errorf("auth member's new value:\n got: %s\nwant: %s", middle, expectedMarshaled)
	}
}

func TestApplyAuthInsertsAuthMemberWhenAbsentByteRangeOnly(t *testing.T) {
	original := []byte(fixtureWithoutAuth)
	loc, err := locateAuthMember(original)
	if err != nil {
		t.Fatalf("locateAuthMember(original): %v", err)
	}
	if loc.found {
		t.Fatalf("locateAuthMember(original).found = true, want false")
	}
	if !loc.needsComma {
		t.Fatalf("locateAuthMember(original).needsComma = false, want true (fixture has existing members)")
	}

	newAuth := config.AuthConfig{
		Modules: map[string][]string{"todo": {"pin"}},
		PINs:    []config.NamedHash{{Name: "alice", Hash: bcryptHash(t, "1234")}},
	}
	expectedMarshaled, err := json.MarshalIndent(newAuth, loc.indent, "  ")
	if err != nil {
		t.Fatalf("MarshalIndent: %v", err)
	}
	var expectedInserted bytes.Buffer
	expectedInserted.WriteString(",\n")
	expectedInserted.WriteString(loc.indent)
	expectedInserted.WriteString(`"auth": `)
	expectedInserted.Write(expectedMarshaled)

	path := writeApplyFixture(t, fixtureWithoutAuth)
	h := applyTestHandler(t, config.AuthConfig{}, []string{"todo"}, false, path)

	if err := h.applyAuth(newAuth); err != nil {
		t.Fatalf("applyAuth: %v", err)
	}

	newData, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading spliced config: %v", err)
	}

	prefixLen := int(loc.insertAt)
	suffixLen := len(original) - int(loc.insertAt)

	if !bytes.Equal(newData[:prefixLen], original[:prefixLen]) {
		t.Errorf("bytes before the insertion point changed:\n got: %q\nwant: %q", newData[:prefixLen], original[:prefixLen])
	}
	gotSuffix := newData[len(newData)-suffixLen:]
	wantSuffix := original[loc.insertAt:]
	if !bytes.Equal(gotSuffix, wantSuffix) {
		t.Errorf("bytes after the insertion point changed:\n got: %q\nwant: %q", gotSuffix, wantSuffix)
	}
	middle := newData[prefixLen : len(newData)-suffixLen]
	if !bytes.Equal(middle, expectedInserted.Bytes()) {
		t.Errorf("inserted auth member:\n got: %s\nwant: %s", middle, expectedInserted.Bytes())
	}

	// Sanity: the result must still be valid JSON.
	var probe map[string]json.RawMessage
	if err := json.Unmarshal(newData, &probe); err != nil {
		t.Fatalf("spliced file is not valid JSON: %v\ncontent:\n%s", err, newData)
	}
	if _, ok := probe["auth"]; !ok {
		t.Errorf("spliced file has no \"auth\" member")
	}
}

// --- Next request enforces the new policy ---

func TestApplyAuthNextRequestEnforcesNewPolicy(t *testing.T) {
	path := writeApplyFixture(t, "{}\n")
	h := applyTestHandler(t, config.AuthConfig{}, []string{"todo"}, false, path)

	echo := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Reached-Next", "yes")
		w.WriteHeader(http.StatusOK)
	})

	// Before: "todo" is unprotected (no matrix entry at all), so an
	// unauthenticated request passes through.
	before := httptest.NewRecorder()
	h.deps.Service.Gate("todo", echo).ServeHTTP(before, httptest.NewRequest(http.MethodGet, "/", nil))
	if before.Code != http.StatusOK {
		t.Fatalf("before applyAuth: status = %d, want 200 (unprotected)", before.Code)
	}

	newAuth := config.AuthConfig{
		Modules: map[string][]string{"todo": {"pin"}},
		PINs:    []config.NamedHash{{Name: "alice", Hash: bcryptHash(t, "1234")}},
	}
	if err := h.applyAuth(newAuth); err != nil {
		t.Fatalf("applyAuth: %v", err)
	}

	// After: "todo" now requires "pin"; the same unauthenticated request is
	// rejected on the very next request, no restart.
	after := httptest.NewRecorder()
	h.deps.Service.Gate("todo", echo).ServeHTTP(after, httptest.NewRequest(http.MethodGet, "/", nil))
	if after.Code != http.StatusUnauthorized {
		t.Fatalf("after applyAuth: status = %d, want 401 (now protected)", after.Code)
	}
	if after.Header().Get("X-Reached-Next") == "yes" {
		t.Fatalf("after applyAuth: request reached next handler, want it blocked at the gate")
	}
}

// --- Invalid save: nothing changes anywhere ---

func TestApplyAuthInvalidSaveChangesNothing(t *testing.T) {
	const fixture = `{
  "custom_a": 1,
  "auth": {
    "modules": {}
  }
}
`
	original := []byte(fixture)
	path := writeApplyFixture(t, fixture)
	h := applyTestHandler(t, config.AuthConfig{}, []string{"todo"}, false, path)

	echo := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Reached-Next", "yes")
		w.WriteHeader(http.StatusOK)
	})

	before := httptest.NewRecorder()
	h.deps.Service.Gate("todo", echo).ServeHTTP(before, httptest.NewRequest(http.MethodGet, "/", nil))
	if before.Code != http.StatusOK {
		t.Fatalf("before applyAuth: status = %d, want 200 (unprotected)", before.Code)
	}

	// "nope" is not in KnownModules ({"todo"}), so ValidatePolicy rejects
	// this outright -- a real rejection per auth/validate.go, not a
	// contrived one.
	invalid := config.AuthConfig{Modules: map[string][]string{"nope": {"pin"}}}
	err := h.applyAuth(invalid)
	if err == nil {
		t.Fatalf("applyAuth(invalid) = nil error, want a validation error")
	}

	got, readErr := os.ReadFile(path)
	if readErr != nil {
		t.Fatalf("reading config after rejected apply: %v", readErr)
	}
	if !bytes.Equal(got, original) {
		t.Errorf("config file changed after a rejected apply:\n got: %s\nwant (unchanged): %s", got, original)
	}

	after := httptest.NewRecorder()
	h.deps.Service.Gate("todo", echo).ServeHTTP(after, httptest.NewRequest(http.MethodGet, "/", nil))
	if after.Code != http.StatusOK {
		t.Fatalf("after rejected applyAuth: status = %d, want 200 (policy unchanged)", after.Code)
	}
}

// --- Crash-window safety: tmp+rename, never a partial file ---

func TestApplyAuthLeavesNoStrayTmpFiles(t *testing.T) {
	path := writeApplyFixture(t, "{}\n")
	dir := filepath.Dir(path)
	h := applyTestHandler(t, config.AuthConfig{}, []string{"todo"}, false, path)

	newAuth := config.AuthConfig{
		Modules: map[string][]string{"todo": {"pin"}},
		PINs:    []config.NamedHash{{Name: "alice", Hash: bcryptHash(t, "1234")}},
	}
	if err := h.applyAuth(newAuth); err != nil {
		t.Fatalf("applyAuth: %v", err)
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("ReadDir: %v", err)
	}
	if len(entries) != 1 || entries[0].Name() != filepath.Base(path) {
		names := make([]string, len(entries))
		for i, e := range entries {
			names[i] = e.Name()
		}
		t.Fatalf("config dir after apply = %v, want exactly [%s]", names, filepath.Base(path))
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("Stat: %v", err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Errorf("config file mode = %#o, want 0600", perm)
	}
}

// --- Round-trip: config.Load on the spliced file yields the new auth ---

func TestApplyAuthRoundTripsThroughConfigLoad(t *testing.T) {
	path := writeApplyFixture(t, "{\n  \"port\": 8080\n}\n")
	h := applyTestHandler(t, config.AuthConfig{}, []string{"todo"}, false, path)

	newAuth := config.AuthConfig{
		Modules:      map[string][]string{"todo": {"pin", "key"}},
		PINs:         []config.NamedHash{{Name: "alice", Hash: bcryptHash(t, "1234")}},
		APIKeys:      []config.NamedHash{{Name: "svc1", Hash: "sha256:deadbeef"}},
		CookieSecure: true,
		CookieDomain: "example.com",
	}
	if err := h.applyAuth(newAuth); err != nil {
		t.Fatalf("applyAuth: %v", err)
	}

	loaded, err := config.Load(path)
	if err != nil {
		t.Fatalf("config.Load(spliced file): %v", err)
	}
	if loaded.Port != 8080 {
		t.Errorf("loaded.Port = %d, want 8080 (untouched member)", loaded.Port)
	}

	gotMarshaled, _ := json.Marshal(loaded.Auth)
	wantMarshaled, _ := json.Marshal(newAuth)
	if !bytes.Equal(gotMarshaled, wantMarshaled) {
		t.Errorf("config.Load(spliced file).Auth = %s, want %s", gotMarshaled, wantMarshaled)
	}
}
