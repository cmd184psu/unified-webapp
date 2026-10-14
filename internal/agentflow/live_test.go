package agentflow

import (
	"context"
	"encoding/json"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"cmd184psu/unified-webapp/internal/agentflow/model"
	"cmd184psu/unified-webapp/internal/agentflow/runner"
	"cmd184psu/unified-webapp/internal/agentflow/store"
	"cmd184psu/unified-webapp/internal/platform/config"
	"cmd184psu/unified-webapp/internal/taskmaster/worker"
)

// P2: settings and backend keys take effect on save, with no rebuild of the
// engine or the runner.

// deadlineRunner records the time left on the context of every call.
type deadlineRunner struct {
	runner.Runner
	mu    sync.Mutex
	left  []time.Duration
	roles []string
}

func (d *deadlineRunner) Run(ctx context.Context, req runner.Request) (runner.Result, error) {
	d.mu.Lock()
	if dl, ok := ctx.Deadline(); ok {
		d.left = append(d.left, time.Until(dl))
	} else {
		d.left = append(d.left, 0)
	}
	d.roles = append(d.roles, req.Role)
	d.mu.Unlock()
	return d.Runner.Run(ctx, req)
}

func (d *deadlineRunner) lastLeft() time.Duration {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.left[len(d.left)-1]
}

// liveProject builds an e2e harness with a gpu backend, candidates for every
// role, one project and an approved FRD; the planning loop can then be run.
func liveProject(t *testing.T, rn runner.Runner, backend string) (*e2e, string, int64, string) {
	t.Helper()
	worker.SetPollIntervalForTest(20 * time.Millisecond)
	t.Cleanup(func() { worker.SetPollIntervalForTest(5 * time.Second) })
	repo := t.TempDir()
	e2eGit(t, repo, "init", "-q", "-b", "main")
	e2eWrite(t, filepath.Join(repo, "README"), "readme\n")
	e2eGit(t, repo, "add", "-A")
	e2eGit(t, repo, "commit", "-q", "-m", "initial")
	cfg := config.AgentflowConfig{StaticDir: t.TempDir(), DataDir: filepath.Join(t.TempDir(), "af")}
	e := newE2E(t, cfg, rn)
	e.must("POST", "/api/backends", backend, 201)
	for role, m := range map[string]string{"planner": "m1", "architect": "m2", "critic": "m3", "compiler": "m1", "worker": "m1", "reviewer": "m2"} {
		e.must("PUT", "/api/candidates/"+role, `{"candidates":[{"backend":"gpu","model":"`+m+`"}]}`, 200)
	}
	e.must("POST", "/api/projects", `{"slug":"demo","repo_path":`+strconv.Quote(repo)+`,"model_scope":"any"}`, 201)
	var ef struct{ ID int64 }
	if err := json.Unmarshal(e.must("POST", "/api/projects/demo/efforts", `{"title":"live"}`, 201), &ef); err != nil {
		t.Fatal(err)
	}
	base := "/api/projects/demo/efforts/" + strconv.FormatInt(ef.ID, 10)
	e.must("PUT", base+"/frd", `{"id":"FRD-L","title":"t","objective":"o","requirements":[{"id":"R1","description":"d"}]}`, 200)
	e.must("POST", base+"/frd/approve", ``, 200)
	return e, base, ef.ID, cfg.DataDir
}

func scriptPlanRounds(fake *runner.Fake, rounds int) {
	for i := 0; i < rounds; i++ {
		fake.Script("planner", runner.Step{Reply: e2ePlan})
		fake.Script("architect", runner.Step{Reply: e2eApprove})
		fake.Script("critic", runner.Step{Reply: e2eApprove})
	}
}

const gpuBackend = `{"name":"gpu","host":"http://127.0.0.1:11434","api_type":"ollama","slots":1,"models":[{"model":"m1"},{"model":"m2"},{"model":"m3"}]}`

func TestSettingsLive_LimitAppliesToNextStage(t *testing.T) {
	fake := runner.NewFake()
	scriptPlanRounds(fake, 2)
	rn := &deadlineRunner{Runner: fake}
	e, base, id, _ := liveProject(t, rn, gpuBackend)

	e.must("POST", base+"/plan/generate", ``, 202)
	e.waitEffort("demo", id, model.EffortPlanReady)
	if got := rn.lastLeft(); got < 25*time.Minute || got > 30*time.Minute {
		t.Fatalf("default plan_timeout: deadline in %v, want about 30m", got)
	}
	e.must("PUT", "/api/settings", `{"plan_timeout":"7m"}`, 200)
	e.must("POST", base+"/plan/generate", ``, 202)
	deadline := time.Now().Add(30 * time.Second)
	for len(fake.CallsFor("planner")) < 2 || e.effortStatus("demo", id) != model.EffortPlanReady {
		if time.Now().After(deadline) {
			t.Fatal("second plan never finished")
		}
		time.Sleep(25 * time.Millisecond)
	}
	if got := rn.lastLeft(); got < 6*time.Minute || got > 7*time.Minute {
		t.Fatalf("after saving plan_timeout=7m: deadline in %v, want about 7m", got)
	}
}

// stubOpencode writes an executable that answers --version with ver.
func stubOpencode(t *testing.T, ver string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "opencode")
	if err := os.WriteFile(p, []byte("#!/bin/sh\necho \"opencode "+ver+"\"\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestSettingsLive_OpencodePathReprobesWithoutRestart(t *testing.T) {
	cfg := config.AgentflowConfig{StaticDir: t.TempDir(), DataDir: filepath.Join(t.TempDir(), "af")}
	e := newE2E(t, cfg, nil) // the production adapter, not a fake
	version := func() (string, string) {
		var h struct {
			Status   string
			Opencode struct{ Status, Version string }
		}
		if err := json.Unmarshal(e.must("GET", "/api/health", "", 200), &h); err != nil {
			t.Fatal(err)
		}
		return h.Status, h.Opencode.Version
	}
	first, second := stubOpencode(t, "1.1.1"), stubOpencode(t, "2.2.2")
	e.must("PUT", "/api/settings", `{"opencode_path":`+strconv.Quote(first)+`}`, 200)
	if st, v := version(); st != "ok" || v != "1.1.1" {
		t.Fatalf("health after first path = %s %q", st, v)
	}
	e.must("PUT", "/api/settings", `{"opencode_path":`+strconv.Quote(second)+`}`, 200)
	if st, v := version(); st != "ok" || v != "2.2.2" {
		t.Fatalf("health after second path = %s %q", st, v)
	}
	e.must("PUT", "/api/settings", `{"opencode_path":`+strconv.Quote(filepath.Join(t.TempDir(), "gone"))+`}`, 200)
	if st, _ := version(); st != "degraded" {
		t.Fatalf("health with a missing binary = %s, want degraded", st)
	}
}

func TestBackendKey_DatabaseThenEnvAtStageTime_NeverLeaks(t *testing.T) {
	const (
		dbKey   = "sk-db-GOLDEN-1111"
		dbKey2  = "sk-db-GOLDEN-2222"
		envKey  = "sk-env-GOLDEN-3333"
		keyName = "AGENTFLOW_KEY_GPU"
	)
	fake := runner.NewFake()
	scriptPlanRounds(fake, 3)
	backend := `{"name":"gpu","host":"http://127.0.0.1:11434","api_type":"openai_compatible","hosted":true,"slots":1,"models":[{"model":"m1"},{"model":"m2"},{"model":"m3"}],"api_key":"` + dbKey + `"}`
	e, base, id, dataDir := liveProject(t, fake, backend)

	var bodies [][]byte
	keep := func(b []byte) []byte { bodies = append(bodies, b); return b }
	keyOf := func(c runner.Call) string {
		var got []string
		for _, kv := range c.Request.Env {
			if strings.HasPrefix(kv, keyName+"=") {
				got = append(got, strings.TrimPrefix(kv, keyName+"="))
			}
		}
		if len(got) > 1 {
			t.Fatalf("key variable passed %d times", len(got))
		}
		if len(got) == 0 {
			return ""
		}
		return got[0]
	}
	plan := func(want string, calls int) {
		t.Helper()
		e.must("POST", base+"/plan/generate", ``, 202)
		deadline := time.Now().Add(30 * time.Second)
		for len(fake.CallsFor("critic")) < calls || e.effortStatus("demo", id) != model.EffortPlanReady {
			if time.Now().After(deadline) {
				t.Fatal("plan never finished")
			}
			time.Sleep(25 * time.Millisecond)
		}
		for _, c := range fake.Calls()[len(fake.Calls())-3:] {
			if got := keyOf(c); got != want {
				t.Fatalf("%s stage got key %q, want %q", c.Request.Role, got, want)
			}
		}
	}

	plan(dbKey, 1)
	var bv struct {
		Backend struct {
			HasKey bool `json:"has_key"`
		}
	}
	if err := json.Unmarshal(keep(e.must("PATCH", "/api/backends/gpu", `{"api_key":"`+dbKey2+`"}`, 200)), &bv); err != nil || !bv.Backend.HasKey {
		t.Fatalf("patch response has_key = %v (%v)", bv.Backend.HasKey, err)
	}
	plan(dbKey2, 2) // the next stage, no restart

	// A cleared key falls back to the environment, read now rather than at boot.
	t.Setenv(keyName, envKey)
	if err := json.Unmarshal(keep(e.must("PATCH", "/api/backends/gpu", `{"api_key":""}`, 200)), &bv); err != nil || bv.Backend.HasKey {
		t.Fatalf("cleared key: has_key = %v (%v)", bv.Backend.HasKey, err)
	}
	plan(envKey, 3)
	// Leaving api_key out keeps whatever is stored.
	keep(e.must("PATCH", "/api/backends/gpu", `{"slots":1}`, 200))
	keep(e.must("GET", "/api/backends", "", 200))
	keep(e.must("GET", "/api/settings", "", 200))
	keep(e.must("GET", "/api/efforts/"+strconv.FormatInt(id, 10)+"/export?format=json", "", 200))
	keep(e.must("GET", "/api/efforts/"+strconv.FormatInt(id, 10)+"/export?format=md", "", 200))
	keep(e.must("GET", base+"/plan", "", 200))
	if code, out := e.call("PATCH", "/api/backends/gpu", `{"api_key":"bad\nkey"}`); code != 400 || !strings.Contains(string(out), `"field":"api_key"`) {
		t.Fatalf("multi-line key = %d %s", code, out)
	}

	secrets := []string{dbKey, dbKey2, envKey}
	for _, b := range bodies {
		for _, s := range secrets {
			if strings.Contains(string(b), s) {
				t.Fatalf("API output holds %q: %s", s, b)
			}
		}
	}
	// Every file of every stage directory (frozen config, prompts) is clean,
	// and the frozen config refers to the key as {env:NAME}.
	sawRef := false
	err := filepath.WalkDir(filepath.Join(dataDir, "stages"), func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		b, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		for _, s := range secrets {
			if strings.Contains(string(b), s) {
				t.Errorf("%s holds %q", p, s)
			}
		}
		if strings.Contains(string(b), "{env:"+keyName+"}") {
			sawRef = true
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if !sawRef {
		t.Error("no frozen config refers to the key as {env:" + keyName + "}")
	}
	for _, c := range fake.Calls() {
		for _, s := range secrets {
			if strings.Contains(c.Prompt, s) {
				t.Errorf("a prompt holds %q", s)
			}
		}
	}
	// Audit events and logs carry the has_key flag, never the value.
	if err := e.h.(io.Closer).Close(); err != nil {
		t.Fatal(err)
	}
	st, err := store.Open(filepath.Join(dataDir, "agentflow.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	evs, err := st.ListEvents(context.Background(), "", "", 100000)
	if err != nil {
		t.Fatal(err)
	}
	for _, ev := range evs {
		for _, s := range secrets {
			if strings.Contains(ev.DetailJSON, s) {
				t.Errorf("event %s holds %q", ev.Action, s)
			}
		}
	}
}
