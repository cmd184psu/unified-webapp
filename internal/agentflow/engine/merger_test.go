package engine

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"cmd184psu/unified-webapp/internal/agentflow/gitsvc"
	"cmd184psu/unified-webapp/internal/agentflow/model"
	"cmd184psu/unified-webapp/internal/agentflow/store"
)

func TestParseMergerReply(t *testing.T) {
	raw, outcome, err := parseFinal("merger", `{"summary":"kept both","files":["a.go"],"notes":[]}`, false)
	if err != nil || outcome != OutcomePass || len(raw) == 0 {
		t.Fatalf("parse merger: raw=%s outcome=%s err=%v", raw, outcome, err)
	}
	blocked, _, err := parseFinal("merger", `{"summary":"cannot hold","blocked":"two behaviours exclude each other","notes":["a.go: one cannot stay"]}`, false)
	if err != nil || len(blocked) == 0 {
		t.Fatalf("parse blocked merger: %s, %v", blocked, err)
	}
	if _, _, err := parseFinal("merger", `{"summary":"","blocked":""}`, false); err == nil {
		t.Fatal("a merger reply with no summary must be rejected")
	}
}

func TestConflictMarkers(t *testing.T) {
	dir := t.TempDir()
	write := func(name, body string) {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("left.go", "package a\n<<<<<<< HEAD\nleft\n=======\nright\n>>>>>>> task\n")
	write("clean.go", "package a\n")
	if got := conflictMarkers(dir, []string{"left.go", "clean.go"}); !slices.Equal(got, []string{"left.go"}) {
		t.Fatalf("markers: %v", got)
	}
	if got := conflictMarkers(dir, []string{"missing.go"}); len(got) != 0 {
		t.Fatalf("missing file: %v", got)
	}
}

func TestIncomingMissingKeepsBothSides(t *testing.T) {
	diff := "+++ b/a.go\n+func left() {}\n+func right() {}\n"
	read := func(string) (string, error) {
		return "package a\nfunc left() {}\nfunc right() {}\n", nil
	}
	if got := incomingMissing(diff, read, nil); len(got) != 0 {
		t.Fatalf("both sides kept, got missing: %v", got)
	}
	oneSide := func(string) (string, error) { return "package a\nfunc right() {}\n", nil }
	if got := incomingMissing(diff, oneSide, nil); !slices.Equal(got, []string{"a.go"}) {
		t.Fatalf("dropped left side: %v", got)
	}
	if got := incomingMissing(diff, oneSide, []string{"a.go: left is replaced by right"}); len(got) != 0 {
		t.Fatalf("reported drop must pass: %v", got)
	}
}

// TestAfterMergerKeepsBothSides builds a real two-branch conflict where each
// side appends to the same file, resolves it the way a merger must, and checks
// that afterMerger queues the merge reviewer, whose pass commits both sides so
// the deterministic merge then passes.
func TestAfterMergerKeepsBothSides(t *testing.T) {
	defs := []model.TaskDef{{Key: "T1", Phase: "P", Title: "first", Prompt: "do", Acceptance: []string{"ok"}}}
	c := newChainRun(t, nil, defs)
	c.candidates("global", "", "merger", "gpu/m2")
	c.candidates("global", "", "reviewer", "gpu/m2")
	ctx := context.Background()
	proj, err := c.st.GetProject(ctx, c.slug)
	if err != nil {
		t.Fatal(err)
	}
	ef, err := c.st.GetEffort(ctx, c.effort)
	if err != nil {
		t.Fatal(err)
	}
	task := c.tasks[0]
	branch, intWT, err := c.eng.ensureIntegration(ctx, proj, ef.ID)
	if err != nil {
		t.Fatal(err)
	}
	if ef, err = c.st.GetEffort(ctx, ef.ID); err != nil {
		t.Fatal(err)
	}
	head, err := c.deps().Git.Head(ctx, intWT)
	if err != nil {
		t.Fatal(err)
	}
	wt, err := c.deps().Git.AddTaskWorktree(ctx, c.repo, c.slug, effortName(ef.ID), taskName(task), head.Commit)
	if err != nil {
		t.Fatal(err)
	}
	att, err := c.st.StartAttempt(ctx, task.ID, model.AttemptNormal, wt.Path, "m1")
	if err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(wt.Path, "notes.txt"), "task side\n")
	if _, err := c.deps().Git.CommitAll(ctx, c.repo, wt.Path, "task side", gitsvc.Trailers{Task: taskName(task), Attempt: 1}); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(intWT, "notes.txt"), "base side\n")
	if _, err := c.deps().Git.CommitAll(ctx, c.repo, intWT, "base side", gitsvc.Trailers{Task: "base", Attempt: 1}); err != nil {
		t.Fatal(err)
	}
	taskBranch := gitsvc.TaskBranch(effortName(ef.ID), taskName(task))
	if _, err := c.deps().Git.Merge(ctx, c.repo, intWT, branch, taskBranch); !errors.Is(err, gitsvc.ErrConflict) {
		t.Fatalf("want a conflict, got %v", err)
	}
	files, err := c.deps().Git.PrepareConflictRepair(ctx, c.repo, wt.Path, branch)
	if err != nil || !slices.Contains(files, "notes.txt") {
		t.Fatalf("prepare conflict: %v, %v", files, err)
	}
	// The merger keeps both sides.
	writeFile(t, filepath.Join(wt.Path, "notes.txt"), "base side\ntask side\n")
	hash, err := c.st.ApprovedQueueHash(ctx, ef.ID)
	if err != nil {
		t.Fatal(err)
	}
	parsed := json.RawMessage(`{"summary":"kept both appends","files":["notes.txt"],"notes":[]}`)
	sg, err := c.st.CreateStage(ctx, store.StageSpec{Attempt: att.ID, Role: "merger", Backend: "gpu", Model: "m2", QueueHash: hash, PromptRef: mergerRef + task.Key})
	if err != nil {
		t.Fatal(err)
	}
	if err := c.st.FinishStage(ctx, sg.ID, OutcomePass, "resolved", parsed); err != nil {
		t.Fatal(err)
	}
	merger := model.Stage{Role: "merger", Backend: "gpu", Model: "m2", QueueHash: hash, PromptRef: mergerRef + task.Key, Parsed: parsed}
	merge := model.Stage{Role: "merge", Parsed: json.RawMessage(`{"kind":"conflict","files":["notes.txt"]}`)}
	ch := &chain{proj: proj, eff: ef, task: task, att: att, stages: []model.Stage{merge, merger}}
	prompt, err := c.eng.mergerReviewPrompt(ctx, c.slug, model.Stage{Effort: ef.ID, Attempt: att.ID, PromptRef: mergeReviewRef + task.Key})
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"base side", "task side"} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("merge review prompt lacks %q:\n%s", want, prompt)
		}
	}
	handled, err := c.eng.afterMerger(ctx, ch, merger)
	if err != nil || !handled {
		t.Fatalf("afterMerger handled=%v err=%v", handled, err)
	}
	review := model.Stage{Role: "reviewer", Backend: "gpu", Model: "m2", PromptRef: mergeReviewRef + task.Key,
		Parsed: json.RawMessage(`{"verdict":"pass","summary":"both appends kept","issues":[],"repair_prompt":""}`)}
	ch.stages = []model.Stage{merge, merger, review}
	if err := c.eng.afterMergeReview(ctx, ch, review); err != nil {
		t.Fatalf("afterMergeReview: %v", err)
	}
	body := gitT(t, c.repo, "show", taskBranch+":notes.txt")
	if !strings.Contains(body, "base side") || !strings.Contains(body, "task side") {
		t.Fatalf("resolution dropped a side: %q", body)
	}
	if _, err := c.deps().Git.Merge(ctx, c.repo, intWT, branch, taskBranch); err != nil {
		t.Fatalf("resolved merge still conflicts: %v", err)
	}
}

func TestMergerTimeoutAndMergeFiles(t *testing.T) {
	set := store.Settings{MergerTimeout: 17 * time.Minute, PlanTimeout: time.Minute}
	if got := roleTimeout(set, "merger"); got != 17*time.Minute {
		t.Fatalf("merger timeout: %s", got)
	}
	stages := []model.Stage{
		{Role: "merge", Parsed: json.RawMessage(`{"kind":"conflict","files":["a.go"]}`)},
		{Role: "merge", Parsed: json.RawMessage(`{"kind":"conflict","files":["b.go","c.go"]}`)},
	}
	if got := mergeConflictFiles(&chain{stages: stages}); !slices.Equal(got, []string{"b.go", "c.go"}) {
		t.Fatalf("newest conflict files: %v", got)
	}
}
