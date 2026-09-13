package coordinator

import (
	"encoding/json"
	"net/http"
	"sync/atomic"

	"github.com/go-chi/chi/v5"

	"cmd184psu/unified-webapp/internal/platform/response"
	"cmd184psu/unified-webapp/internal/taskmaster/db"
	"cmd184psu/unified-webapp/internal/taskmaster/worker"
)

type Coordinator struct {
	db       *db.DB
	registry *worker.OutputRegistry
	sudo     *worker.SudoGate
	sseMax   int
	sseSubs  atomic.Int64
}

func New(database *db.DB, registry *worker.OutputRegistry, sudo *worker.SudoGate, sseMax int) *Coordinator {
	return &Coordinator{db: database, registry: registry, sudo: sudo, sseMax: sseMax}
}

// Routes returns the HTTP handler (exported for testing).
func Routes(c *Coordinator) *chi.Mux { return c.routes() }

func (c *Coordinator) routes() *chi.Mux {
	r := chi.NewRouter()

	r.Get("/api/health", c.handleHealth)
	r.Get("/api/capabilities", c.handleCapabilities)
	r.Post("/api/capabilities", c.handleSetCapabilities)

	r.Get("/api/groups", c.handleListGroups)
	r.Post("/api/groups", c.handleCreateGroup)
	r.Get("/api/groups/{name}", c.handleGetGroup)
	r.Put("/api/groups/{name}", c.handleUpdateGroup)
	r.Delete("/api/groups/{name}", c.handleDeleteGroup)
	r.Post("/api/groups/{name}/pause", c.handlePauseGroup)
	r.Post("/api/groups/{name}/resume", c.handleResumeGroup)

	r.Get("/api/tasks", c.handleListTasks)
	r.Post("/api/tasks", c.handleAddTask)
	r.Get("/api/tasks/{name}", c.handleGetTask)
	r.Put("/api/tasks/{name}", c.handleUpdateTask)
	r.Delete("/api/tasks/{name}", c.handleDeleteTask)
	r.Post("/api/tasks/{name}/pause", c.handlePauseTask)
	r.Post("/api/tasks/{name}/resume", c.handleResumeTask)
	r.Post("/api/tasks/{name}/enqueue", c.handleEnqueueTask)

	r.Get("/api/executions", c.handleListExecutions)
	r.Get("/api/executions/{id}/output", c.handleExecutionOutput)

	r.Get("/api/metrics", c.handleMetrics)

	return r
}

// ─── Shared helpers ───────────────────────────────────────────────────────────

func (c *Coordinator) handleHealth(w http.ResponseWriter, r *http.Request) {
	if err := c.db.Ping(); err != nil {
		response.WriteError(w, http.StatusServiceUnavailable, "database unavailable: "+err.Error())
		return
	}
	response.WriteJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (c *Coordinator) handleCapabilities(w http.ResponseWriter, r *http.Request) {
	response.WriteJSON(w, http.StatusOK, map[string]bool{"allow_sudo": c.sudo.Allowed()})
}

// handleSetCapabilities toggles allow_sudo at runtime and persists it so the
// change survives restarts (the DB is authoritative once seeded from config).
// The setting takes effect immediately for both task validation and the
// executor, since both share the same SudoGate.
func (c *Coordinator) handleSetCapabilities(w http.ResponseWriter, r *http.Request) {
	var req struct {
		AllowSudo *bool `json:"allow_sudo"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.WriteDecodeError(w, err)
		return
	}
	if req.AllowSudo == nil {
		response.WriteError(w, http.StatusBadRequest, "allow_sudo is required")
		return
	}
	if err := c.db.SetSetting(db.SettingAllowSudo, db.EncodeBoolSetting(*req.AllowSudo)); err != nil {
		response.WriteError(w, http.StatusInternalServerError, "persisting setting: "+err.Error())
		return
	}
	c.sudo.Set(*req.AllowSudo)
	response.WriteJSON(w, http.StatusOK, map[string]bool{"allow_sudo": c.sudo.Allowed()})
}
