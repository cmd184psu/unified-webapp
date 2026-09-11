package main

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"cmd184psu/unified-webapp/internal/platform/config"
	"cmd184psu/unified-webapp/internal/platform/middleware"
)

// mkStaticDir creates a directory containing an index.html file, suitable for
// static.Handler's SPA fallback, and returns its path.
func mkStaticDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "index.html"), []byte("INDEX"), 0o644); err != nil {
		t.Fatalf("write index.html: %v", err)
	}
	return dir
}

// routeMatrixConfig builds a config that routes every write-bearing module to
// its own hostname, all buildable from scratch under t.TempDir(), with
// server.origin_check set to mode.
func routeMatrixConfig(t *testing.T, mode string) *config.Config {
	t.Helper()
	cfg := config.DefaultConfig()
	cfg.Server.OriginCheck = mode
	cfg.Routing = map[string]string{
		"grocery.example":     "grocery",
		"todo.example":        "todo",
		"slideshow.example":   "slideshow",
		"obsidianoid.example": "obsidianoid",
		"ssh.example":         "multissh",
		"menu.example":        "menuserver",
	}

	cfg.Grocery.StaticDir = mkStaticDir(t)
	cfg.Grocery.DataFile = filepath.Join(t.TempDir(), "grocery.json")

	cfg.Todo.StaticDir = mkStaticDir(t)
	cfg.Todo.DataDir = t.TempDir()

	cfg.Slideshow.StaticDir = mkStaticDir(t)
	cfg.Slideshow.ImageDir = t.TempDir()

	cfg.Obsidianoid.StaticDir = mkStaticDir(t)
	cfg.Obsidianoid.DataDir = t.TempDir()
	cfg.Obsidianoid.Vaults = []config.ObsidianoidVault{{Path: t.TempDir(), Name: "test"}}

	cfg.Menuserver.StaticDir = mkStaticDir(t)
	cfg.Menuserver.DataDir = t.TempDir()

	root := t.TempDir()
	sshDir := filepath.Join(root, "ssh")
	if err := os.MkdirAll(sshDir, 0o755); err != nil {
		t.Fatalf("mkdir ssh: %v", err)
	}
	cfg.Multissh.StaticDir = mkStaticDir(t)
	cfg.Multissh.SSHDir = sshDir
	cfg.Multissh.UploadDir = filepath.Join(root, "uploads")
	cfg.Multissh.HostsPath = filepath.Join(root, "data", "hosts.json")
	cfg.Multissh.BrowseRoot = filepath.Join(root, "uploads")
	cfg.Multissh.MaxSessions = 3
	cfg.Multissh.MaxUploadBytes = 1 << 20

	return cfg
}

// newOriginCheckedServer wires a dispatcher the same way cmd/server/main.go
// does: OriginCheck inside Wrap, outside the dispatcher.
func newOriginCheckedServer(t *testing.T, cfg *config.Config) *httptest.Server {
	dispatch := buildDispatcher(cfg, noAuthService(t))
	handler := middleware.Wrap(middleware.OriginCheck(cfg.Server.OriginCheck, dispatch))
	return httptest.NewServer(handler)
}

func foreignOriginPOST(t *testing.T, srv *httptest.Server, host, path string) *http.Response {
	t.Helper()
	req, err := http.NewRequest(http.MethodPost, srv.URL+path, strings.NewReader("not json, not multipart, just text"))
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	req.Host = host
	req.Header.Set("Origin", "http://evil.example")
	req.Header.Set("Content-Type", "text/plain")
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("do request: %v", err)
	}
	return res
}

// FR-O2/O3: under origin_check=enforce, a foreign-Origin write request to any
// module's write route is rejected before it reaches the module handler, and
// the module's on-disk/in-memory state is left untouched.
func TestOriginCheckRouteMatrix_Enforce(t *testing.T) {
	cfg := routeMatrixConfig(t, "enforce")
	srv := newOriginCheckedServer(t, cfg)
	defer srv.Close()

	cases := []struct {
		name  string
		host  string
		path  string
		probe string // cheap GET path to confirm no state change; empty skips the probe
	}{
		{"grocery POST /api/items", "grocery.example", "/api/items", "/api/items"},
		{"todo POST /config/columns", "todo.example", "/config/columns", "/config/columns"},
		{"slideshow POST /api/control", "slideshow.example", "/api/control", "/api/state"},
		{"obsidianoid POST /api/render", "obsidianoid.example", "/api/render", ""},
		{"multissh POST /api/broadcast", "ssh.example", "/api/broadcast", "/api/hosts"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var before string
			if tc.probe != "" {
				res := doHost(t, srv, http.MethodGet, tc.host, tc.probe, "")
				b, _ := io.ReadAll(res.Body)
				res.Body.Close()
				before = string(b)
			}

			res := foreignOriginPOST(t, srv, tc.host, tc.path)
			defer res.Body.Close()
			body, _ := io.ReadAll(res.Body)
			if res.StatusCode != http.StatusForbidden {
				t.Fatalf("status = %d, want 403 (body %q)", res.StatusCode, body)
			}
			var envelope map[string]string
			if err := json.Unmarshal(body, &envelope); err != nil {
				t.Fatalf("403 body is not the standard JSON error envelope: %v (%s)", err, body)
			}
			if _, ok := envelope["error"]; !ok {
				t.Fatalf("403 body missing %q field: %s", "error", body)
			}

			if tc.probe != "" {
				after := doHost(t, srv, http.MethodGet, tc.host, tc.probe, "")
				defer after.Body.Close()
				afterBody, _ := io.ReadAll(after.Body)
				if string(afterBody) != before {
					t.Fatalf("state changed after rejected write -- before %q, after %q", before, string(afterBody))
				}
			}
		})
	}
}

// menuserver registers no POST /items route at all (Register only wires GET
// routes). Under origin_check=enforce the request is still rejected up front;
// under origin_check=off it reaches the module and gets whatever the module
// does today for an unmatched POST -- its static-file catch-all SPA-falls-back
// to index.html regardless of method or body, i.e. 200, not 404/405.
func TestOriginCheckRouteMatrix_Menuserver(t *testing.T) {
	t.Run("enforce rejects", func(t *testing.T) {
		cfg := routeMatrixConfig(t, "enforce")
		srv := newOriginCheckedServer(t, cfg)
		defer srv.Close()

		res := foreignOriginPOST(t, srv, "menu.example", "/items")
		defer res.Body.Close()
		if res.StatusCode != http.StatusForbidden {
			body, _ := io.ReadAll(res.Body)
			t.Fatalf("status = %d, want 403 (body %q)", res.StatusCode, body)
		}
	})

	t.Run("off passes through to the module's own behavior", func(t *testing.T) {
		cfg := routeMatrixConfig(t, "off")
		srv := newOriginCheckedServer(t, cfg)
		defer srv.Close()

		res := foreignOriginPOST(t, srv, "menu.example", "/items")
		defer res.Body.Close()
		body, _ := io.ReadAll(res.Body)
		if res.StatusCode != http.StatusOK {
			t.Fatalf("status = %d, want 200 (module's real behavior for an unmatched POST route today), body %q", res.StatusCode, body)
		}
	})
}
