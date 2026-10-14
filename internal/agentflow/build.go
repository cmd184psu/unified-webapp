// Package agentflow is the composition root of the agentflow module: it opens
// the database, wires the engine to its collaborators (git service, sandbox,
// opencode adapter) and serves the API and the static UI.
package agentflow

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"cmd184psu/unified-webapp/internal/agentflow/api"
	"cmd184psu/unified-webapp/internal/agentflow/engine"
	"cmd184psu/unified-webapp/internal/agentflow/gitsvc"
	"cmd184psu/unified-webapp/internal/agentflow/runner"
	"cmd184psu/unified-webapp/internal/agentflow/runner/opencode"
	"cmd184psu/unified-webapp/internal/agentflow/sandbox"
	"cmd184psu/unified-webapp/internal/agentflow/store"
	"cmd184psu/unified-webapp/internal/platform/config"
	"cmd184psu/unified-webapp/internal/platform/static"
	"cmd184psu/unified-webapp/internal/taskmaster/golane"
)

// keyEnvPrefix names the environment variables that carry backend API keys
// (BK5); the generated opencode config references them as {env:NAME}.
const keyEnvPrefix = "AGENTFLOW_KEY_"

// goModBind is where the host's Go module cache is mounted read-only inside
// the feedback sandbox. It sits outside the sandbox HOME, which is reserved.
const goModBind = "/sandbox/gomod"

// handler is the module's http.Handler. Close stops the engine and closes the
// database; the dispatcher calls it at shutdown, after the taskmaster engine
// has already been closed, so it must not depend on the host being alive.
type handler struct {
	http.Handler
	once sync.Once
	stop func() error
}

// Close implements io.Closer. It is safe to call more than once.
func (h *handler) Close() error {
	var err error
	h.once.Do(func() { err = h.stop() })
	return err
}

// Build returns a ready-to-use http.Handler for the agentflow module. host is
// the shared taskmaster engine: agentflow registers its lanes on it and owns no
// worker loop of its own. The returned handler implements io.Closer.
func Build(cfg config.AgentflowConfig, host golane.Host) (http.Handler, error) {
	return buildWith(cfg, host, nil)
}

// buildWith is the test seam: a non-nil rn replaces the opencode adapter, so
// tests never start a real agent (TM6).
func buildWith(cfg config.AgentflowConfig, host golane.Host, rn runner.Runner) (http.Handler, error) {
	if host == nil {
		return nil, errors.New("agentflow: no taskmaster engine")
	}
	if cfg.DataDir == "" {
		return nil, errors.New("agentflow: data_dir is not set")
	}
	dataDir, err := filepath.Abs(cfg.DataDir)
	if err != nil {
		return nil, fmt.Errorf("agentflow: data_dir %s: %w", cfg.DataDir, err)
	}
	if err := os.MkdirAll(dataDir, 0o750); err != nil {
		return nil, fmt.Errorf("agentflow: data_dir %s: %w", dataDir, err)
	}
	st, err := store.Open(filepath.Join(dataDir, "agentflow.db"))
	if err != nil {
		return nil, fmt.Errorf("agentflow: %w", err)
	}
	git, err := gitsvc.New(dataDir)
	if err != nil {
		st.Close()
		return nil, fmt.Errorf("agentflow: %w", err)
	}
	if rn == nil {
		rn = newOpencode(git, st)
	}
	// The engine exists before the API, so the event hooks reach it through a
	// pointer set once the API is built (D3). A notice before then is dropped.
	var events atomic.Pointer[api.API]
	eng, err := engine.New(engine.Deps{
		Store:        st,
		Host:         host,
		Runner:       rn,
		DataDir:      dataDir,
		FreezeConfig: freezeConfig,
		Git:          git,
		Sandbox:      feedbackSandbox(),
		PublishRun: func(ev engine.RunEvent) {
			if a := events.Load(); a != nil {
				a.PublishRun(ev)
			}
		},
		Kick: func() {
			if a := events.Load(); a != nil {
				a.Notify()
			}
		},
		// Prompt, Verify and Merge stay nil: the engine's defaults already
		// delegate effort stages to EffortPrompt, append the worker context
		// and the reviewer's evidence, and run the built-in verify and merge
		// executors. Kick only adds the event stream notice to the engine's
		// own dispatcher wake-up.
	})
	if err != nil {
		st.Close()
		return nil, fmt.Errorf("agentflow: %w", err)
	}
	// Projects boot stopped (RC1): Start reconciles, lifts the lane hold and
	// runs the engine's one tick goroutine, but dispatches nothing until a
	// project is started through the API.
	if err := eng.Start(context.Background()); err != nil {
		eng.Close()
		st.Close()
		return nil, fmt.Errorf("agentflow: start engine: %w", err)
	}

	mux := http.NewServeMux()
	a := api.New(api.Deps{
		Store:             st,
		Engine:            eng,
		Git:               git,
		Version:           rn.Version,
		SSEMaxSubscribers: cfg.SSEMaxSubscribers,
	})
	a.Register(mux)
	events.Store(a)
	mux.Handle("/", static.NewHandler(cfg.StaticDir))

	log.Printf("agentflow: data %s, static %s", dataDir, cfg.StaticDir)
	return &handler{Handler: mux, stop: func() error {
		eng.Close()
		return st.Close()
	}}, nil
}

// freezeConfig renders the stage's opencode configuration (BK5, OC2). The
// engine cannot import the adapter, so the composition root hands it this.
func freezeConfig(b engine.FrozenBackend, stageDir string) (string, error) {
	return opencode.GenerateConfig([]opencode.BackendSpec{{Backend: b.Backend, Models: b.Models}}, stageDir)
}

// newOpencode builds the production runner: the real opencode binary in a
// bwrap sandbox, with only the AGENTFLOW_KEY_* variables forwarded.
func newOpencode(git *gitsvc.Service, st *store.Store) *opencode.Adapter {
	env := os.Environ()
	var keys []string
	for _, kv := range env {
		if k, _, _ := strings.Cut(kv, "="); strings.HasPrefix(k, keyEnvPrefix) {
			keys = append(keys, k)
		}
	}
	return &opencode.Adapter{
		Path:      opencodePath(),
		Command:   opencode.SandboxCommand(gitDirs(git, st)),
		ParentEnv: env,
		KeyEnvs:   keys,
	}
}

// opencodePath finds the real opencode binary (spike S0): ~/.opencode/bin, then
// PATH. The HOME-relative shim under ~/.agents fails once HOME is replaced, so
// it is never used. With no binary found it returns the default install path,
// so the version probe reports exactly where it looked.
func opencodePath() string {
	home, _ := os.UserHomeDir()
	def := filepath.Join(home, ".opencode", "bin", "opencode")
	if isExecutable(def) {
		return def
	}
	if p, err := exec.LookPath("opencode"); err == nil {
		if r, err := filepath.EvalSymlinks(p); err == nil {
			p = r
		}
		if !strings.Contains(p, string(filepath.Separator)+".agents"+string(filepath.Separator)) {
			return p
		}
	}
	return def
}

func isExecutable(p string) bool {
	fi, err := os.Stat(p)
	return err == nil && fi.Mode().IsRegular() && fi.Mode().Perm()&0o111 != 0
}

// gitDirs resolves the trusted git directories for a runner request. A task
// worktree lives under <data>/worktrees/<project>/...: its admin dir comes from
// gitsvc.AdminGitDir, never from the worktree's own .git. A plan stage runs in
// the project repo itself; both dirs are then its .git and the sandbox binds
// the repo read-only.
func gitDirs(git *gitsvc.Service, st *store.Store) opencode.GitDirs {
	return func(req runner.Request) (string, string, error) {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		wd, err := filepath.EvalSymlinks(req.Workdir)
		if err != nil {
			return "", "", fmt.Errorf("agentflow: workdir: %w", err)
		}
		if rel, err := filepath.Rel(git.WorktreeRoot(), wd); err == nil && rel != "." && !strings.HasPrefix(rel, "..") {
			slug, _, _ := strings.Cut(rel, string(filepath.Separator))
			p, err := st.GetProject(ctx, slug)
			if err != nil {
				return "", "", err
			}
			admin, err := git.AdminGitDir(ctx, p.RepoPath, wd)
			if err != nil {
				return "", "", err
			}
			return filepath.Dir(filepath.Dir(admin)), admin, nil
		}
		gd := filepath.Join(wd, ".git")
		if fi, err := os.Stat(gd); err != nil || !fi.IsDir() {
			return "", "", fmt.Errorf("agentflow: %s is not a git repository root", wd)
		}
		return gd, gd, nil
	}
}

// feedbackSandbox is the sandbox template for verify and baseline commands:
// the host's Go module cache bound read-only, GOPROXY=off so nothing is
// fetched, and the Go toolchain's directory on PATH when it lives under /usr.
func feedbackSandbox() sandbox.Spec {
	var spec sandbox.Spec
	if mc := goModCache(); mc != "" {
		spec.ExtraROBinds = []sandbox.Bind{{Src: mc, Dst: goModBind}}
		spec.Env = map[string]string{"GOMODCACHE": goModBind, "GOPROXY": "off"}
	}
	if p, err := exec.LookPath("go"); err == nil {
		if dir := filepath.Dir(p); strings.HasPrefix(dir, "/usr/") {
			spec.PathDirs = []string{dir}
		}
	}
	return spec
}

// goModCache returns the host's module cache directory, or "" if it has none.
func goModCache() string {
	dir := os.Getenv("GOMODCACHE")
	if dir == "" {
		gopath := os.Getenv("GOPATH")
		if gopath == "" {
			home, _ := os.UserHomeDir()
			gopath = filepath.Join(home, "go")
		}
		dir = filepath.Join(filepath.SplitList(gopath)[0], "pkg", "mod")
	}
	if fi, err := os.Stat(dir); err != nil || !fi.IsDir() {
		return ""
	}
	return dir
}
