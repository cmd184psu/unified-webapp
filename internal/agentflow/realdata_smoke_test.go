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
	"sync"
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
	// No external HTTP leaves this harness: even with the integrations
	// stubbed below, a dial to a production host fails fast at a dead
	// proxy instead of reaching it. Localhost bypasses it for the stream
	// check's own test server.
	t.Setenv("HTTP_PROXY", "http://127.0.0.1:9/")
	t.Setenv("HTTPS_PROXY", "http://127.0.0.1:9/")
	t.Setenv("NO_PROXY", "127.0.0.1,localhost,::1")
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
	// Stub the production integrations on the COPY (the source backup is
	// untouched): no BotChat relay delivery, no macOS/command/URL notices,
	// and the peer poller deferred past any test duration. The runner is
	// already the scripted fake at build time, and the coordinator module
	// stays off (no coord.json in the temp data dir).
	stubCtx, stubCancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer stubCancel()
	if err := st.UpdateSettings(stubCtx, "realdata-smoke", map[string]string{
		"botchat_url": "", "botchat_key_file": "",
		"notify_macos": "off", "notify_command": "", "notify_url": "",
		"peer_poll_seconds": "3600",
	}); err != nil {
		t.Fatalf("stub integrations on the copy: %v", err)
	}
	_ = st.Close()

	h, err := buildWith(config.AgentflowConfig{StaticDir: t.TempDir(), DataDir: dir, SSEMaxSubscribers: 8}, openHost(t), runner.NewFake())
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	// Bounded shutdown: first let in-flight handlers drain (a wedged one
	// keeps burning in the background), then close with a deadline, so a
	// timed-out handler cannot race cleanup. Neither wait can hang the test.
	var inflight sync.WaitGroup
	t.Cleanup(func() {
		drained := make(chan struct{})
		go func() { inflight.Wait(); close(drained) }()
		select {
		case <-drained:
		case <-time.After(10 * time.Second):
			t.Log("smoke shutdown: a handler is still running after 10 s; closing anyway")
		}
		closed := make(chan error, 1)
		go func() { closed <- h.(io.Closer).Close() }()
		select {
		case cerr := <-closed:
			if cerr != nil {
				t.Logf("smoke shutdown: close: %v", cerr)
			}
		case <-time.After(15 * time.Second):
			t.Log("smoke shutdown: close did not finish in 15 s; leaving it in the background")
		}
	})
	serve := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h.ServeHTTP(w, r.WithContext(auth.WithPrincipal(r.Context(), auth.Principal{Method: "ldap", Subject: "alice"})))
	})
	// The reference mux answers which registered pattern a request reaches,
	// the same lookup the API walk uses: a request that lands on the /api/
	// catch-all, the static handler, or a different pattern than intended
	// is a routing failure, not a covered route.
	ref := http.NewServeMux()
	api.New(api.Deps{}).Register(ref)
	reaches := func(path string) (string, bool) {
		_, p := ref.Handler(httptest.NewRequest("GET", path, nil))
		return p, p != "" && p != "/api/"
	}

	type answer struct {
		code int
		body []byte
		took time.Duration
	}
	var problems []string
	var warnings int
	// getOne sends one GET with a 5 s budget and applies the smoke
	// thresholds: a 5xx, an answer at or over 5 s, or a timeout is a
	// recorded problem; over 2 s is a warning. A handler that ignores its
	// context (as the old attention scan did) is abandoned, not waited for:
	// its request keeps its own budget, so one wedged route cannot stall
	// the suite, and the shutdown below still bounds cleanup. Every route is
	// still measured and listed, so one wedge cannot hide another.
	getOne := func(want, path string) answer {
		if got, ok := reaches(path); !ok || got != want {
			problems = append(problems, fmt.Sprintf("%s: reaches %q, want registered pattern %q (no request sent)", path, got, want))
			return answer{code: -2}
		}
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		start := time.Now()
		inflight.Add(1)
		ch := make(chan answer, 1)
		go func() {
			defer inflight.Done()
			rec := httptest.NewRecorder()
			serve.ServeHTTP(rec, httptest.NewRequest("GET", path, nil).WithContext(ctx))
			select {
			case ch <- answer{rec.Code, rec.Body.Bytes(), time.Since(start)}:
			case <-ctx.Done():
			}
		}()
		select {
		case a := <-ch:
			switch {
			case a.code >= 500 || a.took >= 5*time.Second:
				problems = append(problems, fmt.Sprintf("%s -> %d after %s", want, a.code, a.took.Round(time.Millisecond)))
			case a.took >= 2*time.Second:
				warnings++
				t.Logf("WARNING %s -> %d took %s (> 2 s on real data)", want, a.code, a.took.Round(time.Millisecond))
			default:
				t.Logf("%s -> %d in %s", want, a.code, a.took.Round(time.Millisecond))
			}
			return a
		case <-ctx.Done():
			problems = append(problems, fmt.Sprintf("%s: no answer after 5 s (handler abandoned; still running in the background)", want))
			return answer{code: -1, took: time.Since(start)}
		}
	}
	// first returns a field of the first row of the JSON list at path. The
	// discovery call itself runs through getOne, so a slow or failed lookup
	// is checked like any other route; an empty or unusable result just
	// leaves the parameter empty for the caller to report explicitly.
	first := func(want, path, field string) string {
		a := getOne(want, path)
		if a.code != http.StatusOK {
			return ""
		}
		var rows []map[string]any
		if json.Unmarshal(a.body, &rows) != nil || len(rows) == 0 {
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
	// effort and that effort's first task, plus a review, seat task,
	// backend, peer and seat callsign.
	slug := first("GET /api/projects", "/api/projects", "slug")
	effort := ""
	if slug != "" {
		effort = first("GET /api/projects/{slug}/efforts", "/api/projects/"+slug+"/efforts", "id")
	}
	task := ""
	if slug != "" && effort != "" {
		task = first("GET /api/projects/{slug}/efforts/{id}/tasks", "/api/projects/"+slug+"/efforts/"+effort+"/tasks", "key")
	}
	review, seatTask := first("GET /api/reviews", "/api/reviews", "id"), first("GET /api/seat-tasks", "/api/seat-tasks", "id")
	backend, peer, seat := first("GET /api/backends", "/api/backends", "name"), first("GET /api/peers", "/api/peers", "name"), first("GET /api/seats", "/api/seats", "callsign")
	// First revision numbers per document kind, from the revisions lists.
	revOf := func(kind, path string) string {
		a := getOne("GET /api/projects/{slug}/efforts/{id}/"+kind+"/revisions", path)
		if a.code != http.StatusOK {
			return ""
		}
		var rows []map[string]any
		if json.Unmarshal(a.body, &rows) != nil || len(rows) == 0 {
			return ""
		}
		if n, ok := rows[0]["revision"].(float64); ok {
			return strconv.Itoa(int(n))
		}
		return ""
	}
	frdRev, planRev := "", ""
	if slug != "" && effort != "" {
		base := "/api/projects/" + slug + "/efforts/" + effort
		frdRev, planRev = revOf("frd", base+"/frd/revisions"), revOf("plan", base+"/plan/revisions")
	}
	// One real stage id from the task detail's attempt list, for the stage
	// stream's startup check.
	stage := ""
	if task != "" {
		var detail struct {
			AttemptList []struct {
				Stages []struct {
					ID int64 `json:"id"`
				} `json:"stages"`
			} `json:"attempt_list"`
		}
		a := getOne("GET /api/projects/{slug}/efforts/{id}/tasks/{key}", "/api/projects/"+slug+"/efforts/"+effort+"/tasks/"+task)
		if a.code == http.StatusOK && json.Unmarshal(a.body, &detail) == nil {
			for _, at := range detail.AttemptList {
				if len(at.Stages) > 0 {
					stage = strconv.FormatInt(at.Stages[0].ID, 10)
					break
				}
			}
		}
	}

	var skipped int
	skip := func(pattern, what string) {
		skipped++
		t.Logf("SKIP %s: no %s in this fixture database; route not exercised", pattern, what)
	}
	// substitute fills a registered pattern's parameters from the real rows
	// above. A parameter with no real row is reported by the caller, never
	// silently sent empty (empty values miss their pattern: the 307s and
	// catch-all 404s of the old harness).
	substitute := func(pattern, path string) (string, string, bool) {
		use := path
		if strings.Contains(use, "{slug}") {
			if slug == "" {
				return "", "project slug", false
			}
			use = strings.ReplaceAll(use, "{slug}", slug)
		}
		if strings.Contains(use, "{id}") {
			var id, what string
			switch {
			case strings.HasPrefix(path, "/api/reviews/"):
				id, what = review, "review id"
			case strings.HasPrefix(path, "/api/seat-tasks/"):
				id, what = seatTask, "seat-task id"
			default:
				id, what = effort, "effort id"
			}
			if id == "" {
				return "", what, false
			}
			use = strings.ReplaceAll(use, "{id}", id)
		}
		if strings.Contains(use, "{name}") {
			if strings.HasPrefix(path, "/api/peers/") {
				if peer == "" {
					return "", "peer name", false
				}
				use = strings.ReplaceAll(use, "{name}", peer)
			} else {
				if backend == "" {
					return "", "backend name", false
				}
				use = strings.ReplaceAll(use, "{name}", backend)
			}
		}
		if strings.Contains(use, "{key}") {
			if task == "" {
				return "", "task key", false
			}
			use = strings.ReplaceAll(use, "{key}", task)
		}
		if strings.Contains(use, "{n}") {
			n, kind := frdRev, "FRD"
			if strings.Contains(path, "/plan/") {
				n, kind = planRev, "plan"
			}
			if n == "" {
				return "", kind + " revision", false
			}
			use = strings.ReplaceAll(use, "{n}", n)
		}
		if strings.Contains(use, "{callsign}") {
			if seat == "" {
				return "", "seat callsign", false
			}
			use = strings.ReplaceAll(use, "{callsign}", seat)
		}
		if strings.ContainsAny(use, "{}") {
			t.Fatalf("smoke harness bug: unsubstituted placeholder in %s", use)
		}
		return use, "", true
	}

	exercised := 0
	for _, p := range api.Patterns() {
		method, path, ok := strings.Cut(p, " ")
		if !ok || method != "GET" {
			continue
		}
		if path == "/api/events" || path == "/api/stages/{id}/stream" {
			continue // covered below with a startup bound
		}
		use, what, ok := substitute(p, path)
		if !ok {
			skip(p, what)
			continue
		}
		getOne(p, use)
		exercised++
	}
	// Streams never answer: bound them through the first event byte or the
	// expected terminal response, with the same thresholds. The live server
	// observes true first-byte timing; bodies close promptly so nothing
	// outlives its budget.
	srv := httptest.NewServer(serve)
	t.Cleanup(func() {
		done := make(chan struct{})
		go func() { srv.Close(); close(done) }()
		select {
		case <-done:
		case <-time.After(15 * time.Second):
			t.Log("smoke shutdown: stream server did not close in 15 s; leaving it in the background")
		}
	})
	stream := func(want, path string) {
		if got, ok := reaches(path); !ok || got != want {
			problems = append(problems, fmt.Sprintf("%s: reaches %q, want registered pattern %q (no request sent)", path, got, want))
			return
		}
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		req, err := http.NewRequestWithContext(ctx, "GET", srv.URL+path, nil)
		if err != nil {
			problems = append(problems, fmt.Sprintf("%s: stream request: %v", want, err))
			return
		}
		start := time.Now()
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			problems = append(problems, fmt.Sprintf("%s: stream request failed: %v", want, err))
			return
		}
		firstByte := make(chan error, 1)
		go func() {
			var one [1]byte
			_, rerr := io.ReadFull(resp.Body, one[:])
			firstByte <- rerr // nil: first event byte; EOF: terminal response with no event
		}()
		select {
		case rerr := <-firstByte:
			took := time.Since(start)
			resp.Body.Close()
			switch {
			case resp.StatusCode >= 500 || took >= 5*time.Second:
				problems = append(problems, fmt.Sprintf("%s -> %d after %s", want, resp.StatusCode, took.Round(time.Millisecond)))
			case took >= 2*time.Second:
				warnings++
				t.Logf("WARNING %s: stream started after %s (> 2 s on real data)", want, took.Round(time.Millisecond))
			case rerr == nil && resp.StatusCode == http.StatusOK:
				t.Logf("%s: first event in %s (status %d)", want, took.Round(time.Millisecond), resp.StatusCode)
			default:
				t.Logf("%s: terminal response %d in %s", want, resp.StatusCode, took.Round(time.Millisecond))
			}
		case <-ctx.Done():
			resp.Body.Close()
			<-firstByte
			problems = append(problems, fmt.Sprintf("%s: no first event after 5 s (stream abandoned)", want))
		}
	}
	stream("GET /api/events", "/api/events")
	if stage == "" {
		skip("GET /api/stages/{id}/stream", "stage id")
	} else {
		exercised++
		stream("GET /api/stages/{id}/stream", "/api/stages/"+stage+"/stream")
	}
	t.Logf("smoke: exercised %d routes, %d explicit skips, %d slow-route warnings", exercised, skipped, warnings)
	if len(problems) > 0 {
		t.Fatalf("%d read-only route(s) failed on real data:\n  %s", len(problems), strings.Join(problems, "\n  "))
	}
}
