package sandbox

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"
)

type fixture struct {
	root, repo, wt, gitDir, wtGitDir, stageHome string
}

func requireBwrap(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("bwrap"); err != nil {
		t.Fatalf("bwrap not installed; it is a fixed host dependency (D16): %v", err)
	}
}

func hostGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@example.com",
		"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@example.com", "GIT_CONFIG_GLOBAL=/dev/null")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

func newFixture(t *testing.T) fixture {
	t.Helper()
	requireBwrap(t)
	root := t.TempDir()
	f := fixture{
		root:      root,
		repo:      filepath.Join(root, "repo"),
		wt:        filepath.Join(root, "stages", "s1", "wt"),
		stageHome: filepath.Join(root, "stages", "s1", "home"),
	}
	for _, d := range []string{f.repo, f.stageHome, filepath.Join(root, "stages", "s2")} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	hostGit(t, f.repo, "init", "-q", "-b", "main")
	if err := os.WriteFile(filepath.Join(f.repo, "a.txt"), []byte("hello\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	hostGit(t, f.repo, "add", "a.txt")
	hostGit(t, f.repo, "commit", "-q", "-m", "init")
	hostGit(t, f.repo, "worktree", "add", "-q", "-b", "wtb", f.wt)
	f.gitDir = filepath.Join(f.repo, ".git")
	f.wtGitDir = filepath.Join(f.gitDir, "worktrees", "wt")
	return f
}

func (f fixture) spec(argv ...string) Spec {
	return Spec{Worktree: f.wt, RepoGitDir: f.gitDir, WorktreeGitDir: f.wtGitDir,
		StageHome: f.stageHome, Net: true, Argv: argv}
}

func (f fixture) run(t *testing.T, s Spec) (string, error) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
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

func sh(f fixture, script string) Spec { return f.spec("/bin/sh", "-c", script) }

// AC-WC1: credentials unreachable. Existence/readability checks only.
func TestCredentialsUnreachable(t *testing.T) {
	f := newFixture(t)
	fakeHome := filepath.Join(f.root, "fakehome")
	fake := map[string]string{
		".ssh/id_ed25519":      "x",
		".config/gh/hosts.yml": "x",
		".git-credentials":     "x",
	}
	for rel, c := range fake {
		p := filepath.Join(fakeHome, rel)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(c), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	realHome, _ := os.UserHomeDir()
	for _, home := range []string{fakeHome, realHome} {
		for rel := range fake {
			p := filepath.Join(home, rel)
			// Host side: the fake files are readable, proving the check is meaningful.
			if home == fakeHome {
				if _, err := os.ReadFile(p); err != nil {
					t.Fatalf("host cannot read fixture %s: %v", p, err)
				}
			}
			out, err := f.run(t, sh(f, fmt.Sprintf("test -e %q || test -r %q", p, p)))
			if err == nil {
				t.Errorf("%s reachable inside sandbox: %s", p, out)
			}
		}
	}
	// ~ inside the sandbox is the empty stage home.
	out, err := f.run(t, sh(f, `ls -A "$HOME"`))
	if err != nil || strings.TrimSpace(out) != "" {
		t.Errorf("stage home not empty: %v %q", err, out)
	}
}

func TestSSHAuthSockUnset(t *testing.T) {
	f := newFixture(t)
	t.Setenv("SSH_AUTH_SOCK", "/tmp/fake-agent.sock")
	t.Setenv("GITHUB_TOKEN", "should-not-leak")
	out, err := f.run(t, f.spec("/usr/bin/env"))
	if err != nil {
		t.Fatal(err, out)
	}
	if strings.Contains(out, "SSH_AUTH_SOCK") || strings.Contains(out, "TOKEN") {
		t.Errorf("leaked env: %s", out)
	}
}

func TestEnvExactlyAllowlist(t *testing.T) {
	f := newFixture(t)
	t.Setenv("SECRET_THING", "x")
	s := f.spec("/usr/bin/env")
	s.Env = map[string]string{"RUNNER_EXTRA": "1"}
	out, err := f.run(t, s)
	if err != nil {
		t.Fatal(err, out)
	}
	want := []string{
		"GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_NOSYSTEM=1", "GIT_OPTIONAL_LOCKS=0",
		"GIT_TERMINAL_PROMPT=0", "GOCACHE=/home/agent/.cache/go-build",
		"GOMODCACHE=/home/agent/go/pkg/mod", "GOPATH=/home/agent/go", "GOTOOLCHAIN=local",
		"HOME=/home/agent", "LANG=C.UTF-8",
		"PATH=/usr/local/go/bin:/usr/local/bin:/usr/bin:/bin", "RUNNER_EXTRA=1",
		// PWD is set by bwrap itself for --chdir; it is the worktree path.
		"PWD=" + f.wt,
		"TERM=dumb", "XDG_CACHE_HOME=/home/agent/.cache",
		"XDG_CONFIG_HOME=/home/agent/.config", "XDG_DATA_HOME=/home/agent/.local/share",
		"XDG_STATE_HOME=/home/agent/.local/state",
	}
	sort.Strings(want)
	got := strings.Fields(out)
	sort.Strings(got)
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Errorf("env mismatch\ngot:\n%s\nwant:\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
}

func TestGitReadOnlyWorksCommitFails(t *testing.T) {
	f := newFixture(t)
	for _, c := range []string{"status", "diff", "log --oneline", "ls-files", "rev-parse HEAD", "grep hello"} {
		if out, err := f.run(t, sh(f, "git "+c)); err != nil {
			t.Errorf("git %s failed: %v\n%s", c, err, out)
		}
	}
	if err := os.WriteFile(filepath.Join(f.wt, "a.txt"), []byte("changed\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, c := range []string{
		"git -c user.name=a -c user.email=a@b commit -am x",
		"git branch x", "git update-ref refs/heads/y HEAD", "git add a.txt",
	} {
		if out, err := f.run(t, sh(f, c)); err == nil {
			t.Errorf("%q succeeded inside sandbox: %s", c, out)
		}
	}
	out, err := f.run(t, sh(f, "git config --get credential.helper"))
	if err == nil || strings.TrimSpace(out) != "" {
		t.Errorf("credential.helper set: %v %q", err, out)
	}
	// Host repo untouched.
	b, _ := exec.Command("git", "-C", f.repo, "branch", "--list").CombinedOutput()
	if strings.Contains(string(b), " x") || strings.Contains(string(b), " y") {
		t.Errorf("host repo mutated: %s", b)
	}
}

func TestGitPushFailsWithoutCredentials(t *testing.T) {
	f := newFixture(t)
	t.Setenv("SSH_AUTH_SOCK", "/tmp/fake-agent.sock")
	out, err := f.run(t, sh(f, "git push ssh://git@127.0.0.1:1/x.git HEAD"))
	if err == nil {
		t.Fatalf("push succeeded: %s", out)
	}
	if strings.Contains(out, "->") || strings.Contains(strings.ToLower(out), "pushed") {
		t.Errorf("push looks successful: %s", out)
	}
	if !strings.Contains(out, "Could not read from remote repository") &&
		!strings.Contains(out, "Permission denied") && !strings.Contains(out, "ssh") {
		t.Errorf("unexpected push failure reason: %s", out)
	}
}

func TestWritesOutsideWorktreeFail(t *testing.T) {
	f := newFixture(t)
	if out, err := f.run(t, sh(f, "echo ok > "+filepath.Join(f.wt, "new.txt")+" && echo ok > $HOME/h.txt && echo ok > /tmp/t.txt")); err != nil {
		t.Fatalf("allowed writes failed: %v %s", err, out)
	}
	for _, p := range []string{"/usr/x", "/etc/x", f.gitDir + "/x", f.wtGitDir + "/x"} {
		if out, err := f.run(t, sh(f, "echo no > "+p)); err == nil {
			t.Errorf("write to %s succeeded: %s", p, out)
		}
	}
	if _, err := os.Stat("/tmp/t.txt"); err == nil {
		t.Error("sandbox /tmp write reached host /tmp")
	}
}

func TestServerDataAndSiblingStagesInvisible(t *testing.T) {
	f := newFixture(t)
	// A "server data dir" under a simulated server cwd.
	serverCwd := filepath.Join(f.root, "servercwd")
	if err := os.MkdirAll(filepath.Join(serverCwd, "data"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Chdir(serverCwd)
	abs := filepath.Join(serverCwd, "data")
	secret := []string{
		filepath.Join(abs, "auth.txt"),
		filepath.Join(f.root, "agentflow.db"),
		filepath.Join(f.root, "stages", "s2", "prompt.md"),
	}
	for _, p := range secret {
		if err := os.WriteFile(p, []byte("secret"), 0o644); err != nil {
			t.Fatal(err)
		}
		if out, err := f.run(t, sh(f, fmt.Sprintf("cat %q", p))); err == nil {
			t.Errorf("read %s inside sandbox: %s", p, out)
		}
	}
	// Whole worktree root siblings and top-level host dirs are absent.
	out, err := f.run(t, sh(f, "ls -d /opt /var /srv /mnt /root 2>&1; test ! -e /opt && test ! -e /var && test ! -e /srv"))
	if err != nil {
		t.Errorf("/opt, /var or /srv visible: %s", out)
	}
	if out, err := f.run(t, sh(f, "ls "+filepath.Join(f.root, "stages"))); err == nil && strings.Contains(out, "s2") {
		t.Errorf("sibling stage listed: %s", out)
	}
}

func TestExtraROBindsOnlyWhenPassed(t *testing.T) {
	f := newFixture(t)
	fakeHome := filepath.Join(f.root, "fakehome")
	bin := filepath.Join(fakeHome, ".opencode", "bin")
	if err := os.MkdirAll(bin, 0o755); err != nil {
		t.Fatal(err)
	}
	// Symlink to the real file, like ~/.opencode/bin/opencode.
	realBin := filepath.Join(fakeHome, "real-tool")
	if err := os.WriteFile(realBin, []byte("#!/bin/sh\necho tool-ran\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(bin, "tool")
	if err := os.Symlink(realBin, link); err != nil {
		t.Fatal(err)
	}
	if out, err := f.run(t, f.spec(link)); err == nil {
		t.Errorf("fake-HOME binary ran without a bind: %s", out)
	}
	s := f.spec("tool")
	s.ExtraROBinds = []Bind{{Src: link, Dst: "/sandbox/bin/tool"}}
	s.PathDirs = []string{"/sandbox/bin"}
	out, err := f.run(t, s)
	if err != nil || strings.TrimSpace(out) != "tool-ran" {
		t.Errorf("bound binary: %v %q", err, out)
	}
	// The bind is read-only.
	s.Argv = []string{"/bin/sh", "-c", "echo x > /sandbox/bin/tool"}
	if out, err := f.run(t, s); err == nil {
		t.Errorf("extra bind writable: %s", out)
	}
}

func TestGoTestRuns(t *testing.T) {
	f := newFixture(t)
	if err := os.WriteFile(filepath.Join(f.wt, "go.mod"), []byte("module m\n\ngo 1.21\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(f.wt, "a_test.go"), []byte("package m\nimport \"testing\"\nfunc TestA(t *testing.T){}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if out, err := f.run(t, sh(f, "go test ./...")); err != nil {
		t.Errorf("go test in sandbox: %v\n%s", err, out)
	}
}

func procsWithArg(arg string) []string {
	var hits []string
	ents, _ := os.ReadDir("/proc")
	for _, e := range ents {
		b, err := os.ReadFile("/proc/" + e.Name() + "/cmdline")
		if err == nil && bytes.Contains(b, []byte("sleep\x00"+arg)) {
			hits = append(hits, e.Name())
		}
	}
	return hits
}

// AC-RC2 (unit half): cancel leaves no descendants.
func TestCancelKillsProcessGroup(t *testing.T) {
	f := newFixture(t)
	arg := fmt.Sprintf("%d.25", 400+os.Getpid()%500)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	cmd, err := Command(ctx, sh(f, fmt.Sprintf("sleep %s & sleep %s", arg, arg)))
	if err != nil {
		t.Fatal(err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	deadline := time.Now().Add(5 * time.Second)
	for len(procsWithArg(arg)) < 2 && time.Now().Before(deadline) {
		time.Sleep(50 * time.Millisecond)
	}
	if n := len(procsWithArg(arg)); n < 2 {
		t.Fatalf("expected 2 sleeps running, saw %d", n)
	}
	cancel()
	select {
	case <-done:
	case <-time.After(8 * time.Second):
		t.Fatal("Wait did not return after cancel")
	}
	deadline = time.Now().Add(6 * time.Second)
	for len(procsWithArg(arg)) > 0 && time.Now().Before(deadline) {
		time.Sleep(100 * time.Millisecond)
	}
	if left := procsWithArg(arg); len(left) > 0 {
		t.Errorf("orphans after cancel: %v", left)
	}
}

func TestTimeoutKillsProcessGroup(t *testing.T) {
	f := newFixture(t)
	arg := fmt.Sprintf("%d.75", 400+os.Getpid()%500)
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	cmd, err := Command(ctx, sh(f, fmt.Sprintf("sleep %s & sleep %s", arg, arg)))
	if err != nil {
		t.Fatal(err)
	}
	start := time.Now()
	if err := cmd.Run(); err == nil {
		t.Error("expected error from timed-out command")
	}
	if d := time.Since(start); d > 8*time.Second {
		t.Errorf("took %v to stop", d)
	}
	deadline := time.Now().Add(6 * time.Second)
	for len(procsWithArg(arg)) > 0 && time.Now().Before(deadline) {
		time.Sleep(100 * time.Millisecond)
	}
	if left := procsWithArg(arg); len(left) > 0 {
		t.Errorf("orphans after timeout: %v", left)
	}
}

func TestArgvShape(t *testing.T) {
	requireBwrap(t)
	cmd, err := Command(context.Background(), Spec{Worktree: "/w", RepoGitDir: "/r/.git",
		WorktreeGitDir: "/r/.git/worktrees/w", StageHome: "/h", Argv: []string{"true"}})
	if err != nil {
		t.Fatal(err)
	}
	a := strings.Join(cmd.Args, " ")
	if strings.Contains(a, "--ro-bind / /") {
		t.Error("root must not be bound wholesale")
	}
	ci, si := strings.Index(a, "--clearenv"), strings.Index(a, "--setenv")
	if ci < 0 || si < ci {
		t.Errorf("--clearenv must precede --setenv: %s", a)
	}
	for _, f := range []string{"--unshare-pid", "--die-with-parent", "--new-session", "--unshare-net", "--ro-bind /r/.git /r/.git", "--ro-bind /r/.git/worktrees/w"} {
		if !strings.Contains(a, f) {
			t.Errorf("missing %s", f)
		}
	}
	if !cmd.SysProcAttr.Setpgid {
		t.Error("Setpgid not set")
	}
}

func TestValidate(t *testing.T) {
	requireBwrap(t)
	ok := Spec{Worktree: "/w", RepoGitDir: "/g", WorktreeGitDir: "/g/w", StageHome: "/h", Argv: []string{"true"}}
	bad := []Spec{ok, ok, ok, ok, ok, ok, ok, ok, ok}
	bad[0].Worktree = "rel"
	bad[1].Argv = nil
	bad[2].StageHome = ""
	bad[3].ExtraROBinds = []Bind{{Src: "x", Dst: "/y"}}
	bad[4].Worktree = "/r" // contains RepoGitDir
	bad[4].RepoGitDir, bad[4].WorktreeGitDir = "/r/.git", "/r/.git/worktrees/w"
	bad[5].ExtraROBinds = []Bind{{Src: "/usr/bin/true", Dst: "/usr"}}
	bad[6].ExtraROBinds = []Bind{{Src: "/usr/bin/true", Dst: "/w/x"}}
	bad[7].WorktreeGitDir = "/elsewhere/w"
	bad[8].Env = map[string]string{"GITHUB_TOKEN": "x"}
	for i, s := range bad {
		if _, err := Command(context.Background(), s); err == nil {
			t.Errorf("case %d: expected error", i)
		}
	}
}

func TestEnvDenylist(t *testing.T) {
	requireBwrap(t)
	ok := Spec{Worktree: "/w", RepoGitDir: "/g", WorktreeGitDir: "/g/worktrees/w", StageHome: "/h", Argv: []string{"true"}}
	for _, k := range []string{"HOME", "PATH", "LD_PRELOAD", "LD_LIBRARY_PATH", "SSH_AUTH_SOCK",
		"GIT_ASKPASS", "GIT_SSH", "GIT_SSH_COMMAND", "GH_TOKEN", "OPENCODE_CONFIG", "OPENCODE_CONFIG_DIR"} {
		s := ok
		s.Env = map[string]string{k: "x"}
		if _, err := Command(context.Background(), s); err == nil {
			t.Errorf("%s accepted in Spec.Env", k)
		}
	}
	s := ok
	s.Env = map[string]string{"OPENAI_BASE_URL": "http://x"}
	if _, err := Command(context.Background(), s); err != nil {
		t.Errorf("plain extra rejected: %v", err)
	}
}

func TestGitfileReadOnly(t *testing.T) {
	f := newFixture(t)
	if out, err := f.run(t, sh(f, "echo gitdir: /nowhere > $PWD/.git")); err == nil {
		t.Errorf("gitfile rewritable: %s", out)
	}
	if out, err := f.run(t, sh(f, "rm $PWD/.git")); err == nil {
		t.Errorf("gitfile removable: %s", out)
	}
	if out, err := f.run(t, sh(f, "echo ok > $PWD/other.txt")); err != nil {
		t.Errorf("worktree not writable: %v %s", err, out)
	}
}

func TestReadOnlyWorktree(t *testing.T) {
	f := newFixture(t)
	s := sh(f, "echo x > $PWD/new.txt")
	s.ReadOnlyWorktree = true
	if out, err := f.run(t, s); err == nil {
		t.Errorf("write to read-only worktree succeeded: %s", out)
	}
	s = sh(f, "echo x > $PWD/a.txt")
	s.ReadOnlyWorktree = true
	if out, err := f.run(t, s); err == nil {
		t.Errorf("overwrite in read-only worktree succeeded: %s", out)
	}
	s = sh(f, "git status && cat a.txt && echo ok > $HOME/h")
	s.ReadOnlyWorktree = true
	if out, err := f.run(t, s); err != nil {
		t.Errorf("read-only worktree should still be readable and home writable: %v %s", err, out)
	}
}

func TestUserNamespacesDisabled(t *testing.T) {
	f := newFixture(t)
	if out, err := f.run(t, sh(f, "unshare -r true")); err == nil {
		t.Errorf("nested user namespace allowed: %s", out)
	}
	if out, err := f.run(t, sh(f, "true")); err != nil {
		t.Errorf("sanity: %v %s", err, out)
	}
}

// A planning stage runs in the project repository itself: the worktree is the
// repo, both git dirs are its .git, and nothing in it is writable.
func TestReadOnlyRepoStage(t *testing.T) {
	f := newFixture(t)
	s := f.spec("/bin/sh", "-c", `git status --short >/dev/null && ! echo x > a.txt && ! echo x > .git/HEAD`)
	s.Worktree, s.WorktreeGitDir, s.ReadOnlyWorktree = f.repo, f.gitDir, true
	if out, err := f.run(t, s); err != nil {
		t.Fatalf("repo stage: %v: %s", err, out)
	}
	// Without ReadOnlyWorktree the overlap stays rejected.
	s.ReadOnlyWorktree = false
	if _, err := Command(context.Background(), s); err == nil {
		t.Error("writable repo-as-worktree accepted")
	}
}
