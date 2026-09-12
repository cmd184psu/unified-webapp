// Package multissh wires the multissh HTTP surface: the single-page frontend
// served from static_dir plus the SSH endpoints (key listing and the
// per-terminal WebSocket bridge). It is one module of the unified binary,
// reached by Host-header dispatch, and exposes Build like every other module.
package multissh

import (
	"encoding/json"
	"log"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strings"

	"cmd184psu/unified-webapp/internal/multissh/sshproxy"
	"cmd184psu/unified-webapp/internal/platform/response"
)

// Options configures a Server. SSHHandler is the WebSocket bridge and SSHKeyDir
// is the fixed directory whose regular files back the key picker (typically the
// server user's ~/.ssh). StaticDir is the directory holding the built frontend.
// MaxSessions is the terminal/host/target ceiling; config.Load is the single
// validation point for it, so it arrives here already in range.
type Options struct {
	StaticDir      string
	SSHHandler     http.Handler
	SSHKeyDir      string
	UploadDir      string
	HostsPath      string
	BrowseRoot     string
	MaxSessions    int
	MaxUploadBytes int64
	Transferrer    sshproxy.Transferrer
	RemoteLister   sshproxy.RemoteLister
}

// Server holds the resolved dependencies and the routing mux.
type Server struct {
	opts        Options
	maxSessions int
	mux         *http.ServeMux
	uploads     *uploadRegistry
	hosts       *hostStore
	files       *fileBrowser
	broadcasts  *broadcastRegistry
	browseRoot  string
}

// New builds a Server and registers its routes.
func New(opts Options) *Server {
	s := &Server{opts: opts, maxSessions: opts.MaxSessions, mux: http.NewServeMux()}
	if opts.SSHHandler != nil {
		s.mux.HandleFunc("/api/ssh/keys", s.handleSSHKeys)
		s.mux.Handle("/api/ssh/ws", opts.SSHHandler)
	}

	uploadDir := opts.UploadDir
	if uploadDir == "" {
		uploadDir = filepath.Join(os.TempDir(), "multissh-uploads")
	}
	maxUploadBytes := opts.MaxUploadBytes
	if maxUploadBytes <= 0 {
		maxUploadBytes = 8 << 30
	}
	uploads, err := newUploadRegistry(uploadDir, maxUploadBytes)
	if err != nil {
		log.Printf("server: upload storage init failed: %v", err)
	} else {
		s.uploads = uploads
	}
	hosts, err := newHostStore(opts.HostsPath, opts.MaxSessions)
	if err != nil {
		log.Printf("server: host storage init failed: %v", err)
	} else {
		s.hosts = hosts
	}

	browseRoot := strings.TrimSpace(opts.BrowseRoot)
	if browseRoot == "" {
		browseRoot = uploadDir
	}
	browseRoot, err = filepath.Abs(browseRoot)
	if err != nil {
		log.Printf("server: browse root resolve failed: %v", err)
		s.browseRoot = browseRoot
	} else {
		s.browseRoot = browseRoot
	}
	s.files = &fileBrowser{root: s.browseRoot}

	s.mux.HandleFunc("GET /api/config", s.handleConfigGet)
	s.mux.HandleFunc("POST /api/upload", s.handleUploadPost)
	s.mux.HandleFunc("GET /api/uploads", s.handleUploadsGet)
	s.mux.HandleFunc("DELETE /api/uploads/{id}", s.handleUploadsDelete)
	s.mux.HandleFunc("GET /api/hosts", s.handleHostsGet)
	s.mux.HandleFunc("PUT /api/hosts", s.handleHostsPut)
	s.mux.HandleFunc("POST /api/sftp/listdir", s.handleSFTPListDir)
	s.mux.HandleFunc("GET /api/files", s.handleFilesGet)

	if opts.Transferrer != nil {
		s.broadcasts = newBroadcastRegistry(s.uploads, opts.SSHKeyDir, opts.Transferrer)
	}
	s.mux.HandleFunc("POST /api/broadcast", s.handleBroadcastPost)
	s.mux.HandleFunc("GET /api/broadcast/ws", s.handleBroadcastWS)

	if strings.TrimSpace(opts.StaticDir) != "" {
		s.mux.Handle("/", staticHandler(opts.StaticDir))
	}
	return s
}

// Handler returns the server's root http.Handler. It is a pass-through today
// and is kept as the single place anything wrapping the module would attach.
func (s *Server) Handler() http.Handler {
	return s.mux
}

// handleConfigGet reports the settings the SPA needs before it draws anything.
// Today that is max_sessions, which sizes the host rail and the panel grid even
// when no hosts have been saved yet (FR-N1).
func (s *Server) handleConfigGet(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{"maxSessions": s.maxSessions})
}

// handleSSHKeys lists the regular files in the server user's ~/.ssh directory
// for the frontend key picker. Only base names are returned; the directory is
// fixed server-side and cannot be navigated by the client.
func (s *Server) handleSSHKeys(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", http.MethodGet)
		response.WriteError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	keys, err := sshproxy.ListKeys(s.opts.SSHKeyDir)
	if err != nil {
		response.WriteError(w, http.StatusInternalServerError, "unable to read ssh directory")
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{"keys": keys})
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

// staticHandler serves the built frontend from dir. The fallback rules are
// deliberate and are not the usual "index.html for any miss": a mistyped API
// path must fail as an API call, not return an HTML page with status 200.
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
// Directories deliberately report false so they take the SPA fallback instead
// of ever producing a listing. The path is cleaned against "/" first, so ".."
// segments cannot escape dir.
func staticFileExists(dir, urlPath string) bool {
	clean := path.Clean("/" + urlPath)
	if clean == "/" {
		clean = "/index.html"
	}
	info, err := os.Stat(filepath.Join(dir, filepath.FromSlash(clean)))
	return err == nil && info.Mode().IsRegular()
}
