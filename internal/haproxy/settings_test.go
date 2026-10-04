package haproxy

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"cmd184psu/unified-webapp/internal/platform/config"
)

// optDriver is the fake driver honoring DriverOptions path overrides, so a
// live settings change is observable through the driver's reported paths.
type optDriver struct {
	*fakeDriver
	o DriverOptions
}

func pick(v, def string) string {
	if v != "" {
		return v
	}
	return def
}
func (d optDriver) ConfigPath() string  { return pick(d.o.ConfigPath, d.fakeDriver.ConfigPath()) }
func (d optDriver) CertsDir() string    { return pick(d.o.CertsDir, d.fakeDriver.CertsDir()) }
func (d optDriver) CrtListPath() string { return pick(d.o.CrtListPath, d.fakeDriver.CrtListPath()) }
func (d optDriver) BackupDir() string   { return pick(d.o.BackupDir, d.fakeDriver.BackupDir()) }
func (d optDriver) ServiceName() string { return pick(d.o.ServiceName, d.fakeDriver.ServiceName()) }
func (d optDriver) StatsSocketPath() string {
	return pick(d.o.StatsSocketPath, d.fakeDriver.StatsSocketPath())
}

// newSX is newHX with the settings plumbing live: the server rebuilds its
// driver through optDriver over the one fake.
func newSX(t *testing.T, withCM bool) (*hx, *server) {
	t.Helper()
	base := newHX(t, withCM)
	var client *CertMachineClient
	if withCM {
		srv := base.cm.start(t)
		var err error
		client, err = CertMachineNewClient(CertMachineSettings{URL: srv.URL, APIKey: CertMachineAPIKey(testCMKey)})
		if err != nil {
			t.Fatal(err)
		}
		base.cfg.CertMachine.URL = srv.URL
	}
	s, err := buildServer(base.cfg, base.drv, client)
	if err != nil {
		t.Fatalf("buildServer: %v", err)
	}
	s.mk = func(_ OS, o DriverOptions) (Driver, error) { return optDriver{base.drv, o}, nil }
	base.h = s.routes(base.cfg.StaticDir)
	return base, s
}

type settingsGet struct {
	Effective             map[string]any    `json:"effective"`
	Defaults              map[string]string `json:"defaults"`
	APIKeySet             bool              `json:"apiKeySet"`
	CertMachineConfigured bool              `json:"certmachineConfigured"`
	Unavailable           string            `json:"unavailable"`
}

func (x *hx) getSettings() (settingsGet, string) {
	x.t.Helper()
	rec := x.do("GET", "/api/settings", "")
	if rec.Code != 200 {
		x.t.Fatalf("GET /api/settings: %d %s", rec.Code, rec.Body.String())
	}
	var g settingsGet
	if err := json.Unmarshal(rec.Body.Bytes(), &g); err != nil {
		x.t.Fatal(err)
	}
	return g, rec.Body.String()
}

// put sends the full effective set with mod applied.
func (x *hx) put(mod func(m map[string]any)) *httptest.ResponseRecorder {
	x.t.Helper()
	g, _ := x.getSettings()
	m := map[string]any{"apiKey": "", "clearApiKey": false}
	for k, v := range g.Effective {
		m[k] = v
	}
	mod(m)
	b, _ := json.Marshal(m)
	return x.do("PUT", "/api/settings", string(b))
}

func (x *hx) settingsFile() string { return filepath.Join(x.dataDir, "settings.json") }

func TestSettingsGetNeverLeaksKeyAndShowsDefaults(t *testing.T) {
	x, _ := newSX(t, true)
	g, raw := x.getSettings()
	if strings.Contains(raw, testCMKey) {
		t.Fatalf("GET /api/settings leaks the api key: %s", raw)
	}
	if !g.APIKeySet || !g.CertMachineConfigured {
		t.Errorf("apiKeySet=%v certmachineConfigured=%v, want true/true", g.APIKeySet, g.CertMachineConfigured)
	}
	for _, k := range []string{"configPath", "certsDir", "crtListPath", "statsSocketPath", "backupDir", "serviceName"} {
		if g.Defaults[k] == "" {
			t.Errorf("defaults[%s] empty", k)
		}
	}
	if g.Defaults["certsDir"] != "/etc/haproxy/certs" {
		t.Errorf("defaults.certsDir = %q", g.Defaults["certsDir"])
	}
	if g.Effective["backupKeep"].(float64) != float64(config.DefaultHaproxyBackupKeep) || g.Effective["expiryWarnDays"].(float64) != 30 {
		t.Errorf("effective numbers = %v / %v", g.Effective["backupKeep"], g.Effective["expiryWarnDays"])
	}
}

func TestSettingsPutPersistsAndChangesEffective(t *testing.T) {
	x, _ := newSX(t, false)
	rec := x.put(func(m map[string]any) {
		m["backupKeep"] = 7
		m["serviceName"] = "haproxy-edge"
		m["apiKey"] = "new-key-123"
	})
	if rec.Code != 200 {
		t.Fatalf("PUT: %d %s", rec.Code, rec.Body.String())
	}
	if strings.Contains(rec.Body.String(), "new-key-123") {
		t.Fatal("PUT response leaks the api key")
	}
	st, err := os.Stat(x.settingsFile())
	if err != nil {
		t.Fatal(err)
	}
	if st.Mode().Perm() != 0o600 {
		t.Errorf("settings.json mode = %v, want 0600", st.Mode().Perm())
	}
	var stored map[string]any
	b, _ := os.ReadFile(x.settingsFile())
	_ = json.Unmarshal(b, &stored)
	if stored["backup_keep"].(float64) != 7 || stored["service_name"] != "haproxy-edge" || stored["api_key"] != "new-key-123" {
		t.Errorf("stored = %v", stored)
	}
	if _, has := stored["certs_dir"]; has {
		t.Errorf("unchanged field was stored: %v", stored)
	}
	g, raw := x.getSettings()
	if g.Effective["backupKeep"].(float64) != 7 || g.Effective["serviceName"] != "haproxy-edge" {
		t.Errorf("effective = %v", g.Effective)
	}
	if strings.Contains(raw, "new-key-123") || !g.APIKeySet {
		t.Errorf("key leaked or not reported set: %s", raw)
	}
}

func TestSettingsPutValidationFailures(t *testing.T) {
	cases := []struct {
		name string
		mod  func(m map[string]any)
		want string
	}{
		{"http non-loopback", func(m map[string]any) { m["certmachineUrl"] = "http://cm.example.com" }, "https"},
		{"unparsable url", func(m map[string]any) { m["certmachineUrl"] = "://nope" }, "CertMachine URL"},
		{"url no host", func(m map[string]any) { m["certmachineUrl"] = "https://" }, "CertMachine URL"},
		{"ca missing", func(m map[string]any) {
			m["certmachineUrl"] = "https://cm.example.com"
			m["certmachineCaFile"] = "/nonexistent/ca.pem"
		}, "CA file"},
		{"ca not a cert", func(m map[string]any) {
			f := filepath.Join(t.TempDir(), "ca.pem")
			_ = os.WriteFile(f, []byte("not a cert"), 0o600)
			m["certmachineUrl"] = "https://cm.example.com"
			m["certmachineCaFile"] = f
		}, "CA file"},
		{"relative path", func(m map[string]any) { m["certsDir"] = "certs" }, "absolute"},
		{"dotdot", func(m map[string]any) { m["backupDir"] = "/var/../etc/x" }, ".."},
		{"control char", func(m map[string]any) { m["configPath"] = "/etc/ha\nproxy.cfg" }, "control"},
		{"service name", func(m map[string]any) { m["serviceName"] = "ha proxy; rm" }, "service name"},
		{"keep low", func(m map[string]any) { m["backupKeep"] = 0 }, "between 1 and 100"},
		{"keep high", func(m map[string]any) { m["backupKeep"] = 101 }, "between 1 and 100"},
		{"warn low", func(m map[string]any) { m["expiryWarnDays"] = 0 }, "between 1 and 365"},
		{"warn high", func(m map[string]any) { m["expiryWarnDays"] = 366 }, "between 1 and 365"},
		{"bad os", func(m map[string]any) { m["os"] = "windows" }, "operating system"},
		{"key and clear", func(m map[string]any) { m["apiKey"] = "k"; m["clearApiKey"] = true }, "not both"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			x, _ := newSX(t, false)
			before, _ := x.getSettings()
			rec := x.put(c.mod)
			if rec.Code != 400 {
				t.Fatalf("status %d, want 400: %s", rec.Code, rec.Body.String())
			}
			var e struct {
				Error string `json:"error"`
			}
			_ = json.Unmarshal(rec.Body.Bytes(), &e)
			if !strings.Contains(e.Error, c.want) {
				t.Errorf("error %q does not contain %q", e.Error, c.want)
			}
			if strings.Contains(strings.ToLower(e.Error), "edit") || strings.Contains(e.Error, ".json") {
				t.Errorf("error tells the user to edit a file: %q", e.Error)
			}
			if _, err := os.Stat(x.settingsFile()); err == nil {
				t.Error("a failed validation wrote settings.json")
			}
			after, _ := x.getSettings()
			a, _ := json.Marshal(after)
			b, _ := json.Marshal(before)
			if string(a) != string(b) {
				t.Errorf("a failed validation changed the settings:\n%s\n%s", b, a)
			}
		})
	}
}

func TestSettingsApiKeyKeepAndClear(t *testing.T) {
	x, s := newSX(t, true)
	// empty apiKey keeps the key (the CertMachine routes still work)
	if rec := x.put(func(m map[string]any) { m["backupKeep"] = 5 }); rec.Code != 200 {
		t.Fatalf("PUT: %d %s", rec.Code, rec.Body.String())
	}
	if g, _ := x.getSettings(); !g.APIKeySet {
		t.Fatal("empty apiKey cleared the key")
	}
	if got := s.rt().client.settings.APIKey; string(got) != testCMKey {
		t.Fatalf("live client key changed to %q", string(got))
	}
	// a new key replaces it
	x.cm.expectBearer = "rotated-key"
	if rec := x.put(func(m map[string]any) { m["apiKey"] = "rotated-key" }); rec.Code != 200 {
		t.Fatalf("PUT: %d", rec.Code)
	}
	x.json("GET", "/api/certmachine/certs", "", 200, nil)
	// clearApiKey removes it
	if rec := x.put(func(m map[string]any) { m["clearApiKey"] = true }); rec.Code != 200 {
		t.Fatalf("PUT clear: %d %s", rec.Code, rec.Body.String())
	}
	g, _ := x.getSettings()
	if g.APIKeySet {
		t.Error("apiKeySet still true after clearApiKey")
	}
	x.json("GET", "/api/certmachine/certs", "", 502, nil) // CertMachine now answers 401
	if b, _ := os.ReadFile(x.settingsFile()); strings.Contains(string(b), "rotated-key") {
		t.Error("cleared key still in settings.json")
	}
}

func TestSettingsLiveApply(t *testing.T) {
	x, s := newSX(t, true)
	x.cm.addCert(1, "a.example.com", "fp1", "active", []string{"a.example.com"})
	rec := x.put(func(m map[string]any) { m["certsDir"] = "/srv/certs2"; m["expiryWarnDays"] = 12 })
	if rec.Code != 200 {
		t.Fatalf("PUT: %d %s", rec.Code, rec.Body.String())
	}
	var out struct {
		NeedsRestart bool `json:"needsRestart"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &out)
	if out.NeedsRestart {
		t.Error("needsRestart true for a live-applicable change")
	}
	if got := s.rt().driver.CertsDir(); got != "/srv/certs2" {
		t.Fatalf("driver certs dir = %q", got)
	}
	if s.rt().warnDays != 12 {
		t.Errorf("warnDays = %d", s.rt().warnDays)
	}
	name, _ := x.pull("1")
	if _, ok := x.drv.Files[filepath.Join("/srv/certs2", name)]; !ok {
		t.Fatalf("pull did not write under the new certs dir; files: %v", keys(x.drv.Files))
	}
	var list struct {
		Certs []struct{ Name string } `json:"certs"`
	}
	x.json("GET", "/api/certs", "", 200, &list)
	if len(list.Certs) != 1 {
		t.Fatalf("certs = %+v", list.Certs)
	}
}

func keys(m map[string][]byte) []string {
	var out []string
	for k := range m {
		out = append(out, k)
	}
	return out
}

func TestSettingsUnsupportedOSKeepsScoped503(t *testing.T) {
	x, _ := newSX(t, false)
	oldOS, oldArch := hostGOOS, hostGOARCH
	hostGOOS, hostGOARCH = "darwin", "amd64"
	defer func() { hostGOOS, hostGOARCH = oldOS, oldArch }()
	if rec := x.put(func(m map[string]any) { m["os"] = "macos" }); rec.Code != 200 {
		t.Fatalf("PUT os: %d %s", rec.Code, rec.Body.String())
	}
	rec := x.do("GET", "/api/status", "")
	if rec.Code != 503 || !strings.Contains(rec.Body.String(), "Intel macOS") {
		t.Fatalf("status = %d %s", rec.Code, rec.Body.String())
	}
	g, _ := x.getSettings()
	if !strings.Contains(g.Unavailable, "Intel macOS") {
		t.Errorf("unavailable = %q", g.Unavailable)
	}
	// and it recovers
	if rec := x.put(func(m map[string]any) { m["os"] = "rocky" }); rec.Code != 200 {
		t.Fatalf("PUT os back: %d %s", rec.Code, rec.Body.String())
	}
	x.json("GET", "/api/status", "", 200, nil)
}

func testConn(x *hx, body string) (int, struct {
	OK      bool   `json:"ok"`
	Message string `json:"message"`
	Class   string `json:"class"`
}) {
	var out struct {
		OK      bool   `json:"ok"`
		Message string `json:"message"`
		Class   string `json:"class"`
	}
	rec := x.do("POST", "/api/settings/test-connection", body)
	_ = json.Unmarshal(rec.Body.Bytes(), &out)
	return rec.Code, out
}

func TestSettingsTestConnection(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		x, _ := newSX(t, true)
		x.cm.addCert(1, "a.example.com", "fp1", "active", nil)
		x.cm.addCert(2, "b.example.com", "fp2", "active", nil)
		x.cm.addCert(3, "c.example.com", "fp3", "archived", nil)
		code, out := testConn(x, "")
		if code != 200 || !out.OK || out.Message != "Connected: 2 active certs" {
			t.Fatalf("%d %+v", code, out)
		}
	})
	t.Run("not configured", func(t *testing.T) {
		x, _ := newSX(t, false)
		code, out := testConn(x, "")
		if code != 200 || out.OK || !strings.Contains(out.Message, "not configured") {
			t.Fatalf("%d %+v", code, out)
		}
	})
	t.Run("unauthorized", func(t *testing.T) {
		x, _ := newSX(t, true)
		x.cm.expectBearer = "something-else"
		_, out := testConn(x, "")
		if out.OK || out.Class != "unauthorized" || strings.Contains(out.Message, testCMKey) {
			t.Fatalf("%+v", out)
		}
	})
	t.Run("unreachable", func(t *testing.T) {
		x, _ := newSX(t, true)
		srv := httptest.NewServer(http.NotFoundHandler())
		url := srv.URL
		srv.Close()
		_, out := testConn(x, `{"certmachineUrl":"`+url+`"}`)
		if out.OK || out.Class != "unreachable" || strings.Contains(out.Message, testCMKey) {
			t.Fatalf("%+v", out)
		}
	})
	t.Run("tls", func(t *testing.T) {
		x, _ := newSX(t, true)
		srv := httptest.NewTLSServer(http.NotFoundHandler())
		defer srv.Close()
		_, out := testConn(x, `{"certmachineUrl":"`+srv.URL+`"}`)
		if out.OK || out.Class != "tls" {
			t.Fatalf("%+v", out)
		}
	})
	t.Run("server error", func(t *testing.T) {
		x, _ := newSX(t, true)
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			http.Error(w, "boom", 500)
		}))
		defer srv.Close()
		_, out := testConn(x, `{"certmachineUrl":"`+srv.URL+`"}`)
		if out.OK || out.Class != "server" {
			t.Fatalf("%+v", out)
		}
	})
	t.Run("bad unsaved url is a plain 400", func(t *testing.T) {
		x, _ := newSX(t, true)
		rec := x.do("POST", "/api/settings/test-connection", `{"certmachineUrl":"http://cm.example.com"}`)
		if rec.Code != 400 || !strings.Contains(rec.Body.String(), "https") {
			t.Fatalf("%d %s", rec.Code, rec.Body.String())
		}
	})
}

func TestSettingsFileOverridesConfigBlock(t *testing.T) {
	cfg := goodConfig(t)
	cfg.CertsDir = "/block/certs"
	cfg.BackupKeep = 4
	cfg.ServiceName = "block-svc"
	cfg.CertMachine.APIKey = "block-key"
	get := func(h http.Handler) (settingsGet, string) {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest("GET", "/api/settings", nil))
		var g settingsGet
		_ = json.Unmarshal(rec.Body.Bytes(), &g)
		return g, rec.Body.String()
	}
	// absent settings.json: the block is the effective settings
	h, err := Build(cfg)
	if err != nil {
		t.Fatal(err)
	}
	g, raw := get(h)
	if g.Effective["certsDir"] != "/block/certs" || g.Effective["backupKeep"].(float64) != 4 || !g.APIKeySet {
		t.Fatalf("block not used: %s", raw)
	}
	if strings.Contains(raw, "block-key") {
		t.Fatal("leaks block key")
	}
	// present settings.json overrides only what it holds
	if err := os.WriteFile(filepath.Join(cfg.DataDir, "settings.json"), []byte(`{"certs_dir":"/file/certs","backup_keep":9}`), 0o600); err != nil {
		t.Fatal(err)
	}
	h, err = Build(cfg)
	if err != nil {
		t.Fatal(err)
	}
	g, _ = get(h)
	if g.Effective["certsDir"] != "/file/certs" || g.Effective["backupKeep"].(float64) != 9 || g.Effective["serviceName"] != "block-svc" {
		t.Fatalf("file did not override: %v", g.Effective)
	}
}

func TestSettingsConcurrentPutsDoNotRace(t *testing.T) {
	x, _ := newSX(t, true)
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			for j := 0; j < 10; j++ {
				for _, p := range []string{"/api/certs", "/api/status", "/api/changes", "/api/settings", "/api/coverage"} {
					x.do("GET", p, "")
				}
				if i%4 == 0 {
					dir := "/srv/a"
					if j%2 == 1 {
						dir = "/srv/b"
					}
					b, _ := json.Marshal(map[string]any{
						"os": "rocky", "certmachineUrl": x.cfg.CertMachine.URL, "certmachineCaFile": "",
						"configPath": "", "certsDir": dir, "crtListPath": "", "statsSocketPath": "", "backupDir": "",
						"serviceName": "", "backupKeep": 10, "expiryWarnDays": 30, "apiKey": "", "clearApiKey": false,
					})
					if rec := x.do("PUT", "/api/settings", string(b)); rec.Code != 200 {
						t.Errorf("PUT: %d %s", rec.Code, rec.Body.String())
					}
				}
			}
		}(i)
	}
	wg.Wait()
}
