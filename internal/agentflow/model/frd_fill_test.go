package model

import (
	"encoding/json"
	"reflect"
	"testing"
	"time"
)

var fillDay = time.Date(2026, 10, 14, 9, 0, 0, 0, time.UTC)

func TestFillFRD_AssignsIdsLinksAndDefaults(t *testing.T) {
	f := FRD{
		Title: "t", Objective: "o",
		Requirements: []FRDRequirement{{Description: "a"}, {ID: "R2", Description: "b"}, {Description: "c"}},
		AcceptanceCriteria: []FRDAC{
			{Criterion: "x"},
			{ID: "AC1", Requirements: []string{"R2"}, Criterion: "y"},
			{Criterion: "z"},
		},
	}
	FillFRD(&f, 7, fillDay)
	if f.ID != "FRD-7" || f.Status != "draft" || f.Created != "2026-10-14" {
		t.Fatalf("defaults: %q %q %q", f.ID, f.Status, f.Created)
	}
	var reqs []string
	for _, r := range f.Requirements {
		reqs = append(reqs, r.ID)
	}
	if !reflect.DeepEqual(reqs, []string{"R1", "R2", "R3"}) {
		t.Fatalf("requirement ids = %v", reqs)
	}
	// AC1 is taken by the second criterion, so the others skip it.
	var acs []string
	for _, ac := range f.AcceptanceCriteria {
		acs = append(acs, ac.ID)
	}
	if !reflect.DeepEqual(acs, []string{"AC2", "AC1", "AC3"}) {
		t.Fatalf("criterion ids = %v", acs)
	}
	if !reflect.DeepEqual(f.AcceptanceCriteria[0].Requirements, reqs) || !reflect.DeepEqual(f.AcceptanceCriteria[1].Requirements, []string{"R2"}) {
		t.Fatalf("links = %v / %v", f.AcceptanceCriteria[0].Requirements, f.AcceptanceCriteria[1].Requirements)
	}
	if _, err := ParseFRD(mustJSON(t, f)); err != nil {
		t.Fatalf("filled FRD invalid: %v", err)
	}
}

func TestFillFRD_LeavesFilledFRDAlone(t *testing.T) {
	f := FRD{
		ID: "FRD-X", Title: "t", Status: "approved", Created: "2020-01-02", Objective: "o",
		Requirements:       []FRDRequirement{{ID: "REQ-9", Description: "a"}},
		AcceptanceCriteria: []FRDAC{{ID: "A", Requirements: []string{"REQ-9"}, Criterion: "x"}},
	}
	want := mustJSON(t, f)
	FillFRD(&f, 3, fillDay)
	if got := mustJSON(t, f); string(got) != string(want) {
		t.Fatalf("filled FRD changed:\n%s\n%s", want, got)
	}
}

func TestParseFRD_IdsOptionalReferencesChecked(t *testing.T) {
	ok := `{"title":"t","objective":"o","requirements":[{"description":"d"}],"acceptance_criteria":[{"criterion":"c"}]}`
	if _, err := ParseFRD([]byte(ok)); err != nil {
		t.Fatalf("FRD with no ids rejected: %v", err)
	}
	for name, bad := range map[string]string{
		"no description": `{"title":"t","objective":"o","requirements":[{"id":"R1"}]}`,
		"no criterion":   `{"title":"t","objective":"o","requirements":[{"description":"d"}],"acceptance_criteria":[{"id":"AC1"}]}`,
		"unknown link":   `{"title":"t","objective":"o","requirements":[{"id":"R1","description":"d"}],"acceptance_criteria":[{"requirements":["R9"],"criterion":"c"}]}`,
		"dup id":         `{"title":"t","objective":"o","requirements":[{"id":"R1","description":"d"},{"id":"R1","description":"e"}]}`,
	} {
		if _, err := ParseFRD([]byte(bad)); err == nil {
			t.Errorf("%s accepted", name)
		}
	}
}

func TestDropRestatedCriteria(t *testing.T) {
	restated := []string{
		"go test passes", "All tests pass.", "`go build ./...` succeeds", "The build succeeds without errors",
		"go vet is clean", "go build, go vet and go test pass", "Make sure all tests pass",
		"The project builds and all tests pass", "no test failures", "New code has tests",
		"No files outside the task's scope are changed.", "No secrets are added", "unit tests pass",
		"`go test ./...` succeeds.",
	}
	specific := []string{
		"Adding two numbers returns their sum",
		"go test ./calc -run TestAdd covers negative numbers",
		"The tests cover the empty input case",
		"The parser builds an AST for nested lists",
		"The CLI exits with code 2 on a bad flag",
	}
	f := FRD{}
	for _, c := range append(append([]string{}, restated...), specific...) {
		f.AcceptanceCriteria = append(f.AcceptanceCriteria, FRDAC{Criterion: c})
	}
	dod := []string{"`go test ./...` succeeds."}
	dropped := DropRestatedCriteria(&f, dod)
	if !reflect.DeepEqual(dropped, restated) {
		t.Fatalf("dropped:\n%q\nwant:\n%q", dropped, restated)
	}
	var kept []string
	for _, ac := range f.AcceptanceCriteria {
		kept = append(kept, ac.Criterion)
	}
	if !reflect.DeepEqual(kept, specific) {
		t.Fatalf("kept = %q", kept)
	}
}

func mustJSON(t *testing.T, v any) []byte {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return b
}
