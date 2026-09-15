package coordinator

import (
	"encoding/json"
	"net/http"
	"sync"
	"sync/atomic"

	"github.com/go-chi/chi/v5"

	"cmd184psu/unified-webapp/internal/platform/broker"
	"cmd184psu/unified-webapp/internal/platform/response"
	"cmd184psu/unified-webapp/internal/taskmaster/db"
	"cmd184psu/unified-webapp/internal/taskmaster/worker"
)

type Coordinator struct {
	db       *db.DB
	registry *worker.OutputRegistry
	sudo     *worker.SudoGate
	cancels  *worker.CancelRegistry
	brake    *worker.BrakeGate
	procs    *worker.ProcessRegistry
	brakeMu  sync.Mutex // serializes engage/release read-modify-write of the brake + SettingBrakePausedLanes
	sseMax   int
	sseSubs  atomic.Int64
	board    *broker.Broker
}

// New builds a Coordinator. procs is the shared per-execution process
// registry (also wired into the worker), used by the pause/resume endpoints
// and merged into /api/executions responses as pid/suspended. board is the
// shared board-events broker (also wired into the worker); GET
// /api/board/events streams what it publishes, respecting the same
// SSEMaxSubscribers cap as the per-execution output SSE (enforced by the
// broker itself via SetMaxSubscribers). board may be nil in tests that don't
// exercise /api/board/events.
func New(database *db.DB, registry *worker.OutputRegistry, sudo *worker.SudoGate, cancels *worker.CancelRegistry, brake *worker.BrakeGate, procs *worker.ProcessRegistry, sseMax int, board *broker.Broker) *Coordinator {
	return &Coordinator{db: database, registry: registry, sudo: sudo, cancels: cancels, brake: brake, procs: procs, sseMax: sseMax, board: board}
}

// publishBoard is a thin wrapper around worker.PublishBoardEvent so handlers
// don't need to import the broker package directly.
func (c *Coordinator) publishBoard(ev worker.BoardEvent) {
	worker.PublishBoardEvent(c.board, ev)
}

// Routes returns the HTTP handler (exported for testing).
func Routes(c *Coordinator) *chi.Mux { return c.routes() }

func (c *Coordinator) routes() *chi.Mux {
	r := chi.NewRouter()

	r.Get("/api/health", c.handleHealth)
	r.Get("/api/capabilities", c.handleCapabilities)
	r.Post("/api/capabilities", c.handleSetCapabilities)

	r.Get("/api/lanes", c.handleListLanes)
	r.Post("/api/lanes", c.handleCreateLane)
	r.Get("/api/lanes/{name}", c.handleGetLane)
	r.Put("/api/lanes/{name}", c.handleUpdateLane)
	r.Delete("/api/lanes/{name}", c.handleDeleteLane)
	r.Post("/api/lanes/{name}/pause", c.handlePauseLane)
	r.Post("/api/lanes/{name}/resume", c.handleResumeLane)
	r.Put("/api/lanes/{name}/width", c.handleSetLaneWidth)
	r.Put("/api/lanes/{name}/order", c.handleSetLaneOrder)

	r.Get("/api/tasks", c.handleListTasks)
	r.Post("/api/tasks", c.handleAddTask)
	r.Get("/api/tasks/{name}", c.handleGetTask)
	r.Put("/api/tasks/{name}", c.handleUpdateTask)
	r.Delete("/api/tasks/{name}", c.handleDeleteTask)
	r.Post("/api/tasks/{name}/pause", c.handlePauseTask)
	r.Post("/api/tasks/{name}/resume", c.handleResumeTask)
	r.Post("/api/tasks/{name}/up-next", c.handleUpNext)
	r.Post("/api/tasks/{name}/move", c.handleMoveTask)

	r.Get("/api/executions", c.handleListExecutions)
	r.Get("/api/executions/{id}/output", c.handleExecutionOutput)
	r.Post("/api/executions/{id}/cancel", c.handleCancelExecution)
	r.Post("/api/executions/{id}/pause", c.handlePauseExecution)
	r.Post("/api/executions/{id}/resume", c.handleResumeExecution)

	r.Get("/api/metrics", c.handleMetrics)

	// Board-events SSE (plan D7/B5): a live stream of compact change events
	// (task-started/finished, task-enqueued, lane-updated, brake) so the
	// client can patch the lane board surgically instead of polling/full
	// repaint. Deliberately does NOT replay history on connect — clients are
	// expected to fetch a board snapshot via the REST endpoints first, then
	// subscribe here for subsequent changes. Subscriber cap is enforced by
	// the broker itself (SetMaxSubscribers in build.go), returning 503
	// before any SSE header is written once the cap is hit — same behavior
	// as the per-execution output SSE cap.
	r.Get("/api/board/events", c.handleBoardEvents)

	r.Get("/api/brake", c.handleGetBrake)
	r.Post("/api/brake", c.handleEngageBrake)
	r.Delete("/api/brake", c.handleReleaseBrake)

	return r
}

// ─── Shared helpers ───────────────────────────────────────────────────────────

func (c *Coordinator) handleHealth(w http.ResponseWriter, r *http.Request) {
	if err := c.db.Ping(); err != nil {
		response.WriteError(w, http.StatusServiceUnavailable, "database unavailable: "+err.Error())
		return
	}
	response.WriteJSON(w, http.StatusOK, map[string]string{"status": "ok", "build": BuildTime})
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
