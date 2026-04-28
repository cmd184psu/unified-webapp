package todo

import (
	"io"
	"net/http"
	"strings"

	"cmd184psu/unified-webapp/internal/platform/broker"
	"cmd184psu/unified-webapp/internal/platform/config"
	"cmd184psu/unified-webapp/internal/platform/response"
)

// Handler serves all todo module HTTP routes.
type Handler struct {
	store  *Store
	broker *broker.MultiRoomBroker
	cfg    config.TodoConfig
}

// NewHandler constructs a Handler.
func NewHandler(store *Store, mbr *broker.MultiRoomBroker, cfg config.TodoConfig) *Handler {
	return &Handler{store: store, broker: mbr, cfg: cfg}
}

// Register wires all routes onto mux.
func (h *Handler) Register(mux *http.ServeMux) {
	mux.HandleFunc("GET /config", h.handleConfig)
	mux.HandleFunc("GET /config/", h.handleConfig)
	mux.HandleFunc("GET /items", h.handleSubjects)
	mux.HandleFunc("GET /items/{subject}/{item}", h.handleReadFile)
	mux.HandleFunc("POST /items/{subject}/{item}", h.handleWriteFile)
	mux.HandleFunc("POST /items/{subject}/{item}/{newSubject}", h.handleMoveFile)
	mux.HandleFunc("GET /api/events", h.handleEvents)
}

type configResponse struct {
	Ext            string `json:"ext"`
	DefaultSubject string `json:"defaultSubject"`
	DefaultItem    string `json:"defaultItem"`
	Autosave       bool   `json:"autosave"`
}

func (h *Handler) handleConfig(w http.ResponseWriter, r *http.Request) {
	response.WriteJSON(w, http.StatusOK, configResponse{
		Ext:            h.cfg.Ext,
		DefaultSubject: h.cfg.DefaultSubject,
		DefaultItem:    h.cfg.DefaultSubject + "/index.json",
		Autosave:       false,
	})
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

func (h *Handler) handleReadFile(w http.ResponseWriter, r *http.Request) {
	subject := r.PathValue("subject")
	item := r.PathValue("item")
	if !validName(subject) || !validName(item) {
		response.WriteError(w, http.StatusBadRequest, "invalid path")
		return
	}
	w.Header().Set("Content-Type", "application/json")
	if item == "index.json" {
		data, err := h.store.GenerateIndex(subject)
		if err != nil {
			response.WriteError(w, http.StatusInternalServerError, err.Error())
			return
		}
		w.WriteHeader(http.StatusOK)
		w.Write(data) //nolint:errcheck
		return
	}
	data, err := h.store.ReadFile(subject, item)
	if err != nil {
		response.WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}
	w.WriteHeader(http.StatusOK)
	w.Write(data) //nolint:errcheck
}

func (h *Handler) handleWriteFile(w http.ResponseWriter, r *http.Request) {
	subject := r.PathValue("subject")
	item := r.PathValue("item")
	if !validName(subject) || !validName(item) {
		response.WriteError(w, http.StatusBadRequest, "invalid path")
		return
	}
	body, err := io.ReadAll(r.Body)
	if err != nil {
		response.WriteError(w, http.StatusBadRequest, "cannot read body")
		return
	}
	if len(body) == 0 {
		response.WriteError(w, http.StatusBadRequest, "empty body")
		return
	}
	if err := h.store.WriteFile(subject, item, body); err != nil {
		response.WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}
	h.broker.Notify(subject)
	response.WriteJSON(w, http.StatusOK, map[string]string{"msg": "saved"})
}

func (h *Handler) handleMoveFile(w http.ResponseWriter, r *http.Request) {
	subject := r.PathValue("subject")
	item := r.PathValue("item")
	newSubject := r.PathValue("newSubject")
	if !validName(subject) || !validName(item) || !validName(newSubject) {
		response.WriteError(w, http.StatusBadRequest, "invalid path")
		return
	}
	if err := h.store.MoveFile(subject, item, newSubject); err != nil {
		response.WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}
	h.broker.Notify(subject)
	h.broker.Notify(newSubject)
	response.WriteJSON(w, http.StatusOK, map[string]string{"msg": "moved"})
}

func (h *Handler) handleEvents(w http.ResponseWriter, r *http.Request) {
	subject := r.URL.Query().Get("subject")
	if subject == "" {
		response.WriteError(w, http.StatusBadRequest, "subject required")
		return
	}
	h.broker.Room(subject).ServeHTTP(w, r)
}

// validName returns true if name is a safe single-path-component identifier.
// Rejects empty strings, dot-prefixed names, and any embedded separators.
func validName(name string) bool {
	return name != "" &&
		!strings.HasPrefix(name, ".") &&
		!strings.Contains(name, "/") &&
		!strings.Contains(name, "\\")
}
