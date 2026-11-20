// Command agentflow-smoke drives a running agentflow instance through its seat, notify and
// classic paths with stand-ins only: no model, no tokens. scripts/agentflow-smoke.sh runs it.
// Each scenario prints "PASS name" or "FAIL name: why"; the exit status is 1 when any failed.
//
// It touches only what it creates: projects, backends and seat profiles named smoke-<ts>-*,
// tmux sessions named af-smoke-<ts>-*, and throwaway repos under a fresh temp dir. The
// notify_command setting is wrapped for the run (the previous command still runs) and put
// back. The drain restart scenario pauses every running project on the instance until the
// restart, so it runs by default only against dev (port 8788); -restart turns it on elsewhere.
package main

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"cmd184psu/unified-webapp/internal/agentflow/model"

	_ "modernc.org/sqlite"
)

const notifyText = "Agentflow needs attention"

type smoke struct {
	base    string
	db      string
	ts      string
	tmp     string
	repoDir string
	client  *http.Client

	backends []string
	projects []string
	profiles []string
	sessions []string // owner stand-in sessions this run started
	stub     *exec.Cmd

	notifyWrapped bool
	oldNotify     string

	mu      sync.Mutex
	results []result
	efforts map[string]int64
}

type result struct {
	name string
	err  error
	skip string
}

func main() {
	urlFlag := flag.String("url", "http://127.0.0.1:8788", "the agentflow instance")
	dbFlag := flag.String("db", "", "the instance's agentflow.db, read only for the restart scenario's adoption event (default: dev's, for port 8788)")
	restartFlag := flag.String("restart", "auto", "run the drain-restart scenario: yes, no, auto (yes on 8788 only)")
	opencodeStub := flag.String("stub", "", "the opencode stub server binary (scripts/agentflow-smoke.sh builds it)")
	keep := flag.Bool("keep", false, "leave the projects, backends and temp dir for inspection")
	flag.Parse()

	u, err := url.Parse(*urlFlag)
	if err != nil {
		fatal("bad -url: %v", err)
	}
	restart := *restartFlag == "yes" || (*restartFlag == "auto" && u.Port() == "8788")
	db := *dbFlag
	if db == "" && u.Port() == "8788" {
		home, _ := os.UserHomeDir()
		db = filepath.Join(home, "work", "af-dev", "data", "agentflow", "agentflow.db")
	}
	if restart && db == "" {
		fatal("-db is required for the restart scenario on an instance other than dev (8788)")
	}

	s := &smoke{base: strings.TrimRight(*urlFlag, "/"), db: db, ts: time.Now().Format("150405"),
		client: &http.Client{Timeout: 30 * time.Second}}
	if s.tmp, err = os.MkdirTemp("/tmp", "af-smoke-"); err != nil { // short: tmux and sandboxes like short paths
		fatal("temp dir: %v", err)
	}
	fmt.Printf("agentflow-smoke %s against %s (run %s, temp %s)\n", runtime.GOOS, s.base, s.ts, s.tmp)
	start := time.Now()
	if err := s.get("/api/health", nil); err != nil {
		fatal("instance not reachable: %v", err)
	}
	if err := s.setup(*opencodeStub); err != nil {
		s.record("setup", err)
	} else {
		s.runAll(restart)
	}
	failed := s.report(start)
	if !*keep {
		s.cleanup()
	} else {
		fmt.Println("kept: projects, backends and", s.tmp)
	}
	if failed > 0 {
		os.Exit(1)
	}
}

func fatal(format string, a ...any) {
	fmt.Fprintf(os.Stderr, "agentflow-smoke: "+format+"\n", a...)
	os.Exit(2)
}

func (s *smoke) name(suffix string) string { return "smoke-" + s.ts + "-" + suffix }

func (s *smoke) record(name string, err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.results = append(s.results, result{name: name, err: err})
	if err != nil {
		fmt.Printf("FAIL %s: %v\n", name, err)
	} else {
		fmt.Printf("PASS %s\n", name)
	}
}

func (s *smoke) skip(name, why string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.results = append(s.results, result{name: name, skip: why})
	fmt.Printf("SKIP %s: %s\n", name, why)
}

func (s *smoke) report(start time.Time) int {
	failed := 0
	for _, r := range s.results {
		if r.err != nil {
			failed++
		}
	}
	fmt.Printf("agentflow-smoke: %d scenario(s), %d failed, %s\n", len(s.results), failed, time.Since(start).Round(time.Second))
	return failed
}

// --- HTTP ---

func (s *smoke) do(method, path string, body, out any) error {
	var rd io.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		rd = bytes.NewReader(b)
	}
	req, err := http.NewRequest(method, s.base+path, rd)
	if err != nil {
		return err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := s.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 300 {
		return fmt.Errorf("%s %s: %d %s", method, path, resp.StatusCode, strings.TrimSpace(string(b)))
	}
	if out != nil && len(b) > 0 {
		return json.Unmarshal(b, out)
	}
	return nil
}

func (s *smoke) get(path string, out any) error { return s.do("GET", path, nil, out) }

// --- setup ---

const standinSeat = `#!/bin/sh
# agentflow smoke stand-in seat (runs in the stage sandbox: HOME is the stage home).
task="$HOME/TASK.md"
mode=$(sed -n 's/.*SMOKE-MODE: \([a-z]*\).*/\1/p' "$task" | head -1)
file=$(sed -n 's/.*SMOKE-FILE: \([A-Za-z0-9._-]*\).*/\1/p' "$task" | head -1)
res="$HOME/RESULT.json"
if [ "$mode" = halt ] && [ "$SMOKE_ROLE" = worker ]; then
  echo "SMOKE-HALT-TEXT: the owner must act here"
  while read -r l; do echo "GOT-INPUT: $l"; done
  sleep 600
fi
echo "Enter a coding task"
read -r line
echo "got the task line"
if [ "$SMOKE_ROLE" = review ]; then
  printf '{"status":"done","verdict":"pass","summary":"smoke review","issues":[]}' > "$res"
  sleep 600
fi
case "$mode" in
question)
  printf '{"status":"question","question":"SMOKE-QUESTION go on?"}' > "$res"
  a=""; while [ -z "$a" ]; do read -r a; done
  echo "answer: $a" ;;
wait)
  echo "WAITING-FOR-GO"
  a=""; while [ "$a" != go ]; do read -r a; done
  echo "got go" ;;
silent)
  echo "SILENT-NOW"
  sleep 3600 ;;
esac
[ -n "$file" ] && printf 'smoke %s\n' "$mode" > "$file"
printf '{"status":"done","summary":"smoke %s","files_changed":["%s"]}' "$mode" "$file" > "$res"
sleep 600
`

const ownerSeat = `#!/bin/sh
# agentflow smoke stand-in for an owner-run queue seat (unsandboxed, like the owner's own).
echo "smoke owner seat idle"
while read -r line; do
  [ -z "$line" ] && continue
  case "$line" in
  "check your queue"*)
    wt=$(sed -n 's/^Work only in the directory ` + "`" + `\([^` + "`" + `]*\)` + "`" + `.*/\1/p' "$QUEUE_FILE" | head -1)
    res=$(sed -n 's/.*write the file ` + "`" + `\(.*RESULT\.json\)` + "`" + `.*/\1/p' "$QUEUE_FILE" | head -1)
    if [ -z "$res" ] || [ -z "$wt" ]; then echo "nothing queued"; continue; fi
    echo "working in $wt"
    printf 'smoke queue\n' > "$wt/queue.txt"
    printf '{"status":"done","summary":"smoke queue","files_changed":["queue.txt"]}' > "$res"
    echo "reported" ;;
  esac
done
`

type scenarioProject struct {
	key    string // the project suffix
	mode   string // SMOKE-MODE for the stand-in seat
	file   string // the file the worker writes
	worker [2]string
	limits map[string]string
	slug   string
	effort int64
	repo   string
}

func (s *smoke) setup(stubBin string) error {
	write := func(name, text string, mode os.FileMode) (string, error) {
		p := filepath.Join(s.tmp, name)
		return p, os.WriteFile(p, []byte(text), mode)
	}
	seatScript, err := write("standin-seat.sh", standinSeat, 0o755)
	if err != nil {
		return err
	}
	if _, err := write("owner-seat.sh", ownerSeat, 0o755); err != nil {
		return err
	}
	s.repoDir = filepath.Join(s.tmp, "repos")
	if err := os.MkdirAll(s.repoDir, 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(s.tmp, "queue.md"), []byte("# smoke owner queue\n"), 0o644); err != nil {
		return err
	}

	// Seat profiles (merged into seat_profiles, removed at cleanup).
	w, r, q := s.name("w"), s.name("r"), s.name("queue")
	halt := []string{"SMOKE-HALT-TEXT"}
	prof := func(role string) map[string]any {
		return map[string]any{"command": []string{"/bin/sh", seatScript}, "startup_wait": "20s", "ready_text": "Enter a coding task",
			"submit_keys": []string{"Enter"}, "net": false, "env": map[string]string{"TERM": "xterm-256color", "SMOKE_ROLE": role}, "halt_texts": halt}
	}
	if err := s.editProfiles(func(m map[string]json.RawMessage) {
		put := func(k string, v any) { b, _ := json.Marshal(v); m[k] = b }
		put(w, prof("worker"))
		put(r, prof("review"))
		put(q, map[string]any{"tmux_session": s.ownerSession(), "queue_file": filepath.Join(s.tmp, "queue.md"), "nudge_wait": "20s", "halt_texts": halt})
	}); err != nil {
		return fmt.Errorf("seat profiles: %w", err)
	}
	s.profiles = []string{w, r, q}

	// Backends.
	seatB := s.name("seat")
	if err := s.do("POST", "/api/backends", map[string]any{"name": seatB, "host": "", "api_type": "freebuff_seat", "slots": 8, "hosted": false, "paid": false,
		"models": []map[string]any{{"model": w, "paid": false}, {"model": r, "paid": false}}}, nil); err != nil {
		return err
	}
	s.backends = append(s.backends, seatB)
	queueB := s.name("qseat")
	if err := s.do("POST", "/api/backends", map[string]any{"name": queueB, "host": "", "api_type": "freebuff_seat", "slots": 1, "hosted": false, "paid": false,
		"models": []map[string]any{{"model": q, "paid": false}}}, nil); err != nil {
		return err
	}
	s.backends = append(s.backends, queueB)
	if stubBin != "" {
		addr, err := s.startStub(stubBin)
		if err != nil {
			return fmt.Errorf("opencode stub: %w", err)
		}
		ocB := s.name("oc")
		if err := s.do("POST", "/api/backends", map[string]any{"name": ocB, "host": "http://" + addr, "api_type": "ollama", "slots": 1, "hosted": false, "paid": false,
			"models": []map[string]any{{"model": "stub-model", "paid": false, "context_tokens": 32768}}}, nil); err != nil {
			return err
		}
		s.backends = append(s.backends, ocB)
	}
	return nil
}

func (s *smoke) ownerSession() string { return "af-smoke-" + s.ts + "-owner" }

func (s *smoke) editProfiles(fn func(map[string]json.RawMessage)) error {
	var set map[string]any
	if err := s.get("/api/settings", &set); err != nil {
		return err
	}
	m := map[string]json.RawMessage{}
	if txt, _ := set["seat_profiles"].(string); txt != "" {
		if err := json.Unmarshal([]byte(txt), &m); err != nil {
			return fmt.Errorf("the instance's seat_profiles is not a JSON object: %w", err)
		}
	}
	fn(m)
	b, _ := json.Marshal(m)
	return s.do("PUT", "/api/settings", map[string]any{"seat_profiles": string(b)}, nil)
}

func (s *smoke) startStub(bin string) (string, error) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return "", err
	}
	addr := ln.Addr().String()
	ln.Close()
	final, _ := json.Marshal(map[string]any{"summary": "smoke classic", "files_changed": []string{"classic.txt"}, "learnings": []string{}})
	script, _ := json.Marshal([]map[string]any{
		{"kind": "tool", "tool": "bash", "args": map[string]string{"command": "printf 'smoke classic\\n' > classic.txt", "description": "write classic.txt"}},
		{"kind": "text", "text": string(final)},
	})
	sp := filepath.Join(s.tmp, "stub-script.json")
	if err := os.WriteFile(sp, script, 0o644); err != nil {
		return "", err
	}
	cmd := exec.Command(bin, "-agent", "-addr", addr, "-script", sp, "-log", filepath.Join(s.tmp, "stub.log"))
	if err := cmd.Start(); err != nil {
		return "", err
	}
	s.stub = cmd
	for i := 0; i < 50; i++ {
		if c, err := net.Dial("tcp", addr); err == nil {
			c.Close()
			return addr, nil
		}
		time.Sleep(100 * time.Millisecond)
	}
	return "", errors.New("the stub never listened")
}

// newProject makes a repo, a project with stand-in candidates, an effort and its one-task queue.
func (s *smoke) newProject(p *scenarioProject, reviewer [2]string) error {
	p.slug = s.name(p.key)
	p.repo = filepath.Join(s.repoDir, p.key)
	if err := os.MkdirAll(p.repo, 0o755); err != nil {
		return err
	}
	for _, args := range [][]string{
		{"init", "-q", "-b", "main"},
		{"-c", "user.name=agentflow-smoke", "-c", "user.email=smoke@localhost", "commit", "-q", "--allow-empty", "-m", "smoke: start"},
	} {
		if out, err := exec.Command("git", append([]string{"-C", p.repo}, args...)...).CombinedOutput(); err != nil {
			return fmt.Errorf("git %v: %v %s", args, err, out)
		}
	}
	body := map[string]any{"slug": p.slug, "repo_path": p.repo, "model_scope": "any"}
	if err := s.do("POST", "/api/projects", body, nil); err != nil {
		return err
	}
	s.projects = append(s.projects, p.slug)
	if len(p.limits) > 0 {
		if err := s.do("PUT", "/api/projects/"+p.slug+"/limits", map[string]any{"limits": p.limits}, nil); err != nil {
			return err
		}
	}
	for role, c := range map[string][2]string{"worker": p.worker, "reviewer": reviewer} {
		if err := s.do("PUT", "/api/projects/"+p.slug+"/candidates/"+role, map[string]any{"candidates": []map[string]string{{"backend": c[0], "model": c[1]}}}, nil); err != nil {
			return err
		}
	}
	var ef struct {
		ID int64 `json:"id"`
	}
	if err := s.do("POST", "/api/projects/"+p.slug+"/efforts", map[string]any{"title": "smoke " + p.key}, &ef); err != nil {
		return err
	}
	p.effort = ef.ID
	s.mu.Lock()
	if s.efforts == nil {
		s.efforts = map[string]int64{}
	}
	s.efforts[p.slug] = ef.ID
	s.mu.Unlock()
	prompt := fmt.Sprintf("Create the file %s in the repository root containing one line. SMOKE-MODE: %s SMOKE-FILE: %s", p.file, p.mode, p.file)
	defs := []model.TaskDef{{Key: "T001", Phase: "P1", Title: "smoke " + p.key, Prompt: prompt,
		Acceptance: []string{p.file + " exists"}, FilesTouched: []string{p.file}}}
	return s.seed(p.slug, p.effort, defs)
}

// seed saves and approves the effort's queue through the server (smoke-* projects only).
// Never by writing the database from here: an outside writer makes the server's own
// transactions fail with SQLITE_BUSY.
func (s *smoke) seed(slug string, effort int64, defs []model.TaskDef) error {
	return s.do("POST", fmt.Sprintf("/api/projects/%s/efforts/%d/queue/seed", slug, effort), map[string]any{"tasks": defs}, nil)
}

func (s *smoke) start(p *scenarioProject) error {
	return s.do("POST", "/api/projects/"+p.slug+"/run/start", map[string]any{}, nil)
}

// --- views ---

type stageView struct {
	ID      int64  `json:"id"`
	Role    string `json:"role"`
	State   string `json:"state"`
	Outcome string `json:"outcome"`
}

type attemptView struct {
	N       int         `json:"n"`
	Outcome string      `json:"outcome"`
	Stages  []stageView `json:"stages"`
}

type taskView struct {
	Status   string        `json:"status"`
	Attempts []attemptView `json:"attempt_list"`
}

func (s *smoke) task(p *scenarioProject) (taskView, error) {
	var t taskView
	err := s.get(fmt.Sprintf("/api/projects/%s/efforts/%d/tasks/T001", p.slug, p.effort), &t)
	return t, err
}

type effortView struct {
	Status            string `json:"status"`
	IntegrationBranch string `json:"integration_branch"`
}

func (s *smoke) effort(p *scenarioProject) (effortView, error) {
	var e effortView
	err := s.get(fmt.Sprintf("/api/projects/%s/efforts/%d", p.slug, p.effort), &e)
	return e, err
}

type seatView struct {
	Stage   int64  `json:"stage"`
	Project string `json:"project"`
	Role    string `json:"role"`
	Attach  string `json:"attach"`
	Waiting *struct {
		Kind    string `json:"kind"`
		Message string `json:"message"`
		Session string `json:"session"`
	} `json:"waiting"`
}

func (s *smoke) seat(p *scenarioProject, role string) (seatView, bool) {
	var all []seatView
	if s.get("/api/seats", &all) != nil {
		return seatView{}, false
	}
	for _, v := range all {
		if v.Project == p.slug && (role == "" || v.Role == role) {
			return v, true
		}
	}
	return seatView{}, false
}

type attentionItem struct {
	Kind    string `json:"kind"`
	Project string `json:"project"`
	Stage   int64  `json:"stage"`
}

func (s *smoke) attention(p *scenarioProject, kind string) bool {
	var items []attentionItem
	if s.get("/api/attention", &items) != nil {
		return false
	}
	for _, it := range items {
		if it.Project == p.slug && it.Kind == kind {
			return true
		}
	}
	return false
}

func waitUntil(d time.Duration, what string, cond func() (bool, error)) error {
	deadline := time.Now().Add(d)
	var last error
	for time.Now().Before(deadline) {
		ok, err := cond()
		if ok {
			return nil
		}
		last = err
		time.Sleep(time.Second)
	}
	if last != nil {
		return fmt.Errorf("timed out after %s waiting for %s (last: %v)", d, what, last)
	}
	return fmt.Errorf("timed out after %s waiting for %s", d, what)
}

// complete waits for the effort to finish and the file to be on its integration branch.
func (s *smoke) complete(p *scenarioProject, d time.Duration) error {
	var ev effortView
	if err := waitUntil(d, "the effort to complete", func() (bool, error) {
		var err error
		ev, err = s.effort(p)
		if err == nil && (ev.Status == "failed" || ev.Status == "abandoned") {
			return false, fmt.Errorf("effort is %s", ev.Status)
		}
		return err == nil && ev.Status == "complete", err
	}); err != nil {
		if t, terr := s.task(p); terr == nil {
			return fmt.Errorf("%v; task %s, attempts %s", err, t.Status, summarize(t))
		}
		return err
	}
	out, err := exec.Command("git", "-C", p.repo, "show", ev.IntegrationBranch+":"+p.file).CombinedOutput()
	if err != nil {
		return fmt.Errorf("%s not on %s: %v %s", p.file, ev.IntegrationBranch, err, out)
	}
	return nil
}

func summarize(t taskView) string {
	var parts []string
	for _, a := range t.Attempts {
		var st []string
		for _, s := range a.Stages {
			st = append(st, fmt.Sprintf("%s:%s/%s", s.Role, s.State, s.Outcome))
		}
		parts = append(parts, fmt.Sprintf("#%d %s [%s]", a.N, a.Outcome, strings.Join(st, " ")))
	}
	return strings.Join(parts, "; ")
}

func (s *smoke) capture(session string) string {
	out, _ := exec.Command("tmux", "capture-pane", "-p", "-t", "="+session+":").Output()
	return string(out)
}

// events counts a stage's audit events, read from the instance's database through a
// read-only connection (a WAL reader never blocks the server's writes).
func (s *smoke) events(action string, stage int64) (int, error) {
	db, err := sql.Open("sqlite", "file:"+s.db+"?mode=ro")
	if err != nil {
		return 0, err
	}
	defer db.Close()
	var n int
	err = db.QueryRow(`SELECT COUNT(*) FROM events WHERE action = ? AND subject_type = 'stage' AND subject_id = ?`, action, strconv.FormatInt(stage, 10)).Scan(&n)
	return n, err
}

// --- scenarios ---

func (s *smoke) runAll(restart bool) {
	seatB, w, r := s.name("seat"), s.name("w"), s.name("r")
	rev := [2]string{seatB, r}
	mk := func(key, mode, file string, worker [2]string, limits map[string]string) *scenarioProject {
		return &scenarioProject{key: key, mode: mode, file: file, worker: worker, limits: limits}
	}
	seat := [2]string{seatB, w}

	// Phase A: the drain restart, alone, so it pauses nothing else of ours.
	if restart {
		p := mk("restart", "wait", "restart.txt", seat, nil)
		s.record("3 drain restart mid-stage, seat adopted, done", s.scenarioRestart(p, rev))
	} else {
		s.skip("3 drain restart mid-stage, seat adopted, done", "not dev: pass -restart yes to pause and restart this instance")
	}

	// Phase B: everything else at once. The notifier is wrapped first, so the attention items
	// this phase raises are what it announces.
	rec := filepath.Join(s.tmp, "notify.log")
	notifyErr := s.wrapNotify(rec)
	phaseStart := time.Now()
	var wg sync.WaitGroup
	run := func(name string, fn func() error) {
		wg.Add(1)
		go func() { defer wg.Done(); s.record(name, fn()) }()
	}
	flow := mk("flow", "done", "flow.txt", seat, nil)
	run("1 worker, verify, review, merge completes", func() error { return s.scenarioFlow(flow, rev) })
	question := mk("question", "question", "question.txt", seat, nil)
	run("2 question, seat_question, answer, done", func() error { return s.scenarioQuestion(question, rev) })
	cancel := mk("cancel", "silent", "cancel.txt", seat, nil)
	run("4 cancel ends the stage infra:cancel", func() error { return s.scenarioCancel(cancel, rev) })
	silent := mk("silent", "silent", "silent.txt", seat, map[string]string{"worker_timeout": "30s"})
	run("5 silence, seat_silent, no new attempt", func() error { return s.scenarioSilent(silent, rev) })
	halt := mk("halt", "halt", "halt.txt", seat, nil)
	run("7 halt text, no keys, listed as attention", func() error { return s.scenarioHalt(halt, rev) })
	queue := mk("queue", "queue", "queue.txt", [2]string{s.name("qseat"), s.name("queue")}, nil)
	run("8 queue seat nudged, reports, queue file restored", func() error { return s.scenarioQueue(queue, rev) })
	if s.stub != nil {
		classic := mk("classic", "classic", "classic.txt", [2]string{s.name("oc"), "stub-model"}, nil)
		run("9 classic headless runner (opencode on a stub model)", func() error { return s.scenarioClassic(classic, rev) })
	} else {
		s.skip("9 classic headless runner (opencode on a stub model)", "no -stub binary")
	}
	wg.Wait()
	if notifyErr != nil {
		s.record("6 notify text exact and debounced", notifyErr)
	} else {
		s.record("6 notify text exact and debounced", s.checkNotify(rec, phaseStart))
	}
}

func (s *smoke) scenarioFlow(p *scenarioProject, rev [2]string) error {
	if err := s.newProject(p, rev); err != nil {
		return err
	}
	if err := s.start(p); err != nil {
		return err
	}
	if err := s.complete(p, 4*time.Minute); err != nil {
		return err
	}
	t, err := s.task(p)
	if err != nil {
		return err
	}
	want := map[string]bool{"worker": false, "verify": false, "reviewer": false, "merge": false}
	for _, a := range t.Attempts {
		for _, st := range a.Stages {
			if _, ok := want[st.Role]; ok && st.Outcome == "pass" {
				want[st.Role] = true
			}
		}
	}
	var missing []string
	for role, ok := range want {
		if !ok {
			missing = append(missing, role)
		}
	}
	sort.Strings(missing)
	if len(missing) > 0 {
		return fmt.Errorf("no passing %v stage: %s", missing, summarize(t))
	}
	return nil
}

func (s *smoke) scenarioQuestion(p *scenarioProject, rev [2]string) error {
	if err := s.newProject(p, rev); err != nil {
		return err
	}
	if err := s.start(p); err != nil {
		return err
	}
	var sv seatView
	if err := waitUntil(3*time.Minute, "the seat's question", func() (bool, error) {
		v, ok := s.seat(p, "worker")
		sv = v
		return ok && v.Waiting != nil && v.Waiting.Kind == "seat_question" && strings.Contains(v.Waiting.Message, "SMOKE-QUESTION"), nil
	}); err != nil {
		return err
	}
	if !s.attention(p, "seat_question") {
		return errors.New("the question is not on /api/attention")
	}
	if err := s.do("POST", fmt.Sprintf("/api/seats/%d/answer", sv.Stage), map[string]string{"text": "yes, go on"}, nil); err != nil {
		return err
	}
	return s.complete(p, 4*time.Minute)
}

func (s *smoke) scenarioRestart(p *scenarioProject, rev [2]string) error {
	if err := s.newProject(p, rev); err != nil {
		return err
	}
	if err := s.start(p); err != nil {
		return err
	}
	var sv seatView
	if err := waitUntil(3*time.Minute, "the seat to wait mid-stage", func() (bool, error) {
		v, ok := s.seat(p, "worker")
		sv = v
		return ok && v.Attach != "" && strings.Contains(s.capture(strings.TrimPrefix(v.Attach, "tmux attach -t ")), "WAITING-FOR-GO"), nil
	}); err != nil {
		return err
	}
	if err := s.do("POST", "/api/service/restart", map[string]any{}, nil); err != nil {
		return err
	}
	if err := waitUntil(4*time.Minute, "stage.seat_adopted after the restart", func() (bool, error) {
		n, err := s.events("stage.seat_adopted", sv.Stage)
		return n > 0, err
	}); err != nil {
		return err
	}
	if err := waitUntil(time.Minute, "the adopted seat listed again", func() (bool, error) {
		_, ok := s.seat(p, "worker")
		return ok, nil
	}); err != nil {
		return err
	}
	if err := s.do("POST", fmt.Sprintf("/api/seats/%d/answer", sv.Stage), map[string]string{"text": "go"}, nil); err != nil {
		return err
	}
	return s.complete(p, 4*time.Minute)
}

func (s *smoke) scenarioCancel(p *scenarioProject, rev [2]string) error {
	if err := s.newProject(p, rev); err != nil {
		return err
	}
	if err := s.start(p); err != nil {
		return err
	}
	var sv seatView
	if err := waitUntil(3*time.Minute, "the silent seat at work", func() (bool, error) {
		v, ok := s.seat(p, "worker")
		sv = v
		return ok && v.Attach != "" && strings.Contains(s.capture(strings.TrimPrefix(v.Attach, "tmux attach -t ")), "SILENT-NOW"), nil
	}); err != nil {
		return err
	}
	session := strings.TrimPrefix(sv.Attach, "tmux attach -t ")
	if err := s.do("POST", fmt.Sprintf("/api/seats/%d/cancel", sv.Stage), map[string]any{}, nil); err != nil {
		return err
	}
	if err := waitUntil(time.Minute, "the stage to end infra:cancel", func() (bool, error) {
		t, err := s.task(p)
		if err != nil {
			return false, err
		}
		for _, a := range t.Attempts {
			for _, st := range a.Stages {
				if st.ID == sv.Stage && st.Outcome == "infra:cancel" {
					return true, nil
				}
			}
		}
		return false, fmt.Errorf("attempts %s", summarize(t))
	}); err != nil {
		return err
	}
	if exec.Command("tmux", "has-session", "-t", "="+session).Run() == nil {
		return fmt.Errorf("session %s still runs after the cancel", session)
	}
	return s.stopProject(p)
}

func (s *smoke) scenarioSilent(p *scenarioProject, rev [2]string) error {
	if err := s.newProject(p, rev); err != nil {
		return err
	}
	if err := s.start(p); err != nil {
		return err
	}
	if err := waitUntil(3*time.Minute, "seat_silent on /api/attention", func() (bool, error) {
		return s.attention(p, "seat_silent"), nil
	}); err != nil {
		return err
	}
	time.Sleep(10 * time.Second) // a retry, if there were one, would show by now
	t, err := s.task(p)
	if err != nil {
		return err
	}
	if len(t.Attempts) != 1 {
		return fmt.Errorf("%d attempts, want 1 (a silent seat is never retried): %s", len(t.Attempts), summarize(t))
	}
	open := false
	for _, st := range t.Attempts[0].Stages {
		if st.Role == "worker" && st.Outcome == "" {
			open = true
		}
	}
	if !open {
		return fmt.Errorf("the worker stage should still be open: %s", summarize(t))
	}
	return s.stopProject(p)
}

func (s *smoke) scenarioHalt(p *scenarioProject, rev [2]string) error {
	if err := s.newProject(p, rev); err != nil {
		return err
	}
	if err := s.start(p); err != nil {
		return err
	}
	var sv seatView
	if err := waitUntil(3*time.Minute, "the halt notice", func() (bool, error) {
		v, ok := s.seat(p, "worker")
		sv = v
		return ok && v.Waiting != nil && strings.Contains(v.Waiting.Message, "needs the owner"), nil
	}); err != nil {
		return err
	}
	if !s.attention(p, "seat_question") {
		return errors.New("the halted seat is not on /api/attention")
	}
	time.Sleep(10 * time.Second) // several poll rounds: still nothing typed
	if screen := s.capture(strings.TrimPrefix(sv.Attach, "tmux attach -t ")); strings.Contains(screen, "GOT-INPUT") {
		return fmt.Errorf("keys reached a seat showing a halt text:\n%s", screen)
	}
	if err := s.do("POST", fmt.Sprintf("/api/seats/%d/answer", sv.Stage), map[string]string{"text": "no"}, nil); err == nil {
		return errors.New("an answer was accepted while the halt text showed")
	}
	return s.stopProject(p)
}

func (s *smoke) scenarioQueue(p *scenarioProject, rev [2]string) error {
	queueFile := filepath.Join(s.tmp, "queue.md")
	before, err := os.ReadFile(queueFile)
	if err != nil {
		return err
	}
	owner := s.ownerSession()
	cmd := fmt.Sprintf("env QUEUE_FILE=%s /bin/sh %s", queueFile, filepath.Join(s.tmp, "owner-seat.sh"))
	if out, err := exec.Command("tmux", "new-session", "-d", "-s", owner, "-x", "200", "-y", "50", cmd).CombinedOutput(); err != nil {
		return fmt.Errorf("start the stand-in owner session: %v %s", err, out)
	}
	s.sessions = append(s.sessions, owner)
	if err := s.newProject(p, rev); err != nil {
		return err
	}
	if err := s.start(p); err != nil {
		return err
	}
	if err := s.complete(p, 4*time.Minute); err != nil {
		return fmt.Errorf("%v; owner screen:\n%s", err, s.capture(owner))
	}
	screen := s.capture(owner)
	if !strings.Contains(screen, "check your queue") || !strings.Contains(screen, "reported") {
		return fmt.Errorf("the owner session was not nudged as expected:\n%s", screen)
	}
	if exec.Command("tmux", "has-session", "-t", "="+owner).Run() != nil {
		return errors.New("the owner's session is gone: agentflow must never end it")
	}
	after, _ := os.ReadFile(queueFile)
	if !bytes.Equal(before, after) {
		return fmt.Errorf("queue file after the stage = %q, want it back as %q", after, before)
	}
	return nil
}

func (s *smoke) scenarioClassic(p *scenarioProject, rev [2]string) error {
	if err := s.newProject(p, rev); err != nil {
		return err
	}
	if err := s.start(p); err != nil {
		return err
	}
	return s.complete(p, 4*time.Minute)
}

func (s *smoke) stopProject(p *scenarioProject) error {
	return s.do("POST", "/api/projects/"+p.slug+"/run/stop", map[string]any{}, nil)
}

// --- notify ---

func (s *smoke) wrapNotify(rec string) error {
	var set map[string]any
	if err := s.get("/api/settings", &set); err != nil {
		return err
	}
	s.oldNotify, _ = set["notify_command"].(string)
	script := "#!/bin/sh\nprintf '%s\\n' \"$1\" >> " + shQuote(rec) + "\n"
	if s.oldNotify != "" {
		script += "exec /bin/sh -c " + shQuote(s.oldNotify+` "$1"`) + " agentflow-notify \"$1\"\n"
	}
	p := filepath.Join(s.tmp, "notify.sh")
	if err := os.WriteFile(p, []byte(script), 0o755); err != nil {
		return err
	}
	if err := s.do("PUT", "/api/settings", map[string]any{"notify_command": p}, nil); err != nil {
		return err
	}
	s.notifyWrapped = true
	return nil
}

func (s *smoke) checkNotify(rec string, since time.Time) error {
	// The watcher checks every 30s; give it time past the phase's first attention item.
	var lines []string
	if err := waitUntil(90*time.Second, "a notification", func() (bool, error) {
		b, _ := os.ReadFile(rec)
		lines = nonEmpty(string(b))
		return len(lines) > 0, nil
	}); err != nil {
		return fmt.Errorf("%v (a notification sent less than 10 minutes before this run holds the next one back)", err)
	}
	time.Sleep(35 * time.Second) // one more watcher round
	b, _ := os.ReadFile(rec)
	lines = nonEmpty(string(b))
	for _, l := range lines {
		if l != notifyText {
			return fmt.Errorf("notification text %q, want exactly %q", l, notifyText)
		}
	}
	if len(lines) != 1 {
		return fmt.Errorf("%d notifications within %s, want 1 (10 minute debounce)", len(lines), time.Since(since).Round(time.Second))
	}
	return nil
}

func nonEmpty(s string) []string {
	var out []string
	for _, l := range strings.Split(s, "\n") {
		if strings.TrimSpace(l) != "" {
			out = append(out, l)
		}
	}
	return out
}

func shQuote(s string) string { return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'" }

// --- cleanup ---

func (s *smoke) cleanup() {
	for _, slug := range s.projects {
		_ = s.do("POST", "/api/projects/"+slug+"/run/stop", map[string]any{}, nil)
	}
	time.Sleep(2 * time.Second)
	// Only this run's sessions: af-smoke-<ts>-*.
	if out, err := exec.Command("tmux", "list-sessions", "-F", "#{session_name}").Output(); err == nil {
		for _, n := range strings.Fields(string(out)) {
			if strings.HasPrefix(n, "af-smoke-"+s.ts+"-") {
				_ = exec.Command("tmux", "kill-session", "-t", "="+n).Run()
			}
		}
	}
	for _, slug := range s.projects {
		if id, ok := s.efforts[slug]; ok {
			var ev effortView
			if s.get(fmt.Sprintf("/api/projects/%s/efforts/%d", slug, id), &ev) == nil && ev.Status != "complete" && ev.Status != "abandoned" {
				if err := s.do("POST", fmt.Sprintf("/api/projects/%s/efforts/%d/abandon", slug, id), map[string]any{}, nil); err != nil {
					fmt.Println("cleanup:", err)
				}
			}
		}
		if err := s.do("DELETE", "/api/projects/"+slug, nil, nil); err != nil {
			fmt.Println("cleanup:", err)
		}
	}
	for _, b := range s.backends {
		if err := s.do("POST", "/api/backends/"+b+"/retire", map[string]any{}, nil); err != nil {
			fmt.Println("cleanup:", err)
		}
	}
	if len(s.profiles) > 0 {
		if err := s.editProfiles(func(m map[string]json.RawMessage) {
			for _, k := range s.profiles {
				delete(m, k)
			}
		}); err != nil {
			fmt.Println("cleanup: seat profiles:", err)
		}
	}
	if s.notifyWrapped {
		if err := s.do("PUT", "/api/settings", map[string]any{"notify_command": s.oldNotify}, nil); err != nil {
			fmt.Println("cleanup: notify_command:", err)
		}
	}
	if s.stub != nil && s.stub.Process != nil {
		_ = s.stub.Process.Kill()
		_, _ = s.stub.Process.Wait()
	}
	trash, err := os.MkdirTemp("/tmp", "af-smoke-done-")
	if err == nil {
		err = os.Rename(s.tmp, filepath.Join(trash, filepath.Base(s.tmp)))
	}
	if err != nil {
		fmt.Println("cleanup: temp dir:", err)
	} else {
		fmt.Println("cleanup: done; the run's files are in", trash)
	}
}
