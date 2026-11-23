package store

import (
	"testing"
)

func TestBacklogStateAndPriority(t *testing.T) {
	if backlogState("READY") != "ready" || backlogState("PROPOSED") != "ready" || backlogState(" proposed ") != "ready" {
		t.Fatalf("ready states: %s %s", backlogState("READY"), backlogState("PROPOSED"))
	}
	if backlogState("FED TASK-9") != "done" || backlogState("") != "done" || backlogState("DONE") != "done" {
		t.Fatal("non-ready status must be done")
	}
	if backlogPriority([]byte(`"first"`)) != 1 || backlogPriority([]byte(`3`)) != 3 || backlogPriority(nil) != 0 || backlogPriority([]byte(`"nope"`)) != 0 {
		t.Fatal("priority mapping")
	}
}
