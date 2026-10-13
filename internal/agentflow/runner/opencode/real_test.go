package opencode

import (
	"bytes"
	"context"
	"encoding/json"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"cmd184psu/unified-webapp/internal/agentflow/runner"
)

// realEnv gates the one test that starts the real opencode binary. It talks to
// a local stub model server only, never a real model, and is not part of the
// default `go test ./...`. Run it with
//
//	AGENTFLOW_REAL_OPENCODE=/home/me/.opencode/bin/opencode go test -run Real ./internal/agentflow/runner/opencode
//
// and additionally AGENTFLOW_REAL_OPENCODE_SANDBOX=1 to run it inside bwrap.
const realEnv = "AGENTFLOW_REAL_OPENCODE"

func TestRealOpencodeAgainstStub(t *testing.T) {
	bin := os.Getenv(realEnv)
	if bin == "" {
		t.Skip(realEnv + " not set (opt-in)")
	}
	dir := t.TempDir()
	stub := filepath.Join(dir, "stub")
	if out, err := exec.Command("go", "build", "-o", stub, "./testdata/stub/main.go").CombinedOutput(); err != nil {
		t.Fatalf("build stub: %v\n%s", err, out)
	}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := ln.Addr().String()
	ln.Close()
	script := filepath.Join(dir, "script.json")
	if err := os.WriteFile(script, []byte(`[{"kind":"text","text":"REAL-STUB-ANSWER"}]`), 0o644); err != nil {
		t.Fatal(err)
	}
	stubLog := filepath.Join(dir, "stub.log")
	sctx, stop := context.WithCancel(context.Background())
	defer stop()
	sc := exec.CommandContext(sctx, stub, "-addr", addr, "-script", script, "-log", stubLog)
	if err := sc.Start(); err != nil {
		t.Fatal(err)
	}
	defer sc.Wait()
	defer stop()
	for i := 0; ; i++ {
		c, err := net.Dial("tcp", addr)
		if err == nil {
			c.Close()
			break
		}
		if i > 50 {
			t.Fatal("stub did not start")
		}
		time.Sleep(100 * time.Millisecond)
	}

	stage := filepath.Join(dir, "stage")
	wt := filepath.Join(dir, "wt")
	if err := os.MkdirAll(wt, 0o755); err != nil {
		t.Fatal(err)
	}
	backends := []BackendSpec{{Backend: runner.Backend{Name: "stub", Host: "http://" + addr, APIType: "ollama"}, Models: []string{"stub-model"}}}
	if _, err := GenerateConfig(backends, stage); err != nil {
		t.Fatal(err)
	}
	prompt := append(bytes.Repeat([]byte("x"), 500<<10), []byte("\nEND-OF-PROMPT-MARKER\n")...)
	pp := filepath.Join(stage, "prompt.md")
	if err := os.WriteFile(pp, prompt, 0o644); err != nil {
		t.Fatal(err)
	}
	a := &Adapter{Path: bin}
	if os.Getenv(realEnv+"_SANDBOX") != "" {
		gd := filepath.Join(dir, "repo", ".git")
		if err := os.MkdirAll(filepath.Join(gd, "worktrees", "w"), 0o755); err != nil {
			t.Fatal(err)
		}
		a.Command = SandboxCommand(func(runner.Request) (string, string, error) { return gd, filepath.Join(gd, "worktrees", "w"), nil })
	}
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()
	if v, err := a.Version(ctx); err != nil || v == "" {
		t.Fatalf("Version = %q, %v", v, err)
	}
	res, err := a.Run(ctx, runner.Request{
		Role: "reviewer", Backend: backends[0].Backend, Model: "stub-model",
		Workdir: wt, StageDir: stage, PromptPath: pp,
	})
	if err != nil {
		t.Fatalf("run: %v (%+v)", err, res)
	}
	if res.Final != "REAL-STUB-ANSWER" || res.Usage == nil {
		t.Errorf("result = %+v", res)
	}
	log, _ := os.ReadFile(stubLog)
	seen := false
	for _, line := range strings.Split(string(log), "\n") {
		var e struct {
			BodyBytes int  `json:"bodyBytes"`
			Marker    bool `json:"containsMarkerEnd"`
		}
		if json.Unmarshal([]byte(line), &e) == nil && e.BodyBytes >= 500<<10 && e.Marker {
			seen = true
		}
	}
	if !seen {
		t.Errorf("stub never received the whole 500 KB prompt:\n%s", log)
	}
	t.Logf("session %s", res.SessionID)
}
