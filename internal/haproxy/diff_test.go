package haproxy

import (
	"strings"
	"testing"
)

func TestUnifiedDiffEqualIsEmpty(t *testing.T) {
	if d := UnifiedDiff("a", "b", "x\ny\nz\n", "x\ny\nz\n"); d != "" {
		t.Errorf("expected empty diff for equal text, got:\n%s", d)
	}
}

func TestUnifiedDiffShowsChange(t *testing.T) {
	old := "line1\nline2\nline3\n"
	new := "line1\nCHANGED\nline3\n"
	d := UnifiedDiff("live/x", "candidate/x", old, new)
	if d == "" {
		t.Fatal("expected a non-empty diff")
	}
	if !strings.Contains(d, "--- live/x") || !strings.Contains(d, "+++ candidate/x") {
		t.Errorf("missing file headers:\n%s", d)
	}
	if !strings.Contains(d, "-line2") || !strings.Contains(d, "+CHANGED") {
		t.Errorf("diff does not show the changed line:\n%s", d)
	}
	if !strings.Contains(d, "@@") {
		t.Errorf("diff has no hunk header:\n%s", d)
	}
	// Unchanged context lines are carried with a leading space.
	if !strings.Contains(d, " line1") {
		t.Errorf("diff missing context line:\n%s", d)
	}
}

func TestUnifiedDiffAddition(t *testing.T) {
	d := UnifiedDiff("a", "b", "x\n", "x\ny\n")
	if !strings.Contains(d, "+y") {
		t.Errorf("expected added line, got:\n%s", d)
	}
}

func TestUnifiedDiffFromEmpty(t *testing.T) {
	d := UnifiedDiff("a", "b", "", "only\nnew\n")
	if d == "" {
		t.Fatal("expected a diff when old is empty and new is not")
	}
	if !strings.Contains(d, "+only") || !strings.Contains(d, "+new") {
		t.Errorf("expected all-added diff, got:\n%s", d)
	}
}

func TestUnifiedDiffPureDeletion(t *testing.T) {
	d := UnifiedDiff("a", "b", "keep\ndrop\n", "keep\n")
	if !strings.Contains(d, "-drop") {
		t.Errorf("expected deleted line, got:\n%s", d)
	}
}
