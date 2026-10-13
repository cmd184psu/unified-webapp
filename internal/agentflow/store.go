package agentflow

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// Store abstracts persistence for agentflow data.
type Store struct {
	ProjectPath string
}

// NewStore returns a Store configured with the given project path —
// the directory that contains the `.agentflow` subdirectory — and falls
// back to DefaultAgentflowProjectPath when the supplied path is empty
// (0-length).
func NewStore(projectPath string) *Store {
	if projectPath == "" {
		projectPath = DefaultAgentflowProjectPath
	}
	return &Store{ProjectPath: projectPath}
}

// dataPath returns the full path to a JSON file inside the `.agentflow`
// subdirectory of the configured project directory (the project root that
// contains .agentflow — the orchestrator writes queue.json there).
func (s *Store) dataPath(filename string) string {
	return filepath.Join(s.ProjectPath, ".agentflow", filename)
}

// QueueData represents the top-level shape of queue.json: an object with a
// "tasks" array whose elements are {id, title, status, attempts}.
type QueueData struct {
	Tasks []Task `json:"tasks"`
}



// ProgressCount holds the tally derived from a QueueData's task list.
// total = len(tasks), passed/ready/blocked/review/repair/failed = count per status.
type ProgressCount struct {
	Total   int `json:"total"`
	Passed  int `json:"passed"`
	Ready   int `json:"ready"`
	Blocked int `json:"blocked"`
	Review  int `json:"review"`
	Repair  int `json:"repair"`
	Failed  int `json:"failed"`
}

// GetQueue reads and parses queue.json from the store's project path.
// Returns an error when the file cannot be read or parsed.
func (s *Store) GetQueue() (*QueueData, error) {
	path := s.dataPath("queue.json")
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read queue: %w", err)
	}

	var payload QueueData
	if err := json.Unmarshal(data, &payload); err != nil {
		return nil, fmt.Errorf("parse queue JSON: %w", err)
	}
	return &payload, nil
}

// GetProgress derives progress counts from the task list in queue.json.
// Instead of reading a separate progress.json file that never exists,
// it tallies total, passed, ready, blocked, review, repair, and failed
// from each task's status field when queue.json is available; otherwise
// returns a zero ProgressCount with no error so the frontend always gets
// a valid shape on 404.
func (s *Store) GetProgress() (*ProgressCount, error) {
	queueData, err := s.GetQueue()
	if err != nil {
		// queue.json missing / unreadable — return zero counts rather than
		// propagating an error so the progress panel always renders a valid
		// empty summary.
		return &ProgressCount{}, nil
	}

	p := &ProgressCount{Total: len(queueData.Tasks)}
	for _, t := range queueData.Tasks {
		switch t.Status {
		case "passed":
			p.Passed++
		case "ready":
			p.Ready++
		case "blocked":
			p.Blocked++
		case "review":
			p.Review++
		case "repair":
			p.Repair++
		case "failed":
			p.Failed++
		}
	}
	return p, nil
}

// WriteTaskStatus replaces queue.json with an updated task entry whose status
// has been flipped to the new value.  It reads the existing file, mutates the
// matching task (by ID), and writes a canonical JSON snapshot back — so other
// fields (title, attempts, etc.) are preserved atomically.
func (s *Store) WriteTaskStatus(id, newStatus string) error {
	path := s.dataPath("queue.json")
	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read queue: %w", err)
	}

	var payload QueueData
	if err := json.Unmarshal(data, &payload); err != nil {
		return fmt.Errorf("parse queue JSON: %w", err)
	}

	for i := range payload.Tasks {
		if payload.Tasks[i].ID == id {
			payload.Tasks[i].Status = newStatus
			break
		}
	}

	out, err := json.MarshalIndent(&payload, "", " ")
	if err != nil {
		return fmt.Errorf("marshal queue: %w", err)
	}

	// Ensure trailing newline for POSIX-ish cleanliness.
	out = append(out, '\n')

	return os.WriteFile(path, out, 0o644)
}

// RunNext finds the first ready task and flips its status in queue.json to
// "in-progress".  Returns false with no error when there is none (clean slate).
func (s *Store) RunNext() (bool, error) {
	path := s.dataPath("queue.json")
	data, err := os.ReadFile(path)
	if err != nil {
		return false, fmt.Errorf("read queue: %w", err)
	}

	var payload QueueData
	if err := json.Unmarshal(data, &payload); err != nil {
		return false, fmt.Errorf("parse queue JSON: %w", err)
	}

	for i := range payload.Tasks {
		if payload.Tasks[i].Status == "ready" {
			payload.Tasks[i].Status = "in-progress"
			out, err := json.MarshalIndent(&payload, "", " ")
			if err != nil {
				return false, fmt.Errorf("marshal queue: %w", err)
			}
			out = append(out, '\n')
			return true, os.WriteFile(path, out, 0o644)
		}
	}

	return false, nil
}

// ─── FRD Persistence ────────────────────────────────────────────────────────

// GetFRD reads and parses frd.json from the store's project path.
func (s *Store) GetFRD() (*FRD, error) {
	path := s.dataPath("frd.json")
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read frd: %w", err)
	}

	var frd FRD
	if err := json.Unmarshal(data, &frd); err != nil {
		return nil, fmt.Errorf("parse frd JSON: %w", err)
	}
	return &frd, nil
}

// WriteFRD writes frd.json to the store's project path.
func (s *Store) WriteFRD(frd *FRD) error {
	path := s.dataPath("frd.json")
	out, err := json.MarshalIndent(frd, "", " ")
	if err != nil {
		return fmt.Errorf("marshal frd: %w", err)
	}
	out = append(out, '\n')
	return os.WriteFile(path, out, 0o644)
}

// WriteQueue writes queue.json (a full TaskQueue) to the store's project path.
func (s *Store) WriteQueue(q *TaskQueue) error {
	path := s.dataPath("queue.json")
	out, err := json.MarshalIndent(q, "", " ")
	if err != nil {
		return fmt.Errorf("marshal queue: %w", err)
	}
	out = append(out, '\n')
	return os.WriteFile(path, out, 0o644)
}

// ─── Plan Persistence ───────────────────────────────────────────────────────

// GetPlan reads and parses plan.json from the store's project path.
func (s *Store) GetPlan() (*Plan, error) {
	path := s.dataPath("plan.json")
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read plan: %w", err)
	}

	var plan Plan
	if err := json.Unmarshal(data, &plan); err != nil {
		return nil, fmt.Errorf("parse plan JSON: %w", err)
	}
	return &plan, nil
}

// WritePlan writes plan.json to the store's project path.
func (s *Store) WritePlan(plan *Plan) error {
	path := s.dataPath("plan.json")
	out, err := json.MarshalIndent(plan, "", " ")
	if err != nil {
		return fmt.Errorf("marshal plan: %w", err)
	}
	out = append(out, '\n')
	return os.WriteFile(path, out, 0o644)
}
