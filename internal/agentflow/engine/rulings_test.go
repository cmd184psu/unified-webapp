package engine

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"cmd184psu/unified-webapp/internal/agentflow/gitsvc"
	"cmd184psu/unified-webapp/internal/agentflow/model"
	"cmd184psu/unified-webapp/internal/agentflow/runner"
	"cmd184psu/unified-webapp/internal/agentflow/store"
	"cmd184psu/unified-webapp/internal/taskmaster/worker"
)

// reviewContradiction is a reviewer reply whose finding contradicts a ruling
// about the corruption distance factor.
const reviewContradiction = `{"verdict":"repair","summary":"the distance calculation should use connectivity","issues":[{"description":"the distance calculation should set ConnectedToCapital to true","severity":"blocker","file":"src/rules/corruption/distance.go","line":1}],"repair_prompt":"change ConnectedToCapital to true in the distance calculation"}`

// testRulingsFile is a minimal RULINGS.md used by the engine tests.
const testRulingsFile = `# Rulings (the oracle)
This file is the project's single source of truth. Only a ruling is authority; the owner can overrule anything; no ruling = follow the plan and report.

| id | topic | ruling | source | authority | areas | date |
|----|-------|--------|--------|-----------|-------|------|
| R-003 | corruption distance | The distance calculation keeps ConnectedToCapital = false (connectivity does NOT change the distance) | coord-fs3 | owner | corruption,engine | 2026-11-22 |
| R-PROC | process | Every stage reads this rulings file before acting. | coord-fs3 | owner | process | 2026-11-22 |
`

// reviewContradictionNotes is a progress note that contradicts another note.
const reviewContradictionNotes = "ConnectedToCapital must be set to true for correct distance"

// reviewOppositeNotes is a progress note that contradicts reviewContradictionNotes.
const reviewOppositeNotes = "ConnectedToCapital stays false: connectivity does not change distance"

// ---- Test 1: header for task touching corruption files (with area map) ----

// TestEngineRulingsHeader_CorruptionFiles checks that a reviewer prompt for a
// task touching src/rules/corruption/* files carries the ruling header with
// R-003 and R-PROC injected, the full file path named, and the area map used
// to match the prefix src/rules/corruption* -> corruption.
func TestEngineRulingsHeader_CorruptionFiles(t *testing.T) {
	worker.SetPollIntervalForTest(20 * time.Millisecond)
	defer worker.SetPollIntervalForTest(5 * time.Second)

	repo := t.TempDir()
	gitT(t, repo, "init", "-q", "-b", "main")
	writeFile(t, filepath.Join(repo, "README"), "readme\n")
	writeFile(t, filepath.Join(repo, "check.sh"), "exit 0\n")
	gitT(t, repo, "add", "-A")
	gitT(t, repo, "commit", "-q", "-m", "initial")

	// Write the rulings file into the repo.
	rulingsPath := filepath.Join(repo, "docs", "rulings")
	if err := os.MkdirAll(rulingsPath, 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(rulingsPath, "RULINGS.md"), testRulingsFile)
	gitT(t, repo, "add", "docs/rulings/RULINGS.md")
	gitT(t, repo, "commit", "-q", "-m", "add rulings")

	h := newHarness(t)
	dataDir := t.TempDir()
	git, err := gitsvc.New(dataDir)
	if err != nil {
		t.Fatal(err)
	}
	h.tweak = func(d *Deps) { d.Git, d.DataDir = git, dataDir }
	h.start()
	t.Cleanup(h.eng.Close)
	h.addBackend("gpu", 2, false, false, "m1", "m2")
	h.candidates("global", "", "worker", "gpu/m1")
	h.candidates("global", "", "reviewer", "gpu/m2")

	ctx := context.Background()
	slug := "proj"
	if err := h.st.CreateProject(ctx, "t", store.Project{
		Slug:         slug,
		RepoPath:     repo,
		ModelScope:   "any",
		FeedbackCmds: []string{"sh check.sh"},
	}); err != nil {
		t.Fatal(err)
	}
	// Seed the rulings settings.
	if err := h.st.UpdateSettings(ctx, "t", map[string]string{
		"rulings_file":          "docs/rulings/RULINGS.md",
		"rulings_file_area_map": `{"src/rules/corruption*":"corruption"}`,
	}); err != nil {
		t.Fatal(err)
	}
	ef, err := h.st.CreateEffort(ctx, "t", slug, "e", false)
	if err != nil {
		t.Fatal(err)
	}
	q, err := h.st.SaveQueue(ctx, "t", ef.ID, []model.TaskDef{
		{Key: "T1", Phase: "P", Title: "corruption distance", Prompt: "do", Acceptance: []string{"ok"},
			FilesTouched: []string{"src/rules/corruption/distance.go", "src/rules/corruption/helper.go"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := h.st.ApproveQueue(ctx, "t", ef.ID, q.Hash); err != nil {
		t.Fatal(err)
	}

	// Script one worker + reviewer.
	h.fake.Script("worker", runner.Step{Reply: workerOK, Edit: func(r runner.Request) error {
		name := filepath.Base(r.Workdir)
		return os.WriteFile(filepath.Join(r.Workdir, name+".txt"), []byte("work of "+name+"\n"), 0o644)
	}})
	h.fake.Script("reviewer", runner.Step{Reply: reviewPass})

	if err := h.eng.Start(ctx); err != nil {
		t.Fatal(err)
	}
	if err := h.eng.Runs().Start(ctx, "t", slug); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "reviewer called", func() bool { return len(h.fake.CallsFor("reviewer")) > 0 })

	call := h.fake.CallsFor("reviewer")[0]
	raw, err := os.ReadFile(call.Request.PromptPath)
	if err != nil {
		t.Fatal(err)
	}
	p := string(raw)

	// The header must name the full path and list R-003 + R-PROC.
	if !strings.Contains(p, "## Rulings (the oracle) injected for this task") {
		t.Fatalf("prompt lacks ruling header:\n%s", p)
	}
	if !strings.Contains(p, "docs/rulings/RULINGS.md") {
		t.Fatalf("prompt lacks rulings file path:\n%s", p)
	}
	if !strings.Contains(p, "| R-003 | corruption distance |") {
		t.Fatalf("prompt lacks R-003 in header:\n%s", p)
	}
	if !strings.Contains(p, "| R-PROC | process |") {
		t.Fatalf("prompt lacks R-PROC in header:\n%s", p)
	}
	// R-001 (non-matching area) must NOT be in the header. We don't have one
	// in this minimal file, but verify the header shape is right.
}

// ---- Test 2: no header when setting empty ----

// TestEngineRulingsHeader_EmptySetting checks that when rulings_file is empty
// (oracle off), the reviewer prompt carries no ruling header.
func TestEngineRulingsHeader_EmptySetting(t *testing.T) {
	worker.SetPollIntervalForTest(20 * time.Millisecond)
	defer worker.SetPollIntervalForTest(5 * time.Second)

	repo := t.TempDir()
	gitT(t, repo, "init", "-q", "-b", "main")
	writeFile(t, filepath.Join(repo, "README"), "readme\n")
	writeFile(t, filepath.Join(repo, "check.sh"), "exit 0\n")
	gitT(t, repo, "add", "-A")
	gitT(t, repo, "commit", "-q", "-m", "initial")

	h := newHarness(t)
	dataDir := t.TempDir()
	git, err := gitsvc.New(dataDir)
	if err != nil {
		t.Fatal(err)
	}
	h.tweak = func(d *Deps) { d.Git, d.DataDir = git, dataDir }
	h.start()
	t.Cleanup(h.eng.Close)
	h.addBackend("gpu", 2, false, false, "m1", "m2")
	h.candidates("global", "", "worker", "gpu/m1")
	h.candidates("global", "", "reviewer", "gpu/m2")

	ctx := context.Background()
	slug := "proj"
	if err := h.st.CreateProject(ctx, "t", store.Project{
		Slug:         slug,
		RepoPath:     repo,
		ModelScope:   "any",
		FeedbackCmds: []string{"sh check.sh"},
	}); err != nil {
		t.Fatal(err)
	}
	// No rulings_file setting (empty = off). Migration 49 seeds the default,
	// but we override to empty here to test the off path.
	if err := h.st.UpdateSettings(ctx, "t", map[string]string{
		"rulings_file": "",
	}); err != nil {
		t.Fatal(err)
	}
	ef, err := h.st.CreateEffort(ctx, "t", slug, "e", false)
	if err != nil {
		t.Fatal(err)
	}
	q, err := h.st.SaveQueue(ctx, "t", ef.ID, []model.TaskDef{
		{Key: "T1", Phase: "P", Title: "first", Prompt: "do", Acceptance: []string{"ok"},
			FilesTouched: []string{"src/rules/corruption/distance.go"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := h.st.ApproveQueue(ctx, "t", ef.ID, q.Hash); err != nil {
		t.Fatal(err)
	}

	h.fake.Script("worker", runner.Step{Reply: workerOK, Edit: func(r runner.Request) error {
		name := filepath.Base(r.Workdir)
		return os.WriteFile(filepath.Join(r.Workdir, name+".txt"), []byte("work of "+name+"\n"), 0o644)
	}})
	h.fake.Script("reviewer", runner.Step{Reply: reviewPass})

	if err := h.eng.Start(ctx); err != nil {
		t.Fatal(err)
	}
	if err := h.eng.Runs().Start(ctx, "t", slug); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "reviewer called", func() bool { return len(h.fake.CallsFor("reviewer")) > 0 })

	call := h.fake.CallsFor("reviewer")[0]
	raw, err := os.ReadFile(call.Request.PromptPath)
	if err != nil {
		t.Fatal(err)
	}
	p := string(raw)

	if strings.Contains(p, "## Rulings (the oracle) injected for this task") {
		t.Fatalf("prompt must NOT carry a ruling header when setting is empty:\n%s", p)
	}
}

// ---- Test 3: reviewer repair contradicting a ruling is dropped ----

// TestEngineRulings_ReviewerRepairContradictsRuling checks that when a reviewer
// repair contradicts an injected ruling (R-003: ConnectedToCapital stays false),
// the repair is dropped (verdict pass), a coordinator note is appended, and a
// conflicting_guidance attention item is raised — the attempt is NOT failed.
func TestEngineRulings_ReviewerRepairContradictsRuling(t *testing.T) {
	worker.SetPollIntervalForTest(20 * time.Millisecond)
	defer worker.SetPollIntervalForTest(5 * time.Second)

	repo := t.TempDir()
	gitT(t, repo, "init", "-q", "-b", "main")
	writeFile(t, filepath.Join(repo, "README"), "readme\n")
	writeFile(t, filepath.Join(repo, "check.sh"), "exit 0\n")
	gitT(t, repo, "add", "-A")
	gitT(t, repo, "commit", "-q", "-m", "initial")

	rulingsPath := filepath.Join(repo, "docs", "rulings")
	if err := os.MkdirAll(rulingsPath, 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(rulingsPath, "RULINGS.md"), testRulingsFile)
	gitT(t, repo, "add", "docs/rulings/RULINGS.md")
	gitT(t, repo, "commit", "-q", "-m", "add rulings")

	h := newHarness(t)
	dataDir := t.TempDir()
	git, err := gitsvc.New(dataDir)
	if err != nil {
		t.Fatal(err)
	}
	h.tweak = func(d *Deps) { d.Git, d.DataDir = git, dataDir }
	h.start()
	t.Cleanup(h.eng.Close)
	h.addBackend("gpu", 2, false, false, "m1", "m2")
	h.candidates("global", "", "worker", "gpu/m1")
	h.candidates("global", "", "reviewer", "gpu/m2")

	ctx := context.Background()
	slug := "proj"
	if err := h.st.CreateProject(ctx, "t", store.Project{
		Slug:         slug,
		RepoPath:     repo,
		ModelScope:   "any",
		FeedbackCmds: []string{"sh check.sh"},
	}); err != nil {
		t.Fatal(err)
	}
	if err := h.st.UpdateSettings(ctx, "t", map[string]string{
		"rulings_file":          "docs/rulings/RULINGS.md",
		"rulings_file_area_map": `{"src/rules/corruption*":"corruption"}`,
	}); err != nil {
		t.Fatal(err)
	}
	ef, err := h.st.CreateEffort(ctx, "t", slug, "e", false)
	if err != nil {
		t.Fatal(err)
	}
	q, err := h.st.SaveQueue(ctx, "t", ef.ID, []model.TaskDef{
		{Key: "T1", Phase: "P", Title: "corruption distance", Prompt: "do", Acceptance: []string{"ok"},
			FilesTouched: []string{"src/rules/corruption/distance.go"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := h.st.ApproveQueue(ctx, "t", ef.ID, q.Hash); err != nil {
		t.Fatal(err)
	}
	tasks, err := h.st.ListTasks(ctx, ef.ID)
	if err != nil {
		t.Fatal(err)
	}

	// Script worker + reviewer with a repair that contradicts R-003.
	h.fake.Script("worker", runner.Step{Reply: workerOK, Edit: func(r runner.Request) error {
		name := filepath.Base(r.Workdir)
		return os.WriteFile(filepath.Join(r.Workdir, name+".txt"), []byte("work of "+name+"\n"), 0o644)
	}})
	h.fake.Script("reviewer", runner.Step{Reply: reviewContradiction})

	if err := h.eng.Start(ctx); err != nil {
		t.Fatal(err)
	}
	if err := h.eng.Runs().Start(ctx, "t", slug); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "reviewer called", func() bool { return len(h.fake.CallsFor("reviewer")) > 0 })
	waitFor(t, "stage done", func() bool {
		as, err := h.st.ListAttempts(ctx, tasks[0].ID)
		if err != nil || len(as) == 0 {
			return false
		}
		stages, err := h.st.ListAttemptStages(ctx, as[0].ID)
		if err != nil {
			return false
		}
		for _, s := range stages {
			if s.Role == "reviewer" && s.State == model.StageDone {
				return true
			}
		}
		return false
	})

	// The reviewer stage should be pass (repair dropped), not repair.
	as, err := h.st.ListAttempts(ctx, tasks[0].ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(as) == 0 {
		t.Fatal("no attempts")
	}
	stages, err := h.st.ListAttemptStages(ctx, as[0].ID)
	if err != nil {
		t.Fatal(err)
	}
	var revStage model.Stage
	for i := range stages {
		if stages[i].Role == "reviewer" {
			revStage = stages[i]
		}
	}
	if revStage.ID == 0 {
		t.Fatal("no reviewer stage")
	}
	if revStage.Outcome != "pass" {
		t.Fatalf("reviewer outcome = %q, want pass (repair dropped)", revStage.Outcome)
	}

	// A coordinator note must have been appended to the task's progress log.
	entries, err := h.st.ListProgress(ctx, tasks[0].ID, 10)
	if err != nil {
		t.Fatal(err)
	}
	foundNote := false
	for _, e := range entries {
		if strings.Contains(e, "reviewer repair dropped") && strings.Contains(e, "R-003") {
			foundNote = true
			break
		}
	}
	if !foundNote {
		t.Fatalf("no coordinator note found in progress log: %v", entries)
	}

	// A conflicting_guidance attention item must exist.
	att, err := h.eng.Attention(ctx)
	if err != nil {
		t.Fatal(err)
	}
	foundAtt := false
	for _, a := range att {
		if a.Kind == "conflicting_guidance" && strings.Contains(a.Message, "reviewer repair dropped") {
			foundAtt = true
			break
		}
	}
	if !foundAtt {
		t.Fatalf("no conflicting_guidance attention item: %v", att)
	}

	// The effort must NOT be failed — the attempt should still be open for
	// the worker to retry (or the task to be resolved).
	ef2, _ := h.st.GetEffort(ctx, ef.ID)
	if ef2.Status == model.EffortComplete {
		t.Fatal("effort completed despite dropped repair (should still be running/paused)")
	}
}

// ---- Test 4: conflicting-notes attention item ----

// conflictingNoteA and conflictingNoteB are two progress notes that
// contradict each other. The contradiction heuristic (clausePolarityClash)
// requires a shared >=3-word phrase, so these share "the distance calculation".
const (
	conflictingNoteA = `the distance calculation must set ConnectedToCapital to true: the distance calculation should use connectivity`
	conflictingNoteB = `the distance calculation keeps ConnectedToCapital = false: the distance calculation does NOT use connectivity`
)

// TestEngineRulings_ConflictingNotesAttention checks that when two progress
// notes on one task contradict each other, a conflicting_guidance attention
// item is raised naming both.
func TestEngineRulings_ConflictingNotesAttention(t *testing.T) {
	worker.SetPollIntervalForTest(20 * time.Millisecond)
	defer worker.SetPollIntervalForTest(5 * time.Second)

	repo := t.TempDir()
	gitT(t, repo, "init", "-q", "-b", "main")
	writeFile(t, filepath.Join(repo, "README"), "readme\n")
	writeFile(t, filepath.Join(repo, "check.sh"), "exit 0\n")
	gitT(t, repo, "add", "-A")
	gitT(t, repo, "commit", "-q", "-m", "initial")

	h := newHarness(t)
	dataDir := t.TempDir()
	git, err := gitsvc.New(dataDir)
	if err != nil {
		t.Fatal(err)
	}
	h.tweak = func(d *Deps) { d.Git, d.DataDir = git, dataDir }
	h.start()
	t.Cleanup(h.eng.Close)
	h.addBackend("gpu", 2, false, false, "m1", "m2")
	h.candidates("global", "", "worker", "gpu/m1")
	h.candidates("global", "", "reviewer", "gpu/m2")

	ctx := context.Background()
	slug := "proj"
	if err := h.st.CreateProject(ctx, "t", store.Project{
		Slug:         slug,
		RepoPath:     repo,
		ModelScope:   "any",
		FeedbackCmds: []string{"sh check.sh"},
	}); err != nil {
		t.Fatal(err)
	}
	ef, err := h.st.CreateEffort(ctx, "t", slug, "e", false)
	if err != nil {
		t.Fatal(err)
	}
	q, err := h.st.SaveQueue(ctx, "t", ef.ID, []model.TaskDef{
		{Key: "T1", Phase: "P", Title: "first", Prompt: "do", Acceptance: []string{"ok"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := h.st.ApproveQueue(ctx, "t", ef.ID, q.Hash); err != nil {
		t.Fatal(err)
	}
	// The dispatcher sets the effort status to running when it dispatches the
	// first task, but only from queue_approved or ready. Set it explicitly so
	// the attention scan (which checks running/paused for the notes path) fires.
	st := model.EffortQueueAppr
	if _, err := h.st.UpdateEffort(ctx, "t", ef.ID, store.EffortPatch{Status: &st}); err != nil {
		t.Fatal(err)
	}
	tasks, err := h.st.ListTasks(ctx, ef.ID)
	if err != nil {
		t.Fatal(err)
	}

	// Script a worker so the effort starts running.
	h.fake.Script("worker", runner.Step{Reply: workerOK, Edit: func(r runner.Request) error {
		name := filepath.Base(r.Workdir)
		return os.WriteFile(filepath.Join(r.Workdir, name+".txt"), []byte("work of "+name+"\n"), 0o644)
	}})
	h.fake.Script("reviewer", runner.Step{Reply: reviewPass})

	// Append two contradictory progress notes directly.
	if err := h.st.AppendProgress(ctx, tasks[0].ID, 0, conflictingNoteA); err != nil {
		t.Fatal(err)
	}
	if err := h.st.AppendProgress(ctx, tasks[0].ID, 0, conflictingNoteB); err != nil {
		t.Fatal(err)
	}

	// Start the engine and the run so the effort is running and the notes scan fires.
	if err := h.eng.Start(ctx); err != nil {
		t.Fatal(err)
	}
	if err := h.eng.Runs().Start(ctx, "t", slug); err != nil {
		t.Fatal(err)
	}
	// Wait for the effort to be running.
	waitFor(t, "effort running", func() bool {
		ef2, err := h.st.GetEffort(ctx, ef.ID)
		if err != nil {
			return false
		}
		return ef2.Status == model.EffortRunning
	})
	// Give the engine a moment to process the notes.
	waitFor(t, "attention scanned", func() bool {
		att, err := h.eng.Attention(ctx)
		if err != nil {
			return false
		}
		for _, a := range att {
			if a.Kind == "conflicting_guidance" {
				return true
			}
		}
		return false
	})
	att, err := h.eng.Attention(ctx)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, a := range att {
		if a.Kind == "conflicting_guidance" &&
			(a.Message == "conflicting guidance between notes for T1 and T1" ||
				strings.Contains(a.Message, "conflicting guidance")) {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("no conflicting_guidance attention item for contradictory notes: %v", att)
	}
}
