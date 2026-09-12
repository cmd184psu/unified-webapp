package admin

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"cmd184psu/unified-webapp/internal/platform/config"
)

// --- S4 (a): seeding inversion -- a save must protect exactly what was
// submitted, never everything known_modules lists. ---

func TestPutConfigModulesProtectsOnlyTheSubmittedModule(t *testing.T) {
	initial := config.AuthConfig{LDAP: config.LDAPConfig{URL: "ldaps://ldap.example.com"}}
	h, mux, path := newAdminTestHandler(t, initial, []string{"menuserver", "todo", "obsidianoid"}, false)

	rec := doAdmin(t, mux, http.MethodPut, "/api/config/modules", map[string]config.ModuleAuthConfig{"menuserver": {}})
	if rec.Code != http.StatusOK {
		t.Fatalf("PUT /api/config/modules = %d, want 200; body: %s", rec.Code, rec.Body.String())
	}

	loaded, err := config.Load(path)
	if err != nil {
		t.Fatalf("config.Load(spliced file): %v", err)
	}
	if len(loaded.Auth.Modules) != 1 {
		t.Fatalf("auth.modules = %+v, want exactly one entry (menuserver)", loaded.Auth.Modules)
	}
	m, ok := loaded.Auth.Modules["menuserver"]
	if !ok {
		t.Fatalf("auth.modules missing %q: %+v", "menuserver", loaded.Auth.Modules)
	}
	if m.PinFile != "" {
		t.Errorf("menuserver.pin_file = %q, want empty (bare {} entry)", m.PinFile)
	}
	if _, ok := loaded.Auth.Modules["todo"]; ok {
		t.Errorf("todo was protected by the save even though it was never submitted -- seeding inversion")
	}
	if _, ok := loaded.Auth.Modules["obsidianoid"]; ok {
		t.Errorf("obsidianoid was protected by the save even though it was never submitted -- seeding inversion")
	}

	echo := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) })

	// menuserver: now protected -- unauthenticated request is rejected.
	menuRec := httptest.NewRecorder()
	h.deps.Service.Gate("menuserver", echo).ServeHTTP(menuRec, httptest.NewRequest(http.MethodGet, "/", nil))
	if menuRec.Code != http.StatusUnauthorized {
		t.Errorf("menuserver gate = %d, want 401 (protected, bare {} entry)", menuRec.Code)
	}

	// todo: never appeared in the submitted matrix -- must remain open.
	todoRec := httptest.NewRecorder()
	h.deps.Service.Gate("todo", echo).ServeHTTP(todoRec, httptest.NewRequest(http.MethodGet, "/", nil))
	if todoRec.Code != http.StatusOK {
		t.Errorf("todo gate = %d, want 200 (never submitted, must stay open)", todoRec.Code)
	}
}

// --- S4 (b): admin live-save round-trip -- the written file boots via
// config.Load and never regains a "pins" key. ---

func TestAdminLiveSaveRoundTripsThroughConfigLoadWithNoPinsKey(t *testing.T) {
	initial := config.AuthConfig{LDAP: config.LDAPConfig{URL: "ldaps://ldap.example.com"}}
	_, mux, path := newAdminTestHandler(t, initial, []string{"menuserver"}, false)

	pinPath := pinFileFixture(t, "1234")
	rec := doAdmin(t, mux, http.MethodPut, "/api/config/modules", map[string]config.ModuleAuthConfig{
		"menuserver": {PinFile: pinPath},
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("PUT /api/config/modules = %d, want 200; body: %s", rec.Code, rec.Body.String())
	}

	written, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading written config: %v", err)
	}
	if bytes.Contains(written, []byte(`"pins"`)) {
		t.Errorf("written config file contains a %q key:\n%s", "pins", written)
	}

	loaded, err := config.Load(path)
	if err != nil {
		t.Fatalf("config.Load(written file) failed to boot: %v", err)
	}
	m, ok := loaded.Auth.Modules["menuserver"]
	if !ok || m.PinFile != pinPath {
		t.Errorf("loaded auth.modules[menuserver] = %+v, want {PinFile: %q}", loaded.Auth.Modules["menuserver"], pinPath)
	}
}

// --- S4 (c): a relative pin_file submitted through PUT is validated (and
// persisted) against the config file's directory, not the process's CWD. ---

func TestPutConfigModulesExpandsRelativePinFileAgainstConfigDir(t *testing.T) {
	initial := config.AuthConfig{LDAP: config.LDAPConfig{URL: "ldaps://ldap.example.com"}}
	_, mux, path := newAdminTestHandler(t, initial, []string{"menuserver"}, false)
	configDir := filepath.Dir(path)

	// The pin file lives next to the config file, not in the test binary's
	// working directory -- a CWD-relative resolution would never find it.
	const relName = "door.pin"
	if err := os.WriteFile(filepath.Join(configDir, relName), []byte("1234"), 0o400); err != nil {
		t.Fatalf("writing pin file fixture: %v", err)
	}

	rec := doAdmin(t, mux, http.MethodPut, "/api/config/modules", map[string]config.ModuleAuthConfig{
		"menuserver": {PinFile: relName},
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("PUT /api/config/modules (relative pin_file) = %d, want 200; body: %s", rec.Code, rec.Body.String())
	}

	wantAbs := filepath.Join(configDir, relName)

	loaded, err := config.Load(path)
	if err != nil {
		t.Fatalf("config.Load(spliced file): %v", err)
	}
	if got := loaded.Auth.Modules["menuserver"].PinFile; got != wantAbs {
		t.Errorf("persisted menuserver.pin_file = %q, want %q (expanded against the config dir)", got, wantAbs)
	}

	getRec := doAdmin(t, mux, http.MethodGet, "/api/config/auth", nil)
	if getRec.Code != http.StatusOK {
		t.Fatalf("GET /api/config/auth = %d, want 200; body: %s", getRec.Code, getRec.Body.String())
	}
	if got := getRec.Body.String(); !strings.Contains(got, wantAbs) {
		t.Errorf("GET /api/config/auth body = %s, want it to contain the expanded absolute path %q", got, wantAbs)
	}
}

// TestPutConfigModulesMissingPinFileRejected400 confirms a live edit naming a
// pin_file that does not exist is rejected the same way boot would reject it
// -- ValidatePolicy's stat check runs on every apply, not just at startup.
func TestPutConfigModulesMissingPinFileRejected400(t *testing.T) {
	initial := config.AuthConfig{LDAP: config.LDAPConfig{URL: "ldaps://ldap.example.com"}}
	_, mux, path := newAdminTestHandler(t, initial, []string{"menuserver"}, false)

	rec := doAdmin(t, mux, http.MethodPut, "/api/config/modules", map[string]config.ModuleAuthConfig{
		"menuserver": {PinFile: filepath.Join(filepath.Dir(path), "does-not-exist.pin")},
	})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("PUT /api/config/modules (missing pin_file) = %d, want 400; body: %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "menuserver") {
		t.Errorf("400 body doesn't name the offending module: %s", rec.Body.String())
	}
}
