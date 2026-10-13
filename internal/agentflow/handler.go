package agentflow

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strings"

	"cmd184psu/unified-webapp/internal/platform/config"
	"cmd184psu/unified-webapp/internal/platform/response"
)

// Handler defines the agentflow request handler.
type Handler struct {
	store  *Store
	engine *Engine
}

// NewHandler creates a new Handler with the given store and engine.
func NewHandler(store *Store, engine *Engine) *Handler {
	return &Handler{store: store, engine: engine}
}

// Register mounts the agentflow routes on the given ServeMux.
func (h *Handler) Register(mux *http.ServeMux) {
	mux.HandleFunc("GET /queue", h.handleQueue)
	mux.HandleFunc("GET /progress", h.handleProgress)

	// FRD endpoints
	mux.HandleFunc("GET /frd", h.handleFRD)
	mux.HandleFunc("POST /frd", h.postFRD)

	// Plan endpoints
	mux.HandleFunc("GET /plan", h.handlePlan)
	mux.HandleFunc("POST /plan", h.postPlan)
	mux.HandleFunc("POST /plan/review", h.postPlanReview)

	// Task compilation
	mux.HandleFunc("POST /tasks/compile", h.postCompileTasks)

	// Settings
	mux.HandleFunc("GET /settings", h.handleSettings)
	mux.HandleFunc("POST /settings", h.postSettings)
}

// handleQueue reads queue.json from the store's project directory and writes it as JSON.
func (h *Handler) handleQueue(w http.ResponseWriter, _ *http.Request) {
	data, err := h.store.GetQueue()
	if err != nil {
		writeStoreError(w, err)
		return
	}
	response.WriteJSON(w, http.StatusOK, data)
}

// handleProgress derives progress counts from queue.json and writes them as JSON.
func (h *Handler) handleProgress(w http.ResponseWriter, _ *http.Request) {
	counts, err := h.store.GetProgress()
	if err != nil {
		writeStoreError(w, err)
		return
	}
	response.WriteJSON(w, http.StatusOK, counts)
}

// handleFRD returns the current FRD
func (h *Handler) handleFRD(w http.ResponseWriter, _ *http.Request) {
	frd, err := h.store.GetFRD()
	if err != nil {
		writeStoreError(w, err)
		return
	}
	response.WriteJSON(w, http.StatusOK, frd)
}

// postFRD creates/updates the FRD
func (h *Handler) postFRD(w http.ResponseWriter, r *http.Request) {
	var frd FRD
	if err := json.NewDecoder(r.Body).Decode(&frd); err != nil {
		response.WriteError(w, http.StatusBadRequest, "invalid FRD JSON")
		return
	}
	if err := h.store.WriteFRD(&frd); err != nil {
		response.WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}
	response.WriteJSON(w, http.StatusOK, frd)
}

// handlePlan returns the current Plan
func (h *Handler) handlePlan(w http.ResponseWriter, _ *http.Request) {
	plan, err := h.store.GetPlan()
	if err != nil {
		writeStoreError(w, err)
		return
	}
	response.WriteJSON(w, http.StatusOK, plan)
}

// postPlan creates/updates the Plan
func (h *Handler) postPlan(w http.ResponseWriter, r *http.Request) {
	var plan Plan
	if err := json.NewDecoder(r.Body).Decode(&plan); err != nil {
		response.WriteError(w, http.StatusBadRequest, "invalid Plan JSON")
		return
	}
	if err := h.store.WritePlan(&plan); err != nil {
		response.WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}
	response.WriteJSON(w, http.StatusOK, plan)
}

// postPlanReview runs one reviewer pass over the posted plan draft.
func (h *Handler) postPlanReview(w http.ResponseWriter, r *http.Request) {
	if h.engine == nil {
		response.WriteError(w, http.StatusServiceUnavailable, "engine not initialized")
		return
	}
	var req struct {
		Plan *Plan `json:"plan"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.WriteError(w, http.StatusBadRequest, "invalid request")
		return
	}
	if req.Plan == nil || req.Plan.FRDID == "" {
		response.WriteError(w, http.StatusBadRequest, "missing plan")
		return
	}
	review, err := h.engine.reviewPlanOnce(r.Context(), req.Plan)
	if err != nil {
		if os.IsNotExist(err) || strings.Contains(err.Error(), "no such file or directory") {
			response.WriteJSON(w, http.StatusNotFound, map[string]string{"error": "state file not found"})
			return
		}
		response.WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}
	response.WriteJSON(w, http.StatusOK, review)
}

// postCompileTasks compiles the approved plan into the task queue.
func (h *Handler) postCompileTasks(w http.ResponseWriter, r *http.Request) {
	if h.engine == nil {
		response.WriteError(w, http.StatusServiceUnavailable, "engine not initialized")
		return
	}
	plan, err := h.store.GetPlan()
	if err != nil {
		writeStoreError(w, err)
		return
	}
	if plan.Status != "approved" {
		response.WriteError(w, http.StatusConflict, "plan must be approved before compiling tasks")
		return
	}
	queue, warnings, err := h.engine.compileTasksOnce(r.Context(), plan)
	if err != nil {
		response.WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if err := h.store.WriteQueue(queue); err != nil {
		response.WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if warnings == nil {
		warnings = []string{}
	}
	response.WriteJSON(w, http.StatusOK, map[string]any{"queue": queue, "warnings": warnings})
}

// handleSettings returns current settings
func (h *Handler) handleSettings(w http.ResponseWriter, _ *http.Request) {
	if h.engine == nil {
		response.WriteError(w, http.StatusServiceUnavailable, "engine not initialized")
		return
	}
	settings := map[string]any{
		"default_profile": h.engine.defaultProf,
		"profiles":        h.engine.profiles,
		"prompt_dir":      h.engine.promptDir,
		"timeout_secs":    h.engine.timeoutSecs,
	}
	response.WriteJSON(w, http.StatusOK, settings)
}

// postSettings updates settings
func (h *Handler) postSettings(w http.ResponseWriter, r *http.Request) {
	if h.engine == nil {
		response.WriteError(w, http.StatusServiceUnavailable, "engine not initialized")
		return
	}
	var req struct {
		DefaultProfile string                 `json:"default_profile"`
		Profiles       []config.ProfileConfig `json:"profiles"`
		PromptDir      string                 `json:"prompt_dir"`
		TimeoutSecs    int                    `json:"timeout_secs"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.WriteError(w, http.StatusBadRequest, "invalid settings")
		return
	}
	if req.DefaultProfile != "" {
		h.engine.defaultProf = req.DefaultProfile
	}
	if req.Profiles != nil {
		h.engine.profiles = req.Profiles
	}
	if req.PromptDir != "" {
		h.engine.promptDir = req.PromptDir
	}
	if req.TimeoutSecs > 0 {
		h.engine.timeoutSecs = req.TimeoutSecs
	}
	response.WriteJSON(w, http.StatusOK, map[string]string{"status": "updated"})
}

// writeStoreError maps parse/read failures to a 503 response.
func writeStoreError(w http.ResponseWriter, err error) {
	if os.IsNotExist(err) || strings.Contains(err.Error(), "no such file or directory") {
		response.WriteJSON(w, http.StatusNotFound, map[string]string{"error": "state file not found"})
		return
	}
	msg := fmt.Sprintf("agentflow state parse failed: %s", err.Error())
	response.WriteJSON(w, http.StatusServiceUnavailable, map[string]string{"error": msg})
}
