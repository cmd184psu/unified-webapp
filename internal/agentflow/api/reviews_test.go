package api

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"cmd184psu/unified-webapp/internal/agentflow/model"
	"cmd184psu/unified-webapp/internal/agentflow/store"
)

type fakeReviewRunner struct {
	st       *store.Store
	verdict  string
	findings string
}

func (f fakeReviewRunner) RunReview(ctx context.Context, id int64) (model.Review, error) {
	state, done := "done", time.Now().UnixMilli()
	return f.st.UpdateReview(ctx, id, store.ReviewPatch{
		State: &state, Verdict: &f.verdict, Findings: &f.findings, Finished: &done,
	})
}

func TestReviewAPIReportsAndNotifies(t *testing.T) {
	e := newEnv(t, envOpt{})
	e.ok("POST", "/api/projects", map[string]any{"slug": "p", "repo_path": repo(t)}, http.StatusCreated)
	var got struct {
		From    string `json:"from"`
		Content string `json:"content"`
	}
	bc := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(body, &got)
		_, _ = w.Write([]byte(`{}`))
	}))
	defer bc.Close()
	key := filepath.Join(t.TempDir(), "key")
	if err := os.WriteFile(key, []byte("secret"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := e.st.UpdateSettings(context.Background(), "test", map[string]string{
		"botchat_url": bc.URL, "botchat_key_file": key, "review_callsign": "agentflow-review",
	}); err != nil {
		t.Fatal(err)
	}
	e.api.deps.ReviewRunner = fakeReviewRunner{st: e.st, verdict: "PASS", findings: "1. No findings."}
	raw := e.ok("POST", "/api/reviews", map[string]any{
		"project": "p", "branch": "feature/x", "task": map[string]any{
			"id": "TASK-9", "title": "x", "objective": "do x", "acceptance": []string{"x works"}, "exclusions": "none",
		},
		"author_family": "claude",
		"notify":        map[string]string{"botchat_room": "room_review", "prefix": "REVIEW TASK-9:"},
	}, http.StatusCreated)
	rev := decode[model.Review](t, raw)
	if rev.Verdict != "PASS" || rev.State != "done" {
		t.Fatalf("review = %+v", rev)
	}
	if !strings.HasPrefix(got.Content, "REVIEW TASK-9: PASS") || got.From != "agentflow-review" {
		t.Fatalf("notify = %+v", got)
	}
	if list := decode[[]model.Review](t, e.ok("GET", "/api/reviews?project=p", nil, http.StatusOK)); len(list) != 1 || list[0].ID != rev.ID {
		t.Fatalf("list = %+v", list)
	}
	if one := decode[model.Review](t, e.ok("GET", "/api/reviews/"+itoa(rev.ID), nil, http.StatusOK)); one.TaskID != "TASK-9" {
		t.Fatalf("get = %+v", one)
	}
}

func TestReviewAPIChangesRequestedOnVerifyFailure(t *testing.T) {
	e := newEnv(t, envOpt{})
	e.ok("POST", "/api/projects", map[string]any{"slug": "p", "repo_path": repo(t)}, http.StatusCreated)
	e.api.deps.ReviewRunner = fakeReviewRunner{st: e.st, verdict: "CHANGES-REQUESTED", findings: "1. go test: Player-2 gold differs"}
	raw := e.ok("POST", "/api/reviews", map[string]any{
		"project": "p", "branch": "feature/x", "task": map[string]any{"id": "TASK-10"},
	}, http.StatusCreated)
	rev := decode[model.Review](t, raw)
	if rev.Verdict != "CHANGES-REQUESTED" || !strings.Contains(rev.Findings, "Player-2 gold") {
		t.Fatalf("review = %+v", rev)
	}
}
