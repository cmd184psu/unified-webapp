package agentflow

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"cmd184psu/unified-webapp/internal/agentflow/api"
	"cmd184psu/unified-webapp/internal/agentflow/runner"
	"cmd184psu/unified-webapp/internal/agentflow/store"
	"cmd184psu/unified-webapp/internal/platform/auth"
	"cmd184psu/unified-webapp/internal/platform/config"
)

// TestRealDataSmoke calls every read-only API route on a COPY of a real
// agentflow database and fails on any answer slower than 5 s (a warning at
// 2 s) or any 5xx. GET /api/attention hung on production data after the 11-24
// promotion while every unit test and the migration proof passed: the scan
// was only slow at real row counts.
//
// It skips unless AF_REALDATA_DB is set; promote_af.sh points it at the
// migrated copy so a route that cannot answer stops the promotion.
func TestRealDataSmoke(t *testing.T) {
	src := os.Getenv("AF_REALDATA_DB")
	if src == "" {
		t.Skip("AF_REALDATA_DB is unset: skipping the real-data API smoke test (set it to a copy of a production agentflow.db)")
	}
	// Copy first: the borrowed database is never opened, only read as bytes.
	body, err := os.ReadFile(src)
	if err != nil {
		t.Fatalf("read AF_REALDATA_DB: %v", err)
	}
	dir := t.TempDir()
	dst := filepath.Join(dir, "agentflow.db")
	if err := os.WriteFile(dst, body, 0o600); err != nil {
		t.Fatalf("copy AF_REALDATA_DB: %v", err)
	}
	st, err := store.Open(dst) // migrates the copy
	if err != nil {
		t.Fatalf("open the database copy: %v", err)
	}
	_ = st.Close()

	h, err := buildWith(config.AgentflowConfig{StaticDir: t.TempDir(), DataDir: dir, SSEMaxSubscribers: 8}, openHost(t), runner.NewFake())
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	// Close in the background: a route wedged on real data also wedges the
	// module's shutdown, and that must not turn a reported failure into a hang.
	t.Cleanup(func() { go func() { _ = h.(io.Closer).Close() }() })
	serve := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h.ServeHTTP(w, r.WithContext(auth.WithPrincipal(r.Context(), auth.Principal{Method: "ldap", Subject: "alice"})))
	})

	type answer struct {
		code int
		body []byte
		took time.Duration
	}
	// get calls the API directly with a 5 s budget. A handler that ignores its
	// context (as the old attention scan did) is abandoned, not waited for.
	get := func(path string) answer {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		start := time.Now()
		ch := make(chan answer, 1)
		go func() {
			rec := httptest.NewRecorder()
			serve.ServeHTTP(rec, httptest.NewRequest("GET", path, nil).WithContext(ctx))
			ch <- answer{rec.Code, rec.Body.Bytes(), time.Since(start)}
		}()
		select {
		case a := <-ch:
			return a
		case <-ctx.Done():
			return answer{took: time.Since(start)} // a timeout is a failure; the route is named below
		}
	}
	// first returns field of the first row of the JSON list at path.
	first := func(path, field string) string {
		var rows []map[string]any
		if json.Unmarshal(get(path).body, &rows) != nil || len(rows) == 0 {
			return ""
		}
		switch v := rows[0][field].(type) {
		case string:
			return v
		case float64:
			return strconv.FormatInt(int64(v), 10)
		}
		return ""
	}

	// Real rows for the parameterised routes: the first project, its first
	// effort and that effort's first task, plus a review, seat task and backend.
	slug := first("/api/projects", "slug")
	effort := ""
	if slug != "" {
		effort = first("/api/projects/"+slug+"/efforts", "id")
	}
	task := ""
	if slug != "" && effort != "" {
		task = first("/api/projects/"+slug+"/efforts/"+effort+"/tasks", "key")
	}
	review, seatTask, backend := first("/api/reviews", "id"), first("/api/seat-tasks", "id"), first("/api/backends", "name")
	seat := first("/api/seats", "callsign")

	var problems []string
	for _, p := range api.Patterns() {
		method, path, ok := strings.Cut(p, " ")
		if !ok || method != "GET" {
			continue
		}
		if path == "/api/events" || strings.Contains(path, "/stream") {
			t.Logf("skip %s: streaming route, it does not answer", p)
			continue
		}
		id := effort
		switch {
		case strings.HasPrefix(path, "/api/reviews/"):
			id = review
		case strings.HasPrefix(path, "/api/seat-tasks/"):
			id = seatTask
		}
		use := strings.NewReplacer("{slug}", slug, "{id}", id, "{name}", backend, "{key}", task, "{n}", "1", "{callsign}", seat).Replace(path)
		if strings.Contains(use, "{") { // a parameter with no real row
			t.Logf("skip %s: no real row for its parameter", p)
			continue
		}
		a := get(use)
		switch {
		case a.code >= 500 || a.code == 0 || a.took >= 5*time.Second:
			problems = append(problems, fmt.Sprintf("%s -> %d after %s", p, a.code, a.took.Round(time.Millisecond)))
		case a.took >= 2*time.Second:
			t.Logf("WARNING %s -> %d took %s (> 2 s on real data)", p, a.code, a.took.Round(time.Millisecond))
		default:
			t.Logf("%s -> %d in %s", p, a.code, a.took.Round(time.Millisecond))
		}
	}
	if len(problems) > 0 {
		t.Fatalf("%d read-only route(s) failed on real data:\n  %s", len(problems), strings.Join(problems, "\n  "))
	}
}
