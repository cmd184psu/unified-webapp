//go:build darwin

package sandbox

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

// The macOS fixture mirrors the Linux one (bwrap_test.go), plus a fake HOME
// holding credentials and an agentflow.db marking the server's data dir.
type sbFixture struct {
	root, home, repo, wt, gitDir, wtGitDir, stageHome string
}

func sbGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@example.com",
		"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@example.com", "GIT_CONFIG_GLOBAL=/dev/null")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

func newSBFixture(t *testing.T) sbFixture {
	t.Helper()
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	f := sbFixture{
		root:      root,
		home:      filepath.Join(root, "userhome"),
		repo:      filepath.Join(root, "repo"),
		wt:        filepath.Join(root, "data", "worktrees", "p", "wt"),
		stageHome: filepath.Join(root, "data", "stages", "s1", "home"),
	}
	for _, d := range []string{f.repo, f.stageHome, filepath.Join(root, "data", "stages", "s2"),
		filepath.Join(f.home, ".ssh"), filepath.Join(root, "data", "worktrees", "p")} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	write := func(p, s string) {
		if err := os.WriteFile(p, []byte(s), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write(filepath.Join(f.home, ".ssh", "id_ed25519"), "SECRET-SSH\n")
	write(filepath.Join(f.home, ".claude_chat_api"), "SECRET-KEY\n")
	write(filepath.Join(f.home, ".beam_api_code"), "SECRET-BEAM\n")
	write(filepath.Join(f.home, "notes.txt"), "public\n")
	write(filepath.Join(root, "data", "agentflow.db"), "DB\n")
	write(filepath.Join(root, "data", "stages", "s2", "prompt.md"), "SIBLING\n")
	t.Setenv("HOME", f.home)

	sbGit(t, f.repo, "init", "-q", "-b", "main")
	write(filepath.Join(f.repo, "a.txt"), "hello\n")
	sbGit(t, f.repo, "add", "a.txt")
	sbGit(t, f.repo, "commit", "-q", "-m", "init")
	sbGit(t, f.repo, "worktree", "add", "-q", "-b", "wtb", f.wt)
	f.gitDir = filepath.Join(f.repo, ".git")
	f.wtGitDir = filepath.Join(f.gitDir, "worktrees", "wt")
	return f
}

func (f sbFixture) spec(argv ...string) Spec {
	return Spec{Worktree: f.wt, RepoGitDir: f.gitDir, WorktreeGitDir: f.wtGitDir,
		StageHome: f.stageHome, Net: true, Argv: argv}
}

func (f sbFixture) sh(script string) Spec { return f.spec("/bin/sh", "-c", script) }

func sbRun(t *testing.T, s Spec) (string, error) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()
	cmd, err := Command(ctx, s)
	if err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &out
	err = cmd.Run()
	return out.String(), err
}

func TestSeatbeltCredentialsUnreachable(t *testing.T) {
	f := newSBFixture(t)
	for _, p := range []string{".ssh/id_ed25519", ".claude_chat_api", ".beam_api_code"} {
		out, _ := sbRun(t, f.sh("cat "+filepath.Join(f.home, p)))
		if strings.Contains(out, "SECRET") {
			t.Errorf("%s readable inside the sandbox: %q", p, out)
		}
	}
	out, err := sbRun(t, f.sh("cat "+filepath.Join(f.home, "notes.txt")))
	if err != nil || !strings.Contains(out, "public") {
		t.Errorf("ordinary file unreadable: %v %q", err, out)
	}
}

func TestSeatbeltServerDataAndSiblingStagesInvisible(t *testing.T) {
	f := newSBFixture(t)
	for _, p := range []string{filepath.Join(f.root, "data", "agentflow.db"), filepath.Join(f.root, "data", "stages", "s2", "prompt.md")} {
		out, _ := sbRun(t, f.sh("cat "+p))
		if strings.Contains(out, "DB") || strings.Contains(out, "SIBLING") {
			t.Errorf("%s readable: %q", p, out)
		}
	}
	if out, err := sbRun(t, f.sh("cat a.txt && echo x > "+filepath.Join(f.stageHome, "h"))); err != nil {
		t.Errorf("own worktree/home not usable: %v %q", err, out)
	}
}

func TestSeatbeltEnvAllowlistAndHome(t *testing.T) {
	f := newSBFixture(t)
	t.Setenv("SSH_AUTH_SOCK", "/tmp/agent.sock")
	t.Setenv("SERVER_SECRET", "x")
	s := f.sh("env")
	s.Env = map[string]string{"AGENTFLOW_KEY_X": "k"}
	out, err := sbRun(t, s)
	if err != nil {
		t.Fatal(err, out)
	}
	if strings.Contains(out, "SSH_AUTH_SOCK") || strings.Contains(out, "SERVER_SECRET") {
		t.Errorf("server environment leaked: %s", out)
	}
	for _, want := range []string{"HOME=" + f.stageHome, "TMPDIR=" + filepath.Join(f.stageHome, "tmp"),
		"XDG_CONFIG_HOME=" + f.stageHome + "/.config", "GIT_CONFIG_GLOBAL=/dev/null", "AGENTFLOW_KEY_X=k"} {
		if !strings.Contains(out, want+"\n") {
			t.Errorf("missing %q in env:\n%s", want, out)
		}
	}
}

func TestSeatbeltWrites(t *testing.T) {
	f := newSBFixture(t)
	if out, err := sbRun(t, f.sh("echo ok > new.txt")); err != nil {
		t.Errorf("worker cannot write its worktree: %v %q", err, out)
	}
	for _, p := range []string{filepath.Join(f.root, "outside.txt"), filepath.Join(f.home, "x.txt"), filepath.Join(f.wt, ".git")} {
		if _, err := sbRun(t, f.sh("echo x > "+p)); err == nil {
			t.Errorf("write to %s succeeded", p)
		}
	}
	ro := f.sh("echo x > new2.txt")
	ro.ReadOnlyWorktree = true
	if _, err := sbRun(t, ro); err == nil {
		t.Error("read-only worktree was writable")
	}
}

func TestSeatbeltGitReadOnly(t *testing.T) {
	f := newSBFixture(t)
	if out, err := sbRun(t, f.sh("git status --short && git log --oneline -1")); err != nil {
		t.Errorf("read-only git failed: %v %q", err, out)
	}
	out, err := sbRun(t, f.sh("echo y > a.txt && git -c user.name=a -c user.email=a@b commit -qam m"))
	if err == nil {
		t.Errorf("commit succeeded inside the sandbox: %q", out)
	}
}

func TestSeatbeltExtraROBindRewritten(t *testing.T) {
	f := newSBFixture(t)
	bin := filepath.Join(f.root, "bin")
	if err := os.MkdirAll(bin, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(bin, "tool"), []byte("#!/bin/sh\necho tool-ran\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	s := f.spec("/sandbox/bin/tool")
	s.ExtraROBinds = []Bind{{Src: bin, Dst: "/sandbox/bin"}}
	if out, err := sbRun(t, s); err != nil || !strings.Contains(out, "tool-ran") {
		t.Errorf("bind not rewritten: %v %q", err, out)
	}
	s = f.sh("echo x > /sandbox/bin/tool")
	s.ExtraROBinds = []Bind{{Src: bin, Dst: "/sandbox/bin"}}
	sbRun(t, s)
	if b, _ := os.ReadFile(filepath.Join(bin, "tool")); !strings.Contains(string(b), "tool-ran") {
		t.Error("read-only bind was written")
	}
}

func TestSeatbeltNoNetKeepsLoopback(t *testing.T) {
	f := newSBFixture(t)
	s := f.sh("nc -z -G 3 1.1.1.1 443")
	s.Net = false
	if _, err := sbRun(t, s); err == nil {
		t.Error("outbound network allowed without Net")
	}
}

func TestSeatbeltCancelKillsProcessGroup(t *testing.T) {
	f := newSBFixture(t)
	ctx, cancel := context.WithCancel(context.Background())
	cmd, err := Command(ctx, f.sh("sleep 300 & sleep 300; wait"))
	if err != nil {
		t.Fatal(err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	time.Sleep(500 * time.Millisecond)
	start := time.Now()
	cancel()
	_ = cmd.Wait()
	if time.Since(start) > 15*time.Second {
		t.Fatal("cancel did not stop the stage")
	}
	if out, _ := exec.Command("pgrep", "-g", strconv.Itoa(cmd.Process.Pid)).Output(); len(bytes.TrimSpace(out)) > 0 {
		t.Errorf("processes survived cancel: %s", out)
	}
}

func TestSeatbeltGoTestRuns(t *testing.T) {
	if _, err := exec.LookPath("go"); err != nil {
		t.Skip("go not installed")
	}
	f := newSBFixture(t)
	write := func(name, s string) {
		if err := os.WriteFile(filepath.Join(f.wt, name), []byte(s), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("go.mod", "module x\n\ngo 1.21\n")
	write("x_test.go", "package x\n\nimport \"testing\"\n\nfunc TestX(t *testing.T) {}\n")
	s := f.sh("go test ./...")
	s.ReadOnlyWorktree = true
	s.Net = false
	if out, err := sbRun(t, s); err != nil {
		t.Errorf("go test failed inside the sandbox: %v\n%s", err, out)
	}
}

func TestSeatbeltRefusesUnquotablePath(t *testing.T) {
	f := newSBFixture(t)
	bad := filepath.Join(f.root, `we"ird`)
	if err := os.MkdirAll(bad, 0o755); err != nil {
		t.Fatal(err)
	}
	s := f.sh("true")
	s.ExtraROBinds = []Bind{{Src: bad, Dst: "/sandbox/x"}}
	if _, err := Command(context.Background(), s); err == nil {
		t.Error("a path with a quote was accepted into the profile")
	}
}

// netcheckSrc is a helper the loopback test builds on the host and runs inside
// the sandbox: "loopback" listens on 127.0.0.1:0 and connects to itself,
// "external" dials 1.1.1.1:443.
const netcheckSrc = `package main

import (
	"fmt"
	"net"
	"os"
	"time"
)

func main() {
	switch os.Args[1] {
	case "loopback":
		ln, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			fmt.Println("LISTEN-FAIL", err)
			os.Exit(1)
		}
		go func() {
			c, err := ln.Accept()
			if err == nil {
				c.Write([]byte("pong"))
				c.Close()
			}
		}()
		c, err := net.DialTimeout("tcp", ln.Addr().String(), 3*time.Second)
		if err != nil {
			fmt.Println("DIAL-FAIL", err)
			os.Exit(1)
		}
		b := make([]byte, 4)
		n, _ := c.Read(b)
		fmt.Println("LOOPBACK-OK", string(b[:n]))
	case "external":
		c, err := net.DialTimeout("tcp", "1.1.1.1:443", 3*time.Second)
		if err != nil {
			fmt.Println("BLOCKED", err)
			os.Exit(3)
		}
		c.Close()
		fmt.Println("REACHED")
		os.Exit(2)
	}
}
`

func buildNetcheck(t *testing.T, root string) string {
	t.Helper()
	src := filepath.Join(root, "netcheck-src")
	if err := os.MkdirAll(src, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(src, "go.mod"), []byte("module netcheck\n\ngo 1.21\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(src, "main.go"), []byte(netcheckSrc), 0o644); err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(root, "netcheck")
	cmd := exec.Command("go", "build", "-o", out, ".")
	cmd.Dir = src
	if b, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("build netcheck: %v\n%s", err, b)
	}
	return out
}

// AF-6b: a verify stage without Net may listen and connect on 127.0.0.1
// (vstest's testhost handshake) and still reaches no other host, and the
// loopback relaxation does not open the credentials.
func TestSeatbeltLoopbackOnly(t *testing.T) {
	f := newSBFixture(t)
	bin := buildNetcheck(t, f.root)

	s := f.spec(bin, "loopback")
	s.Net, s.LoopbackNet = false, true
	if out, err := sbRun(t, s); err != nil || !strings.Contains(out, "LOOPBACK-OK") {
		t.Fatalf("loopback listen+connect failed: %v %q", err, out)
	}

	s = f.spec(bin, "external")
	s.Net, s.LoopbackNet = false, true
	if out, err := sbRun(t, s); err == nil {
		t.Fatalf("external network reachable with loopback only: %q", out)
	}

	// The relaxation is network only: credentials stay unreadable.
	leak := f.sh("cat " + filepath.Join(f.home, ".claude_chat_api"))
	leak.Net, leak.LoopbackNet = false, true
	if out, _ := sbRun(t, leak); strings.Contains(out, "SECRET") {
		t.Errorf("credential readable in a loopback-relaxed stage: %q", out)
	}
}
