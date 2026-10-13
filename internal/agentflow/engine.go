package agentflow

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"

	"cmd184psu/unified-webapp/internal/platform/broker"
	"cmd184psu/unified-webapp/internal/platform/config"
	"net/http"
)

// Engine is the agentflow engine: task queue, worker, SSE broker, lock management.
type Engine struct {
	store       *Store
	worker      *Worker
	broker      *broker.Broker
	prompts     PromptTemplates
	profiles    []config.ProfileConfig
	defaultProf string
	promptDir   string
	timeoutSecs int

	mu          sync.Mutex
	currentExec *TaskExecution
	lockPath    string
	projectPath string
	modelRun    func(ctx context.Context, model, prompt, ollamaHost string) (string, error)

	stopWorker  func()
	workerDone  chan struct{}
}

// NewEngine creates a new agentflow engine.
func NewEngine(cfg config.AgentflowConfig, store *Store) (*Engine, error) {
	if err := os.MkdirAll(filepath.Dir(cfg.ProjectPath), 0750); err != nil {
		return nil, err
	}

	// Load prompt templates
	prompts, err := loadPrompts(cfg.PromptDir)
	if err != nil {
		return nil, fmt.Errorf("load prompts: %w", err)
	}

	// Resolve profiles
	profiles := cfg.Profiles
	if len(profiles) == 0 {
		profiles = []config.ProfileConfig{
			{
				Name:         "m5",
				OllamaHost:   "http://192.168.1.239:11434",
				WorkerModel:  "qwen3.6:35b-a3b-mxfp8",
				PlannerModel: "qwen3.6:35b-a3b-mxfp8",
				ReviewModel:  "mistral-small3.2",
				TimeoutSecs:  300,
			},
		}
	}

	e := &Engine{
		store:       store,
		prompts:     prompts,
		profiles:    profiles,
		defaultProf: cfg.DefaultProfile,
		promptDir:   cfg.PromptDir,
		timeoutSecs: cfg.DefaultTimeoutSecs,
		lockPath:    filepath.Join(cfg.ProjectPath, ".agentflow", "lock"),
		projectPath: cfg.ProjectPath,
		broker:      broker.NewBroker(0),
	}

	// Load existing queue to recover any running task
	if err := e.recoverLock(); err != nil {
		log.Printf("agentflow: lock recovery: %v", err)
	}

	// Start worker
	e.worker = newWorker(e)
	e.worker.Start()

	return e, nil
}

// recoverLock checks for a stale lock file and cleans it up if the PID is dead.
func (e *Engine) recoverLock() error {
	data, err := os.ReadFile(e.lockPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}

	var lock LockFile
	if err := json.Unmarshal(data, &lock); err != nil {
		// Corrupted lock, remove it
		os.Remove(e.lockPath)
		return nil
	}

	// Check if process is still alive
	if lock.PID > 0 {
		proc, err := os.FindProcess(lock.PID)
		if err == nil {
			// Signal 0 just checks existence
			if err := proc.Signal(syscall.Signal(0)); err == nil {
				log.Printf("agentflow: active lock found (PID %d, task %s), not starting worker", lock.PID, lock.TaskID)
				return nil // Active lock, don't start worker
			}
		}
	}

	// Stale lock - remove it
	log.Printf("agentflow: removing stale lock (PID %d, task %s)", lock.PID, lock.TaskID)
	return os.Remove(e.lockPath)
}

// acquireLock writes the lock file for the current task.
func (e *Engine) acquireLock(taskID string) error {
	lock := LockFile{
		PID:      os.Getpid(),
		TaskID:   taskID,
		Started:  time.Now().Format(time.RFC3339),
		Hostname: mustHostname(),
	}
	data, _ := json.MarshalIndent(lock, "", " ")
	return os.WriteFile(e.lockPath, data, 0644)
}

func (e *Engine) releaseLock() {
	os.Remove(e.lockPath)
}

// Worker runs the task execution loop.
type Worker struct {
	engine   *Engine
	queue    chan *Task
	cancel   context.CancelFunc
	done     chan struct{}
	interval time.Duration
}

func newWorker(e *Engine) *Worker {
	cancel := context.CancelFunc(func() {})
	return &Worker{
		engine:   e,
		queue:    make(chan *Task, 1),
		cancel:   cancel,
		done:     make(chan struct{}),
		interval: 5 * time.Second,
	}
}

func (w *Worker) Start() {
	go w.loop()
}

func (w *Worker) loop() {
	defer close(w.done)
	ticker := time.NewTicker(w.interval)
	defer ticker.Stop()

	for {
		select {
		case task := <-w.queue:
			w.engine.runTask(task)
		case <-ticker.C:
			w.maybeEnqueueNext()
		}
	}
}

func (w *Worker) maybeEnqueueNext() {
	// Find next ready task (respecting dependencies)
	queue, err := w.engine.store.GetQueue()
	if err != nil {
		return
	}

	for _, task := range queue.Tasks {
		if task.Status != "ready" {
			continue
		}
		// Check dependencies
		depsMet := true
		for _, depID := range task.DependsOn {
			var dep *Task
			for _, t := range queue.Tasks {
				if t.ID == depID {
					dep = &t
					break
				}
			}
			if dep == nil || dep.Status != "passed" {
				depsMet = false
				break
			}
		}
		if depsMet {
			select {
			case w.queue <- &task:
			default:
			}
			return
		}
	}
}

// EnqueueReady adds a ready task to the worker queue.
func (w *Worker) EnqueueReady(task *Task) {
	select {
	case w.queue <- task:
	default:
	}
}

func (w *Worker) Stop() {
	w.cancel()
	<-w.done
}

// runTask executes a single task: worker → verify → review.
func (e *Engine) runTask(t *Task) {
	e.mu.Lock()
	if e.currentExec != nil {
		e.mu.Unlock()
		return // Already running something
	}

	exec := &TaskExecution{
		TaskID:    t.ID,
		Task:      t,
		Status:    "running",
		StartTime: time.Now(),
		Cancel:    make(chan struct{}),
	}
	e.currentExec = exec
	e.mu.Unlock()

	// Acquire lock
	if err := e.acquireLock(t.ID); err != nil {
		e.finishTask(t, "failed", fmt.Sprintf("lock failed: %v", err))
		return
	}

	// Update task status to running
	t.Status = "running"
	t.Attempts++
	t.LastRun = time.Now().Format(time.RFC3339)
	e.store.WriteTaskStatus(t.ID, "running")
	e.broadcastTaskUpdate(t)

	// Get profile
	prof := e.resolveProfile("")

	// Build worker prompt
	workerPrompt := e.buildWorkerPrompt(t, prof)

	// Run worker model
	ctx, cancel := context.WithTimeout(context.Background(), prof.EffectiveTimeout)
	defer cancel()

	output, err := e.runModel(ctx, prof.WorkerModel, workerPrompt, prof.OllamaHost)
	if err != nil {
		if ctx.Err() == context.DeadlineExceeded {
			t.InfraRetries++
			e.finishTask(t, "ready", "timeout")
			return
		}
		e.finishTask(t, "failed", fmt.Sprintf("worker error: %v", err))
		return
	}

	exec.OutputLines = strings.Split(output, "\n")
	e.broadcastExecutionOutput(exec)

	// Run verify command
	exec.Status = "verifying"
	verifyOut, err := e.runVerify(t)
	exec.VerifyOut = verifyOut
	if err != nil {
		// Verify failed - repair
		t.RepairPrompt = fmt.Sprintf("Verify failed: %s\nOutput: %s", err.Error(), verifyOut)
		e.finishTask(t, "repair", "")
		return
	}

	// Run reviewer model
	exec.Status = "reviewing"
	reviewPrompt := e.buildReviewPrompt(t, exec.OutputLines, verifyOut, prof)
	reviewOut, err := e.runModel(ctx, prof.ReviewModel, reviewPrompt, prof.OllamaHost)
	if err != nil {
		e.finishTask(t, "failed", fmt.Sprintf("reviewer error: %v", err))
		return
	}

	var review Review
	if err := json.Unmarshal([]byte(reviewOut), &review); err != nil {
		e.finishTask(t, "failed", fmt.Sprintf("parse review: %v", err))
		return
	}

	review.TaskID = t.ID
	review.ReviewedAt = time.Now().Format(time.RFC3339)
	review.Reviewer = prof.ReviewModel
	exec.Review = &review

	// Apply review verdict
	switch review.Verdict {
	case "pass":
		e.finishTask(t, "passed", "")
	case "repair":
		t.RepairPrompt = review.RepairPrompt
		t.ReviewFollowups++
		if t.ReviewFollowups >= 2 {
			e.finishTask(t, "failed", "max review followups exceeded")
			return
		}
		e.finishTask(t, "repair", "")
	case "fail":
		e.finishTask(t, "failed", review.Summary)
	default:
		e.finishTask(t, "failed", "unknown verdict: "+review.Verdict)
	}
}

func (e *Engine) finishTask(t *Task, status, errMsg string) {
	t.Status = status
	if errMsg != "" {
		t.RepairPrompt = errMsg
	}
	e.store.WriteTaskStatus(t.ID, status)
	e.broadcastTaskUpdate(t)

	e.mu.Lock()
	e.currentExec = nil
	e.releaseLock()
	e.mu.Unlock()

	// Enqueue next ready task
	e.worker.maybeEnqueueNext()
}

func (e *Engine) resolveProfile(profileName string) *config.ProfileConfig {
	if profileName == "" {
		profileName = e.defaultProf
	}
	for i := range e.profiles {
		if e.profiles[i].Name == profileName {
			p := e.profiles[i]
			if p.TimeoutSecs <= 0 {
				p.TimeoutSecs = e.timeoutSecs
			}
			if p.TimeoutSecs <= 0 {
				p.TimeoutSecs = 300
			}
			p.EffectiveTimeout = time.Duration(p.TimeoutSecs) * time.Second
			return &p
		}
	}
	// Fallback to first profile
	p := e.profiles[0]
	if p.TimeoutSecs <= 0 {
		p.TimeoutSecs = e.timeoutSecs
	}
	if p.TimeoutSecs <= 0 {
		p.TimeoutSecs = 300
	}
	p.EffectiveTimeout = time.Duration(p.TimeoutSecs) * time.Second
	return &p
}

func (e *Engine) buildWorkerPrompt(task *Task, prof *config.ProfileConfig) string {
	prompt := e.prompts.Worker
	prompt = strings.ReplaceAll(prompt, "{{PROJECT_DIR}}", e.projectPath)
	prompt = strings.ReplaceAll(prompt, "{{TASK_ID}}", task.ID)
	prompt = strings.ReplaceAll(prompt, "{{TASK_TITLE}}", task.Title)
	prompt = strings.ReplaceAll(prompt, "{{TASK_PROMPT}}", task.Prompt)
	prompt = strings.ReplaceAll(prompt, "{{TASK_VERIFY}}", task.Verify)
	return prompt
}

func (e *Engine) buildReviewPrompt(task *Task, workerOutput []string, verifyOut string, prof *config.ProfileConfig) string {
	prompt := e.prompts.Reviewer
	prompt = strings.ReplaceAll(prompt, "{{TASK_ID}}", task.ID)
	prompt = strings.ReplaceAll(prompt, "{{TASK_JSON}}", mustMarshalString(task))
	prompt = strings.ReplaceAll(prompt, "{{TASK_VERIFY}}", task.Verify)

	workerLog := strings.Join(workerOutput, "\n")
	prompt = strings.ReplaceAll(prompt, "{{WORKER_LOG}}", workerLog)
	prompt = strings.ReplaceAll(prompt, "{{VERIFY_OUTPUT}}", verifyOut)

	return prompt
}

func mustMarshalString(v any) string {
	b, _ := json.Marshal(v)
	return string(b)
}

// runModel calls the Ollama API to run a model. Tests override e.modelRun.
func (e *Engine) runModel(ctx context.Context, model, prompt, ollamaHost string) (string, error) {
	if e.modelRun != nil {
		return e.modelRun(ctx, model, prompt, ollamaHost)
	}
	cmd := exec.CommandContext(ctx, "opencode-run", "--model", model, "--prompt", prompt)
	cmd.Env = append(os.Environ(), "OLLAMA_HOST="+ollamaHost)
	out, err := cmd.CombinedOutput()
	return string(out), err
}

func (e *Engine) runVerify(task *Task) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "bash", "-c", task.Verify)
	cmd.Dir = filepath.Join(e.projectPath, task.WorkingDir)
	out, err := cmd.CombinedOutput()
	return string(out), err
}

// Broadcast helpers for SSE
func (e *Engine) broadcastTaskUpdate(task *Task) {
	data, _ := json.Marshal(task)
	e.broker.Publish(string(data))
}

func (e *Engine) broadcastExecutionOutput(exec *TaskExecution) {
	data, _ := json.Marshal(map[string]any{
		"task_id": exec.TaskID,
		"lines":   exec.OutputLines,
	})
	e.broker.Publish(string(data))
}

// SSE handlers
func (e *Engine) handleTaskStream(w http.ResponseWriter, r *http.Request) {
	taskID := r.PathValue("id")
	if taskID == "" {
		http.Error(w, "missing task id", http.StatusBadRequest)
		return
	}

	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming unsupported", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	sub := e.broker.SubscribePayload()
	defer e.broker.UnsubscribePayload(sub)

	// Send current state
	if e.currentExec != nil && e.currentExec.TaskID == taskID {
		data, _ := json.Marshal(map[string]any{
			"type":    "state",
			"status":  e.currentExec.Status,
			"output":  e.currentExec.OutputLines,
			"verify":  e.currentExec.VerifyOut,
			"review":  e.currentExec.Review,
		})
		fmt.Fprintf(w, "data: %s\n\n", data)
		flusher.Flush()
	}

	for {
		select {
		case <-r.Context().Done():
			return
		case msg := <-sub:
			var payload map[string]any
			if json.Unmarshal([]byte(msg), &payload) == nil {
				if payload["task_id"] == taskID {
					data, _ := json.Marshal(map[string]any{
						"type":   "output",
						"lines":  payload["lines"],
					})
					fmt.Fprintf(w, "data: %s\n\n", data)
					flusher.Flush()
				}
			}
			var t Task
			if json.Unmarshal([]byte(msg), &t) == nil && t.ID == taskID {
				data, _ := json.Marshal(map[string]any{
					"type":   "update",
					"task":   t,
				})
				fmt.Fprintf(w, "data: %s\n\n", data)
				flusher.Flush()
			}
		}
	}
}

func mustHostname() string {
	h, _ := os.Hostname()
	return h
}

// loadPrompts loads all prompt templates from the prompt directory.
func loadPrompts(promptDir string) (PromptTemplates, error) {
	var pt PromptTemplates
	var err error

	pt.Planner, err = readPromptFile(promptDir, "planner.md")
	if err != nil {
		return pt, err
	}
	pt.Worker, err = readPromptFile(promptDir, "worker.md")
	if err != nil {
		return pt, err
	}
	pt.Reviewer, err = readPromptFile(promptDir, "reviewer.md")
	if err != nil {
		return pt, err
	}
	pt.PlanReviewer, err = readPromptFile(promptDir, "plan-reviewer.md")
	if err != nil {
		return pt, err
	}
	pt.TaskCompiler, err = readPromptFile(promptDir, "task-compiler.md")
	if err != nil {
		return pt, err
	}
	return pt, nil
}

func readPromptFile(dir, name string) (string, error) {
	path := filepath.Join(dir, name)
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	return string(data), nil
}