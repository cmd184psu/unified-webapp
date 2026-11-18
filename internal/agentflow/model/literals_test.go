package model

import (
	"reflect"
	"testing"
)

const webcalcAsk = "Build a calculator web app in Go that runs locally on port 7070, with no authentication. " +
	"POST /api/add, /api/subtract, /api/multiply and /api/divide each take JSON; dividing by zero returns HTTP 400 with {\"error\": \"division by zero\"}. " +
	"The server (started with `go run .`) serves GET / and DELETE /api/history. Include Go tests."

func TestExtractLiterals(t *testing.T) {
	got := ExtractLiterals(webcalcAsk)
	want := []string{"7070", "/api/add", "/api/subtract", "/api/multiply", "/api/divide", "400", "error", "division by zero", "go run .", "/api/history"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %q\nwant %q", got, want)
	}
}

func TestExtractLiteralsIgnoresPlainProse(t *testing.T) {
	if got := ExtractLiterals("Add and/or remove items; tests are fast (about 10 s). Use either/or."); len(got) != 0 {
		t.Fatalf("plain prose gave literals %q", got)
	}
}

func TestMissingLiterals(t *testing.T) {
	lits := ExtractLiterals(webcalcAsk)
	frd := FRD{Objective: "A calculator", Requirements: []FRDRequirement{
		{Description: "POST /api/add, /api/subtract, /api/multiply, /api/divide return {result}."},
		{Description: "Division by zero returns HTTP 400 with an error: Division  By Zero."},
		{Description: "GET /api/history and DELETE /api/history; run with go run ."},
	}}
	got := MissingLiterals(lits, FRDText(frd))
	if !reflect.DeepEqual(got, []string{"7070"}) {
		t.Fatalf("missing = %q, want just the port", got)
	}
	frd.Requirements[0].Description += " Listens on port 7070."
	if got := MissingLiterals(lits, FRDText(frd)); len(got) != 0 {
		t.Fatalf("still missing %q", got)
	}
}

func TestTasksTextCoversPromptAcceptanceAndFiles(t *testing.T) {
	defs := []TaskDef{{Title: "t", Prompt: "p `a.go`", Acceptance: []string{"returns 400"}, FilesTouched: []string{"a.go"}}}
	if got := MissingLiterals([]string{"a.go", "400", "nope"}, TasksText(defs)); !reflect.DeepEqual(got, []string{"nope"}) {
		t.Fatalf("got %q", got)
	}
}

// The real request of the webcalc trial: no junk from the JSON examples or the bare "/".
func TestExtractLiteralsFromTheWebcalcRequest(t *testing.T) {
	ask := "serves GET /. All four are Go API calls: POST /api/add, /api/subtract each take JSON {\"a\": number, \"b\": number} and return " +
		"{\"result\": number}; dividing by zero returns HTTP 400 with {\"error\": \"division by zero\"}; (started with `go run .`) on port 7070."
	want := []string{"/api/add", "/api/subtract", "result", "400", "error", "division by zero", "go run .", "7070"}
	if got := ExtractLiterals(ask); !reflect.DeepEqual(got, want) {
		t.Fatalf("got %q\nwant %q", got, want)
	}
}
