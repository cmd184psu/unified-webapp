package api

import (
	"net/http"
	"strings"
	"testing"
)

// DELETE /api/backends/{name}: unknown 404, enabled 409, still on a candidate
// list 409 naming the list, then 204, and the card is gone from the list.
func TestBackendDeleteRefusesUntilItIsDisabledAndOffEveryList(t *testing.T) {
	e := newEnv(t, envOpt{})
	e.ok("POST", "/api/backends", backendReq("b1"), http.StatusCreated)
	e.ok("POST", "/api/backends", backendReq("b2"), http.StatusCreated)
	e.ok("PUT", "/api/candidates/worker", map[string]any{"candidates": []map[string]string{{"backend": "b1", "model": "m1"}}}, http.StatusOK)

	e.ok("DELETE", "/api/backends/ghost", nil, http.StatusNotFound)
	if out := string(e.ok("DELETE", "/api/backends/b1", nil, http.StatusConflict)); !strings.Contains(out, "disable") {
		t.Fatalf("an enabled backend: %s", out)
	}
	e.ok("POST", "/api/backends/b1/retire", map[string]any{"cancel_running": true}, http.StatusOK)
	if out := string(e.ok("DELETE", "/api/backends/b1", nil, http.StatusConflict)); !strings.Contains(out, "global worker") {
		t.Fatalf("a listed backend: %s", out)
	}
	e.ok("PUT", "/api/candidates/worker", map[string]any{"candidates": []map[string]string{{"backend": "b2", "model": "m1"}}}, http.StatusOK)
	e.ok("DELETE", "/api/backends/b1", nil, http.StatusNoContent)
	list := decode[[]BackendView](t, e.ok("GET", "/api/backends", nil, http.StatusOK))
	if len(list) != 1 || list[0].Name != "b2" {
		t.Fatalf("after delete: %+v", list)
	}
	e.ok("DELETE", "/api/backends/b1", nil, http.StatusNotFound)
}
