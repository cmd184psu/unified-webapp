// Package golane is the in-process interface between taskmaster (the one
// queue engine) and any module that wants a Go-function ("func") task lane
// (plan §4.3). It imports only the standard library and contains no
// module-specific names (R4) — a media-download module is its first
// consumer, but the registration API names no module.
package golane

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"time"
)

// MaxPayloadBytes is the size cap for a job's payload and result JSON.
const MaxPayloadBytes = 64 << 10

var (
	ErrNotFound        = errors.New("job not found")
	ErrBusy            = errors.New("job is queued or running")
	ErrRunning         = errors.New("job is running")
	ErrUnknownKind     = errors.New("kind not registered on this lane")
	ErrKindRegistered  = errors.New("kind already registered")
	ErrLaneRegistered  = errors.New("lane already registered in this process")
	ErrInvalidSettings = errors.New("invalid lane settings")
	// ErrSucceeded is returned by Lane.Rerun (and surfaced as HTTP 409 on
	// both rerun routes) for a one-shot job whose latest execution already
	// succeeded (P18): a succeeded one-shot job is done, generically, not
	// tied to any one module.
	ErrSucceeded = errors.New("job already succeeded")
)

// RunContext is handed to a Kind's Run. It is safe for concurrent use and is
// a no-op after Run returns (P13/§4.4 step 6).
type RunContext interface {
	JobID() string                  // the task name
	ExecID() int64                  // the current execution's id
	Progress(pct int, label string) // pct 0..100; pct < 0 = indeterminate; values > 100 clamp to 100
	SetLabel(label string)          // retitle the job (db.UpdateTaskLabel, immediately; <= 200 runes)
	Log() io.Writer                 // lines appear in TM's per-execution output (SSE replay)
	Stderr() io.Writer              // the execution's stderr stream, captured separately from Log()/stdout
}

// Kind is one registrable Go-function task kind. Build one with NewKind.
type Kind struct {
	Name    string // ^[a-z0-9]+(\.[a-z0-9_-]+)+$  e.g. "<owner>.<verb>"
	Version int    // >= 1; stored per task as payload_version
	Decode  func(version int, raw json.RawMessage) (any, error)
	Run     func(ctx context.Context, rc RunContext, payload any) (result any, err error)
}

var kindNamePattern = regexp.MustCompile(`^[a-z0-9]+(\.[a-z0-9_-]+)+$`)

// NewKind builds a typed Kind: strict JSON decode into P
// (DisallowUnknownFields), the version must equal `version`, then validate
// runs. Run receives the decoded P (Q1/Q2).
func NewKind[P any](name string, version int, validate func(P) error,
	run func(ctx context.Context, rc RunContext, p P) (any, error)) Kind {
	return Kind{
		Name:    name,
		Version: version,
		Decode: func(v int, raw json.RawMessage) (any, error) {
			if v != version {
				return nil, fmt.Errorf("unsupported payload version %d for kind %s (supports %d)", v, name, version)
			}
			dec := json.NewDecoder(bytes.NewReader(raw))
			dec.DisallowUnknownFields()
			var p P
			if err := dec.Decode(&p); err != nil {
				return nil, err
			}
			if validate != nil {
				if err := validate(p); err != nil {
					return nil, err
				}
			}
			return p, nil
		},
		Run: func(ctx context.Context, rc RunContext, payload any) (any, error) {
			p, ok := payload.(P)
			if !ok {
				return nil, fmt.Errorf("golane: internal error: payload type mismatch for kind %s", name)
			}
			return run(ctx, rc, p)
		},
	}
}

// LaneSpec describes a module-owned lane at registration time. Width/
// RetentionDays/Hidden are seeds only (P8/P10/P9): the DB is authoritative
// afterward.
type LaneSpec struct {
	Name                 string // ^[a-z0-9][a-z0-9_-]{0,31}$
	Owner                string // same pattern
	InitialWidth         int    // seed only (>= 0; 0 = never runs, see R7)
	InitialRetentionDays int    // seed only (>= 0)
	InitialHidden        bool   // seed only
}

// Progress is a single progress report: Pct 0-100, or nil for indeterminate.
type Progress struct {
	Pct   *int   `json:"pct"`
	Label string `json:"label"`
}

// Job is one queue item (a one-shot func task, plus its latest execution).
type Job struct {
	ID             string          // task name
	Kind           string          `json:"kind"`
	Label          string          `json:"label"`
	Payload        json.RawMessage `json:"payload"`
	PayloadVersion int             `json:"payload_version"`
	Status         string          // queued | running | success | failed | canceled
	ExecID         int64           // latest execution
	Progress       *Progress       // nil = never reported
	Result         json.RawMessage // latest execution's result (nil unless success)
	Error          string
	CreatedAt      time.Time
	StartedAt      *time.Time
	FinishedAt     *time.Time
}

// LaneSettings is the DB-authoritative state of one owned lane (P7).
type LaneSettings struct {
	Width, RetentionDays int
	Hidden, Paused       bool
	PausedBy             string // from lanes.paused_by ("brake", "owner:<o>", "api", …)
	BrakeEngaged         bool   // read-only: the engine's BrakeGate; ignored by UpdateSettings
}

// SettingsPatch is a partial update to LaneSettings; nil fields are left
// unchanged.
type SettingsPatch struct {
	Width, RetentionDays *int
	Hidden, Paused       *bool
}

// CancelOutcome is the result of Lane.Cancel (and the shared cancel
// protocol, worker.CancelExecution).
type CancelOutcome string

// Lane is a module's handle on its own queue (all scheduling/persistence
// lives in taskmaster; a module never holds queue state itself, D1).
type Lane interface {
	Name() string
	Submit(kind, label string, payload any) (Job, error)
	List() ([]Job, error) // ordered by task position ASC (FIFO)
	Get(id string) (Job, error)
	Cancel(id string) (CancelOutcome, error)
	Rerun(id string) (Job, error) // ErrBusy if queued/running; ErrSucceeded if latest is success
	Remove(id string) error       // ErrRunning if running; queued removal is allowed
	Settings() (LaneSettings, error)
	UpdateSettings(p SettingsPatch) (LaneSettings, error) // width >= 1, retention >= 0 -> else ErrInvalidSettings
	// StreamOutput streams the job's latest execution's stdout+stderr as SSE;
	// ErrNotFound if jobID doesn't exist. This is the one deliberate exception
	// to golane being transport-agnostic: SSE streaming fundamentally requires
	// direct access to the http.ResponseWriter/Flusher, so it cannot be hidden
	// behind a transport-neutral return value.
	StreamOutput(w http.ResponseWriter, r *http.Request, jobID string) error
}

// Host registers a module-owned lane with its kinds. Implemented by
// *taskmaster.Engine. Contains no module-specific names (Q5): a second
// module could call RegisterLane without any refactor.
type Host interface {
	RegisterLane(spec LaneSpec, kinds ...Kind) (Lane, error)
}
