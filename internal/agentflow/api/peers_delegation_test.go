// Copyright 2026 FreeSociety3 contributors
// SPDX-License-Identifier: Apache-2.0

package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"cmd184psu/unified-webapp/internal/agentflow/model"
	"cmd184psu/unified-webapp/internal/agentflow/store"
)

// gitRepo makes a real repository on main with one commit, because delegation
// ships a git bundle and a fake .git directory cannot be bundled.
func gitRepo(t *testing.T) string {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "repo")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	git := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
		cmd.Env = append(os.Environ(), "GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_NOSYSTEM=1",
			"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@example.com", "GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@example.com")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	git("init", "-q", "-b", "main")
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("one\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	git("add", "-A")
	git("commit", "-q", "-m", "init")
	return dir
}

// flowWithRepo is newFlow, but the project points at a real git repository and
// the engine gets the git service, so base commits exist.
func flowWithRepo(t *testing.T) *flow {
	t.Helper()
	e := newEnv(t, envOpt{fastPoll: true, engineGit: true})
	f := &flow{env: e, srv: httptest.NewServer(e.mux)}
	t.Cleanup(f.srv.Close)
	b := backendReq("gpu")
	b["models"] = []map[string]any{{"model": "m1"}, {"model": "m2"}, {"model": "m3"}}
	e.ok("POST", "/api/backends", b, http.StatusCreated)
	for role, m := range map[string]string{"planner": "m1", "architect": "m2", "critic": "m3", "compiler": "m1", "worker": "m1", "reviewer": "m2"} {
		e.ok("PUT", "/api/candidates/"+role, map[string]any{"candidates": []map[string]string{{"backend": "gpu", "model": m}}}, http.StatusOK)
	}
	e.ok("POST", "/api/projects", map[string]any{"slug": "p1", "repo_path": gitRepo(t)}, http.StatusCreated)
	ef := decode[EffortView](t, e.ok("POST", "/api/projects/p1/efforts", map[string]any{"title": "first"}, http.StatusCreated))
	f.effort, f.base = ef.ID, "/api/projects/p1/efforts/"+itoa(ef.ID)
	return f
}

// approvedEffort runs the planning path through to an approved queue, which is
// the only state a delegation accepts.
func approvedEffort(t *testing.T, f *flow) {
	t.Helper()
	f.planAndCompile()
	f.ok("POST", f.base+"/frd/approve", map[string]any{"revision": 1}, http.StatusOK)
	q := decode[struct{ Hash string }](t, f.ok("GET", f.base+"/queue", nil, http.StatusOK))
	f.ok("POST", f.base+"/queue/approve", map[string]any{"hash": q.Hash}, http.StatusOK)
}

// pair makes a invite b, b accept, and returns both servers' base URLs.
func pair(t *testing.T, a, b *env) (*httptest.Server, *httptest.Server) {
	t.Helper()
	for _, e := range []*env{a, b} {
		enablePeers(t, e)
		if err := e.st.UpdateSettings(t.Context(), "test", map[string]string{"load_cap_tenths": "100", "min_free_mem_gb": "0", "min_free_space_gb": "1"}); err != nil {
			t.Fatal(err)
		}
	}
	if err := a.st.UpdateSettings(t.Context(), "test", map[string]string{"node_name": "laptop"}); err != nil {
		t.Fatal(err)
	}
	if err := b.st.UpdateSettings(t.Context(), "test", map[string]string{"node_name": "builder"}); err != nil {
		t.Fatal(err)
	}
	srvA, srvB := httptest.NewServer(a.mux), httptest.NewServer(b.mux)
	t.Cleanup(srvA.Close)
	t.Cleanup(srvB.Close)
	resp, err := postJSON(srvA.URL+"/api/peers/invite", map[string]any{"name": "builder"})
	if err != nil {
		t.Fatal(err)
	}
	var inv struct {
		Code string `json:"code"`
	}
	json.NewDecoder(resp.Body).Decode(&inv)
	resp.Body.Close()
	if inv.Code == "" {
		t.Fatal("no invite code")
	}
	acc, err := postJSON(srvB.URL+"/api/peers/accept", map[string]any{"a_url": srvA.URL, "code": inv.Code, "name": "laptop", "b_url": srvB.URL})
	if err != nil {
		t.Fatal(err)
	}
	defer acc.Body.Close()
	if acc.StatusCode != http.StatusOK {
		t.Fatalf("accept = %d", acc.StatusCode)
	}
	return srvA, srvB
}

// TestDelegationToPeer covers AF-25 end to end: the owner hands an approved
// effort to a paired node, the peer runs a shadow copy marked delegated-from,
// a repeated delivery is idempotent, the owner collects the result bundle into
// its own branch names, and cancel stops the shadow.
func TestDelegationToPeer(t *testing.T) {
	a := flowWithRepo(t)
	approvedEffort(t, a)
	b := newEnv(t, envOpt{fastPoll: true, engineGit: true})
	b.ok("POST", "/api/projects", map[string]any{"slug": "p1", "repo_path": gitRepo(t)}, http.StatusCreated)
	pair(t, a.env, b)

	out := a.ok("POST", "/api/peers/builder/delegate", map[string]any{
		"project": "p1", "effort": a.effort, "tasks": []string{"T001"}, "reason": "laptop at capacity",
	}, http.StatusCreated)
	dv := decode[delegationView](t, out)
	if dv.ID == "" || dv.Peer != "builder" || dv.RemoteEffort == 0 || dv.Direction != "out" {
		t.Fatalf("delegation = %+v", dv)
	}
	if dv.Status != store.DelegationRunning && dv.Status != store.DelegationPending {
		t.Fatalf("status = %q", dv.Status)
	}
	if dv.BaseCommit == "" {
		t.Fatal("no base commit recorded")
	}
	if len(dv.Tasks) != 1 || dv.Tasks[0] != "T001" {
		t.Fatalf("tasks = %v", dv.Tasks)
	}

	// The peer has a shadow effort for the same queue, marked delegated-from.
	in, err := b.st.GetDelegation(t.Context(), dv.ID)
	if err != nil {
		t.Fatalf("peer row: %v", err)
	}
	if in.Direction != "in" || in.SourceEffort != a.effort || in.Effort == 0 || in.Project != "p1" {
		t.Fatalf("peer row = %+v", in)
	}
	if node, err := b.st.EffortDelegatedFrom(t.Context(), in.Effort); err != nil || node != "laptop" {
		t.Fatalf("delegated from = %q, %v", node, err)
	}
	shadow, err := b.st.GetEffort(t.Context(), in.Effort)
	if err != nil {
		t.Fatal(err)
	}
	if shadow.Status == model.EffortAbandoned {
		t.Fatalf("shadow effort = %+v", shadow)
	}

	// A repeated delivery with the same id returns the existing shadow instead
	// of creating another one.
	keyA, err := a.api.readPeerKey("builder")
	if err != nil {
		t.Fatal(err)
	}
	dup := map[string]any{
		"format": delegationFormat, "version": delegationVersion, "id": dv.ID,
		"owner_node": "laptop", "project": "p1", "export": json.RawMessage(`{}`),
	}
	body, _ := json.Marshal(dup)
	r := httptest.NewRequest(http.MethodPost, "/api/peer/delegate", strings.NewReader(string(body)))
	r.Header.Set("Authorization", "Bearer "+keyA)
	rec := httptest.NewRecorder()
	b.mux.ServeHTTP(rec, r)
	if rec.Code != http.StatusOK {
		t.Fatalf("duplicate delivery = %d: %s", rec.Code, rec.Body.String())
	}
	var again delegationResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &again); err != nil {
		t.Fatal(err)
	}
	if again.RemoteEffort != in.Effort {
		t.Fatalf("duplicate delivery made effort %d, want %d", again.RemoteEffort, in.Effort)
	}
	if rows, err := b.st.ListDelegations(t.Context(), ""); err != nil || len(rows) != 1 {
		t.Fatalf("peer rows = %+v, %v", rows, err)
	}

	// Give the peer a finished task branch, then collect it on the owner.
	repoB, err := b.st.GetProject(t.Context(), "p1")
	if err != nil {
		t.Fatal(err)
	}
	runGit(t, repoB.RepoPath, "branch", "agentflow-task/"+itoa(in.Effort)+"/T001")
	a.ok("POST", "/api/delegations/"+dv.ID+"/collect", nil, http.StatusOK)
	if res := runGit(t, gitRepoPathOf(t, a), "rev-parse", "refs/heads/agentflow-task/"+itoa(a.effort)+"/T001"); res == "" {
		t.Fatal("the collected branch did not arrive")
	}

	// Cancel stops the shadow on the peer.
	a.ok("POST", "/api/peers/builder/delegations/"+dv.ID+"/cancel", nil, http.StatusOK)
	in, err = b.st.GetDelegation(t.Context(), dv.ID)
	if err != nil || in.Status != store.DelegationCancelled {
		t.Fatalf("peer row after cancel = %+v, %v", in, err)
	}
	dead, err := b.st.GetEffort(t.Context(), in.Effort)
	if err != nil || dead.Status != model.EffortAbandoned {
		t.Fatalf("shadow after cancel = %+v, %v", dead, err)
	}
}

// TestDelegationFallbackRequeuesLocal covers the timeout: an owner whose peer
// stopped answering re-queues the effort locally instead of waiting forever.
func TestDelegationFallbackRequeuesLocal(t *testing.T) {
	a := flowWithRepo(t)
	approvedEffort(t, a)
	b := newEnv(t, envOpt{fastPoll: true, engineGit: true})
	b.ok("POST", "/api/projects", map[string]any{"slug": "p1", "repo_path": gitRepo(t)}, http.StatusCreated)
	_, srvB := pair(t, a.env, b)
	if err := a.st.UpdateSettings(t.Context(), "test", map[string]string{"delegation_timeout_minutes": "1"}); err != nil {
		t.Fatal(err)
	}

	dv := decode[delegationView](t, a.ok("POST", "/api/peers/builder/delegate",
		map[string]any{"project": "p1", "effort": a.effort}, http.StatusCreated))
	// The peer goes away and the last contact is older than the timeout.
	if err := a.st.TouchDelegation(t.Context(), dv.ID, time.Now().Add(-5*time.Minute)); err != nil {
		t.Fatal(err)
	}
	srvB.Close()
	if err := a.api.PeerPass(context.Background()); err != nil {
		t.Fatalf("pass: %v", err)
	}
	d, err := a.st.GetDelegation(t.Context(), dv.ID)
	if err != nil {
		t.Fatal(err)
	}
	if d.Status != store.DelegationFallback {
		t.Fatalf("status = %q, want %q", d.Status, store.DelegationFallback)
	}
	if d.Detail == "" {
		t.Fatal("no detail on the fallback row")
	}
}

// TestDelegateRefusesUnknownProject pins the rule the peer must keep: an
// effort is never shipped to a node that does not already have the project.
func TestDelegateRefusesUnknownProject(t *testing.T) {
	a := flowWithRepo(t)
	approvedEffort(t, a)
	b := newEnv(t, envOpt{fastPoll: true, engineGit: true})
	pair(t, a.env, b)
	code, out := a.call(nil, http.MethodPost, "/api/peers/builder/delegate",
		map[string]any{"project": "p1", "effort": a.effort})
	if code != http.StatusConflict || !strings.Contains(string(out), "not present on peer") {
		t.Fatalf("delegate = %d: %s", code, out)
	}
}

func runGit(t *testing.T, repo string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", repo}, args...)...)
	cmd.Env = append(os.Environ(), "GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_NOSYSTEM=1")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}

func gitRepoPathOf(t *testing.T, f *flow) string {
	t.Helper()
	p, err := f.st.GetProject(t.Context(), "p1")
	if err != nil {
		t.Fatal(err)
	}
	return p.RepoPath
}

// TestPeerRestsAreShared covers AF-25's shared rests: a peer's rate limit holds
// the same backend/model on this node, and a longer local rest is kept.
func TestPeerRestsAreShared(t *testing.T) {
	a := newEnv(t, envOpt{fastPoll: true, engineGit: true})
	b := newEnv(t, envOpt{fastPoll: true, engineGit: true})
	for _, e := range []*env{a, b} {
		e.ok("POST", "/api/backends", backendReq("gpu"), http.StatusCreated)
	}
	pair(t, a, b)

	until := time.Now().Add(20 * time.Minute)
	if err := b.st.RestModelUntil(t.Context(), "gpu", "m1", "429", until, time.Now()); err != nil {
		t.Fatal(err)
	}
	if err := a.api.PeerPass(context.Background()); err != nil {
		t.Fatalf("pass: %v", err)
	}
	rests, err := a.st.ModelRests(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	var got int64
	for _, r := range rests {
		if r.Backend == "gpu" && r.Model == "m1" {
			got = r.Until
		}
	}
	if got != until.UnixMilli() {
		t.Fatalf("shared rest = %d, want %d (%+v)", got, until.UnixMilli(), rests)
	}

	// A longer local rest is not shortened by a shorter peer rest.
	longer := time.Now().Add(60 * time.Minute)
	if err := a.st.RestModelUntil(t.Context(), "gpu", "m1", "local", longer, time.Now()); err != nil {
		t.Fatal(err)
	}
	if err := a.api.PeerPass(context.Background()); err != nil {
		t.Fatalf("pass: %v", err)
	}
	rests, _ = a.st.ModelRests(t.Context())
	for _, r := range rests {
		if r.Backend == "gpu" && r.Model == "m1" && r.Until != longer.UnixMilli() {
			t.Fatalf("local rest was shortened to %d", r.Until)
		}
	}
}
