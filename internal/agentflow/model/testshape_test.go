package model

import (
	"strings"
	"testing"
)

func TestQueueTestShapeProblems(t *testing.T) {
	code := TaskDef{Key: "T001", FilesTouched: []string{"main.go", "main_test.go"}}
	later := TaskDef{Key: "T002", DependsOn: []string{"T001"}, FilesTouched: []string{"main.go", "main_test.go"}}
	if p := QueueTestShapeProblems([]TaskDef{code, later}); len(p) != 0 {
		t.Fatalf("code with its tests: %v", p)
	}
	only := TaskDef{Key: "T003", DependsOn: []string{"T002"}, FilesTouched: []string{"main_test.go"}}
	p := QueueTestShapeProblems([]TaskDef{code, later, only})
	if len(p) != 1 || !strings.Contains(p[0], "T003") || !strings.Contains(p[0], "main.go (T001)") {
		t.Fatalf("tests-only task for code in the same directory: %v", p)
	}
	e2e := TaskDef{Key: "T003", DependsOn: []string{"T002"}, FilesTouched: []string{"e2e/flow_test.go"}}
	if p := QueueTestShapeProblems([]TaskDef{code, later, e2e}); len(p) != 0 {
		t.Fatalf("a test in another directory is allowed: %v", p)
	}
	unrelated := TaskDef{Key: "T003", FilesTouched: []string{"main_test.go"}}
	if p := QueueTestShapeProblems([]TaskDef{code, unrelated}); len(p) != 0 {
		t.Fatalf("a task not built on the code is not flagged: %v", p)
	}
}
