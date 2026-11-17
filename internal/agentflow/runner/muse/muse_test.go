package muse

import (
	"io"
	"strings"
	"testing"

	"cmd184psu/unified-webapp/internal/agentflow/runner"
)

func TestArgvFollowsTheRole(t *testing.T) {
	a := &Adapter{}
	w := strings.Join(a.Argv("muse", runner.Request{Role: "worker", Model: "muse-spark-1.3-contributor", Workdir: "/w", PromptPath: "/p"}), "\n")
	for _, want := range []string{"exec", "--json", "--prompt-file\n/p", "--workspace\n/w", "--approval-mode\nnever", "--disable-web-tools"} {
		if !strings.Contains(w, want) {
			t.Errorf("worker argv lacks %q:\n%s", want, w)
		}
	}
	if strings.Contains(w, "--disable-write") {
		t.Errorf("a worker must be able to write:\n%s", w)
	}
	r := strings.Join(a.Argv("muse", runner.Request{Role: "reviewer", Model: "m", Workdir: "/w", PromptPath: "/p"}), "\n")
	if !strings.Contains(r, "--disable-write") {
		t.Errorf("a reviewer must not write:\n%s", r)
	}
}

func TestParseStreamTakesTheTerminalTextAndUsage(t *testing.T) {
	in := `muse: noise that is not json
{"payload_type":"run.output.delta","payload":{"text":"po"}}
{"payload_type":"run.usage","payload":{"models":[{"input_tokens":100,"output_tokens":7},{"input_tokens":5,"output_tokens":1}]}}
{"payload_type":"run.terminal.completed","payload":{"terminal":"completed","text":"pong"}}
`
	out, err := parseStream(strings.NewReader(in), io.Discard, nil)
	if err != nil {
		t.Fatal(err)
	}
	if out.final() != "pong" || !out.completed || out.errMsg != "" {
		t.Fatalf("out = %+v", out)
	}
	if out.usage == nil || out.usage.InputTokens != 105 || out.usage.TotalTokens != 113 {
		t.Fatalf("usage = %+v", out.usage)
	}
}

func TestParseStreamOtherTerminalIsTheFailure(t *testing.T) {
	in := `{"payload_type":"run.terminal.failed","payload":{"terminal":"failed","reason":"rate limit exceeded"}}` + "\n"
	out, _ := parseStream(strings.NewReader(in), io.Discard, nil)
	if out.completed || !strings.Contains(out.errMsg, "rate limit") {
		t.Fatalf("out = %+v", out)
	}
}

func TestParseVersion(t *testing.T) {
	if v, err := parseVersion("Muse Code 1.4.14 (1.4.14-R7309.1)\n"); err != nil || v != "1.4.14" {
		t.Fatalf("%q %v", v, err)
	}
}
