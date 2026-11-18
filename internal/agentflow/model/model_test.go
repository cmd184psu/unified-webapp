package model

import (
	"encoding/json"
	"os"
	"reflect"
	"strings"
	"testing"
)

func def(key string, deps ...string) TaskDef {
	return TaskDef{Key: key, Phase: "P1", Title: key, Prompt: "do " + key, Acceptance: []string{"ok"}, FilesTouched: []string{"a.go"}, DependsOn: deps}
}

func TestParseQueueRejectsVerifyField(t *testing.T) {
	_, err := ParseQueue([]byte(`{"tasks":[{"key":"T1","title":"t","prompt":"p","acceptance":["a"],"verify":"x"}]}`))
	if err == nil || !strings.Contains(err.Error(), "verify") {
		t.Fatalf("err = %v, want unknown field verify", err)
	}
	if _, err := ParseQueue([]byte(`{"tasks":[{"key":"T1","title":"t","prompt":"p","acceptance":["a"]}]}`)); err != nil {
		t.Fatalf("clean queue: %v", err)
	}
	if _, err := ParseQueue([]byte(`{"tasks":[]} {}`)); err == nil {
		t.Fatal("trailing data accepted")
	}
}

func TestValidateTasks(t *testing.T) {
	cases := map[string][]TaskDef{
		"empty":      nil,
		"duplicate":  {def("A"), def("A")},
		"dangling":   {def("A", "Z")},
		"cycle":      {def("A", "B"), def("B", "A")},
		"self cycle": {def("A", "A")},
		"dup dep":    {def("A"), def("B", "A", "A")},
		"no prompt":  {{Key: "A", Title: "t", Acceptance: []string{"x"}}},
	}
	for name, tasks := range cases {
		if err := ValidateTasks(tasks); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
	if err := ValidateTasks([]TaskDef{def("A"), def("B", "A"), def("C", "A", "B")}); err != nil {
		t.Fatalf("diamond: %v", err)
	}
}

func TestQueueHashStableAndSensitive(t *testing.T) {
	a := []TaskDef{def("A"), def("B", "A", "C"), def("C")}
	b := []TaskDef{def("C"), def("B", "C", "A"), def("A")}
	if QueueHash(a) != QueueHash(b) {
		t.Fatal("hash changed under reordering")
	}
	base := QueueHash(a)
	edits := map[string]func(d *TaskDef){
		"phase":      func(d *TaskDef) { d.Phase = "P2" },
		"title":      func(d *TaskDef) { d.Title += "x" },
		"prompt":     func(d *TaskDef) { d.Prompt += "x" },
		"acceptance": func(d *TaskDef) { d.Acceptance = append(d.Acceptance, "more") },
		"files":      func(d *TaskDef) { d.FilesTouched = nil },
		"deps":       func(d *TaskDef) { d.DependsOn = nil },
	}
	for name, edit := range edits {
		c := []TaskDef{def("A"), def("B", "A", "C"), def("C")}
		edit(&c[1])
		if QueueHash(c) == base {
			t.Errorf("hash unchanged after editing %s", name)
		}
	}
}

func TestFRDRoundTripsRealDocument(t *testing.T) {
	raw, err := os.ReadFile("../../../docs/agentflow-remediation/frd.json")
	if err != nil {
		t.Fatal(err)
	}
	f, err := ParseFRD(raw)
	if err != nil {
		t.Fatal(err)
	}
	if f.ID != "FRD-AF-REM-001" || len(f.Requirements) < 100 || len(f.AcceptanceCriteria) == 0 {
		t.Fatalf("parsed FRD looks wrong: %s %d reqs", f.ID, len(f.Requirements))
	}
	out, _ := json.Marshal(f)
	g, err := ParseFRD(out)
	if err != nil {
		t.Fatal(err)
	}
	if out2, _ := json.Marshal(g); string(out) != string(out2) {
		t.Fatal("marshal is not stable across a round trip")
	}
	for name, bad := range map[string]string{
		"no objective": `{"id":"a","title":"t","requirements":[{"id":"R1","description":"d"}]}`,
		"unknown ac":   `{"id":"a","title":"t","objective":"o","requirements":[{"id":"R1","description":"d"}],"acceptance_criteria":[{"id":"AC1","requirements":["R9"],"criterion":"c"}]}`,
		"dup req":      `{"id":"a","title":"t","objective":"o","requirements":[{"id":"R1","description":"d"},{"id":"R1","description":"d"}]}`,
	} {
		if _, err := ParseFRD([]byte(bad)); err == nil {
			t.Errorf("%s accepted", name)
		}
	}
}

var lim = Limits{MaxAttempts: 5, StuckThreshold: 3, MaxInfraRetries: 3, MaxPolicyViolations: 2}

func att(id int64, n int, outcome, fp string) Attempt {
	return Attempt{ID: id, N: n, Outcome: outcome, Fingerprint: fp}
}

func TestDeriveTaskStatus(t *testing.T) {
	fail := func(id int64, n int, fp string) Attempt { return att(id, n, OutcomeFailed, fp) }
	cases := []struct {
		name string
		in   TaskInput
		deps []Status
		want Status
	}{
		{"ready no deps", TaskInput{}, nil, StatusReady},
		{"ready deps done", TaskInput{}, []Status{StatusPassed, StatusOverridden}, StatusReady},
		{"blocked dep ready", TaskInput{}, []Status{StatusPassed, StatusReady}, StatusBlocked},
		{"blocked by halted dep", TaskInput{}, []Status{StatusStuck}, StatusBlocked},
		{"running via queued stage", TaskInput{Attempts: []Attempt{att(1, 1, "", "")}, Stages: []Stage{{Attempt: 1, State: StageQueued}}}, nil, StatusRunning},
		{"running via running stage after failure", TaskInput{Attempts: []Attempt{fail(1, 1, "a")}, Stages: []Stage{{Attempt: 1, State: StageRunning}}}, nil, StatusRunning},
		{"done stage is not running", TaskInput{Attempts: []Attempt{fail(1, 1, "a")}, Stages: []Stage{{Attempt: 1, State: StageDone}}}, nil, StatusRepair},
		{"repair after one failure", TaskInput{Attempts: []Attempt{fail(1, 1, "a")}}, nil, StatusRepair},
		{"repair after two same failures", TaskInput{Attempts: []Attempt{fail(1, 1, "a"), fail(2, 2, "a")}}, nil, StatusRepair},
		{"stuck after three same", TaskInput{Attempts: []Attempt{fail(1, 1, "a"), fail(2, 2, "a"), fail(3, 3, "a")}}, nil, StatusStuck},
		{"different fingerprints not stuck", TaskInput{Attempts: []Attempt{fail(1, 1, "a"), fail(2, 2, "b"), fail(3, 3, "a")}}, nil, StatusRepair},
		{"empty fingerprints not stuck", TaskInput{Attempts: []Attempt{fail(1, 1, ""), fail(2, 2, ""), fail(3, 3, "")}}, nil, StatusRepair},
		{"exhausted", TaskInput{Attempts: []Attempt{fail(1, 1, "a"), fail(2, 2, "b"), fail(3, 3, "c"), fail(4, 4, "d"), fail(5, 5, "e")}}, nil, StatusExhausted},
		{"passed", TaskInput{Attempts: []Attempt{fail(1, 1, "a"), att(2, 2, OutcomePassed, "")}}, nil, StatusPassed},
		{"overridden beats halted", TaskInput{Overridden: true, Attempts: []Attempt{att(1, 1, OutcomeBlocked, "")}}, nil, StatusOverridden},
		{"overridden beats blocked", TaskInput{Overridden: true}, []Status{StatusReady}, StatusOverridden},
		{"needs_input", TaskInput{Attempts: []Attempt{att(1, 1, OutcomeBlocked, "")}}, nil, StatusNeedsInput},
		{"needs_attention infra", TaskInput{InfraIDs: []int64{1, 2, 3}}, nil, StatusNeedsAttention},
		{"infra below limit", TaskInput{InfraIDs: []int64{1, 2}}, nil, StatusReady},
		{"needs_attention policy", TaskInput{Attempts: []Attempt{att(1, 1, OutcomePolicyViolation, ""), att(2, 2, OutcomePolicyViolation, "")}}, nil, StatusNeedsAttention},
		{"one policy violation repairs", TaskInput{Attempts: []Attempt{att(1, 1, OutcomePolicyViolation, "")}}, nil, StatusRepair},
		{"abandoned never counts", TaskInput{Attempts: []Attempt{att(1, 1, OutcomeAbandoned, ""), att(2, 2, OutcomeAbandoned, ""), att(3, 3, OutcomeAbandoned, "")}}, nil, StatusReady},
		{"abandoned between same failures", TaskInput{Attempts: []Attempt{fail(1, 1, "a"), att(2, 2, OutcomeAbandoned, ""), fail(3, 3, "a"), fail(4, 4, "a")}}, nil, StatusStuck},
		{"reset clears attempts", TaskInput{Attempts: []Attempt{fail(1, 1, "a"), fail(2, 2, "a"), fail(3, 3, "a")}, Resets: []Reset{{AfterAttemptID: 3}}}, nil, StatusReady},
		{"reset keeps later attempts", TaskInput{Attempts: []Attempt{fail(1, 1, "a"), fail(2, 2, "a"), fail(3, 3, "a"), fail(4, 4, "a")}, Resets: []Reset{{AfterAttemptID: 3}}}, nil, StatusRepair},
		{"reset clears infra", TaskInput{InfraIDs: []int64{1, 2, 3}, Resets: []Reset{{AfterInfraID: 3}}}, nil, StatusReady},
		{"reset clears needs_input", TaskInput{Attempts: []Attempt{att(1, 1, OutcomeBlocked, "")}, Resets: []Reset{{AfterAttemptID: 1}}}, nil, StatusReady},
		{"stage of pre-reset attempt ignored", TaskInput{Attempts: []Attempt{fail(1, 1, "a")}, Stages: []Stage{{Attempt: 1, State: StageRunning}}, Resets: []Reset{{AfterAttemptID: 1}}}, nil, StatusReady},
	}
	for _, c := range cases {
		if got := DeriveTaskStatus(c.in, c.deps, lim); got != c.want {
			t.Errorf("%s: got %s, want %s", c.name, got, c.want)
		}
	}
}

func TestDeriveAllHaltedBlocksOnlyDependents(t *testing.T) {
	stuck := []Attempt{att(1, 1, OutcomeFailed, "a"), att(2, 2, OutcomeFailed, "a"), att(3, 3, OutcomeFailed, "a")}
	tasks := []TaskInput{
		{Key: "A", Attempts: stuck},
		{Key: "B", Deps: []string{"A"}},
		{Key: "C", Deps: []string{"B"}},
		{Key: "D"},
		{Key: "E", Deps: []string{"D"}},
	}
	got := DeriveAll(tasks, lim)
	want := map[string]Status{"A": StatusStuck, "B": StatusBlocked, "C": StatusBlocked, "D": StatusReady, "E": StatusBlocked}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	// Override of the halted task unblocks its dependents.
	tasks[0].Overridden = true
	got = DeriveAll(tasks, lim)
	if got["A"] != StatusOverridden || got["B"] != StatusReady || got["C"] != StatusBlocked {
		t.Fatalf("after override: %v", got)
	}
}

func TestDeriveAllSurvivesCycles(t *testing.T) {
	got := DeriveAll([]TaskInput{{Key: "A", Deps: []string{"B"}}, {Key: "B", Deps: []string{"A"}}, {Key: "C"}}, lim)
	if got["A"] != StatusBlocked || got["B"] != StatusBlocked || got["C"] != StatusReady {
		t.Fatalf("got %v", got)
	}
}

func TestOpenAttemptShowsRunningEvenWithUnfinishedDeps(t *testing.T) {
	in := TaskInput{Attempts: []Attempt{att(1, 1, "", "")}}
	if got := DeriveTaskStatus(in, []Status{StatusReady}, lim); got != StatusRunning {
		t.Fatalf("got %s", got)
	}
}

func TestAllDone(t *testing.T) {
	if AllDone(nil) {
		t.Fatal("empty effort is not complete")
	}
	if !AllDone(map[string]Status{"A": StatusPassed, "B": StatusOverridden}) {
		t.Fatal("passed+overridden should be complete")
	}
	if AllDone(map[string]Status{"A": StatusPassed, "B": StatusStuck}) {
		t.Fatal("halted task should block completion")
	}
}
