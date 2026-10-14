package engine

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"cmd184psu/unified-webapp/internal/agentflow/model"
	"cmd184psu/unified-webapp/internal/agentflow/runner"
	"cmd184psu/unified-webapp/internal/agentflow/store"
)

const (
	draftReply = `{"title":"Greeter","objective":"Print a greeting.","requirements":[{"description":"The program prints hello","priority":"must"},{"description":"The greeting name is configurable"}],` +
		`"acceptance_criteria":[{"criterion":"Running with no flag prints hello"},{"criterion":"go test passes"},{"criterion":"With --name Bo it prints hello Bo","requirements":["R2"]}],"out_of_scope":["translations"]}`
	polishReply = `{"objective":"Say hello.","requirements":[{"description":"Print hello"}],"acceptance_criteria":[{"criterion":"hello is printed"},{"criterion":"The build succeeds"}]}`
	splitReply  = `{"parts":[` +
		`{"title":"Core","objective":"Build the core.","requirements":[{"description":"Core works"}],"acceptance_criteria":[{"criterion":"Core output is right"}]},` +
		`{"title":"Flags","objective":"Add flags to the core.","requirements":[{"description":"Flags parse"}],"acceptance_criteria":[{"criterion":"All tests pass"},{"criterion":"Bad flag exits 2"}]},` +
		`{"title":"Docs","objective":"Document it.","requirements":[{"description":"Usage is documented"}],"acceptance_criteria":[{"criterion":"README shows usage"}]}]}`
)

func (f *planFix) repo() string {
	f.t.Helper()
	p, err := f.st.GetProject(context.Background(), f.slug)
	if err != nil {
		f.t.Fatal(err)
	}
	return p.RepoPath
}

func (f *planFix) draftState() DraftView {
	f.t.Helper()
	v, err := f.eng.DraftState(context.Background(), f.effort)
	if err != nil {
		f.t.Fatal(err)
	}
	return v
}

func (f *planFix) waitDraft(want string) DraftView {
	f.t.Helper()
	var v DraftView
	waitFor(f.t, "draft state "+want, func() bool { v = f.draftState(); return v.State == want })
	return v
}

func (f *planFix) latestFRD() (model.FRD, int) {
	f.t.Helper()
	d, err := f.st.GetDoc(context.Background(), f.effort, model.DocFRD, 0)
	if err != nil {
		f.t.Fatal(err)
	}
	frd, err := model.ParseFRD(d.JSON)
	if err != nil {
		f.t.Fatal(err)
	}
	return frd, d.Revision
}

// N2, N3, N1: a description becomes a new unapproved FRD revision. The prompt
// carries the description, the README head and the definition of done; the
// reply's boilerplate criterion is dropped by code; ids, links, status, date
// and the FRD id are filled by code.
func TestN2_DraftWritesAnFRDFromADescription(t *testing.T) {
	f := newPlanFix(t, false)
	f.standardPool()
	repo := f.repo()
	if err := os.WriteFile(filepath.Join(repo, "go.mod"), []byte("module x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repo, "README.md"), []byte("# Hello tool\nPrints things.\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	f.fake.Script("planner", runner.Step{Reply: draftReply})
	f.startEngine()
	ctx := context.Background()
	if err := f.eng.StartDraft(ctx, "alice", f.effort, DraftRequest{Purpose: PurposeDraft, Description: "make a greeter"}); err != nil {
		t.Fatal(err)
	}
	if v := f.draftState(); v.Purpose != PurposeDraft || v.Started == 0 {
		t.Fatalf("state after start = %+v", v)
	}
	if v := f.waitDraft("idle"); v.Error != "" {
		t.Fatalf("state = %+v", v)
	}

	calls := f.fake.CallsFor("planner")
	if len(calls) != 1 {
		t.Fatalf("planner calls = %d", len(calls))
	}
	for _, want := range []string{"make a greeter", "# Hello tool", "`go build ./...` succeeds.", "`go test ./...` succeeds.", "New code has tests.", "Never write a criterion that only restates"} {
		if !strings.Contains(calls[0].Prompt, want) {
			t.Fatalf("drafter prompt lacks %q:\n%s", want, calls[0].Prompt)
		}
	}
	if strings.Contains(calls[0].Prompt, "{{") {
		t.Fatalf("drafter prompt has an unfilled placeholder:\n%s", calls[0].Prompt)
	}
	if st := f.effortStages("planner"); len(st) != 1 || st[0].Role != "planner" || draftPurpose(st[0]) != PurposeDraft {
		t.Fatalf("stages = %+v", st)
	}

	frd, rev := f.latestFRD()
	if rev != 2 {
		t.Fatalf("FRD revision = %d, want a new one", rev)
	}
	if frd.ID != "FRD-"+itoa(f.effort) || frd.Title != "Greeter" || frd.Status != "draft" || frd.Created == "" || len(frd.OutOfScope) != 1 {
		t.Fatalf("FRD = %+v", frd)
	}
	if len(frd.Requirements) != 2 || frd.Requirements[0].ID != "R1" || frd.Requirements[1].ID != "R2" {
		t.Fatalf("requirements = %+v", frd.Requirements)
	}
	if len(frd.AcceptanceCriteria) != 2 {
		t.Fatalf("criteria = %+v, want the boilerplate one dropped", frd.AcceptanceCriteria)
	}
	if a := frd.AcceptanceCriteria[0]; a.ID != "AC1" || strings.Join(a.Requirements, ",") != "R1,R2" {
		t.Fatalf("unlinked criterion = %+v, want all requirements", a)
	}
	if a := frd.AcceptanceCriteria[1]; a.ID != "AC2" || strings.Join(a.Requirements, ",") != "R2" {
		t.Fatalf("linked criterion = %+v", a)
	}
	if _, err := f.st.ApprovedRevision(ctx, f.effort, model.DocFRD); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("a draft must not be approved: %v", err)
	}
}

func itoa(n int64) string { b, _ := json.Marshal(n); return string(b) }

// N2: polish tidies the existing FRD, keeps what the drafter does not write
// (id, constraints) and sends the FRD and the notes in the prompt.
func TestN2_PolishKeepsWhatTheDrafterDoesNotWrite(t *testing.T) {
	f := newPlanFix(t, false)
	f.standardPool()
	ctx := context.Background()
	if _, err := f.st.SaveDoc(ctx, "t", f.effort, model.DocFRD, []byte(`{"id":"FRD-KEEP","title":"Old","status":"approved","objective":"A long winded objective.","constraints":["stay small"],`+
		`"requirements":[{"id":"R1","description":"Print hello"},{"id":"R2","description":"Print hello again"}],"acceptance_criteria":[{"id":"AC1","criterion":"go test passes"}]}`)); err != nil {
		t.Fatal(err)
	}
	f.fake.Script("planner", runner.Step{Reply: polishReply})
	f.startEngine()
	if err := f.eng.StartDraft(ctx, "alice", f.effort, DraftRequest{Purpose: PurposePolish, Description: "merge the duplicate"}); err != nil {
		t.Fatal(err)
	}
	f.waitDraft("idle")
	p := f.fake.CallsFor("planner")[0].Prompt
	if !strings.Contains(p, "A long winded objective.") || !strings.Contains(p, "merge the duplicate") || !strings.Contains(p, "Tidy the existing FRD") {
		t.Fatalf("polish prompt:\n%s", p)
	}
	frd, rev := f.latestFRD()
	if rev != 3 || frd.ID != "FRD-KEEP" || frd.Title != "Old" || frd.Status != "draft" || frd.Objective != "Say hello." || len(frd.Constraints) != 1 {
		t.Fatalf("polished FRD (rev %d) = %+v", rev, frd)
	}
	if len(frd.Requirements) != 1 || len(frd.AcceptanceCriteria) != 1 || frd.AcceptanceCriteria[0].Criterion != "hello is printed" {
		t.Fatalf("polished body = %+v / %+v", frd.Requirements, frd.AcceptanceCriteria)
	}
}

// N4: split stores parts 2..N, makes part 1 the effort's FRD, drops the
// boilerplate criteria in every part and tells the model the token budget.
func TestN4_SplitStoresThePartsAndPartOneBecomesTheFRD(t *testing.T) {
	f := newPlanFix(t, false)
	f.standardPool()
	ctx := context.Background()
	f.setContext("gpu", "m1", 8000)
	f.fake.Script("planner", runner.Step{Reply: splitReply})
	f.startEngine()
	if err := f.eng.StartDraft(ctx, "alice", f.effort, DraftRequest{Purpose: PurposeSplit, MaxParts: 3}); err != nil {
		t.Fatal(err)
	}
	f.waitDraft("idle")
	p := f.fake.CallsFor("planner")[0].Prompt
	if !strings.Contains(p, "fewest ordered parts") || !strings.Contains(p, "at most 3 parts") || !strings.Contains(p, "stay under about 4000 tokens") || !strings.Contains(p, `{"parts"`) {
		t.Fatalf("split prompt:\n%s", p)
	}
	frd, rev := f.latestFRD()
	if rev != 2 || frd.Title != "Core" || frd.ID != "FRD-"+itoa(f.effort) {
		t.Fatalf("part 1 as FRD (rev %d) = %+v", rev, frd)
	}
	parts, err := f.st.ListFRDParts(ctx, f.effort)
	if err != nil || len(parts) != 2 || parts[0].N != 2 || parts[0].Title != "Flags" || parts[1].N != 3 || parts[1].Tokens == 0 || parts[0].Summary != "Add flags to the core." {
		t.Fatalf("parts = %+v (%v)", parts, err)
	}
	var second model.FRD
	if err := json.Unmarshal(parts[0].JSON, &second); err != nil || len(second.AcceptanceCriteria) != 1 || second.AcceptanceCriteria[0].Criterion != "Bad flag exits 2" {
		t.Fatalf("part 2 = %+v (%v), want the boilerplate criterion dropped", second, err)
	}
}

// N2: a failing stage leaves the FRD untouched and shows failed with the
// error; a new request is then accepted and can succeed.
func TestN2_FailedDraftLeavesTheFRDAndCanBeRetried(t *testing.T) {
	f := newPlanFix(t, false)
	f.standardPool()
	ctx := context.Background()
	if err := f.st.UpdateSettings(ctx, "t", map[string]string{"max_infra_retries": "1"}); err != nil {
		t.Fatal(err)
	}
	f.fake.Script("planner", runner.Step{Reply: "no json here"}, runner.Step{Reply: "still none"}, runner.Step{Reply: draftReply})
	f.startEngine()
	if err := f.eng.StartDraft(ctx, "alice", f.effort, DraftRequest{Purpose: PurposeDraft, Description: "x"}); err != nil {
		t.Fatal(err)
	}
	v := f.waitDraft("failed")
	if !strings.Contains(v.Error, "infra:unparseable") {
		t.Fatalf("error = %q", v.Error)
	}
	if _, rev := f.latestFRD(); rev != 1 {
		t.Fatalf("a failed draft changed the FRD: revision %d", rev)
	}
	if err := f.eng.StartDraft(ctx, "alice", f.effort, DraftRequest{Purpose: PurposeDraft, Description: "x"}); err != nil {
		t.Fatalf("retry after a failure: %v", err)
	}
	f.waitDraft("idle")
	if _, rev := f.latestFRD(); rev != 2 {
		t.Fatalf("revision after the retry = %d", rev)
	}
}

// N2: one active stage per effort. A second request while one runs is a
// conflict, as are plan generation, a polish without an FRD, a blank
// description and an effort that is no longer a draft.
func TestN2_DraftRulesAndConflicts(t *testing.T) {
	f := newPlanFix(t, true) // stages queue and never run
	f.standardPool()
	ctx := context.Background()
	if err := f.eng.StartDraft(ctx, "alice", f.effort, DraftRequest{Purpose: PurposeDraft, Description: "x"}); err != nil {
		t.Fatal(err)
	}
	if v := f.draftState(); v.State != "running" {
		t.Fatalf("state = %+v", v)
	}
	for name, err := range map[string]error{
		"second draft": f.eng.StartDraft(ctx, "alice", f.effort, DraftRequest{Purpose: PurposeDraft, Description: "y"}),
		"split":        f.eng.StartDraft(ctx, "alice", f.effort, DraftRequest{Purpose: PurposeSplit}),
		"plan":         f.eng.GeneratePlan(ctx, "alice", f.effort),
	} {
		if !errors.Is(err, store.ErrConflict) {
			t.Errorf("%s while drafting = %v, want conflict", name, err)
		}
	}

	if err := f.eng.StartDraft(ctx, "alice", f.effort, DraftRequest{Purpose: PurposeDraft}); store.HTTPStatus(err) != 400 {
		t.Fatalf("blank description = %v, want 400", err)
	}
	if err := f.eng.StartDraft(ctx, "alice", f.effort, DraftRequest{Purpose: "bogus"}); store.HTTPStatus(err) != 400 {
		t.Fatalf("unknown purpose = %v, want 400", err)
	}
	repo := t.TempDir()
	if err := os.Mkdir(filepath.Join(repo, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := f.st.CreateProject(ctx, "t", store.Project{Slug: "bare", RepoPath: repo, ModelScope: "any"}); err != nil {
		t.Fatal(err)
	}
	bare, err := f.st.CreateEffort(ctx, "t", "bare", "no frd", false)
	if err != nil {
		t.Fatal(err)
	}
	if err := f.eng.StartDraft(ctx, "alice", bare.ID, DraftRequest{Purpose: PurposePolish}); !errors.Is(err, store.ErrConflict) {
		t.Fatalf("polish without an FRD = %v, want conflict", err)
	}
	planning := model.EffortPlanning
	if _, err := f.st.UpdateEffort(ctx, "t", bare.ID, store.EffortPatch{Status: &planning}); err != nil {
		t.Fatal(err)
	}
	if err := f.eng.StartDraft(ctx, "alice", bare.ID, DraftRequest{Purpose: PurposeDraft, Description: "x"}); !errors.Is(err, store.ErrConflict) {
		t.Fatalf("draft while planning = %v, want conflict", err)
	}
}

func (f *planFix) setContext(backend, mdl string, tokens int) {
	f.t.Helper()
	b, err := f.st.GetBackend(context.Background(), backend)
	if err != nil {
		f.t.Fatal(err)
	}
	for i := range b.Models {
		if b.Models[i].Model == mdl {
			b.Models[i].ContextTokens = tokens
		}
	}
	if err := f.st.UpdateBackend(context.Background(), "t", b); err != nil {
		f.t.Fatal(err)
	}
}

func (f *planFix) size() SizeView {
	f.t.Helper()
	v, err := f.eng.FRDSize(context.Background(), f.effort)
	if err != nil {
		f.t.Fatal(err)
	}
	return v
}

// N4: tokens are a quarter of the JSON length; the budget is half the
// smallest known context among the planning roles' candidates (the project's
// own lists included); nothing is "over" while no context is known.
func TestN4_SizeEstimateAndBudgetRule(t *testing.T) {
	f := newPlanFix(t, true)
	f.standardPool()
	ctx := context.Background()
	d, _ := f.st.GetDoc(ctx, f.effort, model.DocFRD, 0)
	wantFRD := len(compactJSON(d.JSON)) / 4

	big := `{"id":"FRD-B","title":"t","objective":"o","requirements":[{"id":"R1","description":"` + strings.Repeat("x", 4000) + `"}]}`
	if _, err := f.st.SaveDoc(ctx, "t", f.effort, model.DocFRD, []byte(big)); err != nil {
		t.Fatal(err)
	}
	v := f.size()
	if v.FRDTokens < 1000 || v.FRDTokens > 1100 || v.PlanTokens != 0 || len(v.Models) != 3 {
		t.Fatalf("size = %+v (small FRD was %d)", v, wantFRD)
	}
	if v.ContextTokensMin != 0 || v.BudgetTokens != 0 || v.Over || !strings.Contains(v.Advice, "context") {
		t.Fatalf("unknown contexts: %+v", v)
	}

	f.setContext("gpu", "m1", 8000) // planner and compiler
	f.setContext("gpu", "m2", 4000) // architect
	v = f.size()
	if v.ContextTokensMin != 4000 || v.BudgetTokens != 2000 || v.Over {
		t.Fatalf("known contexts: %+v", v)
	}
	if !strings.HasPrefix(v.Advice, "Fits") {
		t.Fatalf("advice = %q", v.Advice)
	}

	// The project's own critic list wins over the global one.
	f.addBackend("small", 1, false, false, "tiny")
	f.setContext("small", "tiny", 1500)
	f.candidates("project", f.slug, "critic", "small/tiny")
	v = f.size()
	if v.ContextTokensMin != 1500 || v.BudgetTokens != 750 || !v.Over || !strings.Contains(v.Advice, "split") {
		t.Fatalf("project override: %+v", v)
	}
	var tiny *SizeModel
	for i := range v.Models {
		if v.Models[i].Model == "tiny" {
			tiny = &v.Models[i]
		}
	}
	if tiny == nil || tiny.ContextTokens != 1500 || tiny.Backend != "small" {
		t.Fatalf("models = %+v", v.Models)
	}

	// A plan over the budget also counts.
	f.setContext("small", "tiny", 100000)
	if _, err := f.st.SaveDoc(ctx, "t", f.effort, model.DocPlan, []byte(`{"phases":"`+strings.Repeat("y", 40000)+`"}`)); err != nil {
		t.Fatal(err)
	}
	v = f.size()
	if v.PlanTokens < 10000 || v.BudgetTokens != 2000 || !v.Over || !strings.Contains(v.Advice, "plan") {
		t.Fatalf("big plan: %+v", v)
	}
}

// N1: the worker and reviewer prompts carry the definition of done, derived
// from the files in the attempt's worktree, and the reviewer is told it is the
// standard; a project override replaces the derived items.
func TestN1_PromptsCarryTheDefinitionOfDone(t *testing.T) {
	fastPoll(t)
	h := newStageHarness(t)
	h.fake.Script("worker", runner.Step{Reply: workerOK}, runner.Step{Reply: workerOK})
	h.fake.Script("reviewer", runner.Step{Reply: reviewPass}, runner.Step{Reply: reviewPass})
	w := h.attemptStage("alpha", "worker")
	if err := os.WriteFile(filepath.Join(w.worktree, "go.mod"), []byte("module x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	h.run(w)
	r := h.attemptStage("alpha", "reviewer")
	if err := os.WriteFile(filepath.Join(r.worktree, "go.mod"), []byte("module x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	h.run(r)

	wp := h.fake.CallsFor("worker")[0].Prompt
	for _, want := range []string{"## Definition of done", "- `go build ./...` succeeds.", "- `go vet ./...` succeeds.", "- `go test ./...` succeeds.", "- New code has tests.", "not a task of its own"} {
		if !strings.Contains(wp, want) {
			t.Fatalf("worker prompt lacks %q:\n%s", want, wp)
		}
	}
	rp := h.fake.CallsFor("reviewer")[0].Prompt
	if !strings.Contains(rp, "## Definition of done") || !strings.Contains(rp, "- `go test ./...` succeeds.") || !strings.Contains(rp, "Judge the work against this standard") {
		t.Fatalf("reviewer prompt:\n%s", rp)
	}

	// A project override replaces the derived items.
	o := h.attemptStage("alpha", "worker")
	p, err := h.st.GetProject(context.Background(), o.payload.Project)
	if err != nil {
		t.Fatal(err)
	}
	p.DefinitionOfDone = []string{"The changelog is updated."}
	if err := h.st.UpdateProject(context.Background(), "t", p); err != nil {
		t.Fatal(err)
	}
	h.run(o)
	op := h.fake.CallsFor("worker")[1].Prompt
	if !strings.Contains(op, "- The changelog is updated.") || strings.Contains(op, "go build") || strings.Contains(op, "New code has tests") {
		t.Fatalf("override prompt:\n%s", op)
	}
}
