package admin

import (
	"bytes"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"cmd184psu/unified-webapp/internal/platform/auth"
	"cmd184psu/unified-webapp/internal/platform/config"
)

// --- fixtures ---

// writeHandlerFixtureConfig writes a minimal config file with a "port"
// sibling member alongside "auth": initial, so AC-14-style byte-identity
// checks below have something outside "auth" to prove untouched.
func writeHandlerFixtureConfig(t *testing.T, initial config.AuthConfig) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	doc := struct {
		Port int               `json:"port"`
		Auth config.AuthConfig `json:"auth"`
	}{Port: 8080, Auth: initial}
	data, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		t.Fatalf("marshaling fixture config: %v", err)
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatalf("writing fixture config: %v", err)
	}
	return path
}

// newAdminTestHandler builds a *Handler wired to a real *auth.Service (via
// auth.FromConfig, exactly as boot does) and to a real temp config file on
// disk, plus the *http.ServeMux Register wires it onto -- so every test
// below drives the actual applyAuth pipeline (T5.3), never a fake.
func newAdminTestHandler(t *testing.T, initial config.AuthConfig, knownModules []string, adminRouted bool) (*Handler, *http.ServeMux, string) {
	t.Helper()
	path := writeHandlerFixtureConfig(t, initial)
	svc, err := auth.FromConfig(initial, knownModules, adminRouted)
	if err != nil {
		t.Fatalf("auth.FromConfig: %v", err)
	}
	h := NewHandler(config.AdminConfig{}, initial, Deps{
		Service:      svc,
		ConfigPath:   path,
		KnownModules: knownModules,
		AdminRouted:  adminRouted,
	})
	mux := http.NewServeMux()
	h.Register(mux)
	return h, mux, path
}

// doAdmin issues method/path against mux with bodyVal JSON-encoded as the
// request body (nil for no body), returning the recorder.
func doAdmin(t *testing.T, mux *http.ServeMux, method, path string, bodyVal any) *httptest.ResponseRecorder {
	t.Helper()
	var body *bytes.Buffer
	if bodyVal != nil {
		data, err := json.Marshal(bodyVal)
		if err != nil {
			t.Fatalf("marshaling request body: %v", err)
		}
		body = bytes.NewBuffer(data)
	} else {
		body = bytes.NewBuffer(nil)
	}
	req := httptest.NewRequest(method, path, body)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	return rec
}

// deadTCPAddr returns a "host:port" with nothing listening on it (a real
// listener is opened and immediately closed, so the port is guaranteed free
// but any dial to it gets an actual connection-refused from the kernel,
// not a fabricated error).
func deadTCPAddr(t *testing.T) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("net.Listen: %v", err)
	}
	addr := ln.Addr().String()
	if err := ln.Close(); err != nil {
		t.Fatalf("closing listener: %v", err)
	}
	return addr
}

// --- GET /api/config/auth: redaction shape ---

func TestGetConfigAuthRedactsHashesAndBindPassword(t *testing.T) {
	initial := config.AuthConfig{
		DataDir:  t.TempDir(),
		Modules:  map[string][]string{"todo": {"pin"}},
		PINs:     []config.NamedHash{{Name: "alice", Hash: bcryptHash(t, "1234")}},
		APIKeys:  []config.NamedHash{{Name: "svc1", Hash: "sha256:deadbeefdeadbeef"}},
		AdminPIN: bcryptHash(t, "opPIN"),
		LDAP:     config.LDAPConfig{URL: "ldaps://dc.example.com", BindDN: "cn=svc", BindPassword: "topsecret"},
	}
	_, mux, _ := newAdminTestHandler(t, initial, []string{"todo"}, false)

	rec := doAdmin(t, mux, http.MethodGet, "/api/config/auth", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /api/config/auth = %d, want 200; body: %s", rec.Code, rec.Body.String())
	}

	body := rec.Body.String()
	if strings.Contains(body, "1234") || strings.Contains(body, "opPIN") || strings.Contains(body, "topsecret") {
		t.Fatalf("GET /api/config/auth leaked a plaintext secret: %s", body)
	}
	if strings.Contains(body, "$2a$") || strings.Contains(body, "$2b$") {
		t.Fatalf("GET /api/config/auth leaked a bcrypt hash: %s", body)
	}
	if strings.Contains(body, "deadbeefdeadbeef") {
		t.Fatalf("GET /api/config/auth leaked an api-key hash: %s", body)
	}
	// The matrix editor needs every routable module, not just protected
	// ones, so the view carries deps.KnownModules verbatim.
	if !strings.Contains(body, `"known_modules":["todo"]`) {
		t.Fatalf("GET /api/config/auth missing known_modules: %s", body)
	}

	var got authConfigView
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decoding response: %v\nbody: %s", err, body)
	}
	if len(got.PINs) != 1 || got.PINs[0].Name != "alice" || got.PINs[0].Hash != "(set)" {
		t.Errorf("PINs = %+v, want [{alice (set)}]", got.PINs)
	}
	if len(got.APIKeys) != 1 || got.APIKeys[0].Name != "svc1" || got.APIKeys[0].Hash != "(set)" {
		t.Errorf("APIKeys = %+v, want [{svc1 (set)}]", got.APIKeys)
	}
	if got.AdminPIN.DefinedBy != "config" {
		t.Errorf("AdminPIN.DefinedBy = %q, want %q", got.AdminPIN.DefinedBy, "config")
	}
	if got.LDAP.BindPassword != "(set)" {
		t.Errorf("LDAP.BindPassword = %q, want %q", got.LDAP.BindPassword, "(set)")
	}
	if got.LDAP.BindDN != "cn=svc" {
		t.Errorf("LDAP.BindDN = %q, want %q (not a secret, should round-trip)", got.LDAP.BindDN, "cn=svc")
	}
}

func TestGetConfigAuthAdminPINDefinedByVariants(t *testing.T) {
	t.Run("none", func(t *testing.T) {
		_, mux, _ := newAdminTestHandler(t, config.AuthConfig{}, nil, false)
		rec := doAdmin(t, mux, http.MethodGet, "/api/config/auth", nil)
		var got authConfigView
		if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
			t.Fatalf("decoding response: %v", err)
		}
		if got.AdminPIN.DefinedBy != "none" || got.AdminPIN.Path != "" {
			t.Errorf("AdminPIN = %+v, want {none }", got.AdminPIN)
		}
	})

	t.Run("file", func(t *testing.T) {
		dir := t.TempDir()
		pinPath := filepath.Join(dir, "admin.pin")
		if err := os.WriteFile(pinPath, []byte("999999\n"), 0o400); err != nil {
			t.Fatalf("writing pin file: %v", err)
		}
		initial := config.AuthConfig{AdminPINFile: pinPath, DataDir: t.TempDir()}
		_, mux, _ := newAdminTestHandler(t, initial, nil, true)

		rec := doAdmin(t, mux, http.MethodGet, "/api/config/auth", nil)
		body := rec.Body.String()
		if strings.Contains(body, "999999") {
			t.Fatalf("response leaked the operator PIN file's contents: %s", body)
		}
		var got authConfigView
		if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
			t.Fatalf("decoding response: %v\nbody: %s", err, body)
		}
		if got.AdminPIN.DefinedBy != "file" || got.AdminPIN.Path != pinPath {
			t.Errorf("AdminPIN = %+v, want {file %s}", got.AdminPIN, pinPath)
		}
	})
}

// --- PUT /api/config/modules: typo'd module rejected, nothing changes ---

func TestPutConfigModulesTypoRejected400NothingChanges(t *testing.T) {
	initial := config.AuthConfig{
		DataDir: t.TempDir(),
		Modules: map[string][]string{"todo": {"pin"}},
		PINs:    []config.NamedHash{{Name: "alice", Hash: bcryptHash(t, "1234")}},
	}
	h, mux, path := newAdminTestHandler(t, initial, []string{"todo"}, false)

	original, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading fixture config: %v", err)
	}

	echo := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) })
	before := httptest.NewRecorder()
	h.deps.Service.Gate("todo", echo).ServeHTTP(before, httptest.NewRequest(http.MethodGet, "/", nil))
	if before.Code != http.StatusUnauthorized {
		t.Fatalf("before PUT: todo gate = %d, want 401 (protected)", before.Code)
	}

	rec := doAdmin(t, mux, http.MethodPut, "/api/config/modules", map[string][]string{"totally-bogus-module": {"pin"}})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("PUT /api/config/modules (typo) = %d, want 400; body: %s", rec.Code, rec.Body.String())
	}

	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading config after rejected PUT: %v", err)
	}
	if !bytes.Equal(got, original) {
		t.Errorf("config file changed after a rejected PUT:\n got: %s\nwant (unchanged): %s", got, original)
	}

	after := httptest.NewRecorder()
	h.deps.Service.Gate("todo", echo).ServeHTTP(after, httptest.NewRequest(http.MethodGet, "/", nil))
	if after.Code != http.StatusUnauthorized {
		t.Fatalf("after rejected PUT: todo gate = %d, want 401 (policy unchanged)", after.Code)
	}

	getRec := doAdmin(t, mux, http.MethodGet, "/api/config/auth", nil)
	var view authConfigView
	if err := json.Unmarshal(getRec.Body.Bytes(), &view); err != nil {
		t.Fatalf("decoding GET response: %v", err)
	}
	if methods, ok := view.Modules["todo"]; !ok || len(methods) != 1 || methods[0] != "pin" {
		t.Errorf("Handler's stored auth config changed after a rejected PUT: modules[todo] = %v", view.Modules["todo"])
	}
	if _, ok := view.Modules["totally-bogus-module"]; ok {
		t.Errorf("rejected module leaked into the stored auth config")
	}
}

// --- pin add -> live login on a protected module; pin delete -> login stops working ---

func TestPinAddEnablesLiveLoginThenDeleteRevokesIt(t *testing.T) {
	// "seed" is already protected at boot so the Service's HMAC session key
	// is loaded (auth.FromConfig only loads it when something is already
	// protected at boot -- see policy.go's FromConfig doc comment); this
	// isolates the test from that boot-time behavior and exercises the
	// live-apply path T5.4 is actually meant to prove.
	initial := config.AuthConfig{
		DataDir: t.TempDir(),
		Modules: map[string][]string{"seed": {"pin"}},
		PINs:    []config.NamedHash{{Name: "seeduser", Hash: bcryptHash(t, "seedpin")}},
	}
	h, mux, _ := newAdminTestHandler(t, initial, []string{"seed", "todo"}, false)

	pinRec := doAdmin(t, mux, http.MethodPost, "/api/pins", map[string]string{"name": "alice", "pin": "alice-secret-pin"})
	if pinRec.Code != http.StatusOK {
		t.Fatalf("POST /api/pins = %d, want 200; body: %s", pinRec.Code, pinRec.Body.String())
	}

	matRec := doAdmin(t, mux, http.MethodPut, "/api/config/modules", map[string][]string{
		"seed": {"pin"},
		"todo": {"pin"},
	})
	if matRec.Code != http.StatusOK {
		t.Fatalf("PUT /api/config/modules = %d, want 200; body: %s", matRec.Code, matRec.Body.String())
	}

	echo := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) })
	gated := h.deps.Service.Gate("todo", echo)

	unauth := httptest.NewRecorder()
	gated.ServeHTTP(unauth, httptest.NewRequest(http.MethodGet, "/", nil))
	if unauth.Code != http.StatusUnauthorized {
		t.Fatalf("todo unauthenticated = %d, want 401", unauth.Code)
	}

	loginBody, _ := json.Marshal(map[string]string{"method": "pin", "pin": "alice-secret-pin"})
	loginReq := httptest.NewRequest(http.MethodPost, "/api/auth/login", bytes.NewReader(loginBody))
	loginRec := httptest.NewRecorder()
	gated.ServeHTTP(loginRec, loginReq)
	if loginRec.Code != http.StatusOK {
		t.Fatalf("login with newly-added pin = %d, want 200; body: %s", loginRec.Code, loginRec.Body.String())
	}
	var cookie *http.Cookie
	for _, c := range loginRec.Result().Cookies() {
		if c.Name == "uw_session" {
			cookie = c
		}
	}
	if cookie == nil {
		t.Fatalf("login response set no uw_session cookie")
	}

	authed := httptest.NewRecorder()
	authedReq := httptest.NewRequest(http.MethodGet, "/", nil)
	authedReq.AddCookie(cookie)
	gated.ServeHTTP(authed, authedReq)
	if authed.Code != http.StatusOK {
		t.Fatalf("todo with session from newly-added pin = %d, want 200", authed.Code)
	}

	// Now delete the pin and confirm login with it no longer works.
	delRec := doAdmin(t, mux, http.MethodDelete, "/api/pins/alice", nil)
	if delRec.Code != http.StatusNoContent {
		t.Fatalf("DELETE /api/pins/alice = %d, want 204; body: %s", delRec.Code, delRec.Body.String())
	}

	loginAgainReq := httptest.NewRequest(http.MethodPost, "/api/auth/login", bytes.NewReader(loginBody))
	loginAgainRec := httptest.NewRecorder()
	gated.ServeHTTP(loginAgainRec, loginAgainReq)
	if loginAgainRec.Code == http.StatusOK {
		t.Fatalf("login with deleted pin still succeeded")
	}
}

// --- generated key round-trips through a Gate; delete revokes it ---

func TestKeyGenerateRoundTripsThenDeleteRevokesIt(t *testing.T) {
	h, mux, _ := newAdminTestHandler(t, config.AuthConfig{}, []string{"keymod"}, false)

	generate := func(name string) string {
		t.Helper()
		rec := doAdmin(t, mux, http.MethodPost, "/api/keys", map[string]string{"name": name})
		if rec.Code != http.StatusOK {
			t.Fatalf("POST /api/keys {name:%q} = %d, want 200; body: %s", name, rec.Code, rec.Body.String())
		}
		var resp struct {
			Key string `json:"key"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
			t.Fatalf("decoding key-generate response: %v", err)
		}
		if resp.Key == "" {
			t.Fatalf("POST /api/keys {name:%q} returned an empty key", name)
		}
		return resp.Key
	}

	// A second, undeleted "keeper" key stays configured throughout, so
	// deleting "svc1" below revokes only that one key rather than emptying
	// auth.api_keys entirely while "keymod" still requires the "key"
	// method (which ValidatePolicy correctly rejects as an orphaned
	// method -- deleting the *last* key for a module still requiring "key"
	// auth is a separate, expected 400 case, exercised at the end).
	svc1Key := generate("svc1")
	keeperKey := generate("keeper")

	matRec := doAdmin(t, mux, http.MethodPut, "/api/config/modules", map[string][]string{"keymod": {"key"}})
	if matRec.Code != http.StatusOK {
		t.Fatalf("PUT /api/config/modules = %d, want 200; body: %s", matRec.Code, matRec.Body.String())
	}

	echo := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) })
	gated := h.deps.Service.Gate("keymod", echo)

	withKey := func(key string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		req.Header.Set("Authorization", "Bearer "+key)
		rec := httptest.NewRecorder()
		gated.ServeHTTP(rec, req)
		return rec
	}

	noAuth := httptest.NewRecorder()
	gated.ServeHTTP(noAuth, httptest.NewRequest(http.MethodGet, "/", nil))
	if noAuth.Code != http.StatusUnauthorized {
		t.Fatalf("keymod unauthenticated = %d, want 401", noAuth.Code)
	}

	if rec := withKey(svc1Key); rec.Code != http.StatusOK {
		t.Fatalf("keymod with generated key = %d, want 200; body: %s", rec.Code, rec.Body.String())
	}

	delRec := doAdmin(t, mux, http.MethodDelete, "/api/keys/svc1", nil)
	if delRec.Code != http.StatusNoContent {
		t.Fatalf("DELETE /api/keys/svc1 = %d, want 204; body: %s", delRec.Code, delRec.Body.String())
	}

	if rec := withKey(svc1Key); rec.Code != http.StatusUnauthorized {
		t.Fatalf("keymod with deleted key = %d, want 401", rec.Code)
	}
	if rec := withKey(keeperKey); rec.Code != http.StatusOK {
		t.Fatalf("keymod with the untouched keeper key = %d, want 200 (deleting svc1 must not revoke other keys); body: %s", rec.Code, rec.Body.String())
	}

	// Deleting a name that no longer exists reports 404.
	delAgainRec := doAdmin(t, mux, http.MethodDelete, "/api/keys/svc1", nil)
	if delAgainRec.Code != http.StatusNotFound {
		t.Fatalf("DELETE /api/keys/svc1 (again) = %d, want 404", delAgainRec.Code)
	}

	// Deleting the last remaining key while "keymod" still requires "key"
	// auth would orphan that method -- ValidatePolicy rejects it, and the
	// key (and the module's protection) must remain intact.
	delLastRec := doAdmin(t, mux, http.MethodDelete, "/api/keys/keeper", nil)
	if delLastRec.Code != http.StatusBadRequest {
		t.Fatalf("DELETE /api/keys/keeper (last remaining, still required) = %d, want 400; body: %s", delLastRec.Code, delLastRec.Body.String())
	}
	if rec := withKey(keeperKey); rec.Code != http.StatusOK {
		t.Fatalf("keeper key stopped working after a rejected delete: %d", rec.Code)
	}
}

// --- LDAP test: never echoes the password, classifies a dead port as unreachable ---

func TestLDAPTestUnreachableNeverEchoesPassword(t *testing.T) {
	initial := config.AuthConfig{LDAP: config.LDAPConfig{URL: "ldap://" + deadTCPAddr(t)}}
	_, mux, _ := newAdminTestHandler(t, initial, nil, false)

	rec := doAdmin(t, mux, http.MethodPost, "/api/ldap/test", map[string]string{
		"username": "bob",
		"password": "super-secret-password",
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("POST /api/ldap/test = %d, want 200; body: %s", rec.Code, rec.Body.String())
	}

	body := rec.Body.String()
	if strings.Contains(body, "super-secret-password") {
		t.Fatalf("LDAP test response echoed the posted password: %s", body)
	}

	var got struct {
		Result string `json:"result"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decoding response: %v\nbody: %s", err, body)
	}
	if got.Result != "unreachable" {
		t.Errorf("result = %q, want %q", got.Result, "unreachable")
	}
}

// --- PUT /api/config/session ---

func TestPutConfigSessionAppliesAndPersists(t *testing.T) {
	h, mux, path := newAdminTestHandler(t, config.AuthConfig{}, nil, false)

	rec := doAdmin(t, mux, http.MethodPut, "/api/config/session", map[string]any{
		"ttl_hours":     8,
		"cookie_domain": "example.com",
		"cookie_secure": true,
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("PUT /api/config/session = %d, want 200; body: %s", rec.Code, rec.Body.String())
	}

	loaded, err := config.Load(path)
	if err != nil {
		t.Fatalf("config.Load(spliced file): %v", err)
	}
	if loaded.Auth.Session.TTLHours != 8 {
		t.Errorf("loaded Session.TTLHours = %d, want 8", loaded.Auth.Session.TTLHours)
	}
	if loaded.Auth.CookieDomain != "example.com" {
		t.Errorf("loaded CookieDomain = %q, want %q", loaded.Auth.CookieDomain, "example.com")
	}
	if !loaded.Auth.CookieSecure {
		t.Errorf("loaded CookieSecure = false, want true")
	}

	h.mu.Lock()
	stored := h.authConfig
	h.mu.Unlock()
	if stored.Session.TTLHours != 8 || stored.CookieDomain != "example.com" || !stored.CookieSecure {
		t.Errorf("Handler's stored auth config not updated: %+v", stored)
	}
}

// --- AC-15: across a whole realistic admin session, no response body ever
// carries a plaintext secret, and the generated key appears exactly once. ---

func TestAC15NoResponseBodyEverLeaksASecret(t *testing.T) {
	const (
		alicePIN   = "alice-super-secret-pin-42"
		ldapPass   = "ldap-super-secret-password-99"
		generateNm = "svc1"
	)

	initial := config.AuthConfig{
		DataDir: t.TempDir(),
		Modules: map[string][]string{"seed": {"pin"}},
		PINs:    []config.NamedHash{{Name: "seeduser", Hash: bcryptHash(t, "seedpin")}},
		LDAP:    config.LDAPConfig{URL: "ldap://" + deadTCPAddr(t)},
	}
	h, mux, _ := newAdminTestHandler(t, initial, []string{"seed", "todo", "keymod"}, false)

	var bodies []string
	capture := func(rec *httptest.ResponseRecorder) *httptest.ResponseRecorder {
		bodies = append(bodies, rec.Body.String())
		return rec
	}

	// 1. Initial redacted view.
	capture(doAdmin(t, mux, http.MethodGet, "/api/config/auth", nil))

	// 2. Add a PIN.
	pinRec := capture(doAdmin(t, mux, http.MethodPost, "/api/pins", map[string]string{"name": "alice", "pin": alicePIN}))
	if pinRec.Code != http.StatusOK {
		t.Fatalf("POST /api/pins = %d, want 200; body: %s", pinRec.Code, pinRec.Body.String())
	}

	// 3. Redacted view again.
	capture(doAdmin(t, mux, http.MethodGet, "/api/config/auth", nil))

	// 4. Generate two API keys -- svc1's plaintext is the one this test
	// tracks for the exactly-once assertion below; "keeper" stays configured
	// throughout so deleting svc1 later doesn't orphan keymod's "key"
	// method requirement (ValidatePolicy rejects emptying auth.api_keys
	// while a module still requires it -- a separate, correct 400 case
	// covered by TestKeyGenerateRoundTripsThenDeleteRevokesIt).
	genRec := capture(doAdmin(t, mux, http.MethodPost, "/api/keys", map[string]string{"name": generateNm}))
	if genRec.Code != http.StatusOK {
		t.Fatalf("POST /api/keys = %d, want 200; body: %s", genRec.Code, genRec.Body.String())
	}
	var genResp struct {
		Key string `json:"key"`
	}
	if err := json.Unmarshal(genRec.Body.Bytes(), &genResp); err != nil {
		t.Fatalf("decoding key-generate response: %v", err)
	}
	if genResp.Key == "" {
		t.Fatalf("generated key is empty")
	}

	keeperRec := capture(doAdmin(t, mux, http.MethodPost, "/api/keys", map[string]string{"name": "keeper"}))
	if keeperRec.Code != http.StatusOK {
		t.Fatalf("POST /api/keys {name:keeper} = %d, want 200; body: %s", keeperRec.Code, keeperRec.Body.String())
	}
	var keeperResp struct {
		Key string `json:"key"`
	}
	if err := json.Unmarshal(keeperRec.Body.Bytes(), &keeperResp); err != nil {
		t.Fatalf("decoding keeper key-generate response: %v", err)
	}

	// 5. Protect todo/keymod live.
	matRec := capture(doAdmin(t, mux, http.MethodPut, "/api/config/modules", map[string][]string{
		"seed":   {"pin"},
		"todo":   {"pin"},
		"keymod": {"key"},
	}))
	if matRec.Code != http.StatusOK {
		t.Fatalf("PUT /api/config/modules = %d, want 200; body: %s", matRec.Code, matRec.Body.String())
	}

	// 6. Login on todo with the newly-added pin, through the real gate (not
	// the admin mux, but still a response body captured for the grep).
	echo := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) })
	gatedTodo := h.deps.Service.Gate("todo", echo)
	loginBody, _ := json.Marshal(map[string]string{"method": "pin", "pin": alicePIN})
	loginRec := httptest.NewRecorder()
	gatedTodo.ServeHTTP(loginRec, httptest.NewRequest(http.MethodPost, "/api/auth/login", bytes.NewReader(loginBody)))
	bodies = append(bodies, loginRec.Body.String())
	if loginRec.Code != http.StatusOK {
		t.Fatalf("login = %d, want 200; body: %s", loginRec.Code, loginRec.Body.String())
	}

	// 7. Redacted view again.
	capture(doAdmin(t, mux, http.MethodGet, "/api/config/auth", nil))

	// 8. The generated key authenticates a "key"-module request through a
	// Gate (round-trip proof) -- the echoed response body is harmless but
	// captured anyway.
	gatedKey := h.deps.Service.Gate("keymod", echo)
	keyReq := httptest.NewRequest(http.MethodGet, "/", nil)
	keyReq.Header.Set("Authorization", "Bearer "+genResp.Key)
	keyRec := httptest.NewRecorder()
	gatedKey.ServeHTTP(keyRec, keyReq)
	bodies = append(bodies, keyRec.Body.String())
	if keyRec.Code != http.StatusOK {
		t.Fatalf("generated key did not authenticate through the gate: %d", keyRec.Code)
	}

	// 9. Revoke the key.
	delKeyRec := capture(doAdmin(t, mux, http.MethodDelete, "/api/keys/"+generateNm, nil))
	if delKeyRec.Code != http.StatusNoContent {
		t.Fatalf("DELETE /api/keys/%s = %d, want 204; body: %s", generateNm, delKeyRec.Code, delKeyRec.Body.String())
	}
	revokedRec := httptest.NewRecorder()
	revokedReq := httptest.NewRequest(http.MethodGet, "/", nil)
	revokedReq.Header.Set("Authorization", "Bearer "+genResp.Key)
	gatedKey.ServeHTTP(revokedRec, revokedReq)
	bodies = append(bodies, revokedRec.Body.String())
	if revokedRec.Code != http.StatusUnauthorized {
		t.Fatalf("revoked key still authenticated: %d", revokedRec.Code)
	}

	keeperStillWorksRec := httptest.NewRecorder()
	keeperStillWorksReq := httptest.NewRequest(http.MethodGet, "/", nil)
	keeperStillWorksReq.Header.Set("Authorization", "Bearer "+keeperResp.Key)
	gatedKey.ServeHTTP(keeperStillWorksRec, keeperStillWorksReq)
	bodies = append(bodies, keeperStillWorksRec.Body.String())
	if keeperStillWorksRec.Code != http.StatusOK {
		t.Fatalf("keeper key stopped working after svc1 was deleted: %d", keeperStillWorksRec.Code)
	}

	// 10. LDAP test against a dead port with a posted password.
	ldapRec := capture(doAdmin(t, mux, http.MethodPost, "/api/ldap/test", map[string]string{"username": "bob", "password": ldapPass}))
	if ldapRec.Code != http.StatusOK {
		t.Fatalf("POST /api/ldap/test = %d, want 200; body: %s", ldapRec.Code, ldapRec.Body.String())
	}

	// 11. A typo'd matrix save -> 400, its error body captured too.
	capture(doAdmin(t, mux, http.MethodPut, "/api/config/modules", map[string][]string{"nonexistent-oops": {"pin"}}))

	// 12. Delete the pin; login with it should now fail.
	delPinRec := capture(doAdmin(t, mux, http.MethodDelete, "/api/pins/alice", nil))
	if delPinRec.Code != http.StatusNoContent {
		t.Fatalf("DELETE /api/pins/alice = %d, want 204; body: %s", delPinRec.Code, delPinRec.Body.String())
	}
	relRec := httptest.NewRecorder()
	gatedTodo.ServeHTTP(relRec, httptest.NewRequest(http.MethodPost, "/api/auth/login", bytes.NewReader(loginBody)))
	bodies = append(bodies, relRec.Body.String())
	if relRec.Code == http.StatusOK {
		t.Fatalf("login with deleted pin still succeeded")
	}

	// --- The AC-15 assertions, over every captured body ---

	all := strings.Join(bodies, "\n---\n")

	if n := strings.Count(all, genResp.Key); n != 1 {
		t.Errorf("generated key appears %d times across all response bodies, want exactly 1 (the generate response)\nall bodies:\n%s", n, all)
	}
	if n := strings.Count(all, keeperResp.Key); n != 1 {
		t.Errorf("keeper key appears %d times across all response bodies, want exactly 1 (its own generate response)\nall bodies:\n%s", n, all)
	}
	if strings.Contains(all, "$2a$") || strings.Contains(all, "$2b$") {
		t.Errorf("a bcrypt hash prefix leaked into a response body:\n%s", all)
	}
	if strings.Contains(all, alicePIN) {
		t.Errorf("the posted plaintext PIN leaked into a response body:\n%s", all)
	}
	if strings.Contains(all, ldapPass) {
		t.Errorf("the posted plaintext LDAP password leaked into a response body:\n%s", all)
	}
	if strings.Contains(all, "seedpin") {
		t.Errorf("the seed PIN's plaintext leaked into a response body:\n%s", all)
	}
}
