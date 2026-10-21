package engine

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"cmd184psu/unified-webapp/internal/agentflow/runner"
	"cmd184psu/unified-webapp/internal/agentflow/store"
)

// modelCheckStale is how long a failed check keeps a model out of use (H27). A
// provider that fixed itself, or a quota that came back, is tried again after
// this without anyone clearing it; a passed check never blocks anything.
const modelCheckStale = 6 * time.Hour

// modelCheckTimeout bounds one model's check.
const modelCheckTimeout = 2 * time.Minute

const checkPrompt = "Reply with exactly: ok\n"

// CheckView is the progress of a backend's model check.
type CheckView struct {
	State   string             `json:"state"` // idle or running
	Current string             `json:"current,omitempty"`
	Results []store.ModelCheck `json:"results"`
}

type checkState struct {
	mu      sync.Mutex
	running map[string]string // backend -> the model being checked
}

// CheckModels starts a check of every model of a backend, one after the
// other, in the background (H27). Each gets a one-word prompt through the same
// runner and config a stage uses, so a model that was withdrawn, a key that no
// longer works and an exhausted quota all show before a stage depends on them.
func (e *Engine) CheckModels(ctx context.Context, actor, backend string) error {
	b, err := e.deps.Store.GetBackend(ctx, backend)
	if err != nil {
		return err
	}
	if e.deps.Runner == nil {
		return errors.New("no runner is configured")
	}
	e.checks.mu.Lock()
	if _, busy := e.checks.running[backend]; busy {
		e.checks.mu.Unlock()
		return fmt.Errorf("%w: backend %s is already being checked", store.ErrConflict, backend)
	}
	if e.checks.running == nil {
		e.checks.running = map[string]string{}
	}
	e.checks.running[backend] = ""
	e.checks.mu.Unlock()
	go func() {
		bg := context.WithoutCancel(ctx)
		defer func() {
			e.checks.mu.Lock()
			delete(e.checks.running, backend)
			e.checks.mu.Unlock()
		}()
		var failed []string
		for _, m := range b.Models {
			e.checks.mu.Lock()
			e.checks.running[backend] = m.Model
			e.checks.mu.Unlock()
			c := e.checkOne(bg, b, m.Model)
			if err := e.deps.Store.SaveModelCheck(bg, c); err != nil {
				return
			}
			if !c.OK {
				failed = append(failed, m.Model+" ("+c.Class+")")
			}
		}
		_ = e.deps.Store.WithTx(bg, func(tx *sql.Tx) error {
			_, err := store.AppendEvent(tx, actor, "backend.checked", "backend", backend, map[string]any{"models": len(b.Models), "failed": failed})
			return err
		})
	}()
	return nil
}

// CheckStatus reports whether a backend's check is running and the last
// result of each of its models.
func (e *Engine) CheckStatus(ctx context.Context, backend string) (CheckView, error) {
	if _, err := e.deps.Store.GetBackend(ctx, backend); err != nil {
		return CheckView{}, err
	}
	all, err := e.deps.Store.ModelChecks(ctx)
	if err != nil {
		return CheckView{}, err
	}
	v := CheckView{State: "idle", Results: []store.ModelCheck{}}
	for _, c := range all {
		if c.Backend == backend {
			v.Results = append(v.Results, c)
		}
	}
	e.checks.mu.Lock()
	if cur, busy := e.checks.running[backend]; busy {
		v.State, v.Current = "running", cur
	}
	e.checks.mu.Unlock()
	sort.Slice(v.Results, func(i, j int) bool { return v.Results[i].Model < v.Results[j].Model })
	return v, nil
}

// checkOne sends the one-word prompt to a model and classifies what comes back.
func (e *Engine) checkOne(ctx context.Context, b store.Backend, mdl string) store.ModelCheck {
	d := e.deps
	c := store.ModelCheck{Backend: b.Name, Model: mdl}
	start := d.Clock()
	finish := func(ok bool, class, msg string) store.ModelCheck {
		c.OK, c.Class, c.Message = ok, class, clip(msg, 300)
		c.Millis = d.Clock().Sub(start).Milliseconds()
		c.Checked = d.Clock().UnixMilli()
		return c
	}
	dir := filepath.Join(d.DataDir, "checks", b.Name, strings.NewReplacer("/", "_", ":", "_").Replace(mdl))
	_ = os.RemoveAll(dir)
	work := filepath.Join(dir, "work")
	if err := os.MkdirAll(work, 0o700); err != nil {
		return finish(false, "failing", err.Error())
	}
	// the runner only works in a git repository root, so the scratch directory is one
	if out, err := exec.CommandContext(ctx, "git", "init", "-q", work).CombinedOutput(); err != nil {
		return finish(false, "failing", "git init: "+strings.TrimSpace(string(out)))
	}
	promptPath := filepath.Join(dir, "prompt.md")
	if err := os.WriteFile(promptPath, []byte(checkPrompt), 0o600); err != nil {
		return finish(false, "failing", err.Error())
	}
	be := runner.Backend{Name: b.Name, Host: b.Host, APIType: b.APIType}
	if d.FreezeConfig != nil {
		fb := FrozenBackend{Backend: be}
		for _, m := range b.Models {
			fb.Models = append(fb.Models, m.Model)
		}
		if _, err := d.FreezeConfig(fb, dir); err != nil {
			return finish(false, "failing", "freeze runner config: "+err.Error())
		}
	}
	req := runner.Request{Role: "critic", Backend: be, Model: mdl, Workdir: work, StageDir: dir, PromptPath: promptPath, Message: promptMessage}
	if d.KeyEnv != nil {
		name := d.KeyEnv(b.Name)
		key, err := d.Store.BackendKey(ctx, b.Name)
		if err != nil {
			return finish(false, "failing", err.Error())
		}
		if key == "" {
			key = os.Getenv(name)
		}
		if key != "" {
			req.Env = append(req.Env, name+"="+key)
		}
	}
	cctx, cancel := context.WithTimeout(ctx, modelCheckTimeout)
	defer cancel()
	res, err := d.Runner.Run(cctx, req)
	if err != nil {
		var re *runner.Error
		class := "failing"
		switch {
		case errors.As(err, &re) && re.Class == runner.ErrRateLimit:
			class = "rate_limited"
		case errors.As(err, &re) && re.Class == runner.ErrUnreachable:
			class = "unreachable"
		case errors.Is(err, context.DeadlineExceeded) || (errors.As(err, &re) && re.Class == runner.ErrTimeout):
			class = "timeout"
		}
		return finish(false, class, err.Error())
	}
	if strings.TrimSpace(res.Final) == "" {
		return finish(false, "empty", "the model returned no text")
	}
	return finish(true, "", "")
}

// failedChecks maps the models whose last check failed, and is still recent
// enough to count, to that check (H27).
func (e *Engine) failedChecks(ctx context.Context, now time.Time) (map[string]store.ModelCheck, error) {
	all, err := e.deps.Store.ModelChecks(ctx)
	if err != nil {
		return nil, err
	}
	out := map[string]store.ModelCheck{}
	for _, c := range all {
		if !c.OK && now.Sub(time.UnixMilli(c.Checked)) < modelCheckStale {
			out[modelKey(c.Backend, c.Model)] = c
		}
	}
	return out, nil
}
