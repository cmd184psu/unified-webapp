package api

import (
	"net/http"
	"testing"
)

// The smoke check seeds a known queue through the server (AF-11b): only for a smoke-*
// project, approved at once; any other project is refused.
func TestSeedQueueOnlyForSmokeProjects(t *testing.T) {
	e := newEnv(t, envOpt{})
	e.addProject("smoke-1-flow")
	e.addProject("real")
	tasks := `{"tasks":[{"key":"T001","phase":"P1","title":"t","prompt":"do it","acceptance":["done"],"files_touched":["a.txt"],"depends_on":[]}]}`
	ef := decode[EffortView](t, e.ok("POST", "/api/projects/smoke-1-flow/efforts", map[string]any{"title": "s"}, http.StatusCreated))
	e.ok("POST", "/api/projects/smoke-1-flow/efforts/"+itoa(ef.ID)+"/queue/seed", tasks, http.StatusOK)
	got := decode[EffortView](t, e.ok("GET", "/api/projects/smoke-1-flow/efforts/"+itoa(ef.ID), nil, http.StatusOK))
	if got.Queue == nil || got.Queue.Status != "approved" {
		t.Fatalf("seeded queue = %+v, want approved", got.Queue)
	}
	real := decode[EffortView](t, e.ok("POST", "/api/projects/real/efforts", map[string]any{"title": "r"}, http.StatusCreated))
	e.ok("POST", "/api/projects/real/efforts/"+itoa(real.ID)+"/queue/seed", tasks, http.StatusForbidden)
}
