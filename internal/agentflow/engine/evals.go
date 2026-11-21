package engine

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"sync/atomic"

	"cmd184psu/unified-webapp/internal/agentflow/model"
	"cmd184psu/unified-webapp/internal/agentflow/prompts"
	"cmd184psu/unified-webapp/internal/agentflow/runner"
	"cmd184psu/unified-webapp/internal/agentflow/schema"
	"cmd184psu/unified-webapp/internal/agentflow/store"
)

// Check reviewers (H32). A model's one-word check says it answers; this says
// whether it reviews. Each seeded-defect case (evalcases.go) is shown to the
// model twice, with the defect and with it fixed, in two layers: the
// per-task review a task goes through, and the acceptance check of a finished
// effort. The result per model is how many defects it caught and how many
// fixed variants it raised a false alarm against, which is evidence for
// choosing reviewers and for the tier ratings.

const (
	LayerTask       = "task"
	LayerAcceptance = "acceptance"
)

// Outcomes of one run.
const (
	evalCaught      = "caught"
	evalMissed      = "missed"
	evalClean       = "clean"
	evalFalseAlarm  = "false_alarm"
	evalUnparseable = "unparseable"
	evalFailed      = "failed"
)

// EvalOptions picks what a run covers; empty fields mean everything the
// backend has, the acceptance layer, every case, once.
type EvalOptions struct {
	Models  []string `json:"models"`
	Layers  []string `json:"layers"`
	Cases   []string `json:"cases"`
	Repeats int      `json:"repeats"`
}

// EvalSummary is one model's score in one layer.
type EvalSummary struct {
	Model       string `json:"model"`
	Layer       string `json:"layer"`
	Caught      int    `json:"caught"`
	Defects     int    `json:"defects"`
	FalseAlarms int    `json:"false_alarms"`
	Fixed       int    `json:"fixed"`
	Unread      int    `json:"unread"` // unparseable replies and failed calls
	Ran         int64  `json:"ran"`
}

// EvalRow is one stored run, for the detail view.
type EvalRow struct {
	Model   string `json:"model"`
	Layer   string `json:"layer"`
	Case    string `json:"case"`
	Variant string `json:"variant"`
	Run     int    `json:"run"`
	Outcome string `json:"outcome"`
	Detail  string `json:"detail,omitempty"`
	Millis  int64  `json:"millis"`
}

// EvalView is the progress and the last results of a backend's reviewer check.
type EvalView struct {
	State   string        `json:"state"` // idle or running
	Current string        `json:"current,omitempty"`
	Done    int           `json:"done"`
	Total   int           `json:"total"`
	Summary []EvalSummary `json:"summary"`
	Rows    []EvalRow     `json:"rows"`
	Cases   []EvalCaseRef `json:"cases"`
}

// EvalCaseRef names a case for the UI.
type EvalCaseRef struct {
	ID    string `json:"id"`
	Title string `json:"title"`
}

type evalProgress struct {
	current     string
	done, total int
}

type evalState struct {
	mu      sync.Mutex
	running map[string]*evalProgress // backend -> progress
}

// CheckReviewers starts the seeded-defect run for a backend in the background.
func (e *Engine) CheckReviewers(ctx context.Context, actor, backend string, opt EvalOptions) error {
	b, err := e.deps.Store.GetBackend(ctx, backend)
	if err != nil {
		return err
	}
	if e.deps.Runner == nil {
		return errors.New("no runner is configured")
	}
	models, err := evalModels(b, opt.Models)
	if err != nil {
		return err
	}
	layers := opt.Layers
	if len(layers) == 0 {
		layers = []string{LayerAcceptance}
	}
	for _, l := range layers {
		if l != LayerTask && l != LayerAcceptance {
			return store.Invalid("layers", fmt.Sprintf("unknown layer %q: use %q or %q", l, LayerTask, LayerAcceptance))
		}
	}
	cases, err := evalPick(opt.Cases)
	if err != nil {
		return err
	}
	repeats := max(opt.Repeats, 1)
	if repeats > 5 {
		return store.Invalid("repeats", "at most 5")
	}
	total := len(models) * len(layers) * len(cases) * 2 * repeats
	e.evals.mu.Lock()
	if _, busy := e.evals.running[backend]; busy {
		e.evals.mu.Unlock()
		return fmt.Errorf("%w: backend %s is already being checked for review", store.ErrConflict, backend)
	}
	if e.evals.running == nil {
		e.evals.running = map[string]*evalProgress{}
	}
	prog := &evalProgress{total: total}
	e.evals.running[backend] = prog
	e.evals.mu.Unlock()
	go func() {
		bg := context.WithoutCancel(ctx)
		defer func() {
			e.evals.mu.Lock()
			delete(e.evals.running, backend)
			e.evals.mu.Unlock()
		}()
		for _, m := range models {
			for _, layer := range layers {
				var rows []store.ReviewerEval
				for _, c := range cases {
					for _, variant := range []string{"defect", "fixed"} {
						for run := 1; run <= repeats; run++ {
							e.evals.mu.Lock()
							prog.current = fmt.Sprintf("%s: %s, %s, %s", m, layer, c.ID, variant)
							e.evals.mu.Unlock()
							r := e.evalOne(bg, b, m, layer, c, variant)
							r.Run = run
							rows = append(rows, r)
							e.evals.mu.Lock()
							prog.done++
							e.evals.mu.Unlock()
						}
					}
				}
				if err := e.deps.Store.ReplaceReviewerEvals(bg, backend, m, layer, rows); err != nil {
					return
				}
			}
		}
		_ = e.deps.Store.WithTx(bg, func(tx *sql.Tx) error {
			_, err := store.AppendEvent(tx, actor, "backend.reviewers_checked", "backend", backend, map[string]any{"models": models, "layers": layers})
			return err
		})
	}()
	return nil
}

func evalModels(b store.Backend, want []string) ([]string, error) {
	if len(want) == 0 {
		var out []string
		for _, m := range b.Models {
			out = append(out, m.Model)
		}
		return out, nil
	}
	for _, w := range want {
		found := false
		for _, m := range b.Models {
			found = found || m.Model == w
		}
		if !found {
			return nil, store.Invalid("models", fmt.Sprintf("backend %s has no model %q", b.Name, w))
		}
	}
	return want, nil
}

func evalPick(ids []string) ([]evalCase, error) {
	if len(ids) == 0 {
		return evalCases, nil
	}
	var out []evalCase
	for _, id := range ids {
		found := false
		for _, c := range evalCases {
			if c.ID == id {
				out, found = append(out, c), true
			}
		}
		if !found {
			return nil, store.Invalid("cases", fmt.Sprintf("unknown case %q", id))
		}
	}
	return out, nil
}

// EvalStatus reports whether a backend's reviewer check is running and the
// last stored results.
func (e *Engine) EvalStatus(ctx context.Context, backend string) (EvalView, error) {
	if _, err := e.deps.Store.GetBackend(ctx, backend); err != nil {
		return EvalView{}, err
	}
	rows, err := e.deps.Store.ReviewerEvals(ctx, backend)
	if err != nil {
		return EvalView{}, err
	}
	v := EvalView{State: "idle", Summary: []EvalSummary{}, Rows: []EvalRow{}, Cases: []EvalCaseRef{}}
	for _, c := range evalCases {
		v.Cases = append(v.Cases, EvalCaseRef{ID: c.ID, Title: c.Title})
	}
	sums := map[string]*EvalSummary{}
	for _, r := range rows {
		v.Rows = append(v.Rows, EvalRow{Model: r.Model, Layer: r.Layer, Case: r.Case, Variant: r.Variant, Run: r.Run, Outcome: r.Outcome, Detail: r.Detail, Millis: r.Millis})
		k := r.Model + "\x00" + r.Layer
		s := sums[k]
		if s == nil {
			s = &EvalSummary{Model: r.Model, Layer: r.Layer}
			sums[k] = s
		}
		s.Ran = max(s.Ran, r.Ran)
		switch r.Outcome {
		case evalCaught:
			s.Caught++
			s.Defects++
		case evalMissed:
			s.Defects++
		case evalClean:
			s.Fixed++
		case evalFalseAlarm:
			s.Fixed++
			s.FalseAlarms++
		default:
			s.Unread++
		}
	}
	for _, s := range sums {
		v.Summary = append(v.Summary, *s)
	}
	sort.Slice(v.Summary, func(i, j int) bool {
		if v.Summary[i].Model != v.Summary[j].Model {
			return v.Summary[i].Model < v.Summary[j].Model
		}
		return v.Summary[i].Layer < v.Summary[j].Layer
	})
	e.evals.mu.Lock()
	if p, busy := e.evals.running[backend]; busy {
		v.State, v.Current, v.Done, v.Total = "running", p.current, p.done, p.total
	}
	e.evals.mu.Unlock()
	return v, nil
}

// evalOne runs one case variant through one model and scores the reply.
func (e *Engine) evalOne(ctx context.Context, b store.Backend, mdl, layer string, c evalCase, variant string) store.ReviewerEval {
	d := e.deps
	start := d.Clock()
	r := store.ReviewerEval{Backend: b.Name, Model: mdl, Layer: layer, Case: c.ID, Variant: variant}
	done := func(outcome, detail string) store.ReviewerEval {
		r.Outcome, r.Detail = outcome, clip(detail, 400)
		r.Millis, r.Ran = d.Clock().Sub(start).Milliseconds(), d.Clock().UnixMilli()
		return r
	}
	files := c.Defect
	if variant == "fixed" {
		files = c.Fixed
	}
	prompt, err := evalPrompt(layer, c, files)
	if err != nil {
		return done(evalFailed, err.Error())
	}
	final, err := e.evalCall(ctx, b, mdl, files, prompt)
	if err != nil {
		return done(evalFailed, err.Error())
	}
	raw, err := schema.ExtractFinal(final)
	if err != nil {
		return done(evalUnparseable, err.Error())
	}
	flagged, caught, detail, err := scoreEval(layer, c.Target, raw)
	if err != nil {
		return done(evalUnparseable, err.Error())
	}
	switch {
	case variant == "defect" && caught:
		return done(evalCaught, detail)
	case variant == "defect":
		return done(evalMissed, detail)
	case flagged:
		return done(evalFalseAlarm, detail)
	}
	return done(evalClean, detail)
}

// scoreEval reads a reviewer's reply. flagged is whether it reported any
// problem at all; caught is whether it reported one against the target
// requirement (acceptance) or asked for a repair (task).
func scoreEval(layer, target string, raw json.RawMessage) (flagged, caught bool, detail string, err error) {
	if layer == LayerAcceptance {
		out, err := schema.Parse[schema.AcceptanceOut](raw)
		if err != nil {
			return false, false, "", err
		}
		var ids []string
		for _, g := range out.Gaps {
			ids = append(ids, g.Requirement)
			// A gap against the target, or against its acceptance criterion
			// (AC1), is the planted one; only a gap on the met requirement
			// (R2) is not.
			req := strings.ToUpper(g.Requirement)
			caught = caught || strings.Contains(req, target) || (strings.Contains(req, "AC") && !strings.Contains(req, "R2"))
		}
		detail = "verdict " + out.Verdict
		if len(out.Gaps) > 0 {
			detail += "; gaps " + strings.Join(ids, ",") + ": " + out.Gaps[0].Problem
		}
		return len(out.Gaps) > 0, caught, detail, nil
	}
	out, err := schema.Parse[schema.ReviewOut](raw)
	if err != nil {
		return false, false, "", err
	}
	if out.Blocked != "" {
		return false, false, "", errors.New("blocked: " + out.Blocked)
	}
	detail = "verdict " + out.Verdict
	if len(out.Issues) > 0 {
		detail += ": " + out.Issues[0].Description
	}
	repair := out.Verdict == "repair" && !onlyMinorIssues(out.Issues)
	return repair, repair, detail, nil
}

// evalPrompt builds what the model is shown, in the shape of the real stage's
// prompt: the acceptance check reads the files itself; the task review is
// handed the task, the requirement wording, the verify report and the files.
func evalPrompt(layer string, c evalCase, files []evalFile) (string, error) {
	if layer == LayerAcceptance {
		text, err := prompts.Load(acceptanceRef)
		if err != nil {
			return "", err
		}
		return strings.ReplaceAll(text, "{{FRD_JSON}}", c.FRD), nil
	}
	text, err := prompts.Load("reviewer")
	if err != nil {
		return "", err
	}
	var frd model.FRD
	if err := json.Unmarshal([]byte(c.FRD), &frd); err != nil {
		return "", err
	}
	taskJSON, _ := json.Marshal(map[string]any{"key": "T1", "title": c.TaskTitle, "prompt": c.TaskPrompt, "acceptance": c.Acceptance})
	var b strings.Builder
	b.WriteString(strings.ReplaceAll(text, "{{TASK_JSON}}", string(taskJSON)))
	fmt.Fprintf(&b, "\n\n## Task: %s\n\n%s\n\n## Acceptance criteria\n", c.TaskTitle, c.TaskPrompt)
	for _, a := range c.Acceptance {
		b.WriteString("- " + a + "\n")
	}
	b.WriteString("\n## The approved requirements behind these criteria\n\nThis is the person's wording. Where it names an exact path, port, status code or message, the work must use exactly that.\n")
	for _, r := range frd.Requirements {
		b.WriteString("- " + r.ID + ": " + r.Description + "\n")
	}
	b.WriteString("\n## Verify report (engine evidence)\n\n```json\n{\"checks\":[{\"cmd\":\"go build ./...\",\"ok\":true},{\"cmd\":\"go vet ./...\",\"ok\":true}],\"new_failures\":[]}\n```\n")
	b.WriteString("\n\n## FILES (read from the worktree after the worker finished)\n")
	for _, f := range evalTouched(c, files) {
		fmt.Fprintf(&b, "\n### %s\n\n```\n%s```\n", f.Path, f.Content)
	}
	return b.String(), nil
}

// evalTouched puts the files the defect lives in first.
func evalTouched(c evalCase, files []evalFile) []evalFile {
	differs := map[string]bool{}
	for i := range c.Defect {
		if c.Defect[i].Content != c.Fixed[i].Content {
			differs[c.Defect[i].Path] = true
		}
	}
	out := make([]evalFile, 0, len(files))
	for _, f := range files {
		if differs[f.Path] {
			out = append(out, f)
		}
	}
	for _, f := range files {
		if !differs[f.Path] && f.Path != "go.mod" {
			out = append(out, f)
		}
	}
	return out
}

// evalCall runs the reviewer role on a model over a scratch repository holding
// files and returns its final message.
func (e *Engine) evalCall(ctx context.Context, b store.Backend, mdl string, files []evalFile, prompt string) (string, error) {
	d := e.deps
	dir := filepath.Join(d.DataDir, "evals", b.Name, strings.NewReplacer("/", "_", ":", "_").Replace(mdl))
	_ = os.RemoveAll(dir)
	work := filepath.Join(dir, "work")
	if err := os.MkdirAll(work, 0o700); err != nil {
		return "", err
	}
	for _, f := range files {
		if err := os.WriteFile(filepath.Join(work, f.Path), []byte(f.Content), 0o600); err != nil {
			return "", err
		}
	}
	for _, args := range [][]string{{"init", "-q"}, {"add", "-A"}, {"-c", "user.name=agentflow", "-c", "user.email=agentflow@localhost", "commit", "-q", "-m", "case"}} {
		if out, err := exec.CommandContext(ctx, "git", append([]string{"-C", work}, args...)...).CombinedOutput(); err != nil {
			return "", fmt.Errorf("git %s: %s", args[0], strings.TrimSpace(string(out)))
		}
	}
	promptPath := filepath.Join(dir, "prompt.md")
	if err := os.WriteFile(promptPath, []byte(prompt), 0o600); err != nil {
		return "", err
	}
	be := runner.Backend{Name: b.Name, Host: b.Host, APIType: b.APIType, CLIPath: b.CLIPath}
	if d.FreezeConfig != nil {
		fb := FrozenBackend{Backend: be}
		for _, m := range b.Models {
			fb.Models = append(fb.Models, m.Model)
		}
		if _, err := d.FreezeConfig(fb, dir); err != nil {
			return "", fmt.Errorf("freeze runner config: %w", err)
		}
	}
	req := runner.Request{Role: "reviewer", Backend: be, Model: mdl, Workdir: work, StageDir: dir, PromptPath: promptPath, Message: promptMessage}
	if d.KeyEnv != nil {
		name := d.KeyEnv(b.Name)
		key, err := d.Store.BackendKey(ctx, b.Name)
		if err != nil {
			return "", err
		}
		if key == "" {
			key = os.Getenv(name)
		}
		if key != "" {
			req.Env = append(req.Env, name+"="+key)
		}
	}
	set, err := d.Store.GetSettings(ctx)
	if err != nil {
		return "", err
	}
	// A model that goes silent is cut off as a stage's would be (H30), so one
	// hung call does not hold up the rest of the run.
	var lastSeen atomic.Int64
	lastSeen.Store(d.Clock().UnixNano())
	req.OnEvent = func(runner.Event) { lastSeen.Store(d.Clock().UnixNano()) }
	cctx, cancel := context.WithTimeout(ctx, set.ReviewTimeout)
	defer cancel()
	res, err := e.runWatched(cctx, req, set.StallMinutes, &lastSeen)
	if err != nil {
		return "", err
	}
	return res.Final, nil
}
