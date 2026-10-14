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
	if !reflect.DeepEqual(reqs, []string{"R3", "R2", "R4"}) {
		t.Fatalf("requirement ids = %v", reqs)
	}
	// Ids go above the highest one in use (R2, AC1), never into a gap.
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
	dropped, keptAll := DropRestatedCriteria(&f, dod)
	if keptAll {
		t.Fatal("keptAll set although specific criteria remain")
	}
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

// B4: only generic wording is a restatement; specific scope and test criteria
// stay, and the filter never empties the criteria.
func TestDropRestatedCriteria_GenericVersusSpecific(t *testing.T) {
	restated := []string{
		"No files outside of the task's declared files were changed", "no unrelated files changed", "No unrelated files were modified",
		"tests continue to pass", "Tests should pass", "all tests must pass", "go test -race ./... passes", "`go test -race -count=1 ./...` succeeds",
		"gofmt reports no diff", "The code is gofmt-clean", "gofmt -l reports no files",
	}
	specific := []string{
		"No files outside internal/agentflow/engine are changed",
		"go test ./calc -run TestAdd covers negatives",
	}
	f := FRD{}
	for _, c := range append(append([]string{}, restated...), specific...) {
		f.AcceptanceCriteria = append(f.AcceptanceCriteria, FRDAC{Criterion: c})
	}
	dropped, keptAll := DropRestatedCriteria(&f, nil)
	if keptAll || !reflect.DeepEqual(dropped, restated) || len(f.AcceptanceCriteria) != len(specific) {
		t.Fatalf("dropped %q keptAll %v kept %+v", dropped, keptAll, f.AcceptanceCriteria)
	}

	all := FRD{AcceptanceCriteria: []FRDAC{{Criterion: "go test passes"}, {Criterion: "tests continue to pass"}}}
	dropped, keptAll = DropRestatedCriteria(&all, nil)
	if !keptAll || len(dropped) != 0 || len(all.AcceptanceCriteria) != 2 {
		t.Fatalf("every criterion was removed: dropped %q keptAll %v kept %+v", dropped, keptAll, all.AcceptanceCriteria)
	}
}

// B3: ids are never reused and links to deleted requirements are dropped.
func TestFillFRD_NeverReusesIdsAndDropsDeadLinks(t *testing.T) {
	f := FRD{
		Title: "t", Objective: "o",
		Requirements: []FRDRequirement{{ID: "R2", Description: "b"}, {ID: "R6", Description: "f"}, {Description: "new"}},
		AcceptanceCriteria: []FRDAC{
			{ID: "AC4", Requirements: []string{"R1"}, Criterion: "linked to deleted R1"},
			{ID: "AC1", Requirements: []string{"R1", "R6"}, Criterion: "partly dead"},
			{Criterion: "new"},
		},
	}
	FillFRD(&f, 1, fillDay)
	if f.Requirements[2].ID != "R7" {
		t.Fatalf("new requirement id = %s, want R7", f.Requirements[2].ID)
	}
	if f.AcceptanceCriteria[2].ID != "AC5" {
		t.Fatalf("new criterion id = %s, want AC5", f.AcceptanceCriteria[2].ID)
	}
	if !reflect.DeepEqual(f.AcceptanceCriteria[0].Requirements, []string{"R2", "R6", "R7"}) {
		t.Fatalf("all-dead links must cover all: %v", f.AcceptanceCriteria[0].Requirements)
	}
	if !reflect.DeepEqual(f.AcceptanceCriteria[1].Requirements, []string{"R6"}) {
		t.Fatalf("partly dead links: %v", f.AcceptanceCriteria[1].Requirements)
	}
	raw, _ := json.Marshal(f)
	if _, err := ParseFRD(raw); err != nil {
		t.Fatal(err)
	}
}

func TestParseFilledFRD_AcceptsLinksWithoutIds(t *testing.T) {
	raw := `{"title":"t","objective":"o","requirements":[{"description":"a"}],"acceptance_criteria":[{"requirements":["R1"],"criterion":"c"}]}`
	if _, err := ParseFRD([]byte(raw)); err == nil {
		t.Fatal("ParseFRD alone should still reject a link to an id that is not there yet")
	}
	if _, err := ParseFilledFRD([]byte(raw)); err != nil {
		t.Fatal(err)
	}
}
