package certmachine

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"database/sql"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

// newHandlerTestServer builds a fully wired Server against a temp static dir
// and temp DB -- the same shape build_test.go's buildConfig/staticDirForBuild
// use, but returning the *Server itself so tests can reach s.db directly to
// set up fixtures no HTTP route can produce (e.g. a hand-inserted quarantined
// row).
func newHandlerTestServer(t *testing.T) *Server {
	t.Helper()
	return newHandlerTestServerWithLegacyDir(t, "")
}

// newHandlerTestServerWithLegacyDir is newHandlerTestServer with
// legacy_import_dir set, for the import routes -- whose behaviour differs by
// design between "unconfigured", "configured but unusable", and "usable".
func newHandlerTestServerWithLegacyDir(t *testing.T, legacyDir string) *Server {
	t.Helper()
	dir := t.TempDir()
	staticDir := filepath.Join(dir, "web")
	if err := os.MkdirAll(staticDir, 0o755); err != nil {
		t.Fatalf("mkdir static: %v", err)
	}
	if err := os.WriteFile(filepath.Join(staticDir, "index.html"), []byte("INDEX"), 0o644); err != nil {
		t.Fatalf("write index: %v", err)
	}
	srv, err := New(Options{
		StaticDir:           staticDir,
		DBPath:              filepath.Join(dir, "data", "certmachine.db"),
		LegacyImportDir:     legacyDir,
		DefaultValidityDays: 365,
		ExpiryWarnDays:      30,
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(func() { srv.Close() })
	return srv
}

func newHandlerTestHTTPServer(t *testing.T) (*Server, *httptest.Server) {
	t.Helper()
	return serveHandlerTestServer(t, newHandlerTestServer(t))
}

func newHandlerTestHTTPServerWithLegacyDir(t *testing.T, legacyDir string) (*Server, *httptest.Server) {
	t.Helper()
	return serveHandlerTestServer(t, newHandlerTestServerWithLegacyDir(t, legacyDir))
}

func serveHandlerTestServer(t *testing.T, srv *Server) (*Server, *httptest.Server) {
	t.Helper()
	httpSrv := httptest.NewServer(srv.Handler())
	t.Cleanup(httpSrv.Close)
	return srv, httpSrv
}

func doJSON(t *testing.T, method, url, body string) *http.Response {
	t.Helper()
	var reader io.Reader
	if body != "" {
		reader = strings.NewReader(body)
	}
	req, err := http.NewRequest(method, url, reader)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("do request: %v", err)
	}
	return res
}

func decodeErrorBody(t *testing.T, res *http.Response) map[string]string {
	t.Helper()
	defer res.Body.Close()
	var decoded map[string]string
	if err := json.NewDecoder(res.Body).Decode(&decoded); err != nil {
		t.Fatalf("body is not a JSON error object: %v", err)
	}
	return decoded
}

func mustReadAll(t *testing.T, r io.Reader) []byte {
	t.Helper()
	b, err := io.ReadAll(r)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	return b
}

// insertQuarantinedCert inserts a quarantined row directly through the store,
// bypassing Generate/Renew (neither can ever produce a quarantined row) --
// the same hand-built-Cert approach bundle_test.go and lifecycle_test.go use
// for shapes not reachable through the public lifecycle API.
func insertQuarantinedCert(t *testing.T, s *Store, fqdn, reason string) int64 {
	t.Helper()
	ctx := context.Background()
	reasonCopy := reason
	c := Cert{
		FQDN:             fqdn,
		Status:           StatusQuarantined,
		QuarantineReason: &reasonCopy,
		SANs:             SANs{DNS: []string{}, IP: []string{}},
		Created:          time.Now().UTC().Format(time.RFC3339),
	}
	var id int64
	if err := s.WithTx(ctx, func(tx *sql.Tx) error {
		var err error
		id, err = s.InsertCert(ctx, tx, c)
		return err
	}); err != nil {
		t.Fatalf("insert quarantined cert: %v", err)
	}
	return id
}

func generateTestCert(t *testing.T, s *Store, fqdn string) IssueResult {
	t.Helper()
	req, err := ValidateRequest(fqdn, nil, nil)
	if err != nil {
		t.Fatalf("ValidateRequest: %v", err)
	}
	result, err := s.Generate(context.Background(), req, 365, 30)
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	return result
}

// --- 405 fallthrough / Allow header contract ---

// TestAllowHeadersMatchRegisteredRoutes asserts every hand-maintained Allow
// string in mountRoutes' fallthrough table actually matches what is
// registered: for each mutating path, every method named in the Allow string
// must not 405, and every method not named must 405 with exactly that Allow
// string. A drift here (e.g. a new method added to a handler without
// updating the fallthrough's Allow argument) would otherwise be invisible
// until a client saw a wrong Allow header.
func TestAllowHeadersMatchRegisteredRoutes(t *testing.T) {
	_, httpSrv := newHandlerTestHTTPServer(t)

	cases := []struct {
		path  string
		allow string
	}{
		{"/api/ca/init", "POST"},
		{"/api/certs", "GET, POST"},
		{"/api/certs/1", "GET, DELETE"},
		{"/api/certs/1/renew", "POST"},
		{"/api/import", "POST"},
	}
	allMethods := []string{http.MethodGet, http.MethodPost, http.MethodPut, http.MethodDelete, http.MethodPatch}

	for _, tc := range cases {
		t.Run(tc.path, func(t *testing.T) {
			allowed := make(map[string]bool)
			for _, m := range strings.Split(tc.allow, ", ") {
				allowed[m] = true
			}
			for _, method := range allMethods {
				res := doJSON(t, method, httpSrv.URL+tc.path, "")
				res.Body.Close()
				if allowed[method] {
					if res.StatusCode == http.StatusMethodNotAllowed {
						t.Errorf("%s %s = 405, want an allowed method to reach its handler", method, tc.path)
					}
					continue
				}
				if res.StatusCode != http.StatusMethodNotAllowed {
					t.Fatalf("%s %s = %d, want 405", method, tc.path, res.StatusCode)
				}
				if got := res.Header.Get("Allow"); got != tc.allow {
					t.Errorf("%s %s Allow header = %q, want %q", method, tc.path, got, tc.allow)
				}
			}
		})
	}
}

// Read-only paths (no fallthrough registered) answer a wrong method with the
// static handler's JSON 404, not a 405 -- a documented asymmetry, not an
// oversight (mountRoutes' doc comment).
func TestReadOnlyPathWrongMethodIsJSON404NotFound(t *testing.T) {
	_, httpSrv := newHandlerTestHTTPServer(t)

	for _, path := range []string{"/api/config", "/api/ca", "/api/ca/root.crt", "/api/import/preview"} {
		res := doJSON(t, http.MethodPost, httpSrv.URL+path, "")
		if res.StatusCode != http.StatusNotFound {
			t.Errorf("POST %s = %d, want 404", path, res.StatusCode)
		}
		body := decodeErrorBody(t, res)
		if body["error"] == "" {
			t.Errorf("POST %s: expected a JSON error body", path)
		}
	}
}

// --- static / unmatched-API contract, mirroring multissh's static_test.go ---

func TestStaticFallbackContract(t *testing.T) {
	_, httpSrv := newHandlerTestHTTPServer(t)

	cases := []struct {
		name       string
		method     string
		path       string
		wantStatus int
		wantBody   string
		wantJSON   bool
	}{
		{"unmatched api GET is a JSON 404", http.MethodGet, "/api/nope", http.StatusNotFound, "", true},
		{"unmatched api POST is a JSON 404", http.MethodPost, "/api/nope", http.StatusNotFound, "", true},
		{"unmatched api DELETE is a JSON 404", http.MethodDelete, "/api/deep/nope", http.StatusNotFound, "", true},
		{"deep link GET falls back to index", http.MethodGet, "/certs/detail", http.StatusOK, "INDEX", false},
		{"deep link POST is 405", http.MethodPost, "/certs/detail", http.StatusMethodNotAllowed, "", true},
		{"root serves index", http.MethodGet, "/", http.StatusOK, "INDEX", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			res := doJSON(t, tc.method, httpSrv.URL+tc.path, "")
			defer res.Body.Close()
			if res.StatusCode != tc.wantStatus {
				t.Fatalf("status = %d, want %d", res.StatusCode, tc.wantStatus)
			}
			body := mustReadAll(t, res.Body)
			if tc.wantBody != "" && !strings.Contains(string(body), tc.wantBody) {
				t.Fatalf("body = %q, want it to contain %q", body, tc.wantBody)
			}
			if tc.wantJSON {
				var decoded map[string]string
				if err := json.Unmarshal(body, &decoded); err != nil {
					t.Fatalf("body is not a JSON error object: %q", body)
				}
				if decoded["error"] == "" {
					t.Fatalf("JSON body has no error message: %q", body)
				}
			}
		})
	}
}

// --- traversal safety ---

func TestFilesRouteRejectsTraversal(t *testing.T) {
	srv, httpSrv := newHandlerTestHTTPServer(t)
	ctx := context.Background()
	if err := srv.db.InitCA(ctx, ""); err != nil {
		t.Fatalf("InitCA: %v", err)
	}
	result := generateTestCert(t, srv.db, "traversal.example.local")
	id := strconv.FormatInt(result.Cert.ID, 10)

	// Literal ".." in the path is cleaned by ServeMux/net/http before the
	// handler ever runs, producing a 307 redirect to the cleaned path.
	noRedirect := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error {
		return http.ErrUseLastResponse
	}}
	traversalPath := "/api/certs/" + id + "/files/../../../etc/passwd"
	req, err := http.NewRequest(http.MethodGet, httpSrv.URL+traversalPath, nil)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	res, err := noRedirect.Do(req)
	if err != nil {
		t.Fatalf("do: %v", err)
	}
	res.Body.Close()
	if res.StatusCode != http.StatusTemporaryRedirect {
		t.Fatalf("literal traversal status = %d, want 307", res.StatusCode)
	}

	// A percent-encoded traversal segment reaches the handler as a literal
	// string inside the {name} wildcard -- the closed enum switch rejects it
	// with a plain 404, same as any other unrecognized name.
	encodedPath := "/api/certs/" + id + "/files/%2e%2e%2frootCA.key"
	res2 := doJSON(t, http.MethodGet, httpSrv.URL+encodedPath, "")
	res2.Body.Close()
	if res2.StatusCode != http.StatusNotFound {
		t.Fatalf("percent-encoded traversal status = %d, want 404", res2.StatusCode)
	}

	// rootCA.key is never in the closed enum -- not servable through this
	// route under any name.
	res3 := doJSON(t, http.MethodGet, httpSrv.URL+"/api/certs/"+id+"/files/rootCA.key", "")
	res3.Body.Close()
	if res3.StatusCode != http.StatusNotFound {
		t.Fatalf("rootCA.key status = %d, want 404", res3.StatusCode)
	}
}

// --- full CA + cert lifecycle through HTTP ---

func TestCAInitAndGetRoundTrip(t *testing.T) {
	_, httpSrv := newHandlerTestHTTPServer(t)

	before := doJSON(t, http.MethodGet, httpSrv.URL+"/api/ca", "")
	var beforeBody struct {
		Exists bool `json:"exists"`
	}
	if err := json.NewDecoder(before.Body).Decode(&beforeBody); err != nil {
		t.Fatalf("decode: %v", err)
	}
	before.Body.Close()
	if beforeBody.Exists {
		t.Fatalf("expected no CA before init")
	}

	init := doJSON(t, http.MethodPost, httpSrv.URL+"/api/ca/init", "")
	defer init.Body.Close()
	if init.StatusCode != http.StatusCreated {
		t.Fatalf("POST /api/ca/init = %d, want 201", init.StatusCode)
	}
	var caBody struct {
		Exists  bool   `json:"exists"`
		Subject string `json:"subject"`
	}
	if err := json.NewDecoder(init.Body).Decode(&caBody); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if !caBody.Exists || caBody.Subject == "" {
		t.Fatalf("unexpected ca response: %+v", caBody)
	}

	// Second init is a conflict.
	again := doJSON(t, http.MethodPost, httpSrv.URL+"/api/ca/init", "")
	again.Body.Close()
	if again.StatusCode != http.StatusConflict {
		t.Fatalf("second POST /api/ca/init = %d, want 409", again.StatusCode)
	}

	root := doJSON(t, http.MethodGet, httpSrv.URL+"/api/ca/root.crt", "")
	defer root.Body.Close()
	if root.StatusCode != http.StatusOK {
		t.Fatalf("GET /api/ca/root.crt = %d, want 200", root.StatusCode)
	}
	cd := root.Header.Get("Content-Disposition")
	if !strings.Contains(cd, `filename="rootCA.crt"`) {
		t.Errorf("root.crt Content-Disposition = %q", cd)
	}
}

func TestGenerateGetRenewDelete(t *testing.T) {
	srv, httpSrv := newHandlerTestHTTPServer(t)
	if err := srv.db.InitCA(context.Background(), ""); err != nil {
		t.Fatalf("InitCA: %v", err)
	}

	genRes := doJSON(t, http.MethodPost, httpSrv.URL+"/api/certs", `{"fqdn":"gen.example.local"}`)
	defer genRes.Body.Close()
	if genRes.StatusCode != http.StatusCreated {
		t.Fatalf("POST /api/certs = %d, want 201", genRes.StatusCode)
	}
	var issued issueResponse
	if err := json.NewDecoder(genRes.Body).Decode(&issued); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if issued.Cert.FQDN != "gen.example.local" || issued.Cert.CertPEM == nil {
		t.Fatalf("unexpected generate response: %+v", issued)
	}
	id := strconv.FormatInt(issued.Cert.ID, 10)

	// Duplicate active fqdn is a 409.
	dupRes := doJSON(t, http.MethodPost, httpSrv.URL+"/api/certs", `{"fqdn":"gen.example.local"}`)
	dupRes.Body.Close()
	if dupRes.StatusCode != http.StatusConflict {
		t.Fatalf("duplicate POST /api/certs = %d, want 409", dupRes.StatusCode)
	}

	// GET list omits certPem.
	listRes := doJSON(t, http.MethodGet, httpSrv.URL+"/api/certs", "")
	listBody := mustReadAll(t, listRes.Body)
	listRes.Body.Close()
	if strings.Contains(string(listBody), "certPem") {
		t.Errorf("GET /api/certs body includes certPem, should be omitted: %s", listBody)
	}

	// GET detail includes certPem, no keyPem/PRIVATE KEY anywhere.
	getRes := doJSON(t, http.MethodGet, httpSrv.URL+"/api/certs/"+id, "")
	getBody := mustReadAll(t, getRes.Body)
	getRes.Body.Close()
	if !strings.Contains(string(getBody), "certPem") {
		t.Errorf("GET /api/certs/%s body missing certPem: %s", id, getBody)
	}
	if strings.Contains(string(getBody), "PRIVATE KEY") {
		t.Errorf("GET /api/certs/%s leaked a private key: %s", id, getBody)
	}

	// DELETE without confirm is 400.
	noConfirm := doJSON(t, http.MethodDelete, httpSrv.URL+"/api/certs/"+id, "")
	noConfirm.Body.Close()
	if noConfirm.StatusCode != http.StatusBadRequest {
		t.Fatalf("DELETE without confirm = %d, want 400", noConfirm.StatusCode)
	}

	// DELETE with wrong confirm is 400.
	wrongConfirm := doJSON(t, http.MethodDelete, httpSrv.URL+"/api/certs/"+id+"?confirm=wrong.example.local", "")
	wrongConfirm.Body.Close()
	if wrongConfirm.StatusCode != http.StatusBadRequest {
		t.Fatalf("DELETE with wrong confirm = %d, want 400", wrongConfirm.StatusCode)
	}

	// Renew.
	renewRes := doJSON(t, http.MethodPost, httpSrv.URL+"/api/certs/"+id+"/renew", "")
	defer renewRes.Body.Close()
	if renewRes.StatusCode != http.StatusCreated {
		t.Fatalf("POST renew = %d, want 201", renewRes.StatusCode)
	}
	var renewed issueResponse
	if err := json.NewDecoder(renewRes.Body).Decode(&renewed); err != nil {
		t.Fatalf("decode: %v", err)
	}
	newID := strconv.FormatInt(renewed.Cert.ID, 10)

	// DELETE with correct confirm (case-insensitive) succeeds with 204.
	delRes := doJSON(t, http.MethodDelete, httpSrv.URL+"/api/certs/"+newID+"?confirm=GEN.EXAMPLE.LOCAL", "")
	delRes.Body.Close()
	if delRes.StatusCode != http.StatusNoContent {
		t.Fatalf("DELETE with correct confirm = %d, want 204", delRes.StatusCode)
	}

	// GET after delete is 404.
	afterDel := doJSON(t, http.MethodGet, httpSrv.URL+"/api/certs/"+newID, "")
	afterDel.Body.Close()
	if afterDel.StatusCode != http.StatusNotFound {
		t.Fatalf("GET deleted cert = %d, want 404", afterDel.StatusCode)
	}
}

// --- downloads: cert.pem / key.pem / haproxy.pem / bundle ---

func TestDownloadRoutesAndQuarantineGuard(t *testing.T) {
	srv, httpSrv := newHandlerTestHTTPServer(t)
	if err := srv.db.InitCA(context.Background(), ""); err != nil {
		t.Fatalf("InitCA: %v", err)
	}
	result := generateTestCert(t, srv.db, "download.example.local")
	id := strconv.FormatInt(result.Cert.ID, 10)

	certRes := doJSON(t, http.MethodGet, httpSrv.URL+"/api/certs/"+id+"/files/cert.pem", "")
	certBody := mustReadAll(t, certRes.Body)
	certRes.Body.Close()
	if certRes.StatusCode != http.StatusOK {
		t.Fatalf("cert.pem = %d, want 200", certRes.StatusCode)
	}
	if !strings.Contains(string(certBody), "CERTIFICATE") {
		t.Errorf("cert.pem body missing CERTIFICATE block")
	}
	if strings.Contains(string(certBody), "PRIVATE KEY") {
		t.Errorf("cert.pem leaked a private key")
	}
	if cd := certRes.Header.Get("Content-Disposition"); !strings.Contains(cd, "download.example.local.cert.pem") {
		t.Errorf("cert.pem Content-Disposition = %q", cd)
	}

	keyRes := doJSON(t, http.MethodGet, httpSrv.URL+"/api/certs/"+id+"/files/key.pem", "")
	keyBody := mustReadAll(t, keyRes.Body)
	keyRes.Body.Close()
	if keyRes.StatusCode != http.StatusOK {
		t.Fatalf("key.pem = %d, want 200", keyRes.StatusCode)
	}
	if !strings.Contains(string(keyBody), "PRIVATE KEY") {
		t.Errorf("key.pem body missing PRIVATE KEY block")
	}

	haRes := doJSON(t, http.MethodGet, httpSrv.URL+"/api/certs/"+id+"/files/haproxy.pem", "")
	haBody := mustReadAll(t, haRes.Body)
	haRes.Body.Close()
	if haRes.StatusCode != http.StatusOK {
		t.Fatalf("haproxy.pem = %d, want 200", haRes.StatusCode)
	}
	if !strings.Contains(string(haBody), "PRIVATE KEY") {
		t.Errorf("haproxy.pem body missing PRIVATE KEY block")
	}

	bundleRes := doJSON(t, http.MethodGet, httpSrv.URL+"/api/certs/"+id+"/bundle", "")
	bundleBody := mustReadAll(t, bundleRes.Body)
	bundleRes.Body.Close()
	if bundleRes.StatusCode != http.StatusOK {
		t.Fatalf("bundle = %d, want 200", bundleRes.StatusCode)
	}
	if cd := bundleRes.Header.Get("Content-Disposition"); !strings.Contains(cd, "download.example.local.tgz") {
		t.Errorf("bundle Content-Disposition = %q", cd)
	}
	if len(bundleBody) == 0 {
		t.Errorf("bundle body is empty")
	}

	// Every download body is key material or wraps it, so none of them may sit
	// in a shared cache or a browser's disk cache after the operator closes the
	// tab. This is one header on the one funnel (writeDownload) all four routes
	// go through, asserted on all four so a future route that bypasses the
	// funnel shows up here.
	for name, res := range map[string]*http.Response{
		"cert.pem":    certRes,
		"key.pem":     keyRes,
		"haproxy.pem": haRes,
		"bundle":      bundleRes,
	} {
		if cc := res.Header.Get("Cache-Control"); cc != "no-store, max-age=0" {
			t.Errorf("%s Cache-Control = %q, want no-store, max-age=0", name, cc)
		}
	}

	// Quarantined row: cert.pem still fails (no cert_pem stored for this
	// fixture), and haproxy.pem/bundle are 409s naming the quarantine reason.
	qID := insertQuarantinedCert(t, srv.db, "broken.example.local", "private key does not match certificate")
	qid := strconv.FormatInt(qID, 10)

	qHA := doJSON(t, http.MethodGet, httpSrv.URL+"/api/certs/"+qid+"/files/haproxy.pem", "")
	qHABody := decodeErrorBody(t, qHA)
	if qHA.StatusCode != http.StatusConflict {
		t.Fatalf("quarantined haproxy.pem = %d, want 409", qHA.StatusCode)
	}
	if !strings.Contains(qHABody["error"], "quarantined") {
		t.Errorf("quarantined haproxy.pem error does not mention quarantine: %v", qHABody)
	}

	qBundle := doJSON(t, http.MethodGet, httpSrv.URL+"/api/certs/"+qid+"/bundle", "")
	qBundle.Body.Close()
	if qBundle.StatusCode != http.StatusConflict {
		t.Fatalf("quarantined bundle = %d, want 409", qBundle.StatusCode)
	}
}

// --- import routes ---

// TestImportPreviewAndExecuteWithoutLegacyDir: with the shipped default
// (legacy_import_dir: ""), both import routes must refuse *before* scanning
// anything, with a 409 that says why. The previous contract here was a 500 --
// which is what the handler actually did, because Preview's
// os.ReadFile("rootCA.crt") resolved against the process working directory and
// failed as a generic internal error. That is unactionable: the operator is
// told "internal error" for a pure configuration gap, and the scan read from
// whatever directory the binary happened to be started in. The body must still
// not leak a path.
func TestImportPreviewAndExecuteWithoutLegacyDir(t *testing.T) {
	_, httpSrv := newHandlerTestHTTPServer(t)

	for _, tc := range []struct {
		name, method, path, body string
	}{
		{"preview", http.MethodGet, "/api/import/preview", ""},
		{"execute", http.MethodPost, "/api/import", `{"confirmNonEmpty":true}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			res := doJSON(t, tc.method, httpSrv.URL+tc.path, tc.body)
			body := decodeErrorBody(t, res)
			if res.StatusCode != http.StatusConflict {
				t.Fatalf("%s with no legacy dir = %d, want 409", tc.name, res.StatusCode)
			}
			if !strings.Contains(body["error"], "legacy_import_dir is not configured") {
				t.Errorf("409 body does not name the reason: %v", body)
			}
			if strings.Contains(body["error"], "/") || strings.Contains(body["error"], "no such file") {
				t.Errorf("409 body leaked path/file detail: %v", body)
			}
		})
	}
}

// scanLegacyTree is the layer under those handlers, and nothing in its
// signature says a caller has gated on legacy_import_dir first -- an empty
// dir must not silently resolve rootCA.crt against the process working
// directory.
func TestScanLegacyTreeRefusesEmptyDir(t *testing.T) {
	if _, _, _, err := scanLegacyTree(""); err == nil {
		t.Fatal("scanLegacyTree(\"\") = nil error, want a refusal")
	}
}

// TestImportRoutesGateOnLegacyImportReason: a configured-but-unusable
// legacy_import_dir (checkLegacyImportDir's reason, e.g. a path that is a file
// rather than a directory) is the same class of answer as an unconfigured one
// -- a 409 quoting the reason, not a scan attempt.
func TestImportRoutesGateOnLegacyImportReason(t *testing.T) {
	dir := t.TempDir()
	notADir := filepath.Join(dir, "pki-is-a-file")
	if err := os.WriteFile(notADir, []byte("x"), 0o600); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
	_, httpSrv := newHandlerTestHTTPServerWithLegacyDir(t, notADir)

	res := doJSON(t, http.MethodGet, httpSrv.URL+"/api/import/preview", "")
	body := decodeErrorBody(t, res)
	if res.StatusCode != http.StatusConflict {
		t.Fatalf("preview with an unusable legacy dir = %d, want 409", res.StatusCode)
	}
	if !strings.Contains(body["error"], "is not a directory") {
		t.Errorf("409 body does not carry the build-time reason: %v", body)
	}
}

// A failed Execute is the case where the operator most needs detail, and it was
// the one case the API threw detail away: the handler wrote only
// {"error": "..."} and dropped the partial ImportReport that Execute
// deliberately returns alongside the error. With a few hundred legacy
// directories, "a certificate is already active for this fqdn" with no report
// and no hostname is unactionable. The failure body must carry both the naming
// error and the classification that got as far as it did.
func TestImportExecuteFailureCarriesPartialReport(t *testing.T) {
	dir, _, _ := writeLegacyTree(t)
	srv, httpSrv := newHandlerTestHTTPServerWithLegacyDir(t, dir)

	// A natively-issued active row for one of the tree's FQDNs: not
	// skip-matchable (no imported_from, unrelated serial), so the leaf batch
	// reaches InsertCert and collides there.
	if _, err := insertCert(srv.db, fixtureCert("valid.example.local", StatusActive, "2030-01-01T00:00:00Z")); err != nil {
		t.Fatalf("seed collision row: %v", err)
	}

	res := doJSON(t, http.MethodPost, httpSrv.URL+"/api/import", `{"confirmNonEmpty":true}`)
	raw := mustReadAll(t, res.Body)
	res.Body.Close()
	if res.StatusCode != http.StatusConflict {
		t.Fatalf("import with a colliding active fqdn = %d, want 409 (body %s)", res.StatusCode, raw)
	}

	var body struct {
		Error  string        `json:"error"`
		Report *ImportReport `json:"report"`
	}
	if err := json.Unmarshal(raw, &body); err != nil {
		t.Fatalf("failure body is not JSON: %v (%s)", err, raw)
	}
	if !strings.Contains(body.Error, "valid.example.local") {
		t.Errorf("failure message names no fqdn, so the operator cannot find the offending directory: %q", body.Error)
	}
	if body.Report == nil {
		t.Fatal("failure body carries no partial report")
	}
	if len(body.Report.Items) != legacyTreeLeafCount {
		t.Errorf("partial report Items = %d, want the full classification (%d)", len(body.Report.Items), legacyTreeLeafCount)
	}
}

// F6: the importer used to store whatever bytes cert.pem held whenever
// classification quarantined the leaf, so a legacy directory whose cert.pem was
// actually a private key (a plausible copy/paste mistake, and exactly the kind
// of thing an "import everything" importer meets) ended up with key material in
// certs.cert_pem -- served back out of the detail route and the cert.pem
// download. Retaining cert_pem only for a real CERTIFICATE block keeps the blast
// radius of a malformed source file on disk.
func TestQuarantinedNonCertificateBytesAreNotStored(t *testing.T) {
	dir := t.TempDir()
	writeLegacyRootFixture(t, dir)
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	keyPEM := encodeKeyPEM(key)
	writeBrokenLeaf(t, dir, "keyincert.example.local", func(leafDir string) {
		if err := os.WriteFile(filepath.Join(leafDir, legacyLeafCertFilename), keyPEM, 0o600); err != nil {
			t.Fatalf("write cert.pem: %v", err)
		}
		if err := os.WriteFile(filepath.Join(leafDir, legacyLeafKeyFilename), keyPEM, 0o600); err != nil {
			t.Fatalf("write key.pem: %v", err)
		}
	})

	srv, httpSrv := newHandlerTestHTTPServerWithLegacyDir(t, dir)
	res := doJSON(t, http.MethodPost, httpSrv.URL+"/api/import", `{"confirmNonEmpty":true}`)
	raw := mustReadAll(t, res.Body)
	res.Body.Close()
	if res.StatusCode != http.StatusOK {
		t.Fatalf("import = %d, want 200 (body %s)", res.StatusCode, raw)
	}

	certs, err := srv.db.ListCerts(context.Background())
	if err != nil {
		t.Fatalf("ListCerts: %v", err)
	}
	if len(certs) != 1 {
		t.Fatalf("len(certs) = %d, want 1", len(certs))
	}
	row := certs[0]
	if row.Status != StatusQuarantined {
		t.Fatalf("status = %q, want quarantined", row.Status)
	}
	if row.CertPEM != nil {
		t.Errorf("cert_pem stored for a non-certificate source file: %q", *row.CertPEM)
	}

	id := strconv.FormatInt(row.ID, 10)
	for _, path := range []string{"/api/certs/" + id, "/api/certs/" + id + "/files/cert.pem"} {
		res := doJSON(t, http.MethodGet, httpSrv.URL+path, "")
		body := string(mustReadAll(t, res.Body))
		res.Body.Close()
		if strings.Contains(body, "PRIVATE KEY") {
			t.Errorf("GET %s served private key material: %s", path, body)
		}
	}
}

// Team-fix loop 2: classifyLeaf used to store cert.pem's raw file bytes
// wholesale once the FIRST PEM block checked out as a CERTIFICATE, so a
// cert.pem holding a CERTIFICATE block followed by a trailing PRIVATE KEY
// block -- exactly the shape bundle.go's own HAProxyPEM concatenation
// produces, and so a plausible operator mis-copy of haproxy.pem onto
// cert.pem -- imported as a normal, non-quarantined active row whose stored
// cert_pem still carried the trailing key. That key was then served back out
// verbatim by both the /api/certs/{id} detail route and the public
// files/cert.pem download. classifyLeaf now stores only the canonical
// re-encoding of the single decoded CERTIFICATE block, so cert_pem can never
// carry anything past it.
func TestLeafCertPEMDiscardsTrailingPrivateKeyBlock(t *testing.T) {
	dir := t.TempDir()
	_, _, rootCert, rootKey := writeLegacyRootFixture(t, dir)
	leafCertPEM, leafKeyPEM, _ := writeLegacyLeafFixture(t, dir, "trailingkey.example.local", "trailingkey.example.local", rootCert, rootKey, leafOpts{})

	// Simulate the mis-copy: cert.pem ends up holding the CERTIFICATE block
	// immediately followed by the leaf's own PRIVATE KEY block, the same
	// two-block shape HAProxyPEM's concatenation produces.
	combined := append(append([]byte{}, leafCertPEM...), leafKeyPEM...)
	leafDir := filepath.Join(dir, "certs", "trailingkey.example.local")
	if err := os.WriteFile(filepath.Join(leafDir, legacyLeafCertFilename), combined, 0o600); err != nil {
		t.Fatalf("overwrite cert.pem with trailing key block: %v", err)
	}

	srv, httpSrv := newHandlerTestHTTPServerWithLegacyDir(t, dir)
	res := doJSON(t, http.MethodPost, httpSrv.URL+"/api/import", `{"confirmNonEmpty":true}`)
	raw := mustReadAll(t, res.Body)
	res.Body.Close()
	if res.StatusCode != http.StatusOK {
		t.Fatalf("import = %d, want 200 (body %s)", res.StatusCode, raw)
	}

	ctx := context.Background()
	metas, err := srv.db.ListCerts(ctx)
	if err != nil {
		t.Fatalf("ListCerts: %v", err)
	}
	var id int64 = -1
	for _, c := range metas {
		if c.FQDN == "trailingkey.example.local" {
			id = c.ID
		}
	}
	if id == -1 {
		t.Fatal("no imported row for trailingkey.example.local")
	}
	row, err := srv.db.GetCert(ctx, id)
	if err != nil {
		t.Fatalf("GetCert: %v", err)
	}
	if row.CertPEM == nil {
		t.Fatal("cert_pem not stored at all for a leaf with a valid leading CERTIFICATE block")
	}
	if strings.Contains(*row.CertPEM, "PRIVATE KEY") {
		t.Errorf("stored cert_pem still carries the trailing private key: %q", *row.CertPEM)
	}
	block, rest := pem.Decode([]byte(*row.CertPEM))
	if block == nil {
		t.Fatal("stored cert_pem does not even decode as PEM")
	}
	if block.Type != pemTypeCertificate {
		t.Errorf("stored cert_pem block type = %q, want %q", block.Type, pemTypeCertificate)
	}
	if trailing, _ := pem.Decode(rest); trailing != nil {
		t.Errorf("stored cert_pem holds more than one PEM block (found a second %q block)", trailing.Type)
	}

	idStr := strconv.FormatInt(row.ID, 10)
	for _, path := range []string{"/api/certs/" + idStr, "/api/certs/" + idStr + "/files/cert.pem"} {
		res := doJSON(t, http.MethodGet, httpSrv.URL+path, "")
		body := string(mustReadAll(t, res.Body))
		res.Body.Close()
		if strings.Contains(body, "PRIVATE KEY") {
			t.Errorf("GET %s served private key material: %s", path, body)
		}
	}
}

// F9: both CA-reconciliation refusals carry caReplacementRemedy -- the only
// documented way out of "the legacy root is not the root I already store" --
// and both used to fall through storeErrorStatus's default branch, which
// replaces the message with "internal error" and logs the rest. A mismatched
// root is a conflict with current state, and the remedy has to reach the
// operator.
func TestImportWithMismatchedRootIsA409CarryingTheRemedy(t *testing.T) {
	for _, tc := range []struct {
		name    string
		corrupt func(t *testing.T, dir string)
		// want is the substring that makes the refusal actionable: the
		// CA-replacement remedy for a fingerprint mismatch (nothing else
		// documents a way out), and the two filenames for a key mismatch,
		// where the fix is on disk and replacing the CA would be wrong advice.
		want string
	}{
		{
			name:    "different root than the stored CA",
			corrupt: func(t *testing.T, dir string) {},
			want:    caReplacementRemedy,
		},
		{
			name: "rootCA.key does not match rootCA.crt",
			corrupt: func(t *testing.T, dir string) {
				other, err := rsa.GenerateKey(rand.Reader, 2048)
				if err != nil {
					t.Fatalf("generate unrelated key: %v", err)
				}
				if err := os.WriteFile(filepath.Join(dir, legacyRootKeyFilename), encodeKeyPEM(other), 0o600); err != nil {
					t.Fatalf("overwrite rootCA.key: %v", err)
				}
			},
			want: "rootCA.key does not match rootCA.crt",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			writeLegacyRootFixture(t, dir)
			tc.corrupt(t, dir)

			srv, httpSrv := newHandlerTestHTTPServerWithLegacyDir(t, dir)
			// An unrelated CA already in the store: the fingerprint of the
			// legacy root cannot match it.
			setupCA(t, srv.db, time.Now().AddDate(5, 0, 0))

			res := doJSON(t, http.MethodPost, httpSrv.URL+"/api/import", `{"confirmNonEmpty":true}`)
			body := decodeErrorBody(t, res)
			if res.StatusCode != http.StatusConflict {
				t.Fatalf("import with a mismatched root = %d, want 409 (body %v)", res.StatusCode, body)
			}
			if !strings.Contains(body["error"], tc.want) {
				t.Errorf("409 message is missing %q: %v", tc.want, body)
			}
			if body["error"] == "internal error" {
				t.Error("mismatch was reported as an internal error")
			}
		})
	}
}

// --- request body bounds (F7) ---

// The POST routes are unauthenticated, so an unbounded body is a free
// memory-exhaustion lever: json.Decode happily materializes a multi-gigabyte
// dnsSans array before ValidateRequest ever sees it. Two independent bounds are
// asserted here -- the byte cap on the wire, and the SAN count checked before
// normalization (which would otherwise allocate a normalized copy of every
// entry first).
func TestPostBodiesAreBounded(t *testing.T) {
	srv, httpSrv := newHandlerTestHTTPServer(t)
	if err := srv.db.InitCA(context.Background(), ""); err != nil {
		t.Fatalf("InitCA: %v", err)
	}

	t.Run("over-limit body is refused without being buffered", func(t *testing.T) {
		var sb strings.Builder
		sb.WriteString(`{"fqdn":"big.example.local","dnsSans":[`)
		for i := 0; sb.Len() < maxRequestBodyBytes+4096; i++ {
			if i > 0 {
				sb.WriteString(",")
			}
			fmt.Fprintf(&sb, `"host%d.example.local"`, i)
		}
		sb.WriteString(`]}`)

		res := doJSON(t, http.MethodPost, httpSrv.URL+"/api/certs", sb.String())
		body := decodeErrorBody(t, res)
		if res.StatusCode != http.StatusRequestEntityTooLarge {
			t.Fatalf("over-limit body = %d, want 413", res.StatusCode)
		}
		if !strings.Contains(body["error"], "limit") {
			t.Errorf("413 body does not say what was exceeded: %v", body)
		}
	})

	t.Run("over-cap SAN count is refused before normalizing", func(t *testing.T) {
		sans := make([]string, maxSANCount+1)
		for i := range sans {
			sans[i] = fmt.Sprintf("host%d.example.local", i)
		}
		encoded, err := json.Marshal(generateRequest{FQDN: "cap.example.local", DNSSans: sans})
		if err != nil {
			t.Fatalf("marshal: %v", err)
		}

		res := doJSON(t, http.MethodPost, httpSrv.URL+"/api/certs", string(encoded))
		body := decodeErrorBody(t, res)
		if res.StatusCode != http.StatusBadRequest {
			t.Fatalf("over-cap SAN list = %d, want 400", res.StatusCode)
		}
		if !strings.Contains(body["error"], strconv.Itoa(maxSANCount)) {
			t.Errorf("400 body does not name the cap: %v", body)
		}
	})
}

// --- forced internal failure: no SQL text in a 500 body ---

func TestInternalErrorBodyNeverLeaksDetail(t *testing.T) {
	srv, httpSrv := newHandlerTestHTTPServer(t)
	// Close the underlying DB handle to force a real driver-level failure on
	// the next query -- the cheapest way to reach writeStoreError's default
	// (unrecognized error) branch without fabricating a new sentinel.
	if err := srv.db.Close(); err != nil {
		t.Fatalf("close db: %v", err)
	}

	res := doJSON(t, http.MethodGet, httpSrv.URL+"/api/certs", "")
	body := decodeErrorBody(t, res)
	if res.StatusCode != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", res.StatusCode)
	}
	if body["error"] != "internal error" {
		t.Errorf("500 body = %v, want a generic message", body)
	}
	lower := strings.ToLower(body["error"])
	for _, leak := range []string{"sql", "database", "sqlite", ".db"} {
		if strings.Contains(lower, leak) {
			t.Errorf("500 body leaked internal detail %q: %v", leak, body)
		}
	}
}
