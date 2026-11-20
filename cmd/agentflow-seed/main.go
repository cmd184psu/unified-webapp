// Command agentflow-seed saves and approves a task queue for an effort of a smoke-* project
// in a running agentflow instance, skipping planning, through the server's
// POST /api/projects/{slug}/efforts/{id}/queue/seed. It is the supported seeding path behind
// scripts/agentflow-smoke.sh, for a known queue without any model.
//
//	agentflow-seed -url http://127.0.0.1:8788 -project smoke-x -effort 12 -tasks tasks.json
//
// tasks.json is a JSON array of task definitions ({"key","phase","title","prompt",
// "acceptance","files_touched","depends_on"}). The server refuses any project not named
// smoke-*: an approved queue skips every planning review. The database is never written from
// outside the server, which would make its own transactions fail with SQLITE_BUSY.
package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"
)

func main() {
	base := flag.String("url", "http://127.0.0.1:8788", "the agentflow instance")
	project := flag.String("project", "", "project slug (smoke-*)")
	effort := flag.Int64("effort", 0, "effort id")
	tasks := flag.String("tasks", "", "JSON file: an array of task definitions")
	flag.Parse()
	if err := run(strings.TrimRight(*base, "/"), *project, *effort, *tasks); err != nil {
		fmt.Fprintln(os.Stderr, "agentflow-seed:", err)
		os.Exit(1)
	}
}

func run(base, project string, effort int64, tasksFile string) error {
	if project == "" || effort <= 0 || tasksFile == "" {
		return fmt.Errorf("-project, -effort and -tasks are required")
	}
	b, err := os.ReadFile(tasksFile)
	if err != nil {
		return err
	}
	var defs []json.RawMessage
	if err := json.Unmarshal(b, &defs); err != nil {
		return fmt.Errorf("%s: %w", tasksFile, err)
	}
	body, _ := json.Marshal(map[string]any{"tasks": defs})
	url := fmt.Sprintf("%s/api/projects/%s/efforts/%d/queue/seed", base, project, effort)
	resp, err := (&http.Client{Timeout: 30 * time.Second}).Post(url, "application/json", bytes.NewReader(body))
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	out, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("%s: %d %s", url, resp.StatusCode, strings.TrimSpace(string(out)))
	}
	fmt.Printf("queue approved: %s effort %d, %d task(s)\n", project, effort, len(defs))
	return nil
}
