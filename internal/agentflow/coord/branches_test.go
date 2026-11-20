package coord

import (
	"reflect"
	"testing"
)

func TestBranchesIn(t *testing.T) {
	resolve := func(sha string) []string {
		if sha == "6de6155" {
			return []string{"origin/af/af10"}
		}
		return nil
	}
	for _, tc := range []struct {
		text, fallback string
		want           []Ref
	}{
		{"DONE TASK-358 feature/pillage-web@abc1234 - what changed", "", []Ref{{"feature/pillage-web", "abc1234"}}},
		{"DONE TASK-361 af/af6b@6f5028a (AF-6b)", "", []Ref{{"af/af6b", "6f5028a"}}},
		{"DONE TASK-343: branch feature/eng-orders at 1a2b3c4d, gates green", "", []Ref{{"feature/eng-orders", "1a2b3c4d"}}},
		{"DONE TASK-347 pushed as commit 6de6155 (local worktree ~/work/af10)", "", []Ref{{"af/af10", "6de6155"}}},
		{"DONE TASK-12 feature/a and fix/b.", "", []Ref{{"feature/a", ""}, {"fix/b", ""}}},
		{"DONE TASK-9 merged into feature/diplomacy-screen, see fix/c", "", []Ref{{"fix/c", ""}}},
		{"DONE TASK-10 all done", "Review TASK-10: branch feature/from-review@deadbee1", []Ref{{"feature/from-review", "deadbee1"}}},
		{"DONE TASK-11 tests 100 passed in 2341 ms", "", nil},
	} {
		got := BranchesIn(tc.text, "feature/diplomacy-screen", resolve, tc.fallback)
		if !reflect.DeepEqual(got, tc.want) {
			t.Errorf("BranchesIn(%q) = %v, want %v", tc.text, got, tc.want)
		}
	}
}
