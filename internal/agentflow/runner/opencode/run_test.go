package opencode

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"cmd184psu/unified-webapp/internal/agentflow/runner"
)

// harness is a stage with a fake opencode binary written by the test. The
// script records its argv, working directory, environment and stdin, then
// plays a fixture and exits with the given code. No model is ever started.
type harness struct {
	t        *testing.T
	stage    string
	worktree string
	bin      string
	rec      string
}

func newHarness(t *testing.T, fixture string, exit int, extra string) *harness {
	t.Helper()
	root := t.TempDir()
	h := &harness{t: t, stage: filepath.Join(root, "stage"), worktree: filepath.Join(root, "wt"), rec: filepath.Join(root, "rec")}
	for _, d := range []string{h.stage, h.worktree, h.rec} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	play := ""
	if fixture != "" {
		fx, err := filepath.Abs(filepath.Join("testdata", "events", fixture+".jsonl"))
		if err != nil {
			t.Fatal(err)
		}
		play = "cat '" + fx + "'"
	}
	script := fmt.Sprintf(`#!/bin/sh
if [ "$1" = "--version" ]; then echo "%s"; exit 0; fi
printf '%%s\n' "$@" > '%s/argv'
pwd > '%s/pwd'
env > '%s/env'
cat > '%s/stdin'
%s
%s
exit %d
`, "opencode 1.18.19", h.rec, h.rec, h.rec, h.rec, play, extra, exit)
	h.bin = filepath.Join(root, "opencode")
	if err := os.WriteFile(h.bin, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	return h
}

func (h *harness) adapter() *Adapter {
	return &Adapter{Path: h.bin, ParentEnv: []string{"PATH=/usr/bin:/bin", "LANG=C", "OPENCODE_CONFIG_DIR=/home/user/.config/opencode"}}
}

func (h *harness) request(prompt []byte) runner.Request {
	h.t.Helper()
	pp := filepath.Join(h.stage, "prompt.md")
	if err := os.WriteFile(pp, prompt, 0o644); err != nil {
		h.t.Fatal(err)
	}
	return runner.Request{
		Role: "reviewer", Backend: runner.Backend{Name: "local"}, Model: "qwen3:32b",
		Workdir: h.worktree, StageDir: h.stage, PromptPath: pp,
	}
}

func (h *harness) read(name string) string {
	h.t.Helper()
	b, err := os.ReadFile(filepath.Join(h.rec, name))
	if err != nil {
		h.t.Fatal(err)
	}
	return string(b)
}

func runCtx(t *testing.T) context.Context {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	t.Cleanup(cancel)
	return ctx
}

func wantClass(t *testing.T, err error, class runner.ErrClass) {
	t.Helper()
	var re *runner.Error
	if !errors.As(err, &re) || re.Class != class {
		t.Fatalf("error = %v, want runner.Error class %q", err, class)
	}
}

func TestFinalMessageUsageAndSession(t *testing.T) {
	h := newHarness(t, "ok", 0, "")
	res, err := h.adapter().Run(runCtx(t), h.request([]byte("hello")))
	if err != nil {
		t.Fatal(err)
	}
	if res.Final != "FINAL-ANSWER-OK" || res.Incomplete || res.ErrClass != runner.ErrNone {
		t.Errorf("result = %+v", res)
	}
	if res.Usage == nil || res.Usage.InputTokens != 5566 || res.Usage.OutputTokens != 7 || res.Usage.TotalTokens != 5573 {
		t.Errorf("usage = %+v", res.Usage)
	}
	if res.SessionID != "ses_eca47694cffe13Q490oqcgoNGj" || len(res.Events) != 3 {
		t.Errorf("session %q, %d events", res.SessionID, len(res.Events))
	}
}

func TestUsageIsSummedAcrossSteps(t *testing.T) {
	h := newHarness(t, "tools-ask-rejected-noauto", 0, "")
	res, err := h.adapter().Run(runCtx(t), h.request([]byte("p")))
	if err != nil {
		t.Fatal(err)
	}
	// Each step_finish carries its own tokens; the run total is the sum.
	var sum int64
	for _, ev := range res.Events {
		if ev.Type == "step_finish" {
			sum += int64(strings.Count(string(ev.Raw), `"output":7`)) * 7
		}
	}
	if res.Usage == nil || res.Usage.OutputTokens != sum || sum < 14 {
		t.Errorf("usage = %+v, summed output = %d", res.Usage, sum)
	}
}

func TestErrorClassification(t *testing.T) {
	for _, tc := range []struct {
		fixture string
		exit    int
		class   runner.ErrClass
	}{
		{"error-429", 1, runner.ErrRateLimit},
		{"error-500", 1, runner.ErrCrash},
		{"error-connection-refused", 1, runner.ErrUnreachable},
		{"error-socket-closed", 1, runner.ErrUnreachable},
		{"", 3, runner.ErrCrash},                     // non-zero exit, no events, no final message
		{"timeout-hang-empty", 0, runner.ErrTimeout}, // zero events
	} {
		t.Run(tc.fixture+"/"+strconv.Itoa(tc.exit), func(t *testing.T) {
			h := newHarness(t, tc.fixture, tc.exit, "echo boom >&2")
			res, err := h.adapter().Run(runCtx(t), h.request([]byte("p")))
			wantClass(t, err, tc.class)
			if res.ErrClass != tc.class {
				t.Errorf("Result.ErrClass = %q", res.ErrClass)
			}
		})
	}
}

func TestCrashNonZeroExitKeepsStderr(t *testing.T) {
	h := newHarness(t, "", 2, "echo kaboom >&2")
	_, err := h.adapter().Run(runCtx(t), h.request([]byte("p")))
	wantClass(t, err, runner.ErrCrash)
	if !strings.Contains(err.Error(), "kaboom") {
		t.Errorf("stderr missing from %v", err)
	}
}

func TestNonZeroExitWithFinalMessageIsStillACrash(t *testing.T) {
	h := newHarness(t, "ok", 1, "")
	_, err := h.adapter().Run(runCtx(t), h.request([]byte("p")))
	wantClass(t, err, runner.ErrCrash)
}

func TestDeadlineIsTimeoutAndParentCancelIsNot(t *testing.T) {
	h := newHarness(t, "", 0, "exec sleep 30")
	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()
	start := time.Now()
	_, err := h.adapter().Run(ctx, h.request([]byte("p")))
	wantClass(t, err, runner.ErrTimeout)
	if time.Since(start) > 15*time.Second {
		t.Errorf("deadline not enforced: %v", time.Since(start))
	}

	ctx2, cancel2 := context.WithCancel(context.Background())
	time.AfterFunc(300*time.Millisecond, cancel2)
	_, err = h.adapter().Run(ctx2, h.request([]byte("p")))
	var re *runner.Error
	if !errors.Is(err, context.Canceled) || errors.As(err, &re) {
		t.Errorf("parent cancel = %v, want bare context.Canceled", err)
	}
}

func TestRejectedAskIsIncompleteNotAnError(t *testing.T) {
	h := newHarness(t, "tools-ask-rejected-noauto", 0, "")
	res, err := h.adapter().Run(runCtx(t), h.request([]byte("p")))
	if err != nil {
		t.Fatal(err)
	}
	if !res.Incomplete || res.Final != "" || res.ErrClass != runner.ErrNone {
		t.Errorf("result = incomplete %v final %q class %q", res.Incomplete, res.Final, res.ErrClass)
	}
}

func TestPromptIsNeverArgv(t *testing.T) {
	h := newHarness(t, "ok", 0, "")
	prompt := bytes.Repeat([]byte("0123456789abcdef"), 500<<10/16)
	prompt = append(prompt, []byte("\nEND-OF-PROMPT-MARKER\n")...)
	if len(prompt) < 500<<10 {
		t.Fatalf("prompt is only %d bytes", len(prompt))
	}
	req := h.request(prompt)
	req.Message = "short instruction that must not be passed"
	if _, err := h.adapter().Run(runCtx(t), req); err != nil {
		t.Fatal(err)
	}
	argv := h.read("argv")
	if len(argv) >= 1024 {
		t.Errorf("argv is %d bytes", len(argv))
	}
	if strings.Contains(argv, "END-OF-PROMPT") || strings.Contains(argv, "instruction") || strings.Contains(argv, req.PromptPath) {
		t.Errorf("argv carries prompt, message or prompt path: %q", argv)
	}
	if got := h.read("stdin"); got != string(prompt) {
		t.Errorf("stdin has %d bytes, want %d intact", len(got), len(prompt))
	}
	for _, flag := range []string{"run", "--pure", "--format", "json", "--auto", "--agent", "af-reviewer", "--model", "local/qwen3:32b"} {
		if !strings.Contains("\n"+argv, "\n"+flag+"\n") {
			t.Errorf("argv lacks %q: %q", flag, argv)
		}
	}
	if strings.Contains(argv, "-f\n") || strings.Contains(argv, "--session") {
		t.Errorf("unexpected flag in %q", argv)
	}
}

func TestSessionContinuation(t *testing.T) {
	h := newHarness(t, "session-continue", 0, "")
	req := h.request([]byte("correction"))
	req.SessionID = "ses_abc"
	res, err := h.adapter().Run(runCtx(t), req)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(h.read("argv"), "--session\nses_abc\n") {
		t.Errorf("argv = %q", h.read("argv"))
	}
	if res.Final != "SECOND-ANSWER-AFTER-CORRECTION" {
		t.Errorf("final = %q", res.Final)
	}
}

func TestWorkingDirectoryIsTheWorktree(t *testing.T) {
	h := newHarness(t, "ok", 0, "")
	if _, err := h.adapter().Run(runCtx(t), h.request([]byte("p"))); err != nil {
		t.Fatal(err)
	}
	want, _ := filepath.EvalSymlinks(h.worktree)
	if got := strings.TrimSpace(h.read("pwd")); got != want {
		t.Errorf("cwd = %q, want %q", got, want)
	}
}

func TestSandboxedRunUsesWorktreeAsCwd(t *testing.T) {
	root := t.TempDir()
	repo := filepath.Join(root, "repo", ".git")
	wtGit := filepath.Join(repo, "worktrees", "w1")
	wt := filepath.Join(root, "wt")
	stage := filepath.Join(root, "stage")
	for _, d := range []string{wtGit, wt, HomeDir(stage)} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := GenerateConfig(specs(), stage); err != nil {
		t.Fatal(err)
	}
	bin := filepath.Join(root, "opencode")
	if err := os.WriteFile(bin, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	factory := SandboxCommand(func(runner.Request) (string, string, error) { return repo, wtGit, nil })
	for role, readOnly := range map[string]bool{"worker": false, "reviewer": true} {
		req := runner.Request{Role: role, Backend: runner.Backend{Name: "local"}, Model: "m", Workdir: wt, StageDir: stage, PromptPath: "p"}
		cmd, err := factory(context.Background(), req, Argv(bin, req), IsolationEnv(stage, []string{"PATH=/usr/bin"}, nil))
		if err != nil {
			t.Fatal(err)
		}
		args := strings.Join(cmd.Args, "\x00")
		if !strings.Contains(args, "--chdir\x00"+wt+"\x00") {
			t.Errorf("%s: sandbox does not chdir to the worktree: %q", role, cmd.Args)
		}
		bindKind := "--bind\x00" + wt + "\x00" + wt
		if readOnly {
			bindKind = "--ro-bind\x00" + wt + "\x00" + wt
		}
		if !strings.Contains(args, bindKind) {
			t.Errorf("%s: worktree bind mode wrong: %q", role, cmd.Args)
		}
		for _, want := range []string{sandboxBin + "\x00run\x00--pure", "--bind\x00" + HomeDir(stage) + "\x00/home/agent"} {
			if !strings.Contains(args, want) {
				t.Errorf("%s: sandbox args lack %q", role, want)
			}
		}
		frozen, _ := os.ReadFile(ConfigPath(stage))
		if copied, err := os.ReadFile(filepath.Join(HomeDir(stage), ".config", "opencode", "opencode.json")); err != nil || !bytes.Equal(copied, frozen) {
			t.Errorf("%s: sandbox config copy missing or different: %v", role, err)
		}
		if !strings.Contains(args, "XDG_CONFIG_HOME\x00/home/agent/.config\x00") || strings.Contains(args, stage+"/xdg") {
			t.Errorf("%s: XDG_CONFIG_HOME should be the sandbox default", role)
		}
		if strings.Contains(args, "OPENCODE_CONFIG_DIR") || strings.Contains(args, "OPENCODE_CONFIG\x00") {
			t.Errorf("%s: leaked opencode config variable", role)
		}
	}
}

func TestChildEnvIsIsolated(t *testing.T) {
	h := newHarness(t, "ok", 0, "")
	if _, err := h.adapter().Run(runCtx(t), h.request([]byte("p"))); err != nil {
		t.Fatal(err)
	}
	env := h.read("env")
	if !strings.Contains(env, "XDG_CONFIG_HOME="+filepath.Join(h.stage, "xdg")+"\n") ||
		!strings.Contains(env, "HOME="+filepath.Join(h.stage, "home")+"\n") {
		t.Errorf("stage isolation env missing:\n%s", env)
	}
	if strings.Contains(env, "OPENCODE_CONFIG_DIR") {
		t.Errorf("parent OPENCODE_CONFIG_DIR leaked:\n%s", env)
	}
}

func TestEventsAreLoggedAndStreamed(t *testing.T) {
	h := newHarness(t, "ok", 0, "")
	req := h.request([]byte("p"))
	var got []string
	req.OnEvent = func(e runner.Event) { got = append(got, e.Type+":"+e.Text) }
	if _, err := h.adapter().Run(runCtx(t), req); err != nil {
		t.Fatal(err)
	}
	if strings.Join(got, ",") != "step_start:,text:FINAL-ANSWER-OK,step_finish:" {
		t.Errorf("streamed events = %v", got)
	}
	want, _ := os.ReadFile(filepath.Join("testdata", "events", "ok.jsonl"))
	logged, err := os.ReadFile(filepath.Join(h.stage, "events.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	if string(logged) != string(want) {
		t.Errorf("events.jsonl differs from the raw stream:\n%s", logged)
	}
	// A second run in the same stage (SO2 follow-up) appends.
	if _, err := h.adapter().Run(runCtx(t), req); err != nil {
		t.Fatal(err)
	}
	logged, _ = os.ReadFile(filepath.Join(h.stage, "events.jsonl"))
	if string(logged) != string(want)+string(want) {
		t.Errorf("second run did not append")
	}
}

func TestRequiredRequestFieldsAndMissingBinary(t *testing.T) {
	h := newHarness(t, "ok", 0, "")
	req := h.request([]byte("p"))
	req.PromptPath = ""
	if _, err := h.adapter().Run(runCtx(t), req); err == nil {
		t.Error("empty PromptPath accepted")
	}
	req = h.request([]byte("p"))
	a := &Adapter{Path: filepath.Join(h.stage, "nope")}
	_, err := a.Run(runCtx(t), req)
	wantClass(t, err, runner.ErrCrash)
}

func TestVersion(t *testing.T) {
	h := newHarness(t, "", 0, "")
	v, err := h.adapter().Version(runCtx(t))
	if err != nil || v != "1.18.19" {
		t.Errorf("Version = %q, %v", v, err)
	}
	for out, ok := range map[string]bool{
		"1.18.19\n": true, "opencode v2.0.1-beta.3\n": true, "\n\n0.9.0": true,
		"": false, "command not found": false, "opencode version\n": false,
	} {
		got, err := parseVersion(out)
		if (err == nil) != ok {
			t.Errorf("parseVersion(%q) = %q, %v", out, got, err)
		}
	}
	if _, err := (&Adapter{Path: filepath.Join(h.stage, "missing")}).Version(runCtx(t)); err == nil {
		t.Error("missing binary accepted")
	}
	bad := filepath.Join(t.TempDir(), "opencode")
	if err := os.WriteFile(bad, []byte("#!/bin/sh\necho garbage out\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := (&Adapter{Path: bad}).Version(runCtx(t)); err == nil {
		t.Error("garbage version output accepted")
	}
}

func TestNoFileAttachFlag(t *testing.T) {
	// Documents S1: "-f" truncates prompts at about 50 KB, so the prompt goes
	// on stdin and the adapter never emits an attach flag.
	argv := Argv("/bin/opencode", runner.Request{Role: "worker", Backend: runner.Backend{Name: "b"}, Model: "m", PromptPath: "/x/prompt.md"})
	for _, a := range argv {
		if a == "-f" || a == "--file" || a == "--attach" || strings.HasSuffix(a, "prompt.md") {
			t.Errorf("argv contains %q", a)
		}
	}
}

// A changed opencode_path applies to the next run and the next probe without
// rebuilding the adapter.
func TestPathFuncIsReadAtUseTime(t *testing.T) {
	first := newHarness(t, "", 0, "echo '{\"type\":\"text\",\"part\":{\"text\":\"x\"}}'")
	second := newHarness(t, "", 0, "echo '{\"type\":\"text\",\"part\":{\"text\":\"x\"}}'")
	use := first.bin
	a := &Adapter{PathFunc: func() string { return use }, ParentEnv: []string{"PATH=/usr/bin:/bin", "LANG=C"}}

	_, _ = a.Run(runCtx(t), second.request([]byte("p")))
	if _, err := os.Stat(filepath.Join(second.rec, "argv")); err == nil {
		t.Fatal("second binary ran before the path moved to it")
	}
	use = second.bin
	_, _ = a.Run(runCtx(t), second.request([]byte("p")))
	if _, err := os.Stat(filepath.Join(second.rec, "argv")); err != nil {
		t.Fatalf("second binary did not run after the path moved: %v", err)
	}
	use = filepath.Join(t.TempDir(), "missing")
	if _, err := a.Version(runCtx(t)); err == nil {
		t.Fatal("Version used a stale path")
	}
}

func TestFullContextWindowIsItsOwnClass(t *testing.T) {
	h := newHarness(t, "context-full", 0, "")
	res, err := h.adapter().Run(runCtx(t), h.request([]byte("p")))
	var re *runner.Error
	if !errors.As(err, &re) || re.Class != runner.ErrContext || res.ErrClass != runner.ErrContext {
		t.Fatalf("err = %v, class %q, want context_full", err, res.ErrClass)
	}
	if !strings.Contains(re.Msg, "16384") {
		t.Errorf("message %q should name the window size", re.Msg)
	}
}
