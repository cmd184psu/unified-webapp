package api

import (
	"context"
	"net/http"
	"testing"

	"cmd184psu/unified-webapp/internal/agentflow/store"
)

// MB8: pass rates per model count infra outcomes apart.
func TestE2_ModelStats(t *testing.T) {
	e := newEnv(t, envOpt{})
	e.addProject("p1")
	ef := decode[EffortView](t, e.ok("POST", "/api/projects/p1/efforts", map[string]any{"title": "src"}, http.StatusCreated))
	ctx := context.Background()
	finish := func(role, backend, mdl, outcome string) {
		t.Helper()
		st, err := e.st.CreateStage(ctx, store.StageSpec{Effort: ef.ID, Role: role, Backend: backend, Model: mdl})
		if err != nil {
			t.Fatal(err)
		}
		if err := e.st.StartStage(ctx, st.ID, "job"); err != nil {
			t.Fatal(err)
		}
		if err := e.st.FinishStage(ctx, st.ID, outcome, "", nil); err != nil {
			t.Fatal(err)
		}
	}
	finish("planner", "b1", "m1", "pass")
	finish("planner", "b1", "m1", "pass")
	finish("planner", "b1", "m1", "repair")
	finish("planner", "b1", "m1", "infra:crash")
	finish("planner", "b1", "m2", "blocked")

	if got := decode[[]ModelStat](t, e.ok("GET", "/api/stats/models", nil, http.StatusOK)); len(got) != 2 {
		t.Fatalf("stats = %+v", got)
	} else {
		m1 := got[0]
		if m1.Model != "m1" || m1.Passed != 2 || m1.Failed != 1 || m1.Infra != 1 || m1.Stages != 4 {
			t.Fatalf("m1 = %+v", m1)
		}
		if m1.PassRate < 0.66 || m1.PassRate > 0.67 {
			t.Fatalf("m1 pass rate = %v, want 2/3", m1.PassRate)
		}
		if got[1].Model != "m2" || got[1].PassRate != 0 || got[1].Failed != 1 {
			t.Fatalf("m2 = %+v", got[1])
		}
	}
}

func TestE2_ModelStatsEmptyIsAnArray(t *testing.T) {
	e := newEnv(t, envOpt{})
	if body := string(e.ok("GET", "/api/stats/models", nil, http.StatusOK)); body != "[]\n" {
		t.Fatalf("body = %q", body)
	}
}

// GIT4: the merge and push commands exist only for a complete effort.
func TestE4_GitCommandsNeedACompleteEffort(t *testing.T) {
	e := newEnv(t, envOpt{engineGit: true})
	e.addProject("p1")
	ef := decode[EffortView](t, e.ok("POST", "/api/projects/p1/efforts", map[string]any{"title": "src"}, http.StatusCreated))
	path := "/api/projects/p1/efforts/" + itoa(ef.ID) + "/git-commands"
	e.ok("GET", path, nil, http.StatusConflict)
	complete := "complete"
	if _, err := e.st.UpdateEffort(context.Background(), "test", ef.ID, store.EffortPatch{Status: &complete, IntegrationBranch: strPtr("af/integ")}); err != nil {
		t.Fatal(err)
	}
	body := decode[struct {
		Commands []string
		Branch   string
		Commits  []any
	}](t, e.ok("GET", path, nil, http.StatusOK))
	got := body.Commands
	if body.Branch != "af/integ" || body.Commits == nil {
		t.Fatalf("branch = %q, commits = %v", body.Branch, body.Commits)
	}
	if len(got) != 5 || got[3] != "git merge --no-ff 'af/integ'" {
		t.Fatalf("commands = %q", got)
	}
}

func strPtr(s string) *string { return &s }
