package slideshow

import (
	"encoding/json"
	"io"
	"net/http"

	"cmd184psu/unified-webapp/internal/platform/config"
	"cmd184psu/unified-webapp/internal/platform/response"
)

// Handler serves all slideshow module HTTP routes.
type Handler struct {
	store      *Store
	conductor  *Conductor
	broker     *SSEBroker
	musicStore *MusicStore
	cfg        config.SlideshowConfig
}

// NewHandler constructs a Handler.
func NewHandler(store *Store, conductor *Conductor, broker *SSEBroker, music *MusicStore, cfg config.SlideshowConfig) *Handler {
	return &Handler{store: store, conductor: conductor, broker: broker, musicStore: music, cfg: cfg}
}

// Register wires all routes onto mux.
func (h *Handler) Register(mux *http.ServeMux) {
	// Legacy aliases kept for old clients.
	mux.HandleFunc("GET /config", h.handleState)
	mux.HandleFunc("GET /config/", h.handleState)

	// Primary API.
	mux.HandleFunc("GET /api/state", h.handleState)
	mux.HandleFunc("GET /api/events", h.broker.ServeSSE(h.conductor.Snapshot))
	mux.HandleFunc("POST /api/control", h.handleControl)

	// Subject + image serving.
	mux.HandleFunc("GET /items", h.handleSubjects)
	mux.HandleFunc("GET /"+h.cfg.Prefix+"/{subject}/{item}", h.handleImage)

	// Audio serving.
	mux.HandleFunc("GET /audio/{collection}/{track}", h.handleAudio)
}

func (h *Handler) handleState(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	w.Write([]byte(h.conductor.Snapshot())) //nolint:errcheck
}

// controlRequest is the JSON body for POST /api/control.
type controlRequest struct {
	Action string          `json:"action"`
	Value  json.RawMessage `json:"value"`
}

func (h *Handler) handleControl(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(r.Body)
	if err != nil {
		response.WriteError(w, http.StatusBadRequest, "cannot read body")
		return
	}
	var req controlRequest
	if err := json.Unmarshal(body, &req); err != nil {
		response.WriteError(w, http.StatusBadRequest, "invalid JSON")
		return
	}
	if req.Action == "" {
		response.WriteError(w, http.StatusBadRequest, "action required")
		return
	}
	if err := h.conductor.ApplyControl(req.Action, req.Value); err != nil {
		response.WriteError(w, http.StatusBadRequest, err.Error())
		return
	}
	response.WriteJSON(w, http.StatusOK, map[string]string{"ok": "true"})
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

func (h *Handler) handleAudio(w http.ResponseWriter, r *http.Request) {
	collection := r.PathValue("collection")
	track := r.PathValue("track")
	if !validName(collection) || !validName(track) {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	path, err := h.musicStore.AudioPath(collection, track)
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
