package graphql

import (
	"encoding/json"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"cmd184psu/unified-webapp/internal/issuetracker/db"
	"cmd184psu/unified-webapp/internal/issuetracker/store"
)

func testHandler(t *testing.T) *Handler {
	t.Helper()
	database, err := db.Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	st := store.New(database)
	if err := st.Seed(); err != nil {
		t.Fatalf("seed: %v", err)
	}
	return New(st, "Unassigned", "unassigned@localhost")
}

// The Linear-compatible endpoint authenticates at the platform gate, not here,
// so a query with no Authorization header must succeed (no token layer).
func TestIssuesQueryNoToken(t *testing.T) {
	h := testHandler(t)
	body := `{"query":"query { issues { nodes { id identifier title } } }"}`
	req := httptest.NewRequest("POST", "/graphql", strings.NewReader(body))
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	if w.Code != 200 {
		t.Fatalf("status = %d, want 200", w.Code)
	}
	var resp struct {
		Data struct {
			Issues struct {
				Nodes []map[string]any `json:"nodes"`
			} `json:"issues"`
		} `json:"data"`
		Errors []map[string]any `json:"errors"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v; body=%s", err, w.Body.String())
	}
	if len(resp.Errors) != 0 {
		t.Fatalf("unexpected errors (auth should not be enforced here): %v", resp.Errors)
	}
	if len(resp.Data.Issues.Nodes) == 0 {
		t.Errorf("expected seeded issue nodes, got none")
	}
}
