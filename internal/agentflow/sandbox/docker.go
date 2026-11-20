package sandbox

// The Docker backend (AF-12) runs a stage as a container instead of under bwrap
// or Seatbelt. A project selects it when its toolchain does not exist on the
// host (the C# SDK plus a Ghidra install), so the whole stage, not only its
// feedback commands, runs in the worker image. The container gets the stage's
// worktree, its git admin directories and its stage home, and nothing else;
// credentials stay out of it: no git or forge configuration, no server
// environment except the variables the stage itself passes, no host HOME.

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strings"
	"time"
)

// DefaultDockerImage is the image a docker project uses unless it names one.
const DefaultDockerImage = "agentflow-worker:0.1"

// Network modes for a stage container.
const (
	// NetworkNone gives the container no network at all. Ghidra report tasks
	// and anything that only reads files use it.
	NetworkNone = "none"
	// NetworkProvider allows the model provider and package mirrors through the
	// configured proxy (Docker.Proxy); nothing else is routed by the tools
	// that honour the proxy variables.
	NetworkProvider = "provider"
	// NetworkFull is plain bridge networking.
	NetworkFull = "full"
)

// Mount is one extra bind mount for a container. The default is read-only; a
// project that needs a writable cache sets RW.
type Mount struct {
	Src string
	Dst string
	RW  bool
}

// Docker describes the container a stage runs in.
type Docker struct {
	// Image is name:tag. Required.
	Image string
	// Network is none, provider or full. Empty means none.
	Network string
	// Name is the container name: af-<project>-<effort>-<task>-<stage>. Required
	// so a cancel, a status check and orphan cleanup can find it by name.
	Name string
	// User is "uid:gid" for a Linux host, where a bind mount keeps the host's
	// ownership. On macOS (Colima, virtiofs) it stays empty: the image's user
	// owns the mounted files.
	User string
	// SELinux relabels the bind mounts (:z) on hosts that need it.
	SELinux bool
	// Runner, when set, replaces Argv[0] with a path inside the image. A model
	// stage's argv[0] is a host binary, and a macOS host cannot run in a Linux
	// container; the image must carry the runner.
	Runner string
	// Mounts are extra project mounts, read-only unless RW is set.
	Mounts []Mount
	// Proxy is the host proxy a provider stage uses. Required for NetworkProvider.
	Proxy string
	// Instance labels the container with the agentflow instance that runs it
	// (agentflow.instance=<id>), so startup cleanup removes only its own leftovers: dev
	// and production share one VM.
	Instance string
	// Shared are the host directories the runtime can see (a Colima VM's mounts). A bind
	// source outside them would be an empty directory in the VM, so it is refused before
	// anything runs. Nil means every path is visible (a native Linux daemon).
	Shared []string
}

var (
	containerNameRE = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.-]{0,62}$`)
	containerUserRE = regexp.MustCompile(`^[0-9]+:[0-9]+$`)
)

// ContainerName builds the name carried by every container of one stage:
// af-<project>-<effort>-<task>-<stage>. Docker names may not contain '/', so a
// project slug with one cannot be a docker project.
func ContainerName(project string, effort, task int64, stage string) string {
	return fmt.Sprintf("af-%s-%d-%d-%s", project, effort, task, stage)
}

// NewDocker builds a container description from a project's stored settings.
// Empty image and network take the defaults; mounts are parsed here so a bad
// project setting fails before a stage starts.
func NewDocker(image, network string, selinux bool, proxy, name, user string, mounts []string) (*Docker, error) {
	d := &Docker{Image: image, Network: network, SELinux: selinux, Proxy: proxy, Name: name, User: user}
	if d.Image == "" {
		d.Image = DefaultDockerImage
	}
	if d.Network == "" {
		d.Network = NetworkNone
	}
	if d.Network == NetworkProvider && d.Proxy == "" {
		return nil, errors.New("sandbox: the provider network needs a proxy URL (AGENTFLOW_DOCKER_PROXY)")
	}
	for _, s := range mounts {
		m, err := ParseMount(s)
		if err != nil {
			return nil, err
		}
		d.Mounts = append(d.Mounts, m)
	}
	return d, nil
}

// HostUser is the user a container runs as on Linux: the host's uid and gid, so
// a bind-mounted worktree keeps its owner and build output is not root-owned.
// On macOS Colima's virtiofs maps the mounts to the container user, and the
// image's user is also the one .NET expects (an arbitrary uid without a passwd
// entry makes its first-run mutex fail), so this stays empty there.
func HostUser() string {
	if runtime.GOOS != "linux" {
		return ""
	}
	return fmt.Sprintf("%d:%d", os.Getuid(), os.Getgid())
}

// ParseMount reads a project's "src:dst" or "src:dst:rw" mount. A path holding
// ':' cannot be expressed; that is a limitation of the format and is reported.
func ParseMount(s string) (Mount, error) {
	parts := strings.Split(s, ":")
	if len(parts) < 2 || len(parts) > 3 {
		return Mount{}, fmt.Errorf("sandbox: mount %q must be src:dst or src:dst:rw", s)
	}
	m := Mount{Src: parts[0], Dst: parts[1]}
	if len(parts) == 3 {
		switch parts[2] {
		case "rw":
			m.RW = true
		case "ro":
		default:
			return Mount{}, fmt.Errorf("sandbox: mount %q: mode must be ro or rw", s)
		}
	}
	if !filepath.IsAbs(m.Src) || !filepath.IsAbs(m.Dst) || filepath.Clean(m.Src) != m.Src || filepath.Clean(m.Dst) != m.Dst {
		return Mount{}, fmt.Errorf("sandbox: mount %q needs absolute, clean paths", s)
	}
	return m, nil
}

// validate checks the container description. It is called from Spec.validate.
func (d *Docker) validate(spec Spec) error {
	if d.Image == "" {
		return errors.New("sandbox: docker image is required")
	}
	if strings.ContainsAny(d.Image, " \t\n") || !strings.Contains(d.Image, ":") {
		return fmt.Errorf("sandbox: docker image %q is not name:tag", d.Image)
	}
	if !containerNameRE.MatchString(d.Name) {
		return fmt.Errorf("sandbox: docker container name %q is invalid", d.Name)
	}
	switch d.Network {
	case "", NetworkNone, NetworkProvider, NetworkFull:
	default:
		return fmt.Errorf("sandbox: docker network %q must be none, provider or full", d.Network)
	}
	if d.Network == NetworkProvider && d.Proxy == "" {
		return errors.New("sandbox: the provider network needs a proxy URL")
	}
	if d.Proxy != "" && !strings.HasPrefix(d.Proxy, "http://") && !strings.HasPrefix(d.Proxy, "https://") {
		return fmt.Errorf("sandbox: proxy %q must be an http(s) URL", d.Proxy)
	}
	if d.User != "" && !containerUserRE.MatchString(d.User) {
		return fmt.Errorf("sandbox: docker user %q must be uid:gid", d.User)
	}
	for _, m := range d.Mounts {
		for _, p := range append([]string{spec.Worktree, spec.RepoGitDir, spec.StageHome}, protectedDsts...) {
			if m.Dst == p || (p != "/" && (within(p, m.Dst) || within(m.Dst, p))) {
				return fmt.Errorf("sandbox: docker mount Dst %q overlaps %q", m.Dst, p)
			}
		}
	}
	return nil
}

// dockerEnv is the environment inside a stage container: the same allowlist as
// the host sandbox, with the image's own PATH and toolchains left alone.
func dockerEnv(d *Docker) map[string]string {
	h := SandboxHome
	env := map[string]string{
		"HOME":                h,
		"LANG":                "C.UTF-8",
		"TERM":                "dumb",
		"GIT_TERMINAL_PROMPT": "0",
		"GIT_CONFIG_GLOBAL":   "/dev/null",
		"GIT_CONFIG_NOSYSTEM": "1",
		"GIT_OPTIONAL_LOCKS":  "0",
		"XDG_CONFIG_HOME":     h + "/.config",
		"XDG_CACHE_HOME":      h + "/.cache",
		"XDG_DATA_HOME":       h + "/.local/share",
		"XDG_STATE_HOME":      h + "/.local/state",
		"GOCACHE":             h + "/.cache/go-build",
		"GOMODCACHE":          h + "/go/pkg/mod",
		"GOPATH":              h + "/go",
		"GOTOOLCHAIN":         "local",
	}
	if d.Proxy != "" {
		for _, k := range []string{"HTTP_PROXY", "HTTPS_PROXY", "http_proxy", "https_proxy"} {
			env[k] = d.Proxy
		}
		env["NO_PROXY"] = "localhost,127.0.0.1,host.docker.internal"
		env["no_proxy"] = env["NO_PROXY"]
	}
	return env
}

// dockerMounts renders every bind in order: the stage home writable at the
// sandbox home, the worktree, the trusted git directories read-only, the
// caller's read-only binds and the project's extra mounts.
func dockerMounts(spec Spec) ([]string, error) {
	d := spec.Docker
	var out []string
	add := func(src, dst string, rw bool) error {
		resolved, err := filepath.EvalSymlinks(src)
		if err != nil {
			return fmt.Errorf("sandbox: docker mount %s: %w", src, err)
		}
		if !sharedWith(resolved, d.Shared) {
			return fmt.Errorf("sandbox: %s is not shared with the docker VM (shared: %s); it would be an empty directory in the container: add it to the VM's mounts (~/.colima/default/colima.yaml) and restart the VM", resolved, strings.Join(d.Shared, ", "))
		}
		if strings.ContainsAny(resolved+dst, ":,") {
			return fmt.Errorf("sandbox: docker mount paths may not contain ':' or ',': %q -> %q", resolved, dst)
		}
		opts := "ro"
		if rw {
			opts = "rw"
		}
		if d.SELinux {
			opts += ",z"
		}
		out = append(out, resolved+":"+dst+":"+opts)
		return nil
	}
	if err := add(spec.StageHome, SandboxHome, true); err != nil {
		return nil, err
	}
	if err := add(spec.Worktree, spec.Worktree, !spec.ReadOnlyWorktree); err != nil {
		return nil, err
	}
	if err := add(spec.RepoGitDir, spec.RepoGitDir, false); err != nil {
		return nil, err
	}
	if err := add(spec.WorktreeGitDir, spec.WorktreeGitDir, false); err != nil {
		return nil, err
	}
	if gitfile := filepath.Join(spec.Worktree, ".git"); exists(gitfile) {
		if err := add(gitfile, gitfile, false); err != nil {
			return nil, err
		}
	}
	for _, b := range spec.ExtraROBinds {
		if err := add(b.Src, b.Dst, false); err != nil {
			return nil, err
		}
	}
	for _, m := range d.Mounts {
		if err := add(m.Src, m.Dst, m.RW); err != nil {
			return nil, err
		}
	}
	return out, nil
}

// sharedWith says the runtime can see path: it is under one of the shared roots, or there is
// no VM between the host and the containers (shared is nil).
func sharedWith(path string, shared []string) bool {
	if shared == nil {
		return true
	}
	for _, root := range shared {
		if r, err := filepath.EvalSymlinks(root); err == nil {
			root = r
		}
		if within(filepath.Clean(root), path) {
			return true
		}
	}
	return false
}

// dockerArgs renders the argv of `docker run` for spec, including the leading
// "run" so a test compares exactly what runs.
func dockerArgs(spec Spec) ([]string, error) {
	if err := spec.validate(); err != nil {
		return nil, err
	}
	d := spec.Docker
	args := []string{"run", "--rm", "--init", "--hostname", "localhost", "--name", d.Name,
		"--label", "agentflow=1", "--label", "agentflow.container=" + d.Name}
	if d.Instance != "" {
		args = append(args, "--label", "agentflow.instance="+d.Instance)
	}
	switch d.Network {
	case "", NetworkNone:
		args = append(args, "--network", "none")
	default:
		// provider and full both use the bridge; provider additionally points
		// the stage's tools at the allowlisting proxy.
		if d.Network == NetworkProvider {
			args = append(args, "--add-host", "host.docker.internal:host-gateway")
		}
	}
	if d.User != "" {
		args = append(args, "--user", d.User)
	}
	// The fixed allowlist goes on the command line with its values. What the stage passes
	// (its backend's API key among it) goes by name only: docker takes the value from its
	// own environment (dockerCommand puts it there), so no secret is ever in an argv that
	// `ps` shows or a log records.
	env := dockerEnv(d)
	for k := range spec.Env {
		delete(env, k)
	}
	keys := make([]string, 0, len(env))
	for k := range env {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		args = append(args, "--env", k+"="+env[k])
	}
	for _, k := range stageEnvKeys(spec) {
		args = append(args, "--env", k)
	}
	mounts, err := dockerMounts(spec)
	if err != nil {
		return nil, err
	}
	for _, m := range mounts {
		args = append(args, "--volume", m)
	}
	// The tool caches (Ghidra's XDG cache above all) live in a container-local
	// tmpfs: Ghidra cannot create its cache directory on a virtiofs bind mount,
	// even as root, and fails to start. The stage home itself stays host-backed
	// so the worktree's build output is visible to the host; only the cache is
	// per-stage.
	args = append(args, "--tmpfs", SandboxHome+"/.cache")
	args = append(args, "--workdir", spec.Worktree, d.Image)
	argv := spec.Argv
	if d.Runner != "" {
		argv = append([]string{d.Runner}, spec.Argv[1:]...)
	}
	return append(args, argv...), nil
}

// stageEnvKeys are the names of the variables the stage passes, sorted.
func stageEnvKeys(spec Spec) []string {
	keys := make([]string, 0, len(spec.Env))
	for k := range spec.Env {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// dockerShellEnv is the environment of the docker CLI itself: the server's PATH
// and, for a Docker context or credential helper, only its own config.
func dockerShellEnv() []string {
	keep := []string{"PATH", "HOME", "DOCKER_HOST", "DOCKER_CONFIG", "DOCKER_CONTEXT", "TMPDIR", "LANG"}
	var out []string
	for _, k := range keep {
		if v, ok := os.LookupEnv(k); ok {
			out = append(out, k+"="+v)
		}
	}
	return out
}

// DockerKill stops a container by name. The stage's container is not a child of
// the server, so cancelling the docker CLI does not stop it; the command's
// Cancel calls this. A missing container is not an error.
func DockerKill(ctx context.Context, name string) error {
	return runDocker(ctx, "kill", name)
}

// DockerRemove force-removes a container and its anonymous volumes, for orphan
// cleanup and for a name a crashed run left behind.
func DockerRemove(ctx context.Context, name string) error {
	return runDocker(ctx, "rm", "-f", "-v", name)
}

// DockerSweep removes every container, running or not, labelled with this agentflow
// instance. It is for startup, when none of them can belong to a live stage (docker stages
// are not adopted across a restart). A runtime that does not answer is left alone: this
// never starts a VM.
func DockerSweep(ctx context.Context, instance string) ([]string, error) {
	if instance == "" {
		return nil, nil
	}
	bin, err := exec.LookPath("docker")
	if err != nil {
		return nil, nil // no docker on this host: nothing of ours can exist
	}
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, bin, "ps", "-a", "--filter", "label=agentflow.instance="+instance, "--format", "{{.Names}}")
	cmd.Env = dockerShellEnv()
	out, err := cmd.Output()
	if err != nil {
		return nil, nil // the runtime is down: nothing runs, and it is not ours to start
	}
	var removed []string
	for _, n := range strings.Fields(string(out)) {
		if err := DockerRemove(ctx, n); err == nil {
			removed = append(removed, n)
		}
	}
	return removed, nil
}

// DockerRunning lists this instance's containers by label.
func DockerRunning(ctx context.Context) ([]string, error) {
	bin, err := exec.LookPath("docker")
	if err != nil {
		return nil, fmt.Errorf("sandbox: docker is required for a docker project: %w", err)
	}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, bin, "ps", "--filter", "label=agentflow=1", "--format", "{{.Names}}")
	cmd.Env = dockerShellEnv()
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("sandbox: docker ps: %w", err)
	}
	var names []string
	for _, l := range strings.Split(string(out), "\n") {
		if l = strings.TrimSpace(l); l != "" {
			names = append(names, l)
		}
	}
	return names, nil
}

func runDocker(ctx context.Context, args ...string) error {
	bin, err := exec.LookPath("docker")
	if err != nil {
		return fmt.Errorf("sandbox: docker is required for a docker project: %w", err)
	}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, bin, args...)
	cmd.Env = dockerShellEnv()
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("sandbox: docker %s: %w: %s", args[0], err, strings.TrimSpace(string(out)))
	}
	return nil
}

// dockerCommand builds the docker run command for spec. Cancelling ctx kills
// the container by name, because it would otherwise outlive the CLI.
func dockerCommand(ctx context.Context, spec Spec) (*exec.Cmd, error) {
	bin, err := exec.LookPath("docker")
	if err != nil {
		return nil, fmt.Errorf("sandbox: docker is required for a docker project: %w", err)
	}
	args, err := dockerArgs(spec)
	if err != nil {
		return nil, err
	}
	// A container a crashed run left under this name would make `docker run --name` fail
	// (exit 125, a hard "could not start"): replace it. Names are per stage, so it is ours.
	rmCtx, rmCancel := context.WithTimeout(ctx, 30*time.Second)
	_ = DockerRemove(rmCtx, spec.Docker.Name)
	rmCancel()
	cmd := exec.CommandContext(ctx, bin, args...)
	cmd.Env = dockerShellEnv()
	for _, k := range stageEnvKeys(spec) {
		cmd.Env = append(cmd.Env, k+"="+spec.Env[k]) // read by `--env NAME`, never in argv
	}
	cmd.WaitDelay = killGrace + time.Second
	name := spec.Docker.Name
	cmd.Cancel = func() error {
		killCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_ = DockerKill(killCtx, name)
		return cmd.Process.Kill()
	}
	return cmd, nil
}
