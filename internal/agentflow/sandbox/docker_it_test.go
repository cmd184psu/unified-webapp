package sandbox

// The container integration test needs the worker image and a running Docker.
// It is skipped unless AGENTFLOW_DOCKER_IT=1, so `go test ./...` on a machine
// without Docker (or without the image) stays green:
//
//	docker buildx build --load -t agentflow-worker:0.1 \
//	  --build-arg GHIDRA_URL=<zip> ~/work/agentflow-image
//	AGENTFLOW_DOCKER_IT=1 go test ./internal/agentflow/sandbox -run Integration -v

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func dockerITImage() string {
	if v := os.Getenv("AGENTFLOW_DOCKER_IT_IMAGE"); v != "" {
		return v
	}
	return DefaultDockerImage
}

func requireDockerIT(t *testing.T) string {
	t.Helper()
	if os.Getenv("AGENTFLOW_DOCKER_IT") == "" {
		t.Skip("set AGENTFLOW_DOCKER_IT=1 to run the container integration test")
	}
	if _, err := exec.LookPath("docker"); err != nil {
		t.Skipf("docker is not installed: %v", err)
	}
	img := dockerITImage()
	if err := exec.Command("docker", "image", "inspect", img).Run(); err != nil {
		t.Skipf("image %s is not loaded: %v", img, err)
	}
	return img
}

// itSpec is a valid container spec over a fresh worktree and stage home.
func itSpec(t *testing.T, image, name string, argv ...string) Spec {
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
	return Spec{
		Worktree: worktree, RepoGitDir: repoGit, WorktreeGitDir: wtGit, StageHome: home,
		Docker: &Docker{Image: image, Name: name, Network: NetworkNone, User: HostUser()},
		Argv:   argv,
	}
}

func runIT(t *testing.T, spec Spec) string {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Minute)
	defer cancel()
	cmd, err := Command(ctx, spec)
	if err != nil {
		t.Fatal(err)
	}
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("%v: %s", err, out)
	}
	return string(out)
}

func writeIT(t *testing.T, dir, name, body string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

// TestDockerIntegrationGo: a Go feedback command runs in the image, offline.
func TestDockerIntegrationGo(t *testing.T) {
	img := requireDockerIT(t)
	spec := itSpec(t, img, "af-it-go", "go", "build", "./...")
	writeIT(t, spec.Worktree, "go.mod", "module example.com/it\n\ngo 1.22\n")
	writeIT(t, spec.Worktree, "main.go", "package main\n\nfunc main() {}\n")
	runIT(t, spec)
}

// TestDockerIntegrationCSharp: a C# feedback command runs in the image with
// -m:1 and no wrapper script.
func TestDockerIntegrationCSharp(t *testing.T) {
	img := requireDockerIT(t)
	spec := itSpec(t, img, "af-it-cs", "dotnet", "build", "-m:1")
	writeIT(t, spec.Worktree, "app.csproj", `<Project Sdk="Microsoft.NET.Sdk">
  <PropertyGroup>
    <OutputType>Exe</OutputType>
    <TargetFramework>net8.0</TargetFramework>
  </PropertyGroup>
</Project>
`)
	writeIT(t, spec.Worktree, "Program.cs", "class Program { static void Main() {} }\n")
	runIT(t, spec)
}

// TestDockerIntegrationIsolation: the container sees its stage home, not the
// host's HOME or a sibling of the mounted paths.
func TestDockerIntegrationIsolation(t *testing.T) {
	img := requireDockerIT(t)
	root := t.TempDir()
	canary := filepath.Join(root, "canary")
	if err := os.WriteFile(canary, []byte("secret"), 0o600); err != nil {
		t.Fatal(err)
	}
	spec := itSpec(t, img, "af-it-iso", "sh", "-c", `test "$HOME" = /home/agent && test ! -e "`+canary+`"`)
	runIT(t, spec)
}

// TestDockerIntegrationNetworkNoneBlocksEgress: --network none has no route to
// the internet.
func TestDockerIntegrationNetworkNoneBlocksEgress(t *testing.T) {
	img := requireDockerIT(t)
	spec := itSpec(t, img, "af-it-net", "sh", "-c", "curl -sS -m 3 https://example.com >/dev/null && exit 1 || exit 0")
	runIT(t, spec)
}

// TestDockerIntegrationGhidra: a Ghidra headless report task runs in the image
// with no network, the way AF-12 is meant to be used.
func TestDockerIntegrationGhidra(t *testing.T) {
	img := requireDockerIT(t)
	// Ghidra cannot create its settings directory on the virtiofs mount, so its
	// config and Java home are container-local; the Java project name check
	// needs user.name.
	spec := itSpec(t, img, "af-it-ghidra", "sh", "-c",
		`mkdir -p /tmp/ghidra-home /tmp/ghidra-config "$HOME/ghidra" && analyzeHeadless "$HOME/ghidra" af12 -import /bin/true -noanalysis -deleteProject`)
	spec.Env = map[string]string{
		"JAVA_TOOL_OPTIONS": "-Duser.home=/tmp/ghidra-home -Duser.name=agentflow",
		"XDG_CONFIG_HOME":   "/tmp/ghidra-config",
	}
	out := runIT(t, spec)
	if !strings.Contains(out, "REPORT: Import succeeded") {
		t.Fatalf("ghidra did not import: %s", out)
	}
}

// TestDockerIntegrationProviderHasProxy: a provider stage gets the proxy
// variables and can reach the host.
func TestDockerIntegrationProviderHasProxy(t *testing.T) {
	img := requireDockerIT(t)
	spec := itSpec(t, img, "af-it-proxy", "sh", "-c", `test -n "$HTTPS_PROXY" && echo "$HTTPS_PROXY"`)
	spec.Docker.Network = NetworkProvider
	spec.Docker.Proxy = "http://127.0.0.1:9"
	out := runIT(t, spec)
	if !strings.Contains(out, "127.0.0.1:9") {
		t.Fatalf("proxy not passed into the container: %q", out)
	}
}
