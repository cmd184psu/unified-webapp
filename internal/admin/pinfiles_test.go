package admin

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"cmd184psu/unified-webapp/internal/platform/config"
)

// --- GET /api/config/pin-files ---

func TestGetPinFilesListsExcludesConfigAndDotfilesSortedByName(t *testing.T) {
	initial := config.AuthConfig{}
	_, mux, path := newAdminTestHandler(t, initial, nil, false)
	dir := filepath.Dir(path)

	for _, name := range []string{"zebra.pin", "admin.pin", ".hidden.pin"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("1234\n"), 0o400); err != nil {
			t.Fatalf("writing fixture file %q: %v", name, err)
		}
	}
	if err := os.Mkdir(filepath.Join(dir, "subdir"), 0o700); err != nil {
		t.Fatalf("mkdir subdir: %v", err)
	}

	rec := doAdmin(t, mux, http.MethodGet, "/api/config/pin-files", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /api/config/pin-files = %d, want 200; body: %s", rec.Code, rec.Body.String())
	}

	var got getPinFilesResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decoding response: %v\nbody: %s", err, rec.Body.String())
	}
	wantDir, err := filepath.Abs(dir)
	if err != nil {
		t.Fatalf("filepath.Abs: %v", err)
	}
	if got.ConfigDir != wantDir {
		t.Errorf("ConfigDir = %q, want %q", got.ConfigDir, wantDir)
	}
	if len(got.Files) != 2 {
		t.Fatalf("Files = %+v, want exactly 2 entries (admin.pin, zebra.pin)", got.Files)
	}
	if got.Files[0].Name != "admin.pin" || got.Files[1].Name != "zebra.pin" {
		t.Errorf("Files names = [%s, %s], want [admin.pin, zebra.pin] (sorted)", got.Files[0].Name, got.Files[1].Name)
	}
	if got.Files[0].Path != filepath.Join(wantDir, "admin.pin") {
		t.Errorf("Files[0].Path = %q, want %q", got.Files[0].Path, filepath.Join(wantDir, "admin.pin"))
	}
}

func TestGetPinFilesEmptyArrayWhenNone(t *testing.T) {
	_, mux, _ := newAdminTestHandler(t, config.AuthConfig{}, nil, false)

	rec := doAdmin(t, mux, http.MethodGet, "/api/config/pin-files", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /api/config/pin-files = %d, want 200; body: %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `"files":[]`) {
		t.Errorf("body = %s, want files to be an empty array, not null", rec.Body.String())
	}
}

// --- POST /api/config/pin-files: by name ---

func TestPostPinFileByNameCreatesFileMode0400(t *testing.T) {
	_, mux, path := newAdminTestHandler(t, config.AuthConfig{}, nil, false)
	dir := filepath.Dir(path)

	rec := doAdmin(t, mux, http.MethodPost, "/api/config/pin-files", map[string]string{"name": "todo.pin", "pin": "111111"})
	if rec.Code != http.StatusOK {
		t.Fatalf("POST /api/config/pin-files = %d, want 200; body: %s", rec.Code, rec.Body.String())
	}

	wantPath := filepath.Join(dir, "todo.pin")
	var got postPinFileResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decoding response: %v", err)
	}
	if got.Path != wantPath {
		t.Errorf("Path = %q, want %q", got.Path, wantPath)
	}

	data, err := os.ReadFile(wantPath)
	if err != nil {
		t.Fatalf("reading written pin file: %v", err)
	}
	if string(data) != "111111\n" {
		t.Errorf("file content = %q, want %q", data, "111111\n")
	}
	fi, err := os.Stat(wantPath)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if fi.Mode().Perm() != 0o400 {
		t.Errorf("mode = %v, want 0400", fi.Mode().Perm())
	}
}

func TestPostPinFileOverwritesExisting0400File(t *testing.T) {
	_, mux, path := newAdminTestHandler(t, config.AuthConfig{}, nil, false)
	dir := filepath.Dir(path)
	target := filepath.Join(dir, "todo.pin")
	if err := os.WriteFile(target, []byte("000000\n"), 0o400); err != nil {
		t.Fatalf("writing pre-existing pin file: %v", err)
	}

	rec := doAdmin(t, mux, http.MethodPost, "/api/config/pin-files", map[string]string{"name": "todo.pin", "pin": "222222"})
	if rec.Code != http.StatusOK {
		t.Fatalf("POST /api/config/pin-files (overwrite) = %d, want 200; body: %s", rec.Code, rec.Body.String())
	}

	data, err := os.ReadFile(target)
	if err != nil {
		t.Fatalf("reading overwritten pin file: %v", err)
	}
	if string(data) != "222222\n" {
		t.Errorf("file content = %q, want %q", data, "222222\n")
	}

	// A second read of the config file is unchanged: this endpoint never
	// touches auth.* config.
	cfgAfter, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading config after pin write: %v", err)
	}
	loaded, err := config.Load(path)
	if err != nil {
		t.Fatalf("config.Load: %v", err)
	}
	if len(loaded.Auth.Modules) != 0 {
		t.Errorf("auth.modules changed by a pin-file write: %+v", loaded.Auth.Modules)
	}
	_ = cfgAfter
}

// --- POST /api/config/pin-files: rejections ---

func TestPostPinFileRejections(t *testing.T) {
	moduleDir := t.TempDir()
	configuredPinFile := filepath.Join(moduleDir, "configured.pin")
	if err := os.WriteFile(configuredPinFile, []byte("1234\n"), 0o400); err != nil {
		t.Fatalf("writing configured pin file fixture: %v", err)
	}
	initial := config.AuthConfig{
		DataDir: t.TempDir(),
		Modules: map[string]config.ModuleAuthConfig{"todo": {PinFile: configuredPinFile}},
		LDAP:    config.LDAPConfig{URL: "ldaps://ldap.example.com"},
	}
	_, mux, path := newAdminTestHandler(t, initial, []string{"todo"}, false)
	dir := filepath.Dir(path)

	otherDir := t.TempDir()
	unconfigured := filepath.Join(otherDir, "unconfigured.pin")

	cases := []struct {
		name string
		body map[string]string
	}{
		{"traversal name", map[string]string{"name": "../x", "pin": "111111"}},
		{"absolute path outside config dir, not configured", map[string]string{"path": unconfigured, "pin": "111111"}},
		{"whitespace pin", map[string]string{"name": "a.pin", "pin": "11 111"}},
		{"short pin", map[string]string{"name": "a.pin", "pin": "111"}},
		{"both name and path", map[string]string{"name": "a.pin", "path": filepath.Join(dir, "a.pin"), "pin": "111111"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			rec := doAdmin(t, mux, http.MethodPost, "/api/config/pin-files", c.body)
			if rec.Code != http.StatusBadRequest {
				t.Fatalf("POST /api/config/pin-files (%s) = %d, want 400; body: %s", c.name, rec.Code, rec.Body.String())
			}
			if pin, ok := c.body["pin"]; ok && strings.Contains(rec.Body.String(), pin) {
				t.Errorf("400 body echoed the submitted pin: %s", rec.Body.String())
			}
		})
	}
}

func TestPostPinFilePathEqualToConfiguredModulePinFileOutsideConfigDirSucceeds(t *testing.T) {
	moduleDir := t.TempDir()
	configuredPinFile := filepath.Join(moduleDir, "configured.pin")
	if err := os.WriteFile(configuredPinFile, []byte("1234\n"), 0o400); err != nil {
		t.Fatalf("writing configured pin file fixture: %v", err)
	}
	initial := config.AuthConfig{
		DataDir: t.TempDir(),
		Modules: map[string]config.ModuleAuthConfig{"todo": {PinFile: configuredPinFile}},
		LDAP:    config.LDAPConfig{URL: "ldaps://ldap.example.com"},
	}
	_, mux, _ := newAdminTestHandler(t, initial, []string{"todo"}, false)

	rec := doAdmin(t, mux, http.MethodPost, "/api/config/pin-files", map[string]string{"path": configuredPinFile, "pin": "999999"})
	if rec.Code != http.StatusOK {
		t.Fatalf("POST /api/config/pin-files (configured path outside config dir) = %d, want 200; body: %s", rec.Code, rec.Body.String())
	}

	data, err := os.ReadFile(configuredPinFile)
	if err != nil {
		t.Fatalf("reading rewritten pin file: %v", err)
	}
	if string(data) != "999999\n" {
		t.Errorf("file content = %q, want %q", data, "999999\n")
	}
}

func TestPostPinFileNeverEchoesPin(t *testing.T) {
	_, mux, _ := newAdminTestHandler(t, config.AuthConfig{}, nil, false)
	const secretPin = "8675309secret"

	rec := doAdmin(t, mux, http.MethodPost, "/api/config/pin-files", map[string]string{"name": "../escape.pin", "pin": secretPin})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("POST /api/config/pin-files = %d, want 400; body: %s", rec.Code, rec.Body.String())
	}
	if strings.Contains(rec.Body.String(), secretPin) {
		t.Errorf("error response leaked the submitted pin: %s", rec.Body.String())
	}
}
