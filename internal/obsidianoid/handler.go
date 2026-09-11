package obsidianoid

import (
	"encoding/json"
	"io"
	"net/http"
	"os"
	"strconv"

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
	mux.HandleFunc("POST /api/render", h.handleRender)
	mux.HandleFunc("GET /api/threads", h.handleThreadsGet)
	mux.HandleFunc("PUT /api/threads", h.handleThreadsPut)
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
	response.WriteJSON(w, http.StatusOK, map[string]bool{"autosave": !h.cfg.AutoSaveDisabled})
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
	_, _ = w.Write(html)
}

func (h *Handler) handleThreadsGet(w http.ResponseWriter, r *http.Request) {
	ts, err := readThreads(h.vaultPath(r), h.cfg.ThreadsFolder, h.cfg.ThreadCount, h.state.States())
	if err != nil {
		response.WriteError(w, http.StatusInternalServerError, "failed to read threads")
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(ts)
}

func (h *Handler) handleThreadsPut(w http.ResponseWriter, r *http.Request) {
	var incoming []Thread
	if err := json.NewDecoder(r.Body).Decode(&incoming); err != nil {
		response.WriteError(w, http.StatusBadRequest, "invalid JSON")
		return
	}
	if len(incoming) != h.cfg.ThreadCount {
		response.WriteError(w, http.StatusBadRequest, "wrong thread count")
		return
	}
	if err := writeThreads(h.vaultPath(r), h.cfg.ThreadsFolder, incoming); err != nil {
		response.WriteError(w, http.StatusInternalServerError, "write error")
		return
	}
	disabled := make([]bool, len(incoming))
	for i, t := range incoming {
		disabled[i] = t.Disabled
	}
	if err := h.state.SetDisabled(disabled); err != nil {
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
		response.WriteError(w, http.StatusBadRequest, "invalid JSON")
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
