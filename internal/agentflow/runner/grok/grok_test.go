package grok

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"cmd184psu/unified-webapp/internal/agentflow/runner"
)

// fakeGrok writes a script that prints body and exits with code. It records its
// argv in args.txt beside it.
func fakeGrok(t *testing.T, body string, code int) (string, string) {
	t.Helper()
	dir := t.TempDir()
	bin := filepath.Join(dir, "grok")
	script := "#!/bin/sh\nprintf '%s\\n' \"$@\" > " + filepath.Join(dir, "args.txt") + "\ncat <<'EOF'\n" + body + "\nEOF\n"
	if code != 0 {
		script += "echo 'boom' >&2\n"
	}
	script += "exit " + string(rune('0'+code)) + "\n"
	if err := os.WriteFile(bin, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	return bin, dir
}

func request(t *testing.T, role string) runner.Request {
	t.Helper()
	stage := t.TempDir()
	prompt := filepath.Join(stage, "prompt.md")
	if err := os.WriteFile(prompt, []byte("do it"), 0o600); err != nil {
		t.Fatal(err)
	}
	return runner.Request{Role: role, Model: "grok-4.5", Workdir: t.TempDir(), StageDir: stage, PromptPath: prompt,
		Backend: runner.Backend{Name: "grok", APIType: "grok"}}
}

const okStream = `{"type":"available_commands","tools":["x"]}
{"type":"text","data":"Let me look."}
{"type":"tool_call","toolName":"run_terminal_command"}
{"type":"tool_call_update","status":"completed"}
{"type":"text","data":"{\"ok\":"}
{"type":"text","data":" true}"}
{"type":"end","stopReason":"end_turn","sessionId":"s-1","usage":{"input_tokens":10,"output_tokens":4,"total_tokens":14}}`

func TestRunFinalIsTextAfterTheLastToolCall(t *testing.T) {
	bin, dir := fakeGrok(t, okStream, 0)
	a := &Adapter{Path: bin}
	var seen []string
	req := request(t, "worker")
	req.OnEvent = func(e runner.Event) { seen = append(seen, e.Type) }
	res, err := a.Run(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	if res.Final != `{"ok": true}` || res.SessionID != "s-1" || res.Incomplete {
		t.Fatalf("result = %+v", res)
	}
	if res.Usage == nil || res.Usage.TotalTokens != 14 {
		t.Fatalf("usage = %+v", res.Usage)
	}
	for _, ty := range seen {
		if ty == "available_commands" {
			t.Fatal("the tool list event reached the stream")
		}
	}
	args, _ := os.ReadFile(filepath.Join(dir, "args.txt"))
	for _, want := range []string{"--prompt-file", req.PromptPath, "streaming-json", "-m\ngrok-4.5", "--sandbox\nagentflow\n", "bypassPermissions", "--no-subagents"} {
		if !strings.Contains(string(args), want) {
			t.Fatalf("argv lacks %q:\n%s", want, args)
		}
	}
	if _, err := os.Stat(filepath.Join(req.StageDir, "events.jsonl")); err != nil {
		t.Fatalf("no event log: %v", err)
	}
}

func TestOnlyWritingRolesGetAWritableSandbox(t *testing.T) {
	a := &Adapter{}
	for role, want := range map[string]string{"worker": "agentflow", "cleanup": "agentflow", "reviewer": "agentflow-ro", "planner": "agentflow-ro", "compiler": "agentflow-ro"} {
		req := request(t, role)
		argv := strings.Join(a.Argv("grok", req), "\n")
		if !strings.Contains(argv, "--sandbox\n"+want+"\n") && !strings.HasSuffix(argv, "--sandbox\n"+want) {
			t.Errorf("%s: argv = %q, want profile %s", role, argv, want)
		}
	}
	req := request(t, "worker")
	req.SessionID = "abc"
	if argv := strings.Join(a.Argv("grok", req), " "); !strings.Contains(argv, "--resume abc") {
		t.Fatalf("a follow-up must resume the session: %s", argv)
	}
}

func TestRunClassifiesFailures(t *testing.T) {
	cases := []struct {
		name, body string
		code       int
		want       runner.ErrClass
	}{
		{"rate limit", `{"type":"error","message":"Usage limit reached, try again in 3 hours"}`, 1, runner.ErrRateLimit},
		{"not signed in", `{"type":"error","message":"Not signed in. run grok login"}`, 1, runner.ErrCrash},
		{"network", `{"type":"error","message":"connection refused"}`, 1, runner.ErrUnreachable},
		{"silent", ``, 0, runner.ErrTimeout},
		{"bad exit", `{"type":"text","data":"hm"}`, 2, runner.ErrCrash},
	}
	for _, c := range cases {
		bin, _ := fakeGrok(t, c.body, c.code)
		_, err := (&Adapter{Path: bin}).Run(context.Background(), request(t, "reviewer"))
		var re *runner.Error
		if !errors.As(err, &re) || re.Class != c.want {
			t.Errorf("%s: err = %v, want class %s", c.name, err, c.want)
		}
	}
}

func TestMaxTurnsIsIncomplete(t *testing.T) {
	bin, _ := fakeGrok(t, `{"type":"text","data":"partial"}
{"type":"end","stopReason":"max_turns","sessionId":"s"}`, 0)
	res, err := (&Adapter{Path: bin}).Run(context.Background(), request(t, "worker"))
	if err != nil || !res.Incomplete {
		t.Fatalf("res = %+v, err = %v, want Incomplete", res, err)
	}
}

func TestBuiltinModelsAndVersion(t *testing.T) {
	got := parseModels("You are logged in with grok.com.\n\nDefault model: grok-4.7\n\nAvailable models:\n  * grok-4.7 (default)\n  - grok-4.7-build-fast\n  - grok-4.6\n  - grok-4.5\n")
	if strings.Join(got, ",") != "grok-4.7,grok-4.7-build-fast,grok-4.6,grok-4.5" {
		t.Fatalf("models = %v", got)
	}
	if v, err := parseVersion("grok 1.0.77 (abc)\n"); err != nil || v != "1.0.77" {
		t.Fatalf("version = %q, %v", v, err)
	}
	if _, err := parseVersion("nope"); err == nil {
		t.Fatal("a version-less output must fail")
	}
}

func TestChildEnvCarriesNoAgentflowKeys(t *testing.T) {
	a := &Adapter{ParentEnv: []string{"PATH=/bin", "HOME=/h", "AGENTFLOW_KEY_X=secret", "OPENAI_API_KEY=k"}}
	env := strings.Join(a.env(runner.Request{}), " ")
	if strings.Contains(env, "secret") || strings.Contains(env, "OPENAI") || !strings.Contains(env, "HOME=/h") {
		t.Fatalf("env = %s", env)
	}
}
