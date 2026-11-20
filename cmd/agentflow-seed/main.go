// Command agentflow-seed saves and approves a task queue for an effort in a running agentflow
// instance's database, skipping planning. It is the supported seeding path for
// scripts/agentflow-smoke.sh, which needs a known one-task queue without any model.
//
//	agentflow-seed -db <data dir>/agentflow.db -effort 12 -tasks tasks.json
//
// tasks.json is a JSON array of task definitions ({"key","phase","title","prompt",
// "acceptance","files_touched","depends_on"}). The database must already be at this build's
// schema version (the tool never migrates), and only an effort of a project named smoke-* is
// seeded unless -any-project is given: an approved queue skips every planning review.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"strings"

	"cmd184psu/unified-webapp/internal/agentflow/model"
	"cmd184psu/unified-webapp/internal/agentflow/store"
)

func main() {
	db := flag.String("db", "", "the instance's agentflow.db")
	effort := flag.Int64("effort", 0, "effort id")
	tasks := flag.String("tasks", "", "JSON file: an array of task definitions")
	actor := flag.String("actor", "agentflow-seed", "actor recorded on the queue events")
	anyProject := flag.Bool("any-project", false, "seed an effort of a project not named smoke-*")
	flag.Parse()
	if err := run(*db, *effort, *tasks, *actor, *anyProject); err != nil {
		fmt.Fprintln(os.Stderr, "agentflow-seed:", err)
		os.Exit(1)
	}
}

func run(db string, effort int64, tasksFile, actor string, anyProject bool) error {
	if db == "" || effort <= 0 || tasksFile == "" {
		return fmt.Errorf("-db, -effort and -tasks are required")
	}
	b, err := os.ReadFile(tasksFile)
	if err != nil {
		return err
	}
	var defs []model.TaskDef
	if err := json.Unmarshal(b, &defs); err != nil {
		return fmt.Errorf("%s: %w", tasksFile, err)
	}
	st, err := store.OpenCurrent(db)
	if err != nil {
		return err
	}
	defer st.Close()
	ctx := context.Background()
	ef, err := st.GetEffort(ctx, effort)
	if err != nil {
		return fmt.Errorf("effort %d: %w", effort, err)
	}
	if !anyProject && !strings.HasPrefix(ef.Project, "smoke-") {
		return fmt.Errorf("effort %d belongs to project %q, not a smoke-* project (pass -any-project to seed it anyway)", effort, ef.Project)
	}
	q, err := st.SaveQueue(ctx, actor, effort, defs)
	if err != nil {
		return err
	}
	if _, err := st.ApproveQueue(ctx, actor, effort, q.Hash); err != nil {
		return err
	}
	fmt.Printf("queue approved: effort %d, %d task(s), hash %s\n", effort, len(defs), q.Hash)
	return nil
}
