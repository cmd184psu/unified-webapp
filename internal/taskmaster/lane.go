package taskmaster

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"

	"cmd184psu/unified-webapp/internal/taskmaster/db"
	"cmd184psu/unified-webapp/internal/taskmaster/golane"
	"cmd184psu/unified-webapp/internal/taskmaster/models"
	"cmd184psu/unified-webapp/internal/taskmaster/worker"
)

// laneHandle implements golane.Lane over the Engine's DB and shared
// registries (§4.5).
type laneHandle struct {
	engine *Engine
	name   string
	owner  string
	kinds  map[string]golane.Kind
}

func (l *laneHandle) Name() string { return l.name }

// Submit validates payload against kind's Decode/validate (defence in
// depth — the caller-supplied payload is untrusted until decoded), then
// inserts a one-shot func task + its first pending execution (Option A).
// The job name is <lane>-<12 lowercase hex> (P1), retried up to 3 times on a
// name collision.
func (l *laneHandle) Submit(kind, label string, payload any) (golane.Job, error) {
	k, ok := l.kinds[kind]
	if !ok {
		return golane.Job{}, golane.ErrUnknownKind
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return golane.Job{}, fmt.Errorf("marshal payload: %w", err)
	}
	if len(raw) > golane.MaxPayloadBytes {
		return golane.Job{}, fmt.Errorf("payload exceeds %d bytes", golane.MaxPayloadBytes)
	}
	if _, err := k.Decode(k.Version, raw); err != nil {
		return golane.Job{}, err
	}

	var name string
	var execID int64
	for attempt := 0; attempt < 3; attempt++ {
		name, err = generateJobName(l.name)
		if err != nil {
			return golane.Job{}, err
		}
		_, execID, err = l.engine.db.SubmitFuncTask(db.FuncTaskSpec{
			Name: name, Lane: l.name, Kind: kind, Label: label,
			Payload: raw, PayloadVersion: k.Version,
		})
		if err == nil {
			break
		}
	}
	if err != nil {
		return golane.Job{}, err
	}

	worker.PublishBoardEvent(l.engine.board, l.engine.hidden, worker.BoardEvent{Type: "task-enqueued", Lane: l.name, Task: name, ExecutionID: execID})
	return l.Get(name)
}

func (l *laneHandle) List() ([]golane.Job, error) {
	tasks, err := l.engine.db.ListTasks(l.name)
	if err != nil {
		return nil, err
	}
	jobs := make([]golane.Job, 0, len(tasks))
	for _, t := range tasks {
		j, err := l.jobFromTask(t)
		if err != nil {
			return nil, err
		}
		jobs = append(jobs, j)
	}
	return jobs, nil
}

func (l *laneHandle) Get(id string) (golane.Job, error) {
	t, err := l.engine.db.GetTask(id)
	if err != nil {
		return golane.Job{}, err
	}
	if t == nil {
		return golane.Job{}, golane.ErrNotFound
	}
	return l.jobFromTask(t)
}

func (l *laneHandle) jobFromTask(t *models.Task) (golane.Job, error) {
	execs, err := l.engine.db.ListExecutions(t.Name, 1)
	if err != nil {
		return golane.Job{}, err
	}
	var latest *models.TaskExecution
	if len(execs) > 0 {
		latest = execs[0]
	}
	return toJob(t, latest, l.engine.progress), nil
}

func toJob(t *models.Task, e *models.TaskExecution, reg *worker.ProgressRegistry) golane.Job {
	j := golane.Job{
		ID:             t.Name,
		Kind:           t.Kind,
		Label:          t.Label,
		Payload:        t.Payload,
		PayloadVersion: t.PayloadVersion,
		CreatedAt:      t.CreatedAt,
		Status:         "queued",
	}
	if e == nil {
		return j
	}
	j.ExecID = e.ID
	j.StartedAt = e.StartedAt
	j.FinishedAt = e.FinishedAt
	j.Result = e.Result
	if e.ErrorMessage != nil {
		j.Error = *e.ErrorMessage
	}
	if e.Status == "pending" {
		j.Status = "queued"
	} else {
		j.Status = e.Status
	}

	pct, label := e.ProgressPct, e.ProgressLabel
	if e.Status == "running" && reg != nil {
		if p, ok := reg.Get(e.ID); ok {
			pct, label = p.Pct, p.Label
		}
	}
	if pct != nil || label != "" {
		j.Progress = &golane.Progress{Pct: pct, Label: label}
	}
	return j
}

func (l *laneHandle) Cancel(id string) (golane.CancelOutcome, error) {
	t, err := l.engine.db.GetTask(id)
	if err != nil {
		return "", err
	}
	if t == nil {
		return "", golane.ErrNotFound
	}
	execs, err := l.engine.db.ListExecutions(id, 1)
	if err != nil {
		return "", err
	}
	if len(execs) == 0 {
		return golane.CancelOutcome("not_running"), nil
	}
	return worker.CancelExecution(l.engine.db, l.engine.cancels, l.engine.board, l.engine.hidden, execs[0].ID)
}

func (l *laneHandle) Rerun(id string) (golane.Job, error) {
	t, err := l.engine.db.GetTask(id)
	if err != nil {
		return golane.Job{}, err
	}
	if t == nil {
		return golane.Job{}, golane.ErrNotFound
	}
	newID, err := l.engine.db.RerunTask(id)
	if err != nil {
		switch {
		case errors.Is(err, db.ErrTaskBusy):
			return golane.Job{}, golane.ErrBusy
		case errors.Is(err, db.ErrTaskSucceeded):
			return golane.Job{}, golane.ErrSucceeded
		case errors.Is(err, db.ErrTaskNotFound):
			return golane.Job{}, golane.ErrNotFound
		default:
			return golane.Job{}, err
		}
	}
	worker.PublishBoardEvent(l.engine.board, l.engine.hidden, worker.BoardEvent{Type: "task-enqueued", Lane: l.name, Task: id, ExecutionID: newID})
	return l.Get(id)
}

func (l *laneHandle) Remove(id string) error {
	err := l.engine.db.RemoveIdleTask(id)
	switch {
	case errors.Is(err, db.ErrTaskBusy):
		return golane.ErrRunning
	case errors.Is(err, db.ErrTaskNotFound):
		return golane.ErrNotFound
	case err != nil:
		return err
	}
	worker.PublishBoardEvent(l.engine.board, l.engine.hidden, worker.BoardEvent{Type: "lane-updated", Lane: l.name})
	return nil
}

func (l *laneHandle) Settings() (golane.LaneSettings, error) {
	ln, err := l.engine.db.GetLane(l.name)
	if err != nil {
		return golane.LaneSettings{}, err
	}
	if ln == nil {
		return golane.LaneSettings{}, golane.ErrNotFound
	}
	return golane.LaneSettings{
		Width:         ln.Width,
		RetentionDays: ln.RetentionDays,
		Hidden:        ln.Hidden,
		Paused:        ln.Paused,
		PausedBy:      ln.PausedBy,
		BrakeEngaged:  l.engine.brake.Engaged(),
	}, nil
}

func (l *laneHandle) UpdateSettings(p golane.SettingsPatch) (golane.LaneSettings, error) {
	if p.Width != nil && *p.Width < 1 {
		return golane.LaneSettings{}, golane.ErrInvalidSettings
	}
	if p.RetentionDays != nil && *p.RetentionDays < 0 {
		return golane.LaneSettings{}, golane.ErrInvalidSettings
	}

	if p.Width != nil {
		if err := l.engine.db.SetLaneWidth(l.name, *p.Width); err != nil {
			return golane.LaneSettings{}, err
		}
	}
	if p.RetentionDays != nil {
		if err := l.engine.db.SetLaneRetention(l.name, *p.RetentionDays); err != nil {
			return golane.LaneSettings{}, err
		}
		l.engine.pruneOnce()
	}
	if p.Paused != nil {
		by := ""
		if *p.Paused {
			by = "owner:" + l.owner
		}
		if err := l.engine.db.SetLanePaused(l.name, *p.Paused, by); err != nil {
			return golane.LaneSettings{}, err
		}
	}
	if p.Hidden != nil {
		if err := l.engine.db.SetLaneHidden(l.name, *p.Hidden); err != nil {
			return golane.LaneSettings{}, err
		}
		l.engine.hidden.Set(l.name, *p.Hidden)
		worker.PublishBoardEvent(l.engine.board, l.engine.hidden, worker.BoardEvent{Type: "lanes-changed"})
	}
	if p.Width != nil || p.Paused != nil {
		worker.PublishBoardEvent(l.engine.board, l.engine.hidden, worker.BoardEvent{Type: "lane-updated", Lane: l.name})
	}
	return l.Settings()
}

// generateJobName returns "<lane>-<12 lowercase hex chars>" (P1).
func generateJobName(lane string) (string, error) {
	b := make([]byte, 6)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return fmt.Sprintf("%s-%s", lane, hex.EncodeToString(b)), nil
}
