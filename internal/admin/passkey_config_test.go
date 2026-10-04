package admin

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"cmd184psu/unified-webapp/internal/platform/config"
)

func passkeyFixture(t *testing.T) config.AuthConfig {
	t.Helper()
	return config.AuthConfig{
		DataDir: t.TempDir(),
		LDAP:    config.LDAPConfig{URL: "ldaps://ldap.example.com"},
		Modules: map[string]config.ModuleAuthConfig{"todo": {}},
	}
}

// liveMethods returns the login methods the live policy offers for "todo".
func liveMethods(t *testing.T, h *Handler) string {
	t.Helper()
	echo := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {})
	rec := httptest.NewRecorder()
	h.deps.Service.Gate("todo", echo).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/auth/mode", nil))
	return rec.Body.String()
}

func TestPutConfigPasskeyPersistsAppliesAndGetReturnsIt(t *testing.T) {
	h, mux, path := newAdminTestHandler(t, passkeyFixture(t), []string{"todo"}, false)
	if strings.Contains(liveMethods(t, h), "passkey") {
		t.Fatalf("passkey offered before configuration: %s", liveMethods(t, h))
	}

	rec := doAdmin(t, mux, http.MethodPut, "/api/config/passkey", map[string]any{
		"rp_id":      "CmdHome.net",
		"rp_origins": []string{"https://certmachine.cmdhome.net", "https://cmdhome.net", "https://cmdhome.net"},
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("PUT = %d; body: %s", rec.Code, rec.Body.String())
	}
	loaded, err := config.Load(path)
	if err != nil {
		t.Fatalf("config.Load: %v", err)
	}
	pk := loaded.Auth.Passkey
	if pk.RPID != "cmdhome.net" || len(pk.RPOrigins) != 2 {
		t.Errorf("persisted passkey = %+v, want lower-cased rp_id and 2 deduped origins", pk)
	}
	if !strings.Contains(liveMethods(t, h), "passkey") {
		t.Errorf("live policy does not offer passkey after PUT: %s", liveMethods(t, h))
	}

	get := doAdmin(t, mux, http.MethodGet, "/api/config/auth", nil)
	if !strings.Contains(get.Body.String(), `"passkey":{"rp_id":"cmdhome.net","rp_origins":["https://certmachine.cmdhome.net","https://cmdhome.net"]}`) {
		t.Errorf("GET does not return passkey config: %s", get.Body.String())
	}
}

func TestGetConfigAuthPasskeyOriginsNeverNull(t *testing.T) {
	_, mux, _ := newAdminTestHandler(t, config.AuthConfig{}, nil, false)
	body := doAdmin(t, mux, http.MethodGet, "/api/config/auth", nil).Body.String()
	if !strings.Contains(body, `"passkey":{"rp_id":"","rp_origins":[]}`) {
		t.Errorf("want empty passkey with [] origins, got: %s", body)
	}
}

func TestPutConfigPasskeyInvalidRejected400UnchangedState(t *testing.T) {
	long := make([]string, 65)
	for i := range long {
		long[i] = "https://h" + strings.Repeat("a", i) + ".example.com"
	}
	cases := []struct {
		name    string
		id      string
		origins []string
		want    string
	}{
		{"scheme in id", "https://example.com", []string{"https://example.com"}, "bare host name"},
		{"port in id", "example.com:8443", []string{"https://example.com"}, "bare host name"},
		{"path in id", "example.com/x", []string{"https://example.com"}, "bare host name"},
		{"space in id", "exa mple.com", []string{"https://example.com"}, "bare host name"},
		{"single label", "net", []string{"https://net"}, "at least one dot"},
		{"localhost https", "localhost", []string{"https://localhost"}, "http://localhost"},
		{"origin outside id", "cmdhome.example", []string{"https://certmachine.cmdhome.net"},
			"certmachine.cmdhome.net is not under cmdhome.example: an origin's host must be the RP ID or a subdomain of it"},
		{"suffix not subdomain", "example.com", []string{"https://badexample.com"}, "is not under example.com"},
		{"http non-localhost", "example.com", []string{"http://example.com"}, "https"},
		{"origin with path", "example.com", []string{"https://example.com/app"}, "no path"},
		{"origin with query", "example.com", []string{"https://example.com?x=1"}, "no path"},
		{"origin with fragment", "example.com", []string{"https://example.com#x"}, "no path"},
		{"origin no host", "example.com", []string{"https://"}, "host"},
		{"origin garbage", "example.com", []string{"not a url"}, "origin"},
		{"id with nothing routed under it", "example.com", nil, "no host is routed under example.com"},
		{"origins without id", "", []string{"https://example.com"}, "Relying Party ID"},
		{"too many origins", "example.com", long, "64"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			initial := passkeyFixture(t)
			h, mux, path := newAdminTestHandler(t, initial, []string{"todo"}, false)
			before, _ := os.ReadFile(path)
			methods := liveMethods(t, h)

			rec := doAdmin(t, mux, http.MethodPut, "/api/config/passkey", map[string]any{"rp_id": tc.id, "rp_origins": tc.origins})
			if rec.Code != http.StatusBadRequest {
				t.Fatalf("PUT = %d, want 400; body: %s", rec.Code, rec.Body.String())
			}
			if !strings.Contains(rec.Body.String(), tc.want) {
				t.Errorf("body %q lacks %q", rec.Body.String(), tc.want)
			}
			after, _ := os.ReadFile(path)
			if !bytes.Equal(before, after) {
				t.Errorf("config file changed by rejected PUT")
			}
			if liveMethods(t, h) != methods {
				t.Errorf("live policy changed by rejected PUT")
			}
			if len(h.authConfig.Passkey.RPOrigins) != 0 || h.authConfig.Passkey.RPID != "" {
				t.Errorf("in-memory config changed: %+v", h.authConfig.Passkey)
			}
		})
	}
}

func TestPutConfigPasskeyLocalhostDevAllowed(t *testing.T) {
	_, mux, _ := newAdminTestHandler(t, passkeyFixture(t), []string{"todo"}, false)
	rec := doAdmin(t, mux, http.MethodPut, "/api/config/passkey", map[string]any{
		"rp_id": "localhost", "rp_origins": []string{"http://localhost:8080"},
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("PUT = %d; body: %s", rec.Code, rec.Body.String())
	}
}

func TestPutConfigPasskeyBothEmptyDisables(t *testing.T) {
	initial := passkeyFixture(t)
	initial.Passkey = config.PasskeyConfig{RPID: "example.com", RPOrigins: []string{"https://example.com"}}
	h, mux, path := newAdminTestHandler(t, initial, []string{"todo"}, false)
	if !strings.Contains(liveMethods(t, h), "passkey") {
		t.Fatalf("precondition: passkey offered: %s", liveMethods(t, h))
	}
	rec := doAdmin(t, mux, http.MethodPut, "/api/config/passkey", map[string]any{"rp_id": "", "rp_origins": []string{}})
	if rec.Code != http.StatusOK {
		t.Fatalf("PUT = %d; body: %s", rec.Code, rec.Body.String())
	}
	if strings.Contains(liveMethods(t, h), "passkey") {
		t.Errorf("passkey still offered: %s", liveMethods(t, h))
	}
	loaded, err := config.Load(path)
	if err != nil {
		t.Fatalf("config.Load: %v", err)
	}
	if loaded.Auth.Passkey.RPID != "" || len(loaded.Auth.Passkey.RPOrigins) != 0 {
		t.Errorf("persisted = %+v", loaded.Auth.Passkey)
	}
}

func TestPutConfigPasskeyPolicyValidationFailureRejected(t *testing.T) {
	// A candidate ValidatePolicy refuses (protected module, no LDAP URL) must
	// be rejected by the shared pipeline, changing nothing.
	h, mux, path := newAdminTestHandler(t, passkeyFixture(t), []string{"todo"}, false)
	h.authConfig.LDAP.URL = ""
	before, _ := os.ReadFile(path)
	rec := doAdmin(t, mux, http.MethodPut, "/api/config/passkey", map[string]any{
		"rp_id": "example.com", "rp_origins": []string{"https://example.com"},
	})
	if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "todo") {
		t.Fatalf("PUT = %d; body: %s", rec.Code, rec.Body.String())
	}
	after, _ := os.ReadFile(path)
	if !bytes.Equal(before, after) {
		t.Errorf("config changed")
	}
}
