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
		Modules:  map[string]config.ModuleAuthConfig{"todo": {PinFile: pinFileFixture(t, "1234")}},
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
	pinPath := pinFileFixture(t, "1234")
	initial := config.AuthConfig{
		DataDir: t.TempDir(),
		Modules: map[string]config.ModuleAuthConfig{"todo": {PinFile: pinPath}},
		LDAP:    config.LDAPConfig{URL: "ldaps://ldap.example.com"},
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

	rec := doAdmin(t, mux, http.MethodPut, "/api/config/modules", map[string]config.ModuleAuthConfig{"totally-bogus-module": {}})
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
	if m, ok := view.Modules["todo"]; !ok || m.PinFile != pinPath {
		t.Errorf("Handler's stored auth config changed after a rejected PUT: modules[todo] = %v", view.Modules["todo"])
	}
	if _, ok := view.Modules["totally-bogus-module"]; ok {
		t.Errorf("rejected module leaked into the stored auth config")
	}
}

// --- generated key round-trips through a Gate; delete revokes it ---

func TestKeyGenerateRoundTripsThenDeleteRevokesIt(t *testing.T) {
	initial := config.AuthConfig{LDAP: config.LDAPConfig{URL: "ldaps://ldap.example.com"}}
	h, mux, _ := newAdminTestHandler(t, initial, []string{"keymod"}, false)

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
	// auth.api_keys entirely.
	svc1Key := generate("svc1")
	keeperKey := generate("keeper")

	matRec := doAdmin(t, mux, http.MethodPut, "/api/config/modules", map[string]config.ModuleAuthConfig{"keymod": {}})
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

	// api_keys may be empty (the two-state auth model does not tie a
	// module's protection to how many keys exist), so deleting the last
	// remaining key succeeds and revokes it like any other.
	delLastRec := doAdmin(t, mux, http.MethodDelete, "/api/keys/keeper", nil)
	if delLastRec.Code != http.StatusNoContent {
		t.Fatalf("DELETE /api/keys/keeper (last remaining) = %d, want 204; body: %s", delLastRec.Code, delLastRec.Body.String())
	}
	if rec := withKey(keeperKey); rec.Code != http.StatusUnauthorized {
		t.Fatalf("keeper key still worked after being deleted: %d", rec.Code)
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
		ldapPass   = "ldap-super-secret-password-99"
		generateNm = "svc1"
	)

	initial := config.AuthConfig{
		DataDir: t.TempDir(),
		Modules: map[string]config.ModuleAuthConfig{"seed": {}},
		LDAP:    config.LDAPConfig{URL: "ldap://" + deadTCPAddr(t)},
	}
	h, mux, _ := newAdminTestHandler(t, initial, []string{"seed", "keymod"}, false)

	var bodies []string
	capture := func(rec *httptest.ResponseRecorder) *httptest.ResponseRecorder {
		bodies = append(bodies, rec.Body.String())
		return rec
	}

	// 1. Initial redacted view.
	capture(doAdmin(t, mux, http.MethodGet, "/api/config/auth", nil))

	// 2. Redacted view again.
	capture(doAdmin(t, mux, http.MethodGet, "/api/config/auth", nil))

	// 3. Generate two API keys -- svc1's plaintext is the one this test
	// tracks for the exactly-once assertion below; "keeper" stays configured
	// throughout so deleting svc1 later doesn't touch it.
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

	// 4. Protect keymod live.
	matRec := capture(doAdmin(t, mux, http.MethodPut, "/api/config/modules", map[string]config.ModuleAuthConfig{
		"seed":   {},
		"keymod": {},
	}))
	if matRec.Code != http.StatusOK {
		t.Fatalf("PUT /api/config/modules = %d, want 200; body: %s", matRec.Code, matRec.Body.String())
	}

	// 5. Redacted view again.
	capture(doAdmin(t, mux, http.MethodGet, "/api/config/auth", nil))

	// 6. The generated key authenticates a keymod request through a Gate
	// (round-trip proof) -- the echoed response body is harmless but
	// captured anyway.
	echo := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) })
	gatedKey := h.deps.Service.Gate("keymod", echo)
	keyReq := httptest.NewRequest(http.MethodGet, "/", nil)
	keyReq.Header.Set("Authorization", "Bearer "+genResp.Key)
	keyRec := httptest.NewRecorder()
	gatedKey.ServeHTTP(keyRec, keyReq)
	bodies = append(bodies, keyRec.Body.String())
	if keyRec.Code != http.StatusOK {
		t.Fatalf("generated key did not authenticate through the gate: %d", keyRec.Code)
	}

	// 7. Revoke the key.
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

	// 8. LDAP test against a dead port with a posted password.
	ldapRec := capture(doAdmin(t, mux, http.MethodPost, "/api/ldap/test", map[string]string{"username": "bob", "password": ldapPass}))
	if ldapRec.Code != http.StatusOK {
		t.Fatalf("POST /api/ldap/test = %d, want 200; body: %s", ldapRec.Code, ldapRec.Body.String())
	}

	// 9. A typo'd matrix save -> 400, its error body captured too.
	capture(doAdmin(t, mux, http.MethodPut, "/api/config/modules", map[string]config.ModuleAuthConfig{"nonexistent-oops": {}}))

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
	if strings.Contains(all, ldapPass) {
		t.Errorf("the posted plaintext LDAP password leaked into a response body:\n%s", all)
	}
}

// --- PUT /api/config/ldap ---

func TestPutConfigLdapReplacesUrlAndBaseDN(t *testing.T) {
	initial := config.AuthConfig{LDAP: config.LDAPConfig{URL: "ldaps://old.example.com", BaseDN: "dc=old"}}
	_, mux, path := newAdminTestHandler(t, initial, nil, false)

	rec := doAdmin(t, mux, http.MethodPut, "/api/config/ldap", map[string]any{
		"url":     "ldaps://new.example.com",
		"base_dn": "dc=new",
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("PUT /api/config/ldap = %d, want 200; body: %s", rec.Code, rec.Body.String())
	}

	loaded, err := config.Load(path)
	if err != nil {
		t.Fatalf("config.Load(spliced file): %v", err)
	}
	if loaded.Auth.LDAP.URL != "ldaps://new.example.com" {
		t.Errorf("loaded LDAP.URL = %q, want %q", loaded.Auth.LDAP.URL, "ldaps://new.example.com")
	}
	if loaded.Auth.LDAP.BaseDN != "dc=new" {
		t.Errorf("loaded LDAP.BaseDN = %q, want %q", loaded.Auth.LDAP.BaseDN, "dc=new")
	}
}

func TestPutConfigLdapBlankAndSetPlaceholderKeepExistingBindPassword(t *testing.T) {
	const existingPassword = "the-real-secret"

	for _, tc := range []struct {
		name         string
		bindPassword *string
	}{
		{"omitted (nil)", nil},
		{"blank string", strPtr("")},
		{"whitespace-only", strPtr("   ")},
		{"(set) placeholder", strPtr("(set)")},
	} {
		t.Run(tc.name, func(t *testing.T) {
			initial := config.AuthConfig{LDAP: config.LDAPConfig{URL: "ldaps://ldap.example.com", BindDN: "cn=svc", BindPassword: existingPassword}}
			_, mux, path := newAdminTestHandler(t, initial, nil, false)

			body := map[string]any{"url": "ldaps://ldap.example.com", "bind_dn": "cn=svc"}
			if tc.bindPassword != nil {
				body["bind_password"] = *tc.bindPassword
			}
			rec := doAdmin(t, mux, http.MethodPut, "/api/config/ldap", body)
			if rec.Code != http.StatusOK {
				t.Fatalf("PUT /api/config/ldap = %d, want 200; body: %s", rec.Code, rec.Body.String())
			}

			if strings.Contains(rec.Body.String(), existingPassword) {
				t.Fatalf("response leaked the bind password: %s", rec.Body.String())
			}
			var view ldapConfigView
			if err := json.Unmarshal(rec.Body.Bytes(), &view); err != nil {
				t.Fatalf("decoding response: %v\nbody: %s", err, rec.Body.String())
			}
			if view.BindPassword != "(set)" {
				t.Errorf("response BindPassword = %q, want %q", view.BindPassword, "(set)")
			}

			loaded, err := config.Load(path)
			if err != nil {
				t.Fatalf("config.Load(spliced file): %v", err)
			}
			if loaded.Auth.LDAP.BindPassword != existingPassword {
				t.Errorf("on-disk BindPassword = %q, want unchanged %q", loaded.Auth.LDAP.BindPassword, existingPassword)
			}
		})
	}
}

func TestPutConfigLdapNonBlankBindPasswordReplaces(t *testing.T) {
	initial := config.AuthConfig{LDAP: config.LDAPConfig{URL: "ldaps://ldap.example.com", BindPassword: "old-secret"}}
	_, mux, path := newAdminTestHandler(t, initial, nil, false)

	rec := doAdmin(t, mux, http.MethodPut, "/api/config/ldap", map[string]any{
		"url":           "ldaps://ldap.example.com",
		"bind_password": "brand-new-secret",
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("PUT /api/config/ldap = %d, want 200; body: %s", rec.Code, rec.Body.String())
	}
	if strings.Contains(rec.Body.String(), "brand-new-secret") || strings.Contains(rec.Body.String(), "old-secret") {
		t.Fatalf("response leaked a bind password: %s", rec.Body.String())
	}

	loaded, err := config.Load(path)
	if err != nil {
		t.Fatalf("config.Load(spliced file): %v", err)
	}
	if loaded.Auth.LDAP.BindPassword != "brand-new-secret" {
		t.Errorf("on-disk BindPassword = %q, want %q", loaded.Auth.LDAP.BindPassword, "brand-new-secret")
	}
}

func TestPutConfigLdapClearingUrlWithProtectedModuleRejected400NamingModule(t *testing.T) {
	initial := config.AuthConfig{
		DataDir: t.TempDir(),
		LDAP:    config.LDAPConfig{URL: "ldaps://ldap.example.com"},
		Modules: map[string]config.ModuleAuthConfig{"todo": {}},
	}
	_, mux, path := newAdminTestHandler(t, initial, []string{"todo"}, false)

	original, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading fixture config: %v", err)
	}

	rec := doAdmin(t, mux, http.MethodPut, "/api/config/ldap", map[string]any{"url": ""})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("PUT /api/config/ldap (clearing url) = %d, want 400; body: %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "todo") {
		t.Errorf("400 body doesn't name the offending module: %s", rec.Body.String())
	}

	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading config after rejected PUT: %v", err)
	}
	if !bytes.Equal(got, original) {
		t.Errorf("config file changed after a rejected PUT:\n got: %s\nwant (unchanged): %s", got, original)
	}
}

// strPtr returns a pointer to s, for populating putLDAPRequest-shaped test
// bodies where nil must be distinguishable from an empty string.
func strPtr(s string) *string { return &s }
