package sandbox

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// containerSpec builds a valid spec with the real directories the mount
// builder resolves.
func containerSpec(t *testing.T, d *Docker, readOnly bool) Spec {
	t.Helper()
	root := t.TempDir()
	repoGit := filepath.Join(root, "repo", ".git")
	wtGit := filepath.Join(repoGit, "worktrees", "w1")
	worktree := filepath.Join(root, "wt")
	home := filepath.Join(root, "home")
	for _, dir := range []string{repoGit, wtGit, worktree, home} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(worktree, ".git"), []byte("gitdir: "+wtGit+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	return Spec{
		Worktree: worktree, ReadOnlyWorktree: readOnly,
		RepoGitDir: repoGit, WorktreeGitDir: wtGit, StageHome: home,
		Docker: d, Argv: []string{"go", "test", "./..."},
	}
}

// resolves is the path a bind mount uses after symlinks are resolved.
func resolves(t *testing.T, p string) string {
	t.Helper()
	r, err := filepath.EvalSymlinks(p)
	if err != nil {
		t.Fatal(err)
	}
	return r
}

func flagValue(args []string, flag string) string {
	for i, a := range args {
		if a == flag && i+1 < len(args) {
			return args[i+1]
		}
	}
	return ""
}

func hasVolume(args []string, want string) bool {
	for i, a := range args {
		if a == "--volume" && i+1 < len(args) && args[i+1] == want {
			return true
		}
	}
	return false
}

func hasPair(args []string, a, b string) bool {
	for i := 0; i+1 < len(args); i++ {
		if args[i] == a && args[i+1] == b {
			return true
		}
	}
	return false
}

// TestDockerArgsShape: a container gets the worktree (read-write for a worker),
// the stage home at the sandbox HOME, the git dirs read-only, no host
// environment and no network.
func TestDockerArgsShape(t *testing.T) {
	spec := containerSpec(t, &Docker{Image: "agentflow-worker:0.1", Name: "af-oc3-1-2-verify", Network: NetworkNone}, false)
	args, err := dockerArgs(spec)
	if err != nil {
		t.Fatal(err)
	}
	if len(args) < 2 || args[0] != "run" {
		t.Fatalf("args do not start with docker run: %v", args)
	}
	if flagValue(args, "--name") != "af-oc3-1-2-verify" || flagValue(args, "--hostname") != "localhost" {
		t.Fatalf("name/hostname: %v", args)
	}
	if !hasPair(args, "--network", "none") {
		t.Fatalf("network: %v", args)
	}
	wt, home := resolves(t, spec.Worktree), resolves(t, spec.StageHome)
	if !hasVolume(args, wt+":"+spec.Worktree+":rw") {
		t.Fatalf("worker worktree not mounted read-write: %v", args)
	}
	if !hasVolume(args, home+":"+SandboxHome+":rw") {
		t.Fatalf("stage home not mounted at %s: %v", SandboxHome, args)
	}
	if !hasVolume(args, resolves(t, spec.RepoGitDir)+":"+spec.RepoGitDir+":ro") {
		t.Fatalf("git dir not read-only: %v", args)
	}
	if flagValue(args, "--workdir") != spec.Worktree {
		t.Fatalf("workdir: %v", args)
	}
	if args[len(args)-4] != "agentflow-worker:0.1" || strings.Join(args[len(args)-4:], " ") != "agentflow-worker:0.1 go test ./..." {
		t.Fatalf("image and argv: %v", args)
	}
	if !hasPair(args, "--env", "HOME="+SandboxHome) {
		t.Fatalf("HOME is not the stage home: %v", args)
	}
	if !hasPair(args, "--tmpfs", SandboxHome+"/.cache") {
		t.Fatalf("tool caches are not container-local: %v", args)
	}
	for _, a := range args {
		if strings.HasPrefix(a, "PATH=") || strings.Contains(a, "SSH_AUTH_SOCK") || strings.Contains(a, "GIT_ASKPASS") {
			t.Fatalf("host environment leaked into the container: %q", a)
		}
	}
}

// TestDockerArgsReadOnlyStage: a reviewer or verify stage cannot write the
// worktree it reads.
func TestDockerArgsReadOnlyStage(t *testing.T) {
	spec := containerSpec(t, &Docker{Image: "agentflow-worker:0.1", Name: "af-oc3-1-2-review"}, true)
	args, err := dockerArgs(spec)
	if err != nil {
		t.Fatal(err)
	}
	if !hasVolume(args, resolves(t, spec.Worktree)+":"+spec.Worktree+":ro") {
		t.Fatalf("read-only worktree not mounted read-only: %v", args)
	}
}

// TestDockerArgsProviderAndSELinux: a provider stage reaches the proxy, not the
// open internet, and SELinux relabels every bind.
func TestDockerArgsProviderAndSELinux(t *testing.T) {
	spec := containerSpec(t, &Docker{
		Image: "agentflow-worker:0.1", Name: "af-oc3-1-2-work",
		Network: NetworkProvider, Proxy: "http://127.0.0.1:8888", SELinux: true,
	}, false)
	args, err := dockerArgs(spec)
	if err != nil {
		t.Fatal(err)
	}
	if hasPair(args, "--network", "none") {
		t.Fatalf("provider stage has no network at all: %v", args)
	}
	if !hasPair(args, "--add-host", "host.docker.internal:host-gateway") {
		t.Fatalf("provider stage cannot reach the host proxy: %v", args)
	}
	if !hasPair(args, "--env", "HTTPS_PROXY=http://127.0.0.1:8888") {
		t.Fatalf("proxy not passed: %v", args)
	}
	if !hasVolume(args, resolves(t, spec.Worktree)+":"+spec.Worktree+":rw,z") {
		t.Fatalf("SELinux relabel missing: %v", args)
	}
}

// TestDockerArgsRunner: the image's runner replaces a host binary a Linux
// container cannot execute.
func TestDockerArgsRunner(t *testing.T) {
	spec := containerSpec(t, &Docker{Image: "agentflow-worker:0.1", Name: "af-oc3-0-0-stage-7", Runner: "/usr/local/bin/opencode"}, false)
	spec.Argv = []string{"/Users/x/.opencode/bin/opencode", "serve", "--port", "0"}
	args, err := dockerArgs(spec)
	if err != nil {
		t.Fatal(err)
	}
	tail := strings.Join(args[len(args)-4:], " ")
	if tail != "/usr/local/bin/opencode serve --port 0" {
		t.Fatalf("runner not replaced: %v", tail)
	}
}

// TestDockerArgsValidation: a container that cannot be described safely is
// refused before it starts.
func TestDockerArgsValidation(t *testing.T) {
	for _, c := range []struct {
		name string
		d    *Docker
		want string
	}{
		{"no image", &Docker{Name: "af-p-0-0-s"}, "image is required"},
		{"image without tag", &Docker{Image: "worker", Name: "af-p-0-0-s"}, "name:tag"},
		{"bad name", &Docker{Image: "w:1", Name: "af/p/0"}, "container name"},
		{"bad network", &Docker{Image: "w:1", Name: "af-p-0-0-s", Network: "bridge"}, "must be none, provider or full"},
		{"provider without proxy", &Docker{Image: "w:1", Name: "af-p-0-0-s", Network: NetworkProvider}, "needs a proxy"},
		{"bad user", &Docker{Image: "w:1", Name: "af-p-0-0-s", User: "me"}, "uid:gid"},
		{"mount over worktree", &Docker{Image: "w:1", Name: "af-p-0-0-s", Mounts: []Mount{{Src: "/tmp", Dst: "/work"}}}, "overlaps"},
	} {
		t.Run(c.name, func(t *testing.T) {
			spec := containerSpec(t, c.d, false)
			if c.name == "mount over worktree" {
				c.d.Mounts[0].Dst = spec.Worktree
			}
			if _, err := dockerArgs(spec); err == nil || !strings.Contains(err.Error(), c.want) {
				t.Fatalf("error = %v, want %q", err, c.want)
			}
		})
	}
}

func TestParseMount(t *testing.T) {
	for _, c := range []struct {
		in   string
		want Mount
		ok   bool
	}{
		{"/a/b:/c", Mount{Src: "/a/b", Dst: "/c"}, true},
		{"/a/b:/c:ro", Mount{Src: "/a/b", Dst: "/c"}, true},
		{"/a/b:/c:rw", Mount{Src: "/a/b", Dst: "/c", RW: true}, true},
		{"a/b:/c", Mount{}, false},
		{"/a/b:relative", Mount{}, false},
		{"/a/b:/c:z", Mount{}, false},
		{"/a/b", Mount{}, false},
	} {
		got, err := ParseMount(c.in)
		if c.ok && (err != nil || got != c.want) {
			t.Errorf("ParseMount(%q) = %+v, %v", c.in, got, err)
		}
		if !c.ok && err == nil {
			t.Errorf("ParseMount(%q) accepted", c.in)
		}
	}
}

// TestDockerSecretsNeverInArgv: what the stage passes (its backend's API key) is named on the
// docker command line but its value travels in the docker CLI's environment, so `ps` and logs
// never show it (AF-12 review finding 1).
func TestDockerSecretsNeverInArgv(t *testing.T) {
	fake := t.TempDir()
	if err := os.WriteFile(filepath.Join(fake, "docker"), []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", fake+string(os.PathListSeparator)+os.Getenv("PATH"))
	spec := containerSpec(t, &Docker{Image: "agentflow-worker:0.1", Name: "af-oc3-1-2-worker", Network: NetworkNone}, false)
	spec.Env = map[string]string{"AGENTFLOW_KEY_GPU": "sk-very-secret", "LANG": "en_US.UTF-8"}
	cmd, err := dockerCommand(context.Background(), spec)
	if err != nil {
		t.Fatal(err)
	}
	for _, a := range cmd.Args {
		if strings.Contains(a, "sk-very-secret") {
			t.Fatalf("the key is in the docker argv: %v", cmd.Args)
		}
	}
	if !hasPair(cmd.Args, "--env", "AGENTFLOW_KEY_GPU") || !hasPair(cmd.Args, "--env", "LANG") || hasPair(cmd.Args, "--env", "LANG=C.UTF-8") {
		t.Fatalf("stage variables must be passed by name, overriding the allowlist: %v", cmd.Args)
	}
	found := false
	for _, kv := range cmd.Env {
		if kv == "AGENTFLOW_KEY_GPU=sk-very-secret" {
			found = true
		}
	}
	if !found {
		t.Fatal("the docker CLI must carry the value in its environment")
	}
}

// TestDockerRefusesUnsharedPaths: on Colima a bind source the VM does not share is an empty
// directory in the container; it is refused before anything runs (AF-12 review finding 2).
func TestDockerRefusesUnsharedPaths(t *testing.T) {
	spec := containerSpec(t, &Docker{Image: "agentflow-worker:0.1", Name: "af-oc3-1-2-verify", Network: NetworkNone}, true)
	spec.Docker.Shared = []string{t.TempDir()} // somewhere else
	if _, err := dockerArgs(spec); err == nil || !strings.Contains(err.Error(), "not shared with the docker VM") {
		t.Fatalf("an unshared worktree must be refused: %v", err)
	}
	spec.Docker.Shared = []string{filepath.Dir(filepath.Dir(spec.Worktree))} // covers the test root
	if _, err := dockerArgs(spec); err != nil {
		t.Fatalf("a shared tree is fine: %v", err)
	}
	spec.Docker.Shared = nil // a native daemon sees every path
	if _, err := dockerArgs(spec); err != nil {
		t.Fatal(err)
	}
}
