package slideshow

import (
	"encoding/json"
	"io"
	"net/http"
	"sync"

	"cmd184psu/unified-webapp/internal/platform/config"
	"cmd184psu/unified-webapp/internal/platform/response"
)

// Handler serves all slideshow module HTTP routes.
type Handler struct {
	store *Store
	cfg   config.SlideshowConfig

	mu             sync.RWMutex
	defaultSubject string // updated via POST /config
}

// NewHandler constructs a Handler.
func NewHandler(store *Store, cfg config.SlideshowConfig) *Handler {
	return &Handler{
		store:          store,
		cfg:            cfg,
		defaultSubject: cfg.DefaultSubject,
	}
}

// Register wires all routes onto mux.
func (h *Handler) Register(mux *http.ServeMux) {
	mux.HandleFunc("GET /config", h.handleConfigGet)
	mux.HandleFunc("GET /config/", h.handleConfigGet)
	mux.HandleFunc("POST /config", h.handleConfigPost)
	mux.HandleFunc("GET /items", h.handleSubjects)
	mux.HandleFunc("GET /"+h.cfg.Prefix+"/{subject}/{item}", h.handleImage)
}

// configPayload is the JSON shape for GET /config responses and POST /config bodies.
// The JS reads config.prefix and config.defaultSubject; it POSTs the same shape back.
type configPayload struct {
	Prefix         string `json:"prefix"`
	DefaultSubject string `json:"defaultSubject"`
}

func (h *Handler) handleConfigGet(w http.ResponseWriter, r *http.Request) {
	h.mu.RLock()
	ds := h.defaultSubject
	h.mu.RUnlock()
	response.WriteJSON(w, http.StatusOK, configPayload{
		Prefix:         h.cfg.Prefix,
		DefaultSubject: ds,
	})
}

func (h *Handler) handleConfigPost(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(r.Body)
	if err != nil {
		response.WriteError(w, http.StatusBadRequest, "cannot read body")
		return
	}
	var payload configPayload
	if err := json.Unmarshal(body, &payload); err != nil {
		response.WriteError(w, http.StatusBadRequest, "invalid JSON")
		return
	}
	if payload.DefaultSubject != "" {
		h.mu.Lock()
		h.defaultSubject = payload.DefaultSubject
		h.mu.Unlock()
	}
	response.WriteJSON(w, http.StatusOK, map[string]string{"msg": "saved"})
}

func (h *Handler) handleSubjects(w http.ResponseWriter, r *http.Request) {
	subjects, err := h.store.Subjects()
	if err != nil {
		response.WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if subjects == nil {
		subjects = []Subject{}
	}
	response.WriteJSON(w, http.StatusOK, subjects)
}

func (h *Handler) handleImage(w http.ResponseWriter, r *http.Request) {
	subject := r.PathValue("subject")
	item := r.PathValue("item")
	if !validName(subject) || !validName(item) {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	path, err := h.store.ImagePath(subject, item)
	if err != nil {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	http.ServeFile(w, r, path)
}

// validName returns true if name is a safe single-path-component identifier.
func validName(name string) bool {
	if name == "" || name[0] == '.' {
		return false
	}
	for _, c := range name {
		if c == '/' || c == '\\' {
			return false
		}
	}
	return true
}
