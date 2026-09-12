package config_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"cmd184psu/unified-webapp/internal/platform/config"
)

func TestExpandPath_NoTilde(t *testing.T) {
	got, err := config.ExpandPath("/absolute/path")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "/absolute/path" {
		t.Errorf("got %q, want %q", got, "/absolute/path")
	}
}

func TestExpandPath_Empty(t *testing.T) {
	got, err := config.ExpandPath("")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "" {
		t.Errorf("got %q, want empty", got)
	}
}

func TestExpandPath_Tilde(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil {
		t.Skip("cannot determine home dir")
	}
	got, err := config.ExpandPath("~/foo/bar")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := filepath.Join(home, "foo/bar")
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestLoadMissingFile_ReturnsDefaults(t *testing.T) {
	cfg, err := config.Load(filepath.Join(t.TempDir(), "nonexistent.json"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Port != 8080 {
		t.Errorf("default port: got %d, want 8080", cfg.Port)
	}
	if cfg.Grocery.SyncIntervalSeconds != 1 {
		t.Errorf("default sync interval: got %d, want 1", cfg.Grocery.SyncIntervalSeconds)
	}
	if cfg.Grocery.Title != "Grocery List" {
		t.Errorf("default title: got %q, want %q", cfg.Grocery.Title, "Grocery List")
	}
	if len(cfg.Grocery.Groups) == 0 {
		t.Error("default groups should be non-empty")
	}
}

func TestLoadFile_OverridesDefaults(t *testing.T) {
	dir := t.TempDir()
	cfgFile := filepath.Join(dir, "test.json")

	data, _ := json.Marshal(map[string]any{
		"port": 9090,
		"grocery": map[string]any{
			"title":    "Test List",
			"data_dir": dir,
		},
	})
	if err := os.WriteFile(cfgFile, data, 0644); err != nil {
		t.Fatalf("write: %v", err)
	}

	cfg, err := config.Load(cfgFile)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if cfg.Port != 9090 {
		t.Errorf("port: got %d, want 9090", cfg.Port)
	}
	if cfg.Grocery.Title != "Test List" {
		t.Errorf("title: got %q, want %q", cfg.Grocery.Title, "Test List")
	}
}

func TestLoadMissingFile_MenuserverDefaults(t *testing.T) {
	cfg, err := config.Load(filepath.Join(t.TempDir(), "nonexistent.json"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Menuserver.StaticDir == "" {
		t.Error("menuserver static_dir should not be empty")
	}
	if cfg.Menuserver.DataDir == "" {
		t.Error("menuserver data_dir should not be empty")
	}
}

func TestLoadMissingFile_SlideshowDefaults(t *testing.T) {
	cfg, err := config.Load(filepath.Join(t.TempDir(), "nonexistent.json"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Slideshow.Prefix != "slides" {
		t.Errorf("slideshow prefix: got %q, want %q", cfg.Slideshow.Prefix, "slides")
	}
	if cfg.Slideshow.StaticDir == "" {
		t.Error("slideshow static_dir should not be empty")
	}
}

func TestLoadMissingFile_TodoDefaults(t *testing.T) {
	cfg, err := config.Load(filepath.Join(t.TempDir(), "nonexistent.json"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Todo.Ext != "json" {
		t.Errorf("todo ext: got %q, want %q", cfg.Todo.Ext, "json")
	}
	if cfg.Todo.DefaultSubject != "home" {
		t.Errorf("todo defaultSubject: got %q, want %q", cfg.Todo.DefaultSubject, "home")
	}
	if cfg.Todo.SyncIntervalSeconds != 1 {
		t.Errorf("todo sync interval: got %d, want 1", cfg.Todo.SyncIntervalSeconds)
	}
}

func TestWriteDefault_CreatesFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "out.json")
	if err := config.WriteDefault(path); err != nil {
		t.Fatalf("WriteDefault: %v", err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("file not created: %v", err)
	}
}

// TestWriteDefault_FileMode asserts the written config file carries no
// group/other permission bits (FR-R4). Exact equality with 0600 is not
// asserted because umask can only clear bits from the requested mode, never
// set them, so mode&0077==0 is the safe, umask-independent check.
func TestWriteDefault_FileMode(t *testing.T) {
	path := filepath.Join(t.TempDir(), "out.json")
	if err := config.WriteDefault(path); err != nil {
		t.Fatalf("WriteDefault: %v", err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if mode := info.Mode().Perm(); mode&0o077 != 0 {
		t.Errorf("config file mode = %o, want no group/other bits (owner-only)", mode)
	}
}

// --- S1: two-state auth.modules ---

func TestLoad_ModulesTwoState(t *testing.T) {
	dir := t.TempDir()
	cfgFile := filepath.Join(dir, "test.json")
	body := `{
		"auth": {
			"modules": {
				"menuserver": {},
				"todo": {"pin_file": "./todo.pin"}
			}
		}
	}`
	if err := os.WriteFile(cfgFile, []byte(body), 0644); err != nil {
		t.Fatalf("write: %v", err)
	}

	cfg, err := config.Load(cfgFile)
	if err != nil {
		t.Fatalf("load: %v", err)
	}

	if _, ok := cfg.Auth.Modules["menuserver"]; !ok {
		t.Error("menuserver should be present (protected) in Modules")
	}
	if cfg.Auth.Modules["menuserver"].PinFile != "" {
		t.Errorf("menuserver pin_file: got %q, want empty", cfg.Auth.Modules["menuserver"].PinFile)
	}
	todo, ok := cfg.Auth.Modules["todo"]
	if !ok {
		t.Fatal("todo should be present (protected) in Modules")
	}
	if want := filepath.Join(dir, "todo.pin"); todo.PinFile != want {
		t.Errorf("todo pin_file: got %q, want %q", todo.PinFile, want)
	}
	if _, ok := cfg.Auth.Modules["obsidianoid"]; ok {
		t.Error("obsidianoid absent from config should stay absent (open)")
	}
}

func TestLoad_ModulesLegacyArray_Errors(t *testing.T) {
	dir := t.TempDir()
	cfgFile := filepath.Join(dir, "test.json")
	body := `{"auth": {"modules": {"todo": ["ldap", "pin"]}}}`
	if err := os.WriteFile(cfgFile, []byte(body), 0644); err != nil {
		t.Fatalf("write: %v", err)
	}

	_, err := config.Load(cfgFile)
	if err == nil {
		t.Fatal("expected error for legacy module method-list array, got nil")
	}
	if !strings.Contains(err.Error(), "auth.modules.todo") {
		t.Errorf("error should name the module (auth.modules.todo): %v", err)
	}
	if !strings.Contains(err.Error(), "per-module method lists were removed") {
		t.Errorf("error should carry the new-syntax message: %v", err)
	}
}

func TestLoad_LegacyPins_Errors(t *testing.T) {
	dir := t.TempDir()
	cfgFile := filepath.Join(dir, "test.json")
	body := `{"auth": {"pins": [{"name": "alice", "hash": "x"}]}}`
	if err := os.WriteFile(cfgFile, []byte(body), 0644); err != nil {
		t.Fatalf("write: %v", err)
	}

	_, err := config.Load(cfgFile)
	if err == nil {
		t.Fatal("expected error for legacy auth.pins key, got nil")
	}
	want := "auth.pins was removed; identity comes from LDAP, door codes are per-module pin_file"
	if !strings.Contains(err.Error(), want) {
		t.Errorf("error = %v, want to contain %q", err, want)
	}
}

func TestLoad_LiteralPinsNull_Errors(t *testing.T) {
	dir := t.TempDir()
	cfgFile := filepath.Join(dir, "test.json")
	body := `{"auth": {"pins": null}}`
	if err := os.WriteFile(cfgFile, []byte(body), 0644); err != nil {
		t.Fatalf("write: %v", err)
	}

	_, err := config.Load(cfgFile)
	if err == nil {
		t.Fatal("expected error for literal \"pins\": null, got nil")
	}
	want := "auth.pins was removed; identity comes from LDAP, door codes are per-module pin_file"
	if !strings.Contains(err.Error(), want) {
		t.Errorf("error = %v, want to contain %q", err, want)
	}
}

func TestLoad_ModulePinFile_TildeExpansion(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil {
		t.Skip("cannot determine home dir")
	}
	dir := t.TempDir()
	cfgFile := filepath.Join(dir, "test.json")
	body := `{"auth": {"modules": {"todo": {"pin_file": "~/todo.pin"}}}}`
	if err := os.WriteFile(cfgFile, []byte(body), 0644); err != nil {
		t.Fatalf("write: %v", err)
	}

	cfg, err := config.Load(cfgFile)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	want := filepath.Join(home, "todo.pin")
	if got := cfg.Auth.Modules["todo"].PinFile; got != want {
		t.Errorf("pin_file: got %q, want %q", got, want)
	}
}

func TestLoad_ModulePinFile_AbsoluteUnchanged(t *testing.T) {
	dir := t.TempDir()
	cfgFile := filepath.Join(dir, "test.json")
	body := `{"auth": {"modules": {"todo": {"pin_file": "/etc/todo.pin"}}}}`
	if err := os.WriteFile(cfgFile, []byte(body), 0644); err != nil {
		t.Fatalf("write: %v", err)
	}

	cfg, err := config.Load(cfgFile)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if got := cfg.Auth.Modules["todo"].PinFile; got != "/etc/todo.pin" {
		t.Errorf("pin_file: got %q, want unchanged absolute path", got)
	}
}

func TestLoad_ModulesAdminPinFile_PopulatesAdminSource(t *testing.T) {
	dir := t.TempDir()
	cfgFile := filepath.Join(dir, "test.json")
	body := `{"auth": {"modules": {"admin": {"pin_file": "./admin.pin"}}}}`
	if err := os.WriteFile(cfgFile, []byte(body), 0644); err != nil {
		t.Fatalf("write: %v", err)
	}

	cfg, err := config.Load(cfgFile)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	admin, ok := cfg.Auth.Modules["admin"]
	if !ok {
		t.Fatal("admin should be present in Modules")
	}
	want := filepath.Join(dir, "admin.pin")
	if admin.PinFile != want {
		t.Errorf("modules.admin.pin_file: got %q, want %q (expanded relative to config dir)", admin.PinFile, want)
	}
}

// TestAuthConfig_PinsMarshalInvisible is the named marshal-invisibility
// round-trip test (PLAN-auth-two-state.md S1 verification): unmarshal a
// valid pins-free config, marshal it back, and assert the output carries no
// "pins" key and re-loads cleanly. This guards the admin live-apply path,
// which re-marshals AuthConfig into config.json -- a stray "pins": null in
// that output would brick the next boot on the removal error.
func TestAuthConfig_PinsMarshalInvisible(t *testing.T) {
	dir := t.TempDir()
	cfgFile := filepath.Join(dir, "test.json")
	body := `{
		"port": 9191,
		"auth": {
			"modules": {"todo": {"pin_file": "./todo.pin"}},
			"admin_pin": "1234"
		}
	}`
	if err := os.WriteFile(cfgFile, []byte(body), 0644); err != nil {
		t.Fatalf("write: %v", err)
	}

	cfg, err := config.Load(cfgFile)
	if err != nil {
		t.Fatalf("load: %v", err)
	}

	out, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if bytesContainsPinsKey(out) {
		t.Errorf("marshaled config must not contain a \"pins\" key, got:\n%s", out)
	}

	// Re-load the written output to prove it boots cleanly.
	reloadPath := filepath.Join(dir, "reload.json")
	if err := os.WriteFile(reloadPath, out, 0644); err != nil {
		t.Fatalf("write reload: %v", err)
	}
	if _, err := config.Load(reloadPath); err != nil {
		t.Fatalf("re-loading marshaled config failed: %v", err)
	}
}

func bytesContainsPinsKey(data []byte) bool {
	return strings.Contains(string(data), `"pins"`)
}

func TestWriteDefault_Idempotent(t *testing.T) {
	path := filepath.Join(t.TempDir(), "out.json")
	if err := config.WriteDefault(path); err != nil {
		t.Fatalf("first WriteDefault: %v", err)
	}
	// Overwrite with custom content.
	if err := os.WriteFile(path, []byte(`{"port":1234}`), 0644); err != nil {
		t.Fatalf("overwrite: %v", err)
	}
	// Second call must not overwrite.
	if err := config.WriteDefault(path); err != nil {
		t.Fatalf("second WriteDefault: %v", err)
	}
	data, _ := os.ReadFile(path)
	var cfg config.Config
	if err := json.Unmarshal(data, &cfg); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if cfg.Port != 1234 {
		t.Errorf("WriteDefault overwrote existing file: port=%d", cfg.Port)
	}
}

// Regression: when the server is started with a relative -config path (e.g.
// "local-test/config.json"), boot expansion used to produce a merely-joined
// relative path ("local-test/admin.pin"); the admin live-apply pipeline then
// re-expanded that cached value against the same relative baseDir, yielding
// "local-test/local-test/admin.pin" and a stat failure on every mutation
// (first seen generating an API key). ExpandRelativeTo must return absolute
// paths so expansion is idempotent.
func TestExpandRelativeTo_AbsoluteAndIdempotent(t *testing.T) {
	got, err := config.ExpandRelativeTo("./admin.pin", "local-test")
	if err != nil {
		t.Fatalf("ExpandRelativeTo: %v", err)
	}
	if !filepath.IsAbs(got) {
		t.Fatalf("ExpandRelativeTo returned non-absolute path %q for relative baseDir", got)
	}
	again, err := config.ExpandRelativeTo(got, "local-test")
	if err != nil {
		t.Fatalf("ExpandRelativeTo (re-expansion): %v", err)
	}
	if again != got {
		t.Fatalf("ExpandRelativeTo not idempotent: first %q, re-expanded %q", got, again)
	}
	if strings.Contains(again, "local-test/local-test") {
		t.Fatalf("double-joined path: %q", again)
	}
}
