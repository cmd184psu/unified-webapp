package schema

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

const goodReview = `{"verdict":"pass","summary":"ok","issues":[],"repair_prompt":""}`

func TestExtractFinalShapes(t *testing.T) {
	cases := map[string]string{
		"fenced":          "Here you go:\n```json\n" + goodReview + "\n```\nthanks",
		"bare":            goodReview,
		"prose first":     "I will use {braces} in prose. " + goodReview,
		"brace in string": `{"verdict":"pass","summary":"a } and { b","issues":[],"repair_prompt":""}`,
		"trailing prose":  goodReview + "\n\nLet me know if you want {more} detail.",
	}
	for name, in := range cases {
		t.Run(name, func(t *testing.T) {
			raw, err := ExtractFinal(in)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := Parse[ReviewOut](raw); err != nil {
				t.Fatalf("parse: %v (%s)", err, raw)
			}
		})
	}
	if _, err := ExtractFinal("no json here {unclosed"); !errors.Is(err, ErrNoObject) {
		t.Fatalf("want ErrNoObject, got %v", err)
	}
}

func TestExtractFinalPrefersLastObject(t *testing.T) {
	raw, _ := ExtractFinal(`{"a":1} {"b":2}`)
	if string(raw) != `{"b":2}` {
		t.Fatalf("got %s", raw)
	}
	// Nested objects are not top-level candidates.
	raw, _ = ExtractFinal(`{"a":{"b":2}}`)
	if string(raw) != `{"a":{"b":2}}` {
		t.Fatalf("got %s", raw)
	}
}

func TestExtractFinalQuotedExampleThenRealAnswer(t *testing.T) {
	reply := "The contract says to answer like " + goodReview + ".\n\nMy answer:\n```json\n" +
		`{"verdict":"repair","summary":"s","issues":[],"repair_prompt":"fix it"}` + "\n```\n"
	raw, err := ExtractFinal(reply)
	if err != nil {
		t.Fatal(err)
	}
	r, err := Parse[ReviewOut](raw)
	if err != nil || r.Verdict != "repair" {
		t.Fatalf("%+v %v", r, err)
	}
}

func TestReviewBlocked(t *testing.T) {
	r, err := Parse[ReviewOut](json.RawMessage(`{"verdict":"","summary":"could not read files","issues":[],"repair_prompt":"","blocked":"files unreadable"}`))
	if err != nil || r.Blocked == "" {
		t.Fatalf("%+v %v", r, err)
	}
	if _, err := Parse[ReviewOut](json.RawMessage(`{"summary":"s","issues":[],"repair_prompt":"","blocked":"x"}`)); err != nil {
		t.Fatalf("omitted verdict must be allowed when blocked: %v", err)
	}
	if _, err := Parse[ReviewOut](json.RawMessage(`{"verdict":"pass","summary":"s","issues":[],"repair_prompt":"","blocked":"x"}`)); err == nil {
		t.Fatal("blocked with an invented verdict must be rejected")
	}
	if _, err := Parse[ReviewOut](json.RawMessage(`{"verdict":"","summary":"s","issues":[],"repair_prompt":""}`)); err == nil {
		t.Fatal("empty verdict without blocked must be rejected")
	}
	if _, err := Parse[ReviewOut](json.RawMessage(`{"verdict":"","issues":[],"repair_prompt":"","blocked":"x"}`)); err == nil {
		t.Fatal("summary stays required when blocked")
	}
}

func TestParseFencedReviewerReply(t *testing.T) {
	reply := "```json\n{\"verdict\":\"repair\",\"summary\":\"s\",\"issues\":[{\"description\":\"d\",\"severity\":\"major\",\"file\":\"a.go\",\"line\":3}],\"repair_prompt\":\"fix a.go\"}\n```"
	raw, err := ExtractFinal(reply)
	if err != nil {
		t.Fatal(err)
	}
	r, err := Parse[ReviewOut](raw)
	if err != nil {
		t.Fatal(err)
	}
	if r.Verdict != "repair" || r.Issues[0].Line != 3 {
		t.Fatalf("unexpected parse %+v", r)
	}
}

func TestParseRejectsBadVerdictNamingField(t *testing.T) {
	_, err := Parse[ReviewOut](json.RawMessage(`{"verdict":"maybe","summary":"s","issues":[],"repair_prompt":""}`))
	var ve *ValidationError
	if !errors.As(err, &ve) || ve.Field != "verdict" || !strings.Contains(err.Error(), "verdict") {
		t.Fatalf("want validation error naming verdict, got %v", err)
	}
}

func TestParseRejectsUnknownField(t *testing.T) {
	_, err := Parse[ReviewOut](json.RawMessage(`{"verdict":"pass","summary":"s","issues":[],"repair_prompt":"","extra":1}`))
	if err == nil || !strings.Contains(err.Error(), "extra") {
		t.Fatalf("want unknown-field error, got %v", err)
	}
}

func TestParseRejectsTrailingData(t *testing.T) {
	if _, err := Parse[WorkerOut](json.RawMessage(`{"summary":"s"} {"summary":"t"}`)); err == nil {
		t.Fatal("want trailing-data error")
	}
}

func TestParseRepairNeedsRepairPrompt(t *testing.T) {
	_, err := Parse[ReviewOut](json.RawMessage(`{"verdict":"repair","summary":"s","issues":[],"repair_prompt":""}`))
	if err == nil || !strings.Contains(err.Error(), "repair_prompt") {
		t.Fatalf("got %v", err)
	}
}

func TestParseIssueSeverityEnum(t *testing.T) {
	_, err := Parse[PlanReviewOut](json.RawMessage(`{"verdict":"revise","issues":[{"description":"d","severity":"huge"}]}`))
	if err == nil || !strings.Contains(err.Error(), "issues[0].severity") {
		t.Fatalf("got %v", err)
	}
}

func TestWorkerOut(t *testing.T) {
	w, err := Parse[WorkerOut](json.RawMessage(`{"summary":"did it","files_changed":["a.go"],"learnings":[],"blocked":"x"}`))
	if err != nil || w.Blocked != "x" || w.FilesChanged[0] != "a.go" {
		t.Fatalf("%+v %v", w, err)
	}
	if _, err := Parse[WorkerOut](json.RawMessage(`{"files_changed":[]}`)); err == nil {
		t.Fatal("summary is required")
	}
}

func TestPlanReviewVerdicts(t *testing.T) {
	for _, v := range []string{"approve", "revise", "reject"} {
		if _, err := Parse[PlanReviewOut](json.RawMessage(`{"verdict":"` + v + `","issues":[]}`)); err != nil {
			t.Fatalf("%s: %v", v, err)
		}
	}
	if _, err := Parse[PlanReviewOut](json.RawMessage(`{"verdict":"pass","issues":[]}`)); err == nil {
		t.Fatal("pass is not a plan verdict")
	}
}

func planJSON(t *testing.T, mut func(m map[string]any)) json.RawMessage {
	t.Helper()
	m := map[string]any{
		"principles": []string{"p"},
		"drivers":    []string{"d"},
		"options": []any{
			map[string]any{"name": "A", "description": "a"},
			map[string]any{"name": "B", "description": "b"},
		},
		"phases": []any{map[string]any{"id": "P1", "name": "n", "tasks_description": "t"}},
		"adr": map[string]any{
			"decision": "A", "drivers": []string{"d"}, "alternatives": []string{"B"},
			"why": "w", "consequences": []string{"c"}, "followups": []string{"f"},
		},
	}
	if mut != nil {
		mut(m)
	}
	b, err := json.Marshal(m)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func TestPlanOutValid(t *testing.T) {
	if _, err := Parse[PlanOut](planJSON(t, nil)); err != nil {
		t.Fatal(err)
	}
	// RP5 fields are optional but accepted.
	p, err := Parse[PlanOut](planJSON(t, func(m map[string]any) {
		m["premortem"] = []string{"a", "b", "c"}
		m["test_plan"] = []string{"unit"}
	}))
	if err != nil || len(p.Premortem) != 3 {
		t.Fatalf("%+v %v", p, err)
	}
}

func TestPlanOutRequiresTwoOptions(t *testing.T) {
	_, err := Parse[PlanOut](planJSON(t, func(m map[string]any) {
		m["options"] = []any{map[string]any{"name": "A", "description": "a"}}
	}))
	if err == nil || !strings.Contains(err.Error(), "options") {
		t.Fatalf("got %v", err)
	}
}

func TestPlanOutRequiresEveryADRField(t *testing.T) {
	for _, f := range []string{"decision", "drivers", "alternatives", "why", "consequences", "followups"} {
		t.Run(f, func(t *testing.T) {
			_, err := Parse[PlanOut](planJSON(t, func(m map[string]any) {
				delete(m["adr"].(map[string]any), f)
			}))
			if err == nil || !strings.Contains(err.Error(), "adr."+f) {
				t.Fatalf("want error naming adr.%s, got %v", f, err)
			}
		})
	}
}

func TestCompileOutRejectsVerifyField(t *testing.T) {
	in := `{"tasks":[{"id":"T001","phase":"P1","title":"t","prompt":"p","acceptance":["R1: x"],"files_touched":["a.go"],"verify":"go test"}]}`
	_, err := Parse[CompileOut](json.RawMessage(in))
	if err == nil || !strings.Contains(err.Error(), "verify") {
		t.Fatalf("want unknown verify field error, got %v", err)
	}
	ok := `{"tasks":[{"id":"T001","phase":"P1","title":"t","prompt":"p","acceptance":["R1: x"],"files_touched":["a.go"]}]}`
	if _, err := Parse[CompileOut](json.RawMessage(ok)); err != nil {
		t.Fatal(err)
	}
	empty := `{"tasks":[{"id":"T001","phase":"P1","title":"t","prompt":"p","acceptance":[],"files_touched":["a.go"]}]}`
	if _, err := Parse[CompileOut](json.RawMessage(empty)); err == nil {
		t.Fatal("acceptance is required")
	}
}

func TestCorrectiveMessage(t *testing.T) {
	_, err := Parse[ReviewOut](json.RawMessage(`{"verdict":"maybe","summary":"s","issues":[]}`))
	msg := CorrectiveMessage(err)
	if !strings.Contains(msg, "verdict") || !strings.Contains(msg, "exactly one JSON object") {
		t.Fatalf("got %q", msg)
	}
}

func TestRawAndParsedReturnedTogether(t *testing.T) {
	// SO3: the caller keeps the raw object next to the parsed value.
	raw, err := ExtractFinal(goodReview)
	if err != nil {
		t.Fatal(err)
	}
	r, err := Parse[ReviewOut](raw)
	if err != nil || string(raw) != goodReview || r.Verdict != "pass" {
		t.Fatalf("%s %+v %v", raw, r, err)
	}
}
