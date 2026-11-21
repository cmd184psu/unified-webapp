package engine

import (
	"strings"
	"testing"

	"encoding/json"

	"cmd184psu/unified-webapp/internal/agentflow/store"
	"cmd184psu/unified-webapp/internal/agentflow/verify"
)

func TestChooseReviewerAvoidsTheAuthorsFamily(t *testing.T) {
	cands := []store.Candidate{
		{Backend: "a", Model: "claude-sonnet"},
		{Backend: "b", Model: "deepseek-v4"},
		{Backend: "c", Model: "gpt-5"},
	}
	got, err := chooseReviewer(cands, "claude-opus", "")
	if err != nil {
		t.Fatal(err)
	}
	if got.Model != "deepseek-v4" {
		t.Fatalf("chose %s, want a different family", got.Model)
	}
	if _, err := chooseReviewer(cands[:1], "claude-opus", ""); err == nil || !strings.Contains(err.Error(), "no independent reviewer") {
		t.Fatalf("one-family pool: %v", err)
	}
	if _, err := chooseReviewer(cands, "", "gpt"); err != nil || got.Model == "" {
		t.Fatalf("explicit family: %v", err)
	}
}

func TestReviewFindingsCarryFailingTestOutput(t *testing.T) {
	rep := verify.VerifyReport{
		NewFailures: []string{"go test ./...|turn_test.go:1148"},
		Evidence:    []verify.Evidence{{Sig: "turn_test.go:1148", Text: "Player-2 gold = 12995093, oracle = 12995092"}},
	}
	got := reviewFindings(rep, nil)
	for _, want := range []string{"1. go test", "Player-2 gold = 12995093"} {
		if !strings.Contains(got, want) {
			t.Fatalf("findings lack %q:\n%s", want, got)
		}
	}
}

func TestWriteVerifyEvidencePutsFailingOutputInTheRepairPrompt(t *testing.T) {
	raw, err := json.Marshal(verify.VerifyReport{Evidence: []verify.Evidence{{
		Sig: "go test ./...|turn_test.go:1148", Text: "Player-2 gold = 12995093, oracle = 12995092",
	}}})
	if err != nil {
		t.Fatal(err)
	}
	var b strings.Builder
	writeVerifyEvidence(&b, raw)
	for _, want := range []string{"Failing test output", "turn_test.go:1148", "Player-2 gold = 12995093"} {
		if !strings.Contains(b.String(), want) {
			t.Fatalf("prompt lacks %q:\n%s", want, b.String())
		}
	}
}

func TestParseReviewReply(t *testing.T) {
	verdict, findings, err := parseReviewReply(`{"verdict":"pass","summary":"looks right","issues":[]}`)
	if err != nil || verdict != "PASS" || !strings.Contains(findings, "looks right") {
		t.Fatalf("pass: %q %q %v", verdict, findings, err)
	}
	verdict, findings, err = parseReviewReply(`{"verdict":"repair","summary":"bug","issues":[{"description":"off by one","file":"src/x.go","line":9}]}`)
	if err != nil || verdict != "CHANGES-REQUESTED" || !strings.Contains(findings, "src/x.go:9") {
		t.Fatalf("repair: %q %q %v", verdict, findings, err)
	}
}
