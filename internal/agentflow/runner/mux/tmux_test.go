package mux

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestValidAndClean(t *testing.T) {
	for _, n := range []string{"af-x-1-T1-2", "a_b"} {
		if !Valid(n) {
			t.Errorf("%q should be valid", n)
		}
	}
	for _, n := range []string{"", "a.b", "a:b", "a b", "a;rm"} {
		if Valid(n) {
			t.Errorf("%q should be invalid", n)
		}
	}
	if got := Clean("demo.app/x y"); got != "demo-app-x-y" {
		t.Errorf("Clean = %q", got)
	}
}

// TestTmuxRoundTrip drives a real tmux on a private socket, so it never touches the person's
// own sessions. Skipped where tmux is not installed.
func TestTmuxRoundTrip(t *testing.T) {
	bin, lookErr := exec.LookPath("tmux")
	if lookErr != nil {
		if _, err := os.Stat("/opt/homebrew/bin/tmux"); err != nil {
			t.Skip("no tmux")
		}
		bin = "/opt/homebrew/bin/tmux"
	}
	sockDir, err := os.MkdirTemp("/tmp", "afm") // a unix socket path must be short
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(sockDir) })
	sock := filepath.Join(sockDir, "s")
	wrapper := filepath.Join(t.TempDir(), "tmux")
	if err := os.WriteFile(wrapper, []byte(fmt.Sprintf("#!/bin/sh\nexec %s -S %s \"$@\"\n", bin, sock)), 0o700); err != nil {
		t.Fatal(err)
	}
	tm := &Tmux{Bin: wrapper}
	t.Cleanup(func() { _ = exec.Command(bin, "-S", sock, "kill-server").Run() })
	ctx := context.Background()
	name := "af-test-1-T1-9"
	err = tm.Create(ctx, Spec{Name: name, Dir: t.TempDir(), Argv: []string{"/bin/sh", "-c", `echo "secret=[$SECRET] keep=[$KEEP]"; read l; echo "got $l"; sleep 30`},
		Env: []string{"KEEP=yes", "PATH=/usr/bin:/bin"}, Tags: map[string]string{"agentflow_stage": "9"}})
	if err != nil {
		t.Fatal(err)
	}
	if ok, _ := tm.Has(ctx, name); !ok {
		t.Fatal("session missing")
	}
	if ok, _ := tm.Has(ctx, "af-test-1-T1"); ok {
		t.Fatal("Has matched a prefix")
	}
	if v, _ := tm.Tag(ctx, name, "agentflow_stage"); v != "9" {
		t.Fatalf("tag = %q", v)
	}
	if err := tm.SendText(ctx, name, "hello; $(touch /tmp/never)"); err != nil {
		t.Fatal(err)
	}
	if err := tm.SendKeys(ctx, name, "Enter"); err != nil {
		t.Fatal(err)
	}
	var screen string
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		screen, _ = tm.Capture(ctx, name, 50)
		if strings.Contains(screen, "got hello") {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if !strings.Contains(screen, "got hello; $(touch /tmp/never)") || !strings.Contains(screen, "secret=[] keep=[yes]") {
		t.Fatalf("screen:\n%s", screen)
	}
	names, _ := tm.List(ctx, "af-")
	if len(names) != 1 || names[0] != name {
		t.Fatalf("List = %v", names)
	}
	if dead, _ := tm.Dead(ctx, name); dead {
		t.Fatal("pane should be alive")
	}
	if err := tm.Kill(ctx, name); err != nil {
		t.Fatal(err)
	}
	if ok, _ := tm.Has(ctx, name); ok {
		t.Fatal("still there after Kill")
	}
	if err := tm.Kill(ctx, name); err != nil {
		t.Fatalf("killing a missing session: %v", err)
	}
}
