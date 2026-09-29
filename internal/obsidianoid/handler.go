package obsidianoid

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"path"
	"strconv"
	"strings"

	"cmd184psu/unified-webapp/internal/platform/broker"
	"cmd184psu/unified-webapp/internal/platform/config"
	"cmd184psu/unified-webapp/internal/platform/response"
	"github.com/russross/blackfriday/v2"
)

// Handler implements the obsidianoid HTTP API.
type Handler struct {
	cfg     config.ObsidianoidConfig
	state   *StateStore
	brokers []*broker.Broker
}

// NewHandler constructs a Handler. brokers must be indexed in the same order as cfg.Vaults.
func NewHandler(cfg config.ObsidianoidConfig, state *StateStore, brokers []*broker.Broker) *Handler {
	return &Handler{cfg: cfg, state: state, brokers: brokers}
}

// Register mounts all obsidianoid API routes onto mux.
func (h *Handler) Register(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/vaults", h.handleVaults)
	mux.HandleFunc("GET /api/config", h.handleConfig)
	mux.HandleFunc("GET /api/tree", h.handleTree)
	mux.HandleFunc("GET /api/note", h.handleNoteGet)
	mux.HandleFunc("PUT /api/note", h.handleNotePut)
	mux.HandleFunc("DELETE /api/note", h.handleNoteDelete)
	mux.HandleFunc("POST /api/note/rename", h.handleNoteRename)
	mux.HandleFunc("POST /api/note/move", h.handleNoteMove)
	mux.HandleFunc("POST /api/folder", h.handleFolderCreate)
	mux.HandleFunc("DELETE /api/folder", h.handleFolderDelete)
	mux.HandleFunc("POST /api/folder/rename", h.handleFolderRename)
	mux.HandleFunc("GET /api/search", h.handleSearch)
	mux.HandleFunc("POST /api/render", h.handleRender)
	mux.HandleFunc("GET /api/threads", h.handleThreadsGet)
	mux.HandleFunc("PUT /api/threads", h.handleThreadsPut)
	mux.HandleFunc("PUT /api/threads/count", h.handleThreadCountPut)
	mux.HandleFunc("GET /api/git/status", h.handleGitStatus)
	mux.HandleFunc("POST /api/git/sync", h.handleGitSync)
	mux.HandleFunc("GET /api/events", h.handleEvents)
}

func (h *Handler) vaultIdx(r *http.Request) int {
	idx, _ := strconv.Atoi(r.URL.Query().Get("vault"))
	if idx < 0 || idx >= len(h.cfg.Vaults) {
		return 0
	}
	return idx
}

func (h *Handler) vaultPath(r *http.Request) string {
	return h.cfg.Vaults[h.vaultIdx(r)].Path
}

func (h *Handler) handleVaults(w http.ResponseWriter, r *http.Request) {
	info := make([]vaultInfo, len(h.cfg.Vaults))
	for i, v := range h.cfg.Vaults {
		info[i] = vaultInfo{Name: v.Name, Theme: v.Theme}
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(info)
}

func (h *Handler) handleConfig(w http.ResponseWriter, r *http.Request) {
	response.WriteJSON(w, http.StatusOK, map[string]any{
		"autosave": !h.cfg.AutoSaveDisabled,
		// The UI hides rename on this folder: thread mode finds its files here.
		"threads_folder": strings.Trim(h.cfg.ThreadsFolder, "/"),
		"thread_count":   h.state.Count(),
		"max_threads":    MaxThreadCount,
	})
}

func (h *Handler) handleTree(w http.ResponseWriter, r *http.Request) {
	tree, err := vaultTree(h.vaultPath(r))
	if err != nil {
		response.WriteError(w, http.StatusInternalServerError, "failed to list vault")
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(tree)
}

func (h *Handler) handleNoteGet(w http.ResponseWriter, r *http.Request) {
	rel := r.URL.Query().Get("path")
	if rel == "" {
		response.WriteError(w, http.StatusBadRequest, "path required")
		return
	}
	content, err := readNote(h.vaultPath(r), rel)
	if err != nil {
		if os.IsNotExist(err) || os.IsPermission(err) {
			response.WriteError(w, http.StatusNotFound, "note not found")
		} else {
			response.WriteError(w, http.StatusInternalServerError, "read error")
		}
		return
	}
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	_, _ = w.Write(content)
}

func (h *Handler) handleNotePut(w http.ResponseWriter, r *http.Request) {
	rel := r.URL.Query().Get("path")
	if rel == "" {
		response.WriteError(w, http.StatusBadRequest, "path required")
		return
	}
	body, err := io.ReadAll(r.Body)
	if err != nil {
		response.WriteError(w, http.StatusBadRequest, "read body failed")
		return
	}
	if err := writeNote(h.vaultPath(r), rel, body); err != nil {
		if os.IsPermission(err) {
			response.WriteError(w, http.StatusForbidden, "forbidden")
		} else {
			response.WriteError(w, http.StatusInternalServerError, "write error")
		}
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// threadSlot reports which thread slot relPath is the file of, or -1 if it is
// not a thread file.
func (h *Handler) threadSlot(relPath string) int {
	for i, n := 0, h.state.Count(); i < n; i++ {
		if relPath == path.Join(h.cfg.ThreadsFolder, threadFileName(i)) {
			return i
		}
	}
	return -1
}

// moveOutOfThreads handles a thread file that was just renamed or deleted in
// Notes view: the note has left the thread group, so its slot starts over —
// title cleared, re-enabled, and a fresh empty file written right away so the
// group never has a gap. Reports whether relPath was a thread file.
func (h *Handler) moveOutOfThreads(r *http.Request, relPath string) (bool, error) {
	slot := h.threadSlot(relPath)
	if slot < 0 {
		return false, nil
	}
	if err := h.state.ResetThread(slot); err != nil {
		return true, err
	}
	return true, writeNote(h.vaultPath(r), relPath, nil)
}

// handleNoteDelete deletes one markdown note from the vault. Deleting a
// thread file resets that thread (see moveOutOfThreads).
func (h *Handler) handleNoteDelete(w http.ResponseWriter, r *http.Request) {
	rel := r.URL.Query().Get("path")
	if rel == "" {
		response.WriteError(w, http.StatusBadRequest, "path required")
		return
	}
	switch err := deleteNote(h.vaultPath(r), rel); {
	case err == nil:
		reset, err := h.moveOutOfThreads(r, rel)
		if err != nil {
			response.WriteError(w, http.StatusInternalServerError, "note deleted, but resetting its thread failed")
			return
		}
		response.WriteJSON(w, http.StatusOK, map[string]bool{"thread_reset": reset})
	case os.IsNotExist(err) || os.IsPermission(err):
		response.WriteError(w, http.StatusNotFound, "note not found")
	default:
		response.WriteError(w, http.StatusInternalServerError, "delete failed")
	}
}

// handleNoteRename renames a note within its folder. Body: {"path": current
// vault-relative path, "name": new name without folder}. Returns {"path": new
// path, "thread_reset": bool}; 409 if a different note already has that name,
// unless "overwrite": true, which replaces it.
// Renaming a thread file moves it out of the thread group (moveOutOfThreads).
func (h *Handler) handleNoteRename(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Path      string `json:"path"`
		Name      string `json:"name"`
		Overwrite bool   `json:"overwrite"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Path == "" {
		response.WriteError(w, http.StatusBadRequest, "path and name required")
		return
	}
	newPath, err := renameNote(h.vaultPath(r), req.Path, req.Name, req.Overwrite)
	switch {
	case err == nil:
		reset := false
		// A case-only rename can be the very same file on a case-insensitive
		// disk; writing a fresh thread file there would overwrite the note.
		if !strings.EqualFold(newPath, req.Path) {
			var rerr error
			if reset, rerr = h.moveOutOfThreads(r, req.Path); rerr != nil {
				response.WriteError(w, http.StatusInternalServerError, "note renamed, but resetting its thread failed")
				return
			}
		}
		response.WriteJSON(w, http.StatusOK, map[string]any{"path": newPath, "thread_reset": reset})
	case errors.Is(err, errInvalidName):
		response.WriteError(w, http.StatusBadRequest, err.Error())
	case errors.Is(err, errNoteExists):
		response.WriteError(w, http.StatusConflict, err.Error())
	case os.IsNotExist(err) || os.IsPermission(err):
		response.WriteError(w, http.StatusNotFound, "note not found")
	default:
		response.WriteError(w, http.StatusInternalServerError, "rename failed")
	}
}

// handleNoteMove moves a note into another folder, keeping its name. Body:
// {"path": current path, "folder": destination folder, "" for the vault
// root, "overwrite": replace a same-named note}. Returns {"path": new path,
// "thread_reset": bool}; 409 if the folder already has a note by that name
// and overwrite is false, 404 if the note or folder doesn't exist.
// Moving a thread file out of the thread folder resets its slot.
func (h *Handler) handleNoteMove(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Path      string `json:"path"`
		Folder    string `json:"folder"`
		Overwrite bool   `json:"overwrite"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Path == "" {
		response.WriteError(w, http.StatusBadRequest, "path required")
		return
	}
	newPath, err := moveNote(h.vaultPath(r), req.Path, req.Folder, req.Overwrite)
	switch {
	case err == nil:
		reset := false
		if newPath != req.Path {
			var rerr error
			if reset, rerr = h.moveOutOfThreads(r, req.Path); rerr != nil {
				response.WriteError(w, http.StatusInternalServerError, "note moved, but resetting its thread failed")
				return
			}
		}
		response.WriteJSON(w, http.StatusOK, map[string]any{"path": newPath, "thread_reset": reset})
	case errors.Is(err, errInvalidName):
		response.WriteError(w, http.StatusBadRequest, "invalid folder")
	case errors.Is(err, errNoteExists):
		response.WriteError(w, http.StatusConflict, "that folder already has a note with this name")
	case os.IsNotExist(err) || os.IsPermission(err):
		response.WriteError(w, http.StatusNotFound, "note or folder not found")
	default:
		response.WriteError(w, http.StatusInternalServerError, "move failed")
	}
}

// handleFolderCreate creates a folder (nested paths allowed). Body:
// {"path": "Projects/2026"}. 409 if it already exists.
func (h *Handler) handleFolderCreate(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Path string `json:"path"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.WriteError(w, http.StatusBadRequest, "path required")
		return
	}
	switch err := createFolder(h.vaultPath(r), req.Path); {
	case err == nil:
		response.WriteJSON(w, http.StatusCreated, map[string]string{"path": strings.Trim(strings.TrimSpace(req.Path), "/")})
	case errors.Is(err, errInvalidName):
		response.WriteError(w, http.StatusBadRequest, "invalid folder name")
	case errors.Is(err, errNoteExists):
		response.WriteError(w, http.StatusConflict, "that folder already exists")
	case os.IsPermission(err):
		response.WriteError(w, http.StatusForbidden, "forbidden")
	default:
		response.WriteError(w, http.StatusInternalServerError, "create folder failed")
	}
}

// handleFolderDelete removes an empty folder (?path=Projects/2026). A folder
// with anything in it, hidden files included, is refused with 409.
func (h *Handler) handleFolderDelete(w http.ResponseWriter, r *http.Request) {
	switch err := removeFolder(h.vaultPath(r), r.URL.Query().Get("path")); {
	case err == nil:
		w.WriteHeader(http.StatusNoContent)
	case errors.Is(err, errInvalidName):
		response.WriteError(w, http.StatusBadRequest, "invalid folder")
	case errors.Is(err, errFolderNotEmpty):
		response.WriteError(w, http.StatusConflict, "folder is not empty")
	case os.IsNotExist(err) || os.IsPermission(err):
		response.WriteError(w, http.StatusNotFound, "folder not found")
	default:
		response.WriteError(w, http.StatusInternalServerError, "delete folder failed")
	}
}

// handleFolderRename renames a folder in place. Body: {"path": "Projects",
// "name": "Work"}. Returns {"path": new path}. The thread folder, and any
// folder containing it, is refused (403): thread mode finds its files by that
// configured path. 409 if the name is taken.
func (h *Handler) handleFolderRename(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Path string `json:"path"`
		Name string `json:"name"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.WriteError(w, http.StatusBadRequest, "path and name required")
		return
	}
	rel := strings.Trim(strings.TrimSpace(req.Path), "/")
	threads := strings.Trim(h.cfg.ThreadsFolder, "/")
	if threads != "" && (rel == threads || strings.HasPrefix(threads, rel+"/")) {
		response.WriteError(w, http.StatusForbidden, "the thread folder can't be renamed")
		return
	}
	newPath, err := renameFolder(h.vaultPath(r), rel, req.Name)
	switch {
	case err == nil:
		response.WriteJSON(w, http.StatusOK, map[string]string{"path": newPath})
	case errors.Is(err, errInvalidName):
		response.WriteError(w, http.StatusBadRequest, "invalid folder name")
	case errors.Is(err, errNoteExists):
		response.WriteError(w, http.StatusConflict, "that name is already taken")
	case os.IsNotExist(err) || os.IsPermission(err):
		response.WriteError(w, http.StatusNotFound, "folder not found")
	default:
		response.WriteError(w, http.StatusInternalServerError, "rename folder failed")
	}
}

// handleSearch greps the vault: the paths of every note whose name or content
// contains q, case-insensitively. The UI uses them as a filter on the tree.
func (h *Handler) handleSearch(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query().Get("q")
	if q == "" || len(q) > maxSearchQuery {
		response.WriteError(w, http.StatusBadRequest, "q must be 1-200 characters")
		return
	}
	paths, err := vaultSearch(h.vaultPath(r), q)
	if err != nil {
		response.WriteError(w, http.StatusInternalServerError, "search failed")
		return
	}
	response.WriteJSON(w, http.StatusOK, map[string][]string{"paths": paths})
}

func (h *Handler) handleRender(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(r.Body)
	if err != nil {
		response.WriteError(w, http.StatusBadRequest, "read error")
		return
	}
	flags := blackfriday.CommonExtensions |
		blackfriday.AutoHeadingIDs |
		blackfriday.Tables |
		blackfriday.FencedCode |
		blackfriday.Strikethrough
	renderer := blackfriday.NewHTMLRenderer(blackfriday.HTMLRendererParameters{
		Flags: blackfriday.CommonHTMLFlags,
	})
	html := blackfriday.Run(body, blackfriday.WithExtensions(flags), blackfriday.WithRenderer(renderer))
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Content-Security-Policy", "sandbox")
	_, _ = w.Write(html)
}

func (h *Handler) handleThreadsGet(w http.ResponseWriter, r *http.Request) {
	ts, err := readThreads(h.vaultPath(r), h.cfg.ThreadsFolder, h.state.Count(), h.state.States())
	if err != nil {
		response.WriteError(w, http.StatusInternalServerError, "failed to read threads")
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(ts)
}

// handleThreadCountPut sets how many thread slots there are. Body:
// {"count": n}, 1..MaxThreadCount. Lowering it leaves the dropped slots'
// files in the vault as ordinary notes (and keeps their titles, so raising it
// again brings them back); raising it adds empty slots.
func (h *Handler) handleThreadCountPut(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Count int `json:"count"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.WriteError(w, http.StatusBadRequest, "count required")
		return
	}
	if err := h.state.SetCount(req.Count); err != nil {
		if errors.Is(err, errBadThreadCount) {
			response.WriteError(w, http.StatusBadRequest, "count must be between 1 and "+strconv.Itoa(MaxThreadCount))
			return
		}
		response.WriteError(w, http.StatusInternalServerError, "state save error")
		return
	}
	response.WriteJSON(w, http.StatusOK, map[string]int{"count": h.state.Count()})
}

func (h *Handler) handleThreadsPut(w http.ResponseWriter, r *http.Request) {
	var incoming []Thread
	if err := json.NewDecoder(r.Body).Decode(&incoming); err != nil {
		response.WriteDecodeError(w, err)
		return
	}
	if len(incoming) != h.state.Count() {
		response.WriteError(w, http.StatusBadRequest, "wrong thread count")
		return
	}
	if err := writeThreads(h.vaultPath(r), h.cfg.ThreadsFolder, incoming); err != nil {
		response.WriteError(w, http.StatusInternalServerError, "write error")
		return
	}
	disabled := make([]bool, len(incoming))
	titles := make([]string, len(incoming))
	for i, t := range incoming {
		disabled[i] = t.Disabled
		titles[i] = t.Title
	}
	if err := h.state.SetThreads(disabled, titles); err != nil {
		response.WriteError(w, http.StatusInternalServerError, "state save error")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) handleGitStatus(w http.ResponseWriter, r *http.Request) {
	response.WriteJSON(w, http.StatusOK, map[string]bool{"available": gitIsAvailable(h.vaultPath(r))})
}

func (h *Handler) handleGitSync(w http.ResponseWriter, r *http.Request) {
	root := h.vaultPath(r)
	if !gitIsAvailable(root) {
		response.WriteError(w, http.StatusNotFound, "git not available")
		return
	}
	var body struct {
		Message string `json:"message"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		response.WriteDecodeError(w, err)
		return
	}
	if body.Message == "" {
		body.Message = "obsidianoid sync"
	}
	output, err := gitSync(root, body.Message)
	w.Header().Set("Content-Type", "application/json")
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		_ = json.NewEncoder(w).Encode(gitSyncResult{OK: false, Output: output})
		return
	}
	_ = json.NewEncoder(w).Encode(gitSyncResult{OK: true, Output: output})
}

func (h *Handler) handleEvents(w http.ResponseWriter, r *http.Request) {
	h.brokers[h.vaultIdx(r)].ServeSSE("note-changed", nil)(w, r)
}
