package api

import (
	"net/http"
	"os"
	"testing"

	"cmd184psu/unified-webapp/internal/platform/auth"
)

// AC-SV1: invalid writes are 400 naming the field, never a panic.
func TestSV1_InvalidWritesNameTheField(t *testing.T) {
	e := newEnv(t, envOpt{})
	b := backendReq("b1")
	b["models"] = []any{}
	e.field("POST", "/api/backends", b, "models")
	b["models"] = nil
	e.field("POST", "/api/backends", b, "models")
	b = backendReq("b1")
	b["slots"] = 0
	e.field("POST", "/api/backends", b, "slots")
	b = backendReq("b1")
	b["host"] = "ftp://x"
	e.field("POST", "/api/backends", b, "host")
	b = backendReq("verify")
	e.field("POST", "/api/backends", b, "name")

	e.field("PUT", "/api/settings", map[string]any{"max_attempts": 0}, "max_attempts")
	e.field("PUT", "/api/settings", map[string]any{"worker_timeout": "1s"}, "worker_timeout")
	e.field("PUT", "/api/settings", map[string]any{"no_such": 1}, "no_such")
	e.field("PUT", "/api/settings", map[string]any{"max_attempts": []int{1}}, "max_attempts")
	e.field("PUT", "/api/settings", map[string]any{}, "settings")
	e.field("PUT", "/api/settings", "{not json", "body")

	e.ok("POST", "/api/backends", backendReq("b1"), http.StatusCreated)
	e.field("PUT", "/api/candidates/worker", map[string]any{"candidates": []any{}}, "candidates")
	e.field("PUT", "/api/candidates/worker", map[string]any{}, "candidates")
	e.field("PUT", "/api/candidates/nope", map[string]any{"candidates": []map[string]string{{"backend": "b1", "model": "m1"}}}, "role")
	e.field("PUT", "/api/candidates/worker", map[string]any{"candidates": []map[string]string{{"backend": "b1", "model": "ghost"}}}, "candidates[0]")
	e.field("PATCH", "/api/backends/b1", map[string]any{"models": []any{}}, "models")
	e.field("PATCH", "/api/backends/b1", map[string]any{"name": "other"}, "name")
	e.field("POST", "/api/projects", map[string]any{"slug": "p", "repo_path": "relative"}, "repo_path")
	e.field("POST", "/api/projects", map[string]any{"slug": "p", "repo_path": repo(t), "model_scope": "everything"}, "model_scope")
	e.field("POST", "/api/projects", map[string]any{"slug": "p", "repo_path": repo(t), "surprise": 1}, "surprise")
}

// AC-WC2 (HTTP): the server's own tree cannot be registered.
func TestWC2_ServerTreeRefused(t *testing.T) {
	e := newEnv(t, envOpt{})
	e.field("POST", "/api/projects", map[string]any{"slug": "self", "repo_path": "/opt/unified-webapp"}, "repo_path")
	wd, _ := os.Getwd()
	e.field("POST", "/api/projects", map[string]any{"slug": "self", "repo_path": wd}, "repo_path")
	e.field("POST", "/api/projects", map[string]any{"slug": "self", "repo_path": e.dataDir}, "repo_path")
	e.field("POST", "/api/projects/git-init", map[string]any{"repo_path": wd, "confirm": true}, "repo_path")
}

// AC-BK1 (HTTP): a backend added over HTTP has its lane on the taskmaster
// host straight away, and retiring it hides the lane again.
func TestBK1_AddBackendCreatesLane(t *testing.T) {
	e := newEnv(t, envOpt{})
	lanes := func() map[string]bool {
		rec := newRecorder()
		e.tm.Handler().ServeHTTP(rec, newRequest("GET", "/api/lanes"))
		var ls []struct{ Name string }
		if err := jsonUnmarshal(rec.Body.Bytes(), &ls); err != nil {
			t.Fatal(err)
		}
		m := map[string]bool{}
		for _, l := range ls {
			m[l.Name] = true
		}
		return m
	}
	if lanes()["agentflow-b1"] {
		t.Fatal("lane exists before the backend")
	}
	res := decode[backendResult](t, e.ok("POST", "/api/backends", backendReq("b1"), http.StatusCreated))
	if res.Backend.Name != "b1" || len(res.Backend.Models) != 2 || !res.Backend.Models[1].Paid || res.Warnings == nil {
		t.Fatalf("created = %+v", res)
	}
	if !lanes()["agentflow-b1"] {
		t.Fatal("no lane for the new backend without a restart")
	}
	e.ok("POST", "/api/backends", backendReq("b1"), http.StatusConflict)
	res = decode[backendResult](t, e.ok("PATCH", "/api/backends/b1", map[string]any{"slots": 5}, http.StatusOK))
	if res.Backend.Slots != 5 || res.Backend.Host != "http://127.0.0.1:11434" {
		t.Fatalf("patched = %+v", res)
	}
	res = decode[backendResult](t, e.ok("POST", "/api/backends/b1/retire", map[string]any{"cancel_running": true}, http.StatusOK))
	if !res.Backend.Retired || res.Warnings == nil {
		t.Fatalf("retired = %+v", res)
	}
	if lanes()["agentflow-b1"] {
		t.Fatal("retired backend's lane is still listed")
	}
	res = decode[backendResult](t, e.ok("POST", "/api/backends/b1/enable", nil, http.StatusOK))
	if res.Backend.Retired || !lanes()["agentflow-b1"] {
		t.Fatalf("enabled = %+v", res)
	}
	e.ok("POST", "/api/backends/ghost/retire", nil, http.StatusNotFound)
	list := decode[[]BackendView](t, e.ok("GET", "/api/backends", nil, http.StatusOK))
	if len(list) != 1 || list[0].Slots != 5 {
		t.Fatalf("list = %+v", list)
	}
}

// DBS3: every write is audited with the principal's name, in the same
// transaction as the change; without a principal the actor is anonymous.
func TestDBS3_EveryWriteIsAttributed(t *testing.T) {
	e := newEnv(t, envOpt{})
	alice := &auth.Principal{Method: "ldap", Subject: "alice"}
	do := func(p *auth.Principal, method, path string, body any, want int) {
		t.Helper()
		if code, out := e.call(p, method, path, body); code != want {
			t.Fatalf("%s %s = %d, want %d: %s", method, path, code, want, out)
		}
	}
	r := repo(t)
	do(alice, "POST", "/api/backends", backendReq("b1"), 201)
	do(alice, "PATCH", "/api/backends/b1", map[string]any{"slots": 3}, 200)
	do(alice, "PUT", "/api/candidates/worker", map[string]any{"candidates": []map[string]string{{"backend": "b1", "model": "m1"}}}, 200)
	do(alice, "PUT", "/api/settings", map[string]any{"max_attempts": 7}, 200)
	do(alice, "POST", "/api/projects", map[string]any{"slug": "p1", "repo_path": r}, 201)
	do(alice, "PATCH", "/api/projects/p1", map[string]any{"model_scope": "any"}, 200)
	do(alice, "PUT", "/api/projects/p1/limits", map[string]any{"limits": map[string]any{"max_attempts": 9}}, 200)
	do(alice, "PUT", "/api/projects/p1/candidates/worker", map[string]any{"candidates": []map[string]string{{"backend": "b1", "model": "m1"}}}, 200)
	do(alice, "DELETE", "/api/projects/p1/candidates/worker", nil, 204)
	do(alice, "POST", "/api/backends/b1/retire", nil, 200)
	do(alice, "POST", "/api/backends/b1/enable", nil, 200)
	do(alice, "DELETE", "/api/projects/p1", nil, 204)
	do(nil, "POST", "/api/projects", map[string]any{"slug": "p2", "repo_path": repo(t)}, 201)
	do(&auth.Principal{Method: "apikey", Subject: "ci"}, "PATCH", "/api/projects/p2", map[string]any{"model_scope": "any"}, 200)

	evs, err := e.st.ListEvents(t.Context(), "", "", 1000)
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]string{}
	for _, ev := range evs {
		got[ev.Action+":"+ev.SubjectID] = ev.Actor
	}
	for _, k := range []string{"backend.create:b1", "backend.update:b1", "candidates.set:global::worker", "settings.update:global",
		"project.create:p1", "project.update:p1", "candidates.set:project:p1:worker", "candidates.clear:project:p1:worker",
		"backend.retire:b1", "backend.enable:b1", "project.delete:p1"} {
		if got[k] != "alice" {
			t.Errorf("event %s actor = %q, want alice (all: %v)", k, got[k], got)
		}
	}
	if got["project.create:p2"] != anonymousActor || got["project.update:p2"] != "ci" {
		t.Errorf("anonymous/apikey actors = %q / %q", got["project.create:p2"], got["project.update:p2"])
	}
}

func TestProjectsCRUDAndLimits(t *testing.T) {
	e := newEnv(t, envOpt{})
	r := repo(t)
	v := decode[ProjectView](t, e.ok("POST", "/api/projects", map[string]any{"slug": "p1", "repo_path": r, "feedback_cmds": []string{"go test ./..."}}, 201))
	if v.ModelScope != "local" || v.RunState != "stopped" || v.Effort != nil || len(v.FeedbackCmds) != 1 {
		t.Fatalf("created = %+v", v)
	}
	e.ok("POST", "/api/projects", map[string]any{"slug": "p1", "repo_path": repo(t)}, 409)
	e.ok("POST", "/api/projects", map[string]any{"slug": "p2", "repo_path": r}, 409) // PJ4: one project per repo
	e.ok("GET", "/api/projects/ghost", nil, 404)

	v = decode[ProjectView](t, e.ok("PATCH", "/api/projects/p1", map[string]any{"feedback_cmds": []string{"make"}}, 200))
	if v.FeedbackCmds[0] != "make" || v.RepoPath == "" {
		t.Fatalf("patched = %+v", v)
	}
	lv := decode[LimitsView](t, e.ok("GET", "/api/projects/p1/limits", nil, 200))
	if len(lv.Limits) != 0 || lv.Effective["max_attempts"] != "5" {
		t.Fatalf("limits = %+v", lv)
	}
	lv = decode[LimitsView](t, e.ok("PUT", "/api/projects/p1/limits", map[string]any{"limits": map[string]any{"max_attempts": 8, "worker_timeout": "45m", "plan_max_rounds": ""}}, 200))
	if lv.Effective["max_attempts"] != "8" || lv.Effective["worker_timeout"] != "45m0s" || lv.Limits["max_attempts"] != "8" {
		t.Fatalf("limits = %+v", lv)
	}
	e.field("PUT", "/api/projects/p1/limits", map[string]any{"limits": map[string]any{"max_attempts": 99}}, "limits.max_attempts")
	e.field("PUT", "/api/projects/p1/limits", map[string]any{"limits": map[string]any{"opencode_path": "x"}}, "limits.opencode_path")
	e.ok("GET", "/api/projects/ghost/limits", nil, 404)
	if list := decode[[]ProjectView](t, e.ok("GET", "/api/projects", nil, 200)); len(list) != 1 {
		t.Fatalf("list = %+v", list)
	}
	e.ok("DELETE", "/api/projects/p1", nil, 204)
	e.ok("DELETE", "/api/projects/p1", nil, 404)
}

// PJ1: git init is offered for a directory that is not a repository yet, and
// only after explicit confirmation.
func TestPJ1_GitInit(t *testing.T) {
	e := newEnv(t, envOpt{})
	dir := t.TempDir()
	e.field("POST", "/api/projects", map[string]any{"slug": "g", "repo_path": dir}, "repo_path")
	e.field("POST", "/api/projects/git-init", map[string]any{"repo_path": dir}, "confirm")
	e.ok("POST", "/api/projects/git-init", map[string]any{"repo_path": dir, "confirm": true}, 201)
	if _, err := os.Stat(dir + "/.git"); err != nil {
		t.Fatalf("no repository after git init: %v", err)
	}
	e.ok("POST", "/api/projects/git-init", map[string]any{"repo_path": dir, "confirm": true}, 409)
	e.ok("POST", "/api/projects", map[string]any{"slug": "g", "repo_path": dir}, 201)
	e.field("POST", "/api/projects/git-init", map[string]any{"repo_path": t.TempDir() + "/missing", "confirm": true}, "repo_path")
	evs, _ := e.st.ListEvents(t.Context(), "project", dir, 10)
	if len(evs) != 1 || evs[0].Action != "project.git_init" {
		t.Fatalf("git init not audited: %+v", evs)
	}
}

func TestSettingsAndOpencode(t *testing.T) {
	e := newEnv(t, envOpt{})
	s := decode[map[string]any](t, e.ok("GET", "/api/settings", nil, 200))
	if s["max_attempts"] != float64(5) || s["worker_timeout"] == "" || s["opencode_path"] == "" {
		t.Fatalf("settings = %v", s)
	}
	s = decode[map[string]any](t, e.ok("PUT", "/api/settings", map[string]any{"max_attempts": "9", "stuck_threshold": 4, "verify_timeout": "20m"}, 200))
	if s["max_attempts"] != float64(9) || s["stuck_threshold"] != float64(4) || s["verify_timeout"] != "20m0s" {
		t.Fatalf("settings = %v", s)
	}
	e.field("PUT", "/api/settings", map[string]any{"stuck_threshold": 50}, "stuck_threshold")
	e.fake.SetVersion("1.2.3")
	if oc := decode[OpenCodeStatus](t, e.ok("GET", "/api/opencode/version", nil, 200)); oc.Version == "1.2.3" {
		t.Fatalf("cached probe changed without refresh: %+v", oc)
	}
	if oc := decode[OpenCodeStatus](t, e.ok("GET", "/api/opencode/version?refresh=1", nil, 200)); oc.Status != "ok" || oc.Version != "1.2.3" {
		t.Fatalf("refreshed probe = %+v", oc)
	}
	if oc := decode[OpenCodeStatus](t, e.ok("GET", "/api/opencode/version", nil, 200)); oc.Version != "1.2.3" {
		t.Fatalf("refresh not cached: %+v", oc)
	}
}
