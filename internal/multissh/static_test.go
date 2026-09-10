package multissh

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// staticTestDir lays out a minimal built frontend: an index, one asset, and a
// subdirectory so the directory-listing row can be exercised.
func staticTestDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "index.html"), []byte("<!doctype html>INDEX"), 0o644); err != nil {
		t.Fatalf("write index: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(dir, "assets"), 0o755); err != nil {
		t.Fatalf("mkdir assets: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "assets", "app.js"), []byte("console.log(1)"), 0o644); err != nil {
		t.Fatalf("write asset: %v", err)
	}
	return dir
}

// The four rows of the static fallback contract. The one that matters most is
// the first: an unmatched /api/ path must fail as an API call, because a 200
// HTML body would look like success to the client.
func TestStaticFallbackContract(t *testing.T) {
	srv := New(Options{StaticDir: staticTestDir(t), UploadDir: t.TempDir(), MaxUploadBytes: 1024, MaxSessions: 3})
	httpSrv := httptest.NewServer(srv.Handler())
	defer httpSrv.Close()

	cases := []struct {
		name       string
		method     string
		path       string
		wantStatus int
		wantBody   string // substring; empty means do not check
		wantJSON   bool
	}{
		{"unmatched api GET is a JSON 404", http.MethodGet, "/api/nope", http.StatusNotFound, "", true},
		{"unmatched api POST is a JSON 404", http.MethodPost, "/api/nope", http.StatusNotFound, "", true},
		{"unmatched api DELETE is a JSON 404", http.MethodDelete, "/api/deep/nope", http.StatusNotFound, "", true},
		{"deep link GET falls back to index", http.MethodGet, "/hosts/detail", http.StatusOK, "INDEX", false},
		{"deep link HEAD falls back to index", http.MethodHead, "/hosts/detail", http.StatusOK, "", false},
		{"deep link POST is 405", http.MethodPost, "/hosts/detail", http.StatusMethodNotAllowed, "", true},
		{"root serves index", http.MethodGet, "/", http.StatusOK, "INDEX", false},
		{"existing asset is served", http.MethodGet, "/assets/app.js", http.StatusOK, "console.log(1)", false},
		{"asset POST is 405", http.MethodPost, "/assets/app.js", http.StatusMethodNotAllowed, "", true},
		{"directory never lists", http.MethodGet, "/assets/", http.StatusOK, "INDEX", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req, err := http.NewRequest(tc.method, httpSrv.URL+tc.path, nil)
			if err != nil {
				t.Fatalf("new request: %v", err)
			}
			res, err := http.DefaultClient.Do(req)
			if err != nil {
				t.Fatalf("do: %v", err)
			}
			defer res.Body.Close()
			if res.StatusCode != tc.wantStatus {
				t.Fatalf("status = %d, want %d", res.StatusCode, tc.wantStatus)
			}
			body, err := io.ReadAll(res.Body)
			if err != nil {
				t.Fatalf("read body: %v", err)
			}
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
				if strings.Contains(string(body), "INDEX") {
					t.Fatalf("error response leaked the SPA shell: %q", body)
				}
			}
		})
	}
}

// A registered API route called with the wrong method falls through to the
// static handler, because the catch-all "/" pattern matches it. It must still
// answer as an API call -- a JSON 404, never the SPA shell.
func TestStaticFallbackAnswersWrongMethodAPIRoutesAsAPI(t *testing.T) {
	srv := New(Options{StaticDir: staticTestDir(t), UploadDir: t.TempDir(), MaxUploadBytes: 1024, MaxSessions: 3})
	httpSrv := httptest.NewServer(srv.Handler())
	defer httpSrv.Close()

	res, err := http.Post(httpSrv.URL+"/api/config", "application/json", strings.NewReader("{}"))
	if err != nil {
		t.Fatalf("post: %v", err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", res.StatusCode)
	}
	body, err := io.ReadAll(res.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	var decoded map[string]string
	if err := json.Unmarshal(body, &decoded); err != nil {
		t.Fatalf("body is not a JSON error object: %q", body)
	}
	if strings.Contains(string(body), "INDEX") {
		t.Fatalf("wrong-method API call returned the SPA shell: %q", body)
	}
}

// FR-N1 backend half: the SPA sizes its rail and grid from this number before
// any hosts exist.
func TestConfigEndpointReportsMaxSessions(t *testing.T) {
	srv := New(Options{UploadDir: t.TempDir(), MaxUploadBytes: 1024, MaxSessions: 5})
	httpSrv := httptest.NewServer(srv.Handler())
	defer httpSrv.Close()

	res, err := http.Get(httpSrv.URL + "/api/config")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", res.StatusCode)
	}
	var decoded map[string]int
	if err := json.NewDecoder(res.Body).Decode(&decoded); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if decoded["maxSessions"] != 5 {
		t.Fatalf("maxSessions = %d, want 5", decoded["maxSessions"])
	}
}
