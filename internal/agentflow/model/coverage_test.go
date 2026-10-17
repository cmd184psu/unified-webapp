package model

import (
	"strings"
	"testing"
)

func coverageFRD() FRD {
	return FRD{
		Requirements: []FRDRequirement{{ID: "R1", Description: "api"}, {ID: "R2", Description: "page"}},
		AcceptanceCriteria: []FRDAC{
			{ID: "AC1", Requirements: []string{"R1"}, Criterion: "add works"},
			{ID: "AC2", Requirements: []string{"R2"}, Criterion: "page served"},
		},
	}
}

func TestQueueCoverageProblems(t *testing.T) {
	ok := []TaskDef{
		{Key: "T001", Acceptance: []string{"R1: add works", "AC1: add"}, FilesTouched: []string{"main.go", "main_test.go"}},
		{Key: "T002", Acceptance: []string{"R2: page", "AC2: served"}, FilesTouched: []string{"page.go"}},
	}
	if p := QueueCoverageProblems(coverageFRD(), ok); len(p) != 0 {
		t.Fatalf("a complete queue has problems: %v", p)
	}

	cases := []struct {
		name string
		defs []TaskDef
		want string
	}{
		{"label not in the FRD", []TaskDef{
			{Key: "T001", Acceptance: []string{"R1: a", "AC1: a", "R18: invented"}},
			{Key: "T002", Acceptance: []string{"R2: p", "AC2: p"}},
		}, "R18"},
		{"requirement never cited", []TaskDef{
			{Key: "T001", Acceptance: []string{"R1: a", "AC1: a"}},
		}, "R2"},
		{"acceptance criterion never cited", []TaskDef{
			{Key: "T001", Acceptance: []string{"R1: a", "AC1: a"}},
			{Key: "T002", Acceptance: []string{"R2: p"}},
		}, "AC2"},
		{"go test in a tests directory", []TaskDef{
			{Key: "T001", Acceptance: []string{"R1: a", "AC1: a"}, FilesTouched: []string{"tests/math_test.go"}},
			{Key: "T002", Acceptance: []string{"R2: p", "AC2: p"}},
		}, "tests/math_test.go"},
		{"main.go comes after the first root Go task", []TaskDef{
			{Key: "T001", Acceptance: []string{"R1: a", "AC1: a"}, FilesTouched: []string{"go.mod", "calc.go", "calc_test.go"}},
			{Key: "T002", Acceptance: []string{"R2: p", "AC2: p"}, FilesTouched: []string{"main.go"}},
		}, "main.go comes later"},
	}
	for _, c := range cases {
		p := QueueCoverageProblems(coverageFRD(), c.defs)
		if len(p) == 0 || !strings.Contains(strings.Join(p, "\n"), c.want) {
			t.Errorf("%s: want a problem naming %q, got %v", c.name, c.want, p)
		}
	}
}

func TestQueueCoverageViaAcceptanceCriterion(t *testing.T) {
	// a requirement counts as covered when a cited criterion belongs to it
	defs := []TaskDef{
		{Key: "T001", Acceptance: []string{"AC1: add", "AC2: page"}},
	}
	if p := QueueCoverageProblems(coverageFRD(), defs); len(p) != 0 {
		t.Fatalf("criteria that link the requirements cover them: %v", p)
	}
}

func TestQueueCoverageSkipsFRDWithoutIDs(t *testing.T) {
	if p := QueueCoverageProblems(FRD{}, []TaskDef{{Key: "T001", Acceptance: []string{"x"}}}); len(p) != 0 {
		t.Fatalf("an FRD with no requirements has nothing to cover: %v", p)
	}
}
