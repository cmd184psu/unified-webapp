// Package certmachine wires the certmachine HTTP surface: the single-page
// frontend served from static_dir plus (starting in later slices) the PKI
// API. It is one module of the unified binary, reached by Host-header
// dispatch, and exposes Build like every other module.
package certmachine

import (
	"fmt"
	"log"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strings"

	"cmd184psu/unified-webapp/internal/platform/config"
	"cmd184psu/unified-webapp/internal/platform/response"
)

// Options configures a Server.
type Options struct {
	StaticDir           string
	DBPath              string
	LegacyImportDir     string
	DefaultValidityDays int
	ExpiryWarnDays      int
}

// Server holds the resolved dependencies and the routing mux.
type Server struct {
	opts Options
	db   *Store
	mux  *http.ServeMux

	// legacyImportReason is set when LegacyImportDir is configured but
	// missing or unreadable. It is not a build failure (binding decision,
	// slice 1 doc) -- a later slice surfaces it via /api/config.
	legacyImportReason string
}

// New builds a Server: validates static_dir, opens the store, and mounts the
// static handler plus the (initially empty) API mux. It fails rather than
// degrades -- an uncreatable directory, unopenable DB, or failed migration
// returns an error instead of a Server that silently serves nothing useful.
func New(opts Options) (*Server, error) {
	// Normalize the two path options once, here, so every later reader sees
	// the same value: checkLegacyImportDir used to validate the trimmed
	// string while logBoot and the import handlers used the untrimmed one,
	// which disagree for a config value with stray whitespace.
	opts.StaticDir = strings.TrimSpace(opts.StaticDir)
	opts.LegacyImportDir = strings.TrimSpace(opts.LegacyImportDir)

	if err := checkStaticDir(opts.StaticDir); err != nil {
		return nil, err
	}

	db, err := Open(opts.DBPath)
	if err != nil {
		return nil, err
	}

	s := &Server{opts: opts, db: db, mux: http.NewServeMux()}

	if opts.LegacyImportDir != "" {
		if reason := checkLegacyImportDir(opts.LegacyImportDir); reason != "" {
			s.legacyImportReason = reason
			log.Printf("certmachine: %s", reason)
		}
	}

	s.mountRoutes()
	s.mux.Handle("/", staticHandler(opts.StaticDir))

	return s, nil
}

// Handler returns the server's root http.Handler, with the platform's
// permissive CORS headers stripped back off (see stripCORS).
func (s *Server) Handler() http.Handler {
	return stripCORS(s.mux)
}

// corsResponseHeaders are the headers internal/platform/middleware's Wrap
// sets on every module's response. certmachine deletes all three rather than
// narrowing them: it has no cross-origin caller at all.
var corsResponseHeaders = []string{
	"Access-Control-Allow-Origin",
	"Access-Control-Allow-Methods",
	"Access-Control-Allow-Headers",
}

// stripCORS removes the permissive CORS headers an outer middleware already
// set, and refuses OPTIONS outright.
//
// cmd/server/main.go wraps the whole dispatcher in middleware.Wrap, which
// sets Access-Control-Allow-Origin: * on every response. For certmachine
// that is a live vulnerability, not merely untidy: every download route
// serves private key material, so `*` invites any page on the internet to
// read a leaf's key.pem out of a logged-in operator's browser. The fix lives
// here rather than in the platform middleware because the other six modules
// may depend on those headers.
//
// Deleting them from the inner handler works because Wrap sets them *before*
// calling next and nothing is flushed until the first write: w.Header() is
// still a mutable map at this point, so a Del here is what the client
// actually sees. If Wrap ever moves its Set after next.ServeHTTP, this stops
// working and TestCertmachineResponsesCarryNoCORSHeaders
// (cmd/server/dispatch_test.go) fails -- which is exactly why that test goes
// through the same middleware.Wrap main.go applies.
//
// OPTIONS is answered 405 here for the direct-mount case (Wrap short-circuits
// preflight before this handler ever runs, so behind Wrap the browser sees
// its 204 -- harmless, because a preflight pass still cannot expose a
// response body that carries no Access-Control-Allow-Origin of its own).
func stripCORS(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		for _, h := range corsResponseHeaders {
			w.Header().Del(h)
		}
		if r.Method == http.MethodOptions {
			w.Header().Set("Allow", "GET, HEAD, POST, DELETE")
			response.WriteError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		next.ServeHTTP(w, r)
	})
}

// Close releases the underlying database handle.
func (s *Server) Close() error {
	return s.db.Close()
}

// closableHandler pairs the module handler with the Server's Close. The
// dispatcher (cmd/server/main.go) registers any module handler that
// implements io.Closer and closes it on Dispatcher.Close, which its
// goleak-gated tests rely on to stop the database/sql pool goroutine.
type closableHandler struct {
	http.Handler
	srv *Server
}

func (c closableHandler) Close() error { return c.srv.Close() }

// Build returns a ready-to-use http.Handler for the certmachine module. The
// caller is responsible for wrapping it with middleware. The returned handler
// implements io.Closer (releasing the DB handle) for the dispatcher's
// module-shutdown hook.
func Build(cfg config.CertmachineConfig) (http.Handler, error) {
	srv, err := New(Options{
		StaticDir:           cfg.StaticDir,
		DBPath:              cfg.DBPath,
		LegacyImportDir:     cfg.LegacyImportDir,
		DefaultValidityDays: cfg.DefaultValidityDays,
		ExpiryWarnDays:      cfg.ExpiryWarnDays,
	})
	if err != nil {
		return nil, err
	}
	return closableHandler{Handler: srv.Handler(), srv: srv}, nil
}

// checkStaticDir refuses a static_dir that is not a readable directory. The
// alternative -- warn and serve 404s -- is indistinguishable at runtime from
// a routing mistake, and the operator only finds out by loading the UI.
func checkStaticDir(dir string) error {
	if dir == "" {
		return fmt.Errorf("certmachine: static_dir is not set")
	}
	info, err := os.Stat(dir)
	if err != nil {
		return fmt.Errorf("certmachine: static_dir %s: %w", dir, err)
	}
	if !info.IsDir() {
		return fmt.Errorf("certmachine: static_dir %s is not a directory", dir)
	}
	entries, err := os.Open(dir)
	if err != nil {
		return fmt.Errorf("certmachine: static_dir %s is not readable: %w", dir, err)
	}
	_ = entries.Close()
	return nil
}

// checkLegacyImportDir reports a non-empty reason string when dir cannot be
// used as a legacy import source. An empty return means dir is usable.
func checkLegacyImportDir(dir string) string {
	info, err := os.Stat(dir)
	if err != nil {
		return fmt.Sprintf("legacy_import_dir %s: %v", dir, err)
	}
	if !info.IsDir() {
		return fmt.Sprintf("legacy_import_dir %s is not a directory", dir)
	}
	return ""
}

// onlyGet restricts a handler to GET and HEAD.
func onlyGet(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			w.Header().Set("Allow", "GET, HEAD")
			response.WriteError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		next.ServeHTTP(w, r)
	})
}

// noDirList returns 404 for directory index requests (paths ending in "/",
// other than the SPA root) so the embedded tree is not browsable.
func noDirList(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" && len(r.URL.Path) > 0 && r.URL.Path[len(r.URL.Path)-1] == '/' {
			http.NotFound(w, r)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// staticHandler serves the built frontend from dir, matching multissh's
// contract (internal/multissh/server.go): a mistyped API path fails as an API
// call, not an HTML page with status 200.
//
//	unmatched /api/... (any method) -> 404 with a JSON error body
//	other path, GET or HEAD, no file -> index.html (SPA deep-link fallback)
//	other path, other method, no file -> 405
//	existing file                     -> served, no directory listings
func staticHandler(dir string) http.Handler {
	assets := onlyGet(noDirList(http.FileServer(http.Dir(dir))))
	index := filepath.Join(dir, "index.html")
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/api/") {
			response.WriteError(w, http.StatusNotFound, "not found")
			return
		}
		if staticFileExists(dir, r.URL.Path) {
			assets.ServeHTTP(w, r)
			return
		}
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			w.Header().Set("Allow", "GET, HEAD")
			response.WriteError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		http.ServeFile(w, r, index)
	})
}

// staticFileExists reports whether urlPath names a regular file under dir.
// Directories deliberately report false so they take the SPA fallback
// instead of ever producing a listing. The path is cleaned against "/"
// first, so ".." segments cannot escape dir.
func staticFileExists(dir, urlPath string) bool {
	clean := path.Clean("/" + urlPath)
	if clean == "/" {
		clean = "/index.html"
	}
	info, err := os.Stat(filepath.Join(dir, filepath.FromSlash(clean)))
	return err == nil && info.Mode().IsRegular()
}
