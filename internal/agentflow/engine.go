package agentflow

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	"cmd184psu/unified-webapp/internal/platform/config"
)

// Engine is the agentflow engine: prompt templates, profiles and model calls.
// It runs nothing on its own: no worker, no lock file.
type Engine struct {
	store       *Store
	prompts     PromptTemplates
	profiles    []config.ProfileConfig
	defaultProf string
	promptDir   string
	timeoutSecs int

	projectPath string
	modelRun    func(ctx context.Context, model, prompt, ollamaHost string) (string, error)
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
		projectPath: cfg.ProjectPath,
	}

	return e, nil
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