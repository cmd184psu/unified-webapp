// Package smbedit is the Samba share manager module: a REST API over the
// persisted share/global state, smb.conf rendering and import, service
// restart, SSE log streams, and the built React frontend served from
// static_dir. It is one module of the unified binary, reached by Host-header
// dispatch, and exposes Build like every other module.
package smbedit

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"runtime/debug"
	"strconv"
	"strings"
	"time"
)

// Version is reported by GET /api/version. It stays "dev" unless stamped via
// -ldflags at build time (ADR follow-up #6).
var Version = "dev"

// serverOptions carries the resolved dependencies into newServer.
type serverOptions struct {
	store      *store
	staticDir  string
	pickerRoot string
	version    string
}

// server holds shared state for the module's HTTP surface.
type server struct {
	store      *store
	ops        *opLog
	logf       logFunc
	pickerRoot string
	version    string
	mux        *http.ServeMux
}

// newServer builds a server and registers its routes. staticDir may be empty
// (tests), which skips static file serving the way smbed's NewServer(cfg,
// "test", nil) did.
func newServer(opts serverOptions) *server {
	s := &server{
		store:      opts.store,
		ops:        newOpLog(500),
		pickerRoot: opts.pickerRoot,
		version:    opts.version,
		mux:        http.NewServeMux(),
	}
	// Per-server ops-log hook (D-7): OS-level operations report here instead
	// of through a process-global, so concurrent servers cannot cross wires.
	s.logf = func(format string, args ...any) { s.ops.addf(format, args...) }

	s.mux.HandleFunc("GET /api/config", s.handleGetConfig)
	s.mux.HandleFunc("PUT /api/config", s.handlePutConfig)
	s.mux.HandleFunc("GET /api/shares", s.handleGetShares)
	s.mux.HandleFunc("PUT /api/shares", s.handlePutShares)
	s.mux.HandleFunc("GET /api/globals", s.handleGetGlobals)
	s.mux.HandleFunc("PUT /api/globals", s.handlePutGlobals)
	s.mux.HandleFunc("GET /api/folders", s.handleGetFolders)
	s.mux.HandleFunc("POST /api/import", s.handleImportConf)
	s.mux.HandleFunc("POST /api/save-and-restart", s.handleSaveAndRestart)
	s.mux.HandleFunc("GET /api/preview", s.handlePreview)
	s.mux.HandleFunc("POST /api/preview", s.handlePreviewDraft)
	s.mux.HandleFunc("GET /api/logs/ops/stream", s.handleOpsLogStream)
	s.mux.HandleFunc("GET /api/logs/samba/stream", s.handleSambaLogStream)
	s.mux.HandleFunc("GET /api/version", s.handleVersion)

	if strings.TrimSpace(opts.staticDir) != "" {
		s.mux.Handle("/", staticHandler(opts.staticDir))
	}
	return s
}

// Handler returns the server's root http.Handler: the mux wrapped in the
// module-local panic recovery (D-9).
func (s *server) Handler() http.Handler {
	return recoverPanics(s.mux)
}

// ── Panic recovery (D-9) ──────────────────────────────────────────────────────

// recoveryWriter tracks whether the response header has been committed so the
// recovery wrapper knows if a synthesized 500 is still possible.
type recoveryWriter struct {
	http.ResponseWriter
	wroteHeader bool
}

func (w *recoveryWriter) WriteHeader(code int) {
	w.wroteHeader = true
	w.ResponseWriter.WriteHeader(code)
}

func (w *recoveryWriter) Write(b []byte) (int, error) {
	w.wroteHeader = true
	return w.ResponseWriter.Write(b)
}

// Flush must be forwarded by hand: Go promotes only the methods of the
// embedded http.ResponseWriter *interface*, so without this the wrapper stops
// satisfying http.Flusher and both SSE handlers fail their Flusher assertion
// in production.
func (w *recoveryWriter) Flush() {
	w.wroteHeader = true
	if f, ok := w.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

// Unwrap lets http.ResponseController reach the underlying writer.
func (w *recoveryWriter) Unwrap() http.ResponseWriter {
	return w.ResponseWriter
}

// recoverPanics restores what chi's Recoverer gave smbed: a handler panic
// yields a logged stack and a clean 500 instead of net/http's silent dropped
// connection. The 500 is synthesized only when nothing has been written yet —
// writing a status over a committed streaming response would corrupt it.
func recoverPanics(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rw := &recoveryWriter{ResponseWriter: w}
		defer func() {
			if rec := recover(); rec != nil {
				if rec == http.ErrAbortHandler {
					panic(rec)
				}
				log.Printf("smbedit: panic serving %s %s: %v\n%s", r.Method, r.URL.Path, rec, debug.Stack())
				if !rw.wroteHeader {
					jsonErr(rw, "internal server error", http.StatusInternalServerError)
				}
			}
		}()
		next.ServeHTTP(rw, r)
	})
}

// ── Config ────────────────────────────────────────────────────────────────────

func (s *server) handleGetConfig(w http.ResponseWriter, r *http.Request) {
	jsonOK(w, s.store.snapshot())
}

// patchConfig deliberately has no listen_addr field: the unified server owns
// listening, and an inbound listen_addr key is ignored, not rejected (§5.3).
type patchConfig struct {
	SmbConfPath  *string `json:"smb_conf_path"`
	SambaLogPath *string `json:"samba_log_path"`
	ShareOwner   *string `json:"share_owner"`
	Theme        *string `json:"theme"`
}

func (s *server) handlePutConfig(w http.ResponseWriter, r *http.Request) {
	var patch patchConfig
	if !decodeBody(w, r, &patch) {
		return
	}
	st, err := s.store.update(func(st *State) {
		if patch.SmbConfPath != nil {
			st.SmbConfPath = *patch.SmbConfPath
		}
		if patch.SambaLogPath != nil {
			st.SambaLogPath = *patch.SambaLogPath
		}
		if patch.ShareOwner != nil {
			st.ShareOwner = *patch.ShareOwner
		}
		if patch.Theme != nil {
			st.Theme = *patch.Theme
		}
	})
	if err != nil {
		s.logf("ERROR: saving state.json failed: %s", err.Error())
		jsonErr(w, "saving config: "+err.Error(), http.StatusInternalServerError)
		return
	}
	jsonOK(w, st)
}

// ── Shares ────────────────────────────────────────────────────────────────────

func (s *server) handleGetShares(w http.ResponseWriter, r *http.Request) {
	jsonOK(w, s.store.snapshot().Shares)
}

func (s *server) handlePutShares(w http.ResponseWriter, r *http.Request) {
	var shares []Share
	if !decodeBody(w, r, &shares) {
		return
	}
	names := disabledByMissingPath(shares)
	st, err := s.store.update(func(st *State) { st.Shares = shares })
	if err != nil {
		s.logf("ERROR: saving state.json failed: %s", err.Error())
		jsonErr(w, "saving config: "+err.Error(), http.StatusInternalServerError)
		return
	}
	// Emitted only after the save succeeds: under rollback (D-8) an entry
	// logged before a failed save would assert a change that never persisted.
	if len(names) > 0 {
		s.logf("auto-disabled share(s) with missing path: %s", strings.Join(names, ", "))
	}
	jsonOK(w, st.Shares)
}

// disabledByMissingPath runs DisableMissingPaths over shares and returns the
// names of any that it disabled, for logging to the ops log.
func disabledByMissingPath(shares []Share) []string {
	before := make([]bool, len(shares))
	for i, sh := range shares {
		before[i] = sh.Enabled
	}
	DisableMissingPaths(shares)
	var names []string
	for i, sh := range shares {
		if before[i] && !sh.Enabled {
			names = append(names, sh.Name)
		}
	}
	return names
}

// ── Globals ───────────────────────────────────────────────────────────────────

func (s *server) handleGetGlobals(w http.ResponseWriter, r *http.Request) {
	jsonOK(w, s.store.snapshot().Globals)
}

func (s *server) handlePutGlobals(w http.ResponseWriter, r *http.Request) {
	var globals []GlobalEntry
	if !decodeBody(w, r, &globals) {
		return
	}
	st, err := s.store.update(func(st *State) { st.Globals = globals })
	if err != nil {
		s.logf("ERROR: saving state.json failed: %s", err.Error())
		jsonErr(w, "saving config: "+err.Error(), http.StatusInternalServerError)
		return
	}
	jsonOK(w, st.Globals)
}

// ── Folder picker ─────────────────────────────────────────────────────────────

func (s *server) handleGetFolders(w http.ResponseWriter, r *http.Request) {
	folders, err := ListFolders(s.pickerRoot)
	if err != nil {
		jsonErr(w, "listing folders: "+err.Error(), http.StatusInternalServerError)
		return
	}
	jsonOK(w, folders)
}

// ── Import ────────────────────────────────────────────────────────────────────

type importRequest struct {
	// Path is the smb.conf file to read; defaults to the configured SmbConfPath.
	Path string `json:"path"`
}

type importResponse struct {
	Globals    []GlobalEntry `json:"globals"`
	Shares     []Share       `json:"shares"`
	ShareOwner string        `json:"share_owner,omitempty"`
}

// handleImportConf parses an existing smb.conf and returns the extracted
// globals/shares without touching the saved state; the client stages the
// result like any other edit and persists it via the normal save flow (FR-5).
func (s *server) handleImportConf(w http.ResponseWriter, r *http.Request) {
	var req importRequest
	if r.ContentLength != 0 {
		if !decodeBody(w, r, &req) {
			return
		}
	}

	confPath := strings.TrimSpace(req.Path)
	if confPath == "" {
		confPath = s.store.snapshot().SmbConfPath
	}

	globals, shares, shareOwner, err := ReadConf(confPath)
	if err != nil {
		jsonErr(w, "importing "+confPath+": "+err.Error(), http.StatusInternalServerError)
		return
	}
	disabledByMissingPath(shares)
	s.logf("imported %d share(s), %d global(s) from %s", len(shares), len(globals), confPath)

	jsonOK(w, importResponse{
		Globals:    globals,
		Shares:     shares,
		ShareOwner: shareOwner,
	})
}

// ── Save & Restart ────────────────────────────────────────────────────────────

type saveRestartResponse struct {
	Written bool          `json:"written"`
	Restart RestartResult `json:"restart"`
	Path    string        `json:"path"`
}

func (s *server) handleSaveAndRestart(w http.ResponseWriter, r *http.Request) {
	st := s.store.snapshot()
	s.logf("save & restart requested: writing %s", st.SmbConfPath)
	if err := WriteConf(st, s.logf); err != nil {
		s.logf("ERROR: writing %s failed: %s", st.SmbConfPath, err.Error())
		jsonErr(w, "writing smb.conf: "+err.Error(), http.StatusInternalServerError)
		return
	}
	result := Restart(s.logf)
	jsonOK(w, saveRestartResponse{
		Written: true,
		Restart: result,
		Path:    st.SmbConfPath,
	})
}

// ── Preview ───────────────────────────────────────────────────────────────────

// previewRequest is the draft state a POST /api/preview renders. It carries
// the editor's current, possibly-unsaved edits so the preview reflects what
// the user is looking at rather than the last-saved state.json.
type previewRequest struct {
	Globals    []GlobalEntry `json:"globals"`
	Shares     []Share       `json:"shares"`
	ShareOwner string        `json:"share_owner"`
}

// handlePreview renders the current saved state as smb.conf.
func (s *server) handlePreview(w http.ResponseWriter, r *http.Request) {
	s.writePreview(w, s.store.snapshot())
}

// handlePreviewDraft renders a draft posted by the editor, so unsaved edits
// appear in the preview. Shares whose path is missing are auto-disabled here
// exactly as they would be on save, so the preview matches what a save would
// actually write.
func (s *server) handlePreviewDraft(w http.ResponseWriter, r *http.Request) {
	var req previewRequest
	if !decodeBody(w, r, &req) {
		return
	}
	DisableMissingPaths(req.Shares)
	s.writePreview(w, &State{
		Globals:    req.Globals,
		Shares:     req.Shares,
		ShareOwner: req.ShareOwner,
	})
}

func (s *server) writePreview(w http.ResponseWriter, st *State) {
	rendered, err := Render(st)
	if err != nil {
		jsonErr(w, "rendering smb.conf: "+err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Header().Set("Content-Length", strconv.Itoa(len(rendered)))
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(rendered)
}

// ── Version ───────────────────────────────────────────────────────────────────

func (s *server) handleVersion(w http.ResponseWriter, r *http.Request) {
	jsonOK(w, map[string]string{"version": s.version})
}

// ── Logs ──────────────────────────────────────────────────────────────────────

// handleOpsLogStream serves the module's own operations log (config writes,
// backups, restarts) as Server-Sent Events: the current backlog first, then a
// live tail of new entries as they're added.
func (s *server) handleOpsLogStream(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		jsonErr(w, "streaming unsupported", http.StatusInternalServerError)
		return
	}
	setSSEHeaders(w)

	ch, cancel := s.ops.subscribe()
	defer cancel()

	for _, e := range s.ops.snapshot() {
		writeSSEEntry(w, e.Time, e.Message)
	}
	flusher.Flush()

	ctx := r.Context()
	for {
		select {
		case <-ctx.Done():
			return
		case e, ok := <-ch:
			if !ok {
				return
			}
			writeSSEEntry(w, e.Time, e.Message)
			flusher.Flush()
		}
	}
}

// runStreamingCommand starts name with args and returns its combined output
// stream plus a wait/cleanup func (D-5). Package-level var so SSE tests can
// swap in a fake stream; the handler never touches exec directly (FR-11).
var runStreamingCommand = func(ctx context.Context, name string, args ...string) (io.ReadCloser, func() error, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, nil, err
	}
	cmd.Stderr = cmd.Stdout
	// Process-group teardown (D-6): "sudo tail" leaves a grandchild that a
	// plain kill of sudo orphans; the unix hook kills the whole group and
	// bounds the reap with WaitDelay.
	configureProcessGroup(cmd)
	if err := cmd.Start(); err != nil {
		return nil, nil, err
	}
	return stdout, cmd.Wait, nil
}

// handleSambaLogStream tails the configured Samba daemon log file via "sudo
// tail -F", since that file typically lives under a root-only directory
// (e.g. /var/log/samba, mode 0700) that the server process can't read directly.
func (s *server) handleSambaLogStream(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		jsonErr(w, "streaming unsupported", http.StatusInternalServerError)
		return
	}
	logPath := s.store.snapshot().SambaLogPath
	if logPath == "" {
		jsonErr(w, "no samba_log_path configured", http.StatusBadRequest)
		return
	}
	setSSEHeaders(w)

	ctx := r.Context()
	stdout, wait, err := runStreamingCommand(ctx, "sudo", "tail", "-n", "200", "-F", logPath)
	if err != nil {
		writeSSEEntry(w, time.Now(), "ERROR: starting tail: "+err.Error())
		flusher.Flush()
		return
	}
	defer wait()
	defer stdout.Close()

	scanner := bufio.NewScanner(stdout)
	scanner.Buffer(make([]byte, 64*1024), 1024*1024)
	for scanner.Scan() {
		writeSSEEntry(w, time.Now(), scanner.Text())
		flusher.Flush()
		if ctx.Err() != nil {
			return
		}
	}
}

func setSSEHeaders(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
}

func writeSSEEntry(w http.ResponseWriter, t time.Time, message string) {
	data, err := json.Marshal(struct {
		Time    time.Time `json:"time"`
		Message string    `json:"message"`
	}{t, message})
	if err != nil {
		return
	}
	fmt.Fprintf(w, "data: %s\n\n", data)
}

// ── Helpers ───────────────────────────────────────────────────────────────────

func jsonOK(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}

func jsonErr(w http.ResponseWriter, msg string, code int) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": msg})
}

func decodeBody(w http.ResponseWriter, r *http.Request, v any) bool {
	if err := json.NewDecoder(r.Body).Decode(v); err != nil {
		jsonErr(w, "invalid JSON: "+err.Error(), http.StatusBadRequest)
		return false
	}
	return true
}

// ── Static frontend ───────────────────────────────────────────────────────────

// onlyGet restricts a handler to GET and HEAD.
func onlyGet(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			w.Header().Set("Allow", "GET, HEAD")
			jsonErr(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// noDirList returns 404 for directory index requests (paths ending in "/",
// other than the SPA root) so the static tree is not browsable.
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
			jsonErr(w, "not found", http.StatusNotFound)
			return
		}
		if staticFileExists(dir, r.URL.Path) {
			assets.ServeHTTP(w, r)
			return
		}
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			w.Header().Set("Allow", "GET, HEAD")
			jsonErr(w, "method not allowed", http.StatusMethodNotAllowed)
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
