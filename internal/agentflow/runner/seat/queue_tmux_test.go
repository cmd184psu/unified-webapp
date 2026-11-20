package seat

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"cmd184psu/unified-webapp/internal/agentflow/runner"
	"cmd184psu/unified-webapp/internal/agentflow/runner/mux"
	"cmd184psu/unified-webapp/internal/agentflow/schema"
)

// TestQueueSeatOnRealTmux nudges an owner-style session on a real tmux with a private socket,
// so it never touches anyone's real sessions. Skipped where tmux is not installed.
func TestQueueSeatOnRealTmux(t *testing.T) {
	bin, lookErr := exec.LookPath("tmux")
	if lookErr != nil {
		if _, err := os.Stat("/opt/homebrew/bin/tmux"); err != nil {
			t.Skip("no tmux")
		}
		bin = "/opt/homebrew/bin/tmux"
	}
	sockDir, err := os.MkdirTemp("/tmp", "afq") // a unix socket path must be short
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(sockDir) })
	sock := filepath.Join(sockDir, "s")
	wrapper := filepath.Join(t.TempDir(), "tmux")
	if err := os.WriteFile(wrapper, []byte(fmt.Sprintf("#!/bin/sh\nexec %s -S %s \"$@\"\n", bin, sock)), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = exec.Command(bin, "-S", sock, "kill-server").Run() })
	tm := &mux.Tmux{Bin: wrapper}
	ctx := context.Background()
	queue := filepath.Join(t.TempDir(), "seat.md")
	if err := os.WriteFile(queue, []byte("# brief\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	script, _ := filepath.Abs("testdata/queueseat.sh")
	if err := tm.Create(ctx, mux.Spec{Name: ownerSession, Dir: t.TempDir(), Argv: []string{"/bin/sh", script},
		Env: []string{"QSEAT_MODE=react", "QUEUE_FILE=" + queue, "PATH=/usr/bin:/bin"}}); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "the owner's session", func() bool {
		s, _ := tm.Capture(ctx, ownerSession, 0)
		return strings.Contains(s, "owner seat idle")
	})
	a := &Adapter{
		Mux: func(context.Context) (mux.Multiplexer, error) { return tm, nil },
		Profiles: func(context.Context) map[string]Profile {
			return map[string]Profile{"q": {TmuxSession: ownerSession, QueueFile: queue, HaltTexts: []string{"in use elsewhere"}}}
		},
		Board: &runner.SeatBoard{}, Poll: 20 * time.Millisecond, NudgeGap: 100 * time.Millisecond,
	}
	req := queueReq(t, "worker")
	res, err := a.Run(ctx, req)
	if err != nil {
		t.Fatal(err)
	}
	if w, err := schema.Parse[schema.WorkerOut](json.RawMessage(res.Final)); err != nil || w.Summary != "queue task done" {
		t.Fatalf("%s %v", res.Final, err)
	}
	if screen, _ := tm.Capture(ctx, ownerSession, 0); !strings.Contains(screen, "check your queue") || !strings.Contains(screen, "reading the queue") {
		t.Fatalf("screen:\n%s", screen)
	}
	if ok, _ := tm.Has(ctx, ownerSession); !ok {
		t.Fatal("the owner's session must live on")
	}
	if got := readFile(t, queue); got != "# brief\n" {
		t.Fatalf("queue file %q", got)
	}
}
