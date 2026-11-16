package codex

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"cmd184psu/unified-webapp/internal/agentflow/runner"
)

func fakeCodex(t *testing.T, body string, code int) (string, string) {
	t.Helper()
	dir := t.TempDir()
	bin := filepath.Join(dir, "codex")
	script := "#!/bin/sh\nprintf '%s\\n' \"$@\" > " + filepath.Join(dir, "args.txt") + "\ncat > " + filepath.Join(dir, "stdin.txt") + "\ncat <<'EOF'\n" + body + "\nEOF\n"
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
	if err := os.WriteFile(prompt, []byte("do it please"), 0o600); err != nil {
		t.Fatal(err)
	}
	return runner.Request{Role: role, Model: "gpt-6.1-terra", Workdir: t.TempDir(), StageDir: stage, PromptPath: prompt,
		Backend: runner.Backend{Name: "codex", APIType: "codex"}}
}

const okStream = `{"type":"thread.started","thread_id":"th-1"}
{"type":"item.completed","item":{"id":"i0","type":"error","message":"instructions are long"}}
{"type":"turn.started"}
{"type":"item.completed","item":{"id":"i1","type":"agent_message","text":"I will look."}}
{"type":"item.started","item":{"id":"i2","type":"command_execution","command":"ls"}}
{"type":"item.completed","item":{"id":"i2","type":"command_execution","command":"ls","exit_code":0}}
{"type":"item.completed","item":{"id":"i3","type":"agent_message","text":"{\"ok\": true}"}}
{"type":"turn.completed","usage":{"input_tokens":10,"output_tokens":4}}`

func TestRunFinalIsTheLastAgentMessage(t *testing.T) {
	bin, dir := fakeCodex(t, okStream, 0)
	req := request(t, "worker")
	res, err := (&Adapter{Path: bin}).Run(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	if res.Final != `{"ok": true}` || res.SessionID != "th-1" || res.Incomplete {
		t.Fatalf("result = %+v", res)
	}
	if res.Usage == nil || res.Usage.TotalTokens != 14 {
		t.Fatalf("usage = %+v", res.Usage)
	}
	if in, _ := os.ReadFile(filepath.Join(dir, "stdin.txt")); string(in) != "do it please" {
		t.Fatalf("stdin = %q, want the prompt", in)
	}
	args, _ := os.ReadFile(filepath.Join(dir, "args.txt"))
	for _, want := range []string{"exec\n", "--json", "-m\ngpt-6.1-terra", `sandbox_mode="workspace-write"`, `approval_policy="never"`, `model_reasoning_effort="medium"`, "-C\n" + req.Workdir, "-\n"} {
		if !strings.Contains(string(args), want) {
			t.Fatalf("argv lacks %q:\n%s", want, args)
		}
	}
}

func TestSandboxFollowsTheRoleAndFollowUpResumes(t *testing.T) {
	a := &Adapter{}
	for role, want := range map[string]string{"worker": "workspace-write", "cleanup": "workspace-write", "reviewer": "read-only", "planner": "read-only"} {
		if argv := strings.Join(a.Argv("codex", request(t, role)), " "); !strings.Contains(argv, `sandbox_mode="`+want+`"`) {
			t.Errorf("%s: %s, want %s", role, argv, want)
		}
	}
	req := request(t, "reviewer")
	req.SessionID = "th-9"
	argv := strings.Join(a.Argv("codex", req), " ")
	if !strings.Contains(argv, "exec resume") || !strings.Contains(argv, "th-9") {
		t.Fatalf("a follow-up must resume the thread: %s", argv)
	}
}

func TestRunClassifiesFailures(t *testing.T) {
	cases := []struct {
		name, body string
		code       int
		want       runner.ErrClass
	}{
		{"usage limit", `{"type":"turn.failed","error":{"message":"You've hit your usage limit. Try again in 3 hours."}}`, 1, runner.ErrRateLimit},
		{"network", `{"type":"error","message":"connection refused"}`, 1, runner.ErrUnreachable},
		{"silent", ``, 0, runner.ErrTimeout},
		{"bad exit", `{"type":"thread.started","thread_id":"t"}`, 2, runner.ErrCrash},
	}
	for _, c := range cases {
		bin, _ := fakeCodex(t, c.body, c.code)
		_, err := (&Adapter{Path: bin}).Run(context.Background(), request(t, "reviewer"))
		var re *runner.Error
		if !errors.As(err, &re) || re.Class != c.want {
			t.Errorf("%s: err = %v, want class %s", c.name, err, c.want)
		}
	}
}

func TestARetryNoticeInAFinishedTurnIsNotAFailure(t *testing.T) {
	bin, _ := fakeCodex(t, `{"type":"error","message":"Reconnecting... 1/5"}
{"type":"item.completed","item":{"type":"agent_message","text":"done"}}
{"type":"turn.completed","usage":{"input_tokens":1,"output_tokens":1}}`, 0)
	res, err := (&Adapter{Path: bin}).Run(context.Background(), request(t, "reviewer"))
	if err != nil || res.Final != "done" {
		t.Fatalf("res = %+v, err = %v", res, err)
	}
}

func TestAnUnfinishedTurnIsIncomplete(t *testing.T) {
	bin, _ := fakeCodex(t, `{"type":"item.completed","item":{"type":"agent_message","text":"partial"}}`, 0)
	res, err := (&Adapter{Path: bin}).Run(context.Background(), request(t, "worker"))
	if err != nil || !res.Incomplete {
		t.Fatalf("res = %+v, err = %v, want Incomplete", res, err)
	}
}

func TestModelsAndVersion(t *testing.T) {
	got, err := parseModels([]byte(`{"models":[{"slug":"gpt-6.1-sol","visibility":"list"},{"slug":"gpt-reserve","visibility":"hide"},{"slug":"gpt-6-luna","visibility":"list"}]}`))
	if err != nil || strings.Join(got, ",") != "gpt-6.1-sol,gpt-6-luna" {
		t.Fatalf("models = %v, %v", got, err)
	}
	if v, err := parseVersion("codex-cli 0.169.0\n"); err != nil || v != "0.169.0" {
		t.Fatalf("version = %q, %v", v, err)
	}
}

func TestChildEnvCarriesNoAgentflowKeys(t *testing.T) {
	a := &Adapter{ParentEnv: []string{"PATH=/bin", "HOME=/h", "AGENTFLOW_KEY_X=secret", "OPENAI_API_KEY=k"}}
	if env := strings.Join(a.env(runner.Request{}), " "); strings.Contains(env, "secret") || strings.Contains(env, "OPENAI") {
		t.Fatalf("env = %s", env)
	}
}
