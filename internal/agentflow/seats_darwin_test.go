//go:build darwin

package agentflow

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"cmd184psu/unified-webapp/internal/agentflow/runner"
	"cmd184psu/unified-webapp/internal/agentflow/runner/mux"
	"cmd184psu/unified-webapp/internal/agentflow/runner/seat"
)

// A seat stage runs its program in the stage sandbox even though tmux, not agentflow, starts
// it: credentials under HOME stay unreadable, the server's environment does not leak in, and
// the stage home is HOME. Real tmux on a private socket, never the person's own server.
func TestSeatSessionIsSandboxed(t *testing.T) {
	tmuxBin, err := exec.LookPath("tmux")
	if err != nil {
		if _, err := os.Stat("/opt/homebrew/bin/tmux"); err != nil {
			t.Skip("no tmux")
		}
		tmuxBin = "/opt/homebrew/bin/tmux"
	}
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	home := filepath.Join(root, "userhome")
	repo := filepath.Join(root, "repo")
	wt := filepath.Join(root, "data", "worktrees", "p", "wt")
	stageDir := filepath.Join(root, "data", "stages", "7")
	for _, d := range []string{filepath.Join(home, ".ssh"), repo, stageDir, filepath.Dir(wt)} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	for _, d := range []string{".config/manicode", ".config/other"} {
		if err := os.MkdirAll(filepath.Join(home, d), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(home, ".config/manicode/freebuff"), []byte("#!/bin/sh\necho FB-BIN-OK\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	for p, s := range map[string]string{".ssh/id_ed25519": "SECRET-SSH", ".claude_chat_api": "SECRET-KEY", ".beam_api_x": "SECRET-BEAM",
		".config/other/token": "SECRET-OTHER", ".config/manicode/message-history.json": "SECRET-HISTORY"} {
		if err := os.WriteFile(filepath.Join(home, p), []byte(s+"\n"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("HOME", home)
	t.Setenv("SERVER_ONLY", "LEAKED-ENV")
	git := func(args ...string) {
		cmd := exec.Command("git", args...)
		cmd.Dir = repo
		cmd.Env = append(os.Environ(), "GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@e", "GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@e", "GIT_CONFIG_GLOBAL=/dev/null")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v %s", args, err, out)
		}
	}
	git("init", "-q", "-b", "main")
	if err := os.WriteFile(filepath.Join(repo, "a.txt"), []byte("a\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	git("add", "a.txt")
	git("commit", "-q", "-m", "i")
	git("worktree", "add", "-q", "-b", "wtb", wt)
	gitDir := filepath.Join(repo, ".git")
	dirs := func(runner.Request) (string, string, error) {
		return gitDir, filepath.Join(gitDir, "worktrees", "wt"), nil
	}

	// The stage home links the seat program's binary from the user's config, as installConfig does.
	stageCfg := filepath.Join(stageDir, "home", ".config", "manicode")
	if err := os.MkdirAll(stageCfg, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(home, ".config/manicode/freebuff"), filepath.Join(stageCfg, "freebuff")); err != nil {
		t.Fatal(err)
	}
	script := fmt.Sprintf(`for f in %[1]s/.ssh/id_ed25519 %[1]s/.claude_chat_api %[1]s/.beam_api_x %[1]s/.config/other/token %[1]s/.config/manicode/message-history.json; do cat "$f" 2>/dev/null; done; ls %[1]s/.config 2>/dev/null; sh "$HOME/.config/manicode/freebuff"; echo "env=[$SERVER_ONLY] home=[$HOME]"; echo ok > a.txt && echo wrote; sleep 30`, home)
	req := runner.Request{Role: "worker", Workdir: wt, StageDir: stageDir}
	l, err := seatSandbox(dirs)(req, seat.SandboxOpts{Argv: []string{"/bin/sh", "-c", script}, Env: map[string]string{"TERM": "xterm-256color"}, Net: true,
		HideDirs: []string{filepath.Join(home, ".config")}, ReadFiles: []string{filepath.Join(home, ".config/manicode/freebuff")}})
	if err != nil {
		t.Fatal(err)
	}
	if l.Argv[0] != "/usr/bin/sandbox-exec" {
		t.Fatalf("not sandboxed: %v", l.Argv[:1])
	}
	sockDir, err := os.MkdirTemp("/tmp", "afs") // a unix socket path must be short
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(sockDir) })
	sock := filepath.Join(sockDir, "s")
	wrapper := filepath.Join(root, "tmux")
	if err := os.WriteFile(wrapper, []byte(fmt.Sprintf("#!/bin/sh\nexec %s -S %s \"$@\"\n", tmuxBin, sock)), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = exec.Command(tmuxBin, "-S", sock, "kill-server").Run() })
	tm := &mux.Tmux{Bin: wrapper}
	ctx := context.Background()
	if err := tm.Create(ctx, mux.Spec{Name: "af-p-1-T1-7", Dir: l.Dir, Argv: l.Argv, Env: l.Env, ScriptDir: stageDir}); err != nil {
		cmd := exec.Command("/usr/bin/env", append(append([]string{"-i"}, l.Env...), l.Argv...)...)
		cmd.Dir = l.Dir
		out, _ := cmd.CombinedOutput()
		t.Fatalf("%v; run directly: %s", err, strings.ReplaceAll(string(out), "SECRET", "S*"))
	}
	var screen string
	for deadline := time.Now().Add(20 * time.Second); time.Now().Before(deadline); time.Sleep(50 * time.Millisecond) {
		screen, _ = tm.Capture(ctx, "af-p-1-T1-7", 100)
		if strings.Contains(screen, "wrote") {
			break
		}
	}
	if strings.Contains(screen, "SECRET") {
		t.Fatalf("a credential was readable in the seat's session:\n%s", screen)
	}
	if strings.Contains(screen, "manicode") || strings.Contains(screen, "other") {
		t.Fatalf("~/.config was listable in the seat's session:\n%s", screen)
	}
	if !strings.Contains(screen, "FB-BIN-OK") {
		t.Fatalf("the linked program file was not readable:\n%s", screen)
	}
	if strings.Contains(screen, "LEAKED-ENV") {
		t.Fatalf("the server's environment leaked into the seat:\n%s", screen)
	}
	stageHome := filepath.Join(stageDir, "home")
	if !strings.Contains(screen, "home=["+stageHome+"]") || !strings.Contains(screen, "wrote") {
		t.Fatalf("want HOME=%s and a writable worktree:\n%s", stageHome, screen)
	}
}
