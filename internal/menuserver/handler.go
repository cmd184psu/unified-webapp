package menuserver

import (
	"net/http"
	"strings"

	"cmd184psu/unified-webapp/internal/platform/config"
	"cmd184psu/unified-webapp/internal/platform/response"
)

// Handler serves all menuserver module HTTP routes.
type Handler struct {
	store *Store
	cfg   config.MenuserverConfig
}

// NewHandler constructs a Handler.
func NewHandler(store *Store, cfg config.MenuserverConfig) *Handler {
	return &Handler{store: store, cfg: cfg}
}

// Register wires all routes onto mux.
func (h *Handler) Register(mux *http.ServeMux) {
	mux.HandleFunc("GET /config", h.handleConfig)
	mux.HandleFunc("GET /config/", h.handleConfig)
	mux.HandleFunc("GET /items", h.handleSubjects)
	mux.HandleFunc("GET /menus/{subject}/{item}", h.handleMenu)
}

type configResponse struct {
	ShowAllPages bool `json:"showAllPages"`
}

func (h *Handler) handleConfig(w http.ResponseWriter, r *http.Request) {
	response.WriteJSON(w, http.StatusOK, configResponse{
		ShowAllPages: h.cfg.ShowAllPages,
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

func (h *Handler) handleMenu(w http.ResponseWriter, r *http.Request) {
	subject := r.PathValue("subject")
	item := r.PathValue("item")
	if !validName(subject) || !validName(item) {
		http.NotFound(w, r)
		return
	}
	data, err := h.store.ReadMenu(subject, item)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	w.Write(data) //nolint:errcheck
}

// validName returns true if name is a safe single-path-component identifier.
func validName(name string) bool {
	return name != "" &&
		!strings.HasPrefix(name, ".") &&
		!strings.Contains(name, "/") &&
		!strings.Contains(name, "\\")
}
