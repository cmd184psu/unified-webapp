// Package sandbox runs one stage command inside bwrap (WC1, WC2, GIT6): a
// deny-by-default filesystem, an allowlisted environment, its own process
// group, and no git or forge credentials reachable.
package sandbox

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// Bind is a read-only host path mounted at Dst inside the sandbox.
type Bind struct {
	Src string
	Dst string
}

// Spec describes one sandboxed command.
type Spec struct {
	// Worktree is the working directory; read-write unless ReadOnlyWorktree.
	Worktree string
	// ReadOnlyWorktree binds the worktree read-only, for non-worker stages
	// (planner, architect, critic, compiler, reviewer). This, not the runner's
	// permission layer, is what guarantees such a stage cannot write.
	ReadOnlyWorktree bool
	// RepoGitDir is the main repository's .git, bound read-only.
	RepoGitDir string
	// WorktreeGitDir is <RepoGitDir>/worktrees/<id>, bound read-only.
	WorktreeGitDir string
	// StageHome is bound read-write at SandboxHome and holds XDG dirs and caches.
	StageHome string
	// Net shares the host network namespace; false unshares it.
	Net bool
	// ExtraROBinds are additional read-only mounts, for example the resolved
	// real runner binary or a Go module cache. Never bind a HOME shim.
	ExtraROBinds []Bind
	// PathDirs are sandbox directories appended to PATH.
	PathDirs []string
	// Env holds explicit extra variables; it may override the allowlist.
	Env map[string]string
	// Argv is the command to run inside the sandbox.
	Argv []string
}

// systemDirs are bound read-only when present. Anything not listed here (/opt,
// /var, /srv, /mnt, the server's cwd and data dir) does not exist inside.
var systemDirs = []string{"/usr"}

// etcEntries are the only parts of /etc made visible: loader, name service,
// resolver, TLS roots and timezone. /etc/ssh, /etc/gitconfig and the rest stay
// hidden.
var etcEntries = []string{
	"ld.so.cache", "ld.so.conf", "ld.so.conf.d", "passwd", "group",
	"nsswitch.conf", "resolv.conf", "hosts", "host.conf", "ssl", "pki",
	"ca-certificates", "localtime", "alternatives",
}

// rootLinks are top-level names that are symlinks into /usr on merged-/usr
// hosts and real directories otherwise.
var rootLinks = []string{"/bin", "/sbin", "/lib", "/lib64"}

// Command builds the bwrap command for spec. Cancelling ctx (or its deadline)
// signals the process group, but the guarantee that nothing outlives the stage
// is bwrap's --unshare-pid plus --die-with-parent: when bwrap dies, the kernel
// kills every process in the sandbox's PID namespace. Unlike a bare
// *exec.Cmd constructor it validates spec and so also returns an error.
func Command(ctx context.Context, spec Spec) (*exec.Cmd, error) {
	if err := spec.validate(); err != nil {
		return nil, err
	}
	bwrap, err := exec.LookPath("bwrap")
	if err != nil {
		return nil, fmt.Errorf("sandbox: bwrap is required (D16): %w", err)
	}

	args := []string{"--unshare-user", "--disable-userns", "--unshare-pid", "--die-with-parent", "--new-session",
		"--dev", "/dev", "--proc", "/proc"}
	for _, d := range systemDirs {
		if exists(d) {
			args = append(args, "--ro-bind", d, d)
		}
	}
	for _, l := range rootLinks {
		if target, err := os.Readlink(l); err == nil {
			args = append(args, "--symlink", target, l)
		} else if exists(l) {
			args = append(args, "--ro-bind", l, l)
		}
	}
	for _, e := range etcEntries {
		p := filepath.Join("/etc", e)
		if exists(p) {
			args = append(args, "--ro-bind", p, p)
		}
	}
	args = append(args, "--tmpfs", "/home", "--tmpfs", "/run", "--tmpfs", "/tmp")
	// Writable binds first, read-only protections after them, so a later rw
	// bind can never re-expose a protected path.
	wtBind := "--bind"
	if spec.ReadOnlyWorktree {
		wtBind = "--ro-bind"
	}
	args = append(args, "--bind", spec.StageHome, SandboxHome, wtBind, spec.Worktree, spec.Worktree)
	args = append(args,
		"--ro-bind", spec.RepoGitDir, spec.RepoGitDir,
		"--ro-bind", spec.WorktreeGitDir, spec.WorktreeGitDir)
	// The worktree's gitfile must not be rewritable by the agent.
	if gitfile := filepath.Join(spec.Worktree, ".git"); exists(gitfile) {
		args = append(args, "--ro-bind", gitfile, gitfile)
	}
	args = append(args, "--chdir", spec.Worktree)
	for _, b := range spec.ExtraROBinds {
		src, err := filepath.EvalSymlinks(b.Src)
		if err != nil {
			return nil, fmt.Errorf("sandbox: ExtraROBinds source: %w", err)
		}
		args = append(args, "--ro-bind", src, b.Dst)
	}
	// The synthetic root tmpfs (where /etc, /sandbox ... live) becomes read-only;
	// the separate mounts above keep their own modes.
	args = append(args, "--remount-ro", "/")
	if !spec.Net {
		args = append(args, "--unshare-net")
	}

	path := "/usr/local/go/bin:/usr/local/bin:/usr/bin:/bin"
	for _, d := range spec.PathDirs {
		path += ":" + d
	}
	env := baseEnv(path)
	for k, v := range spec.Env {
		env[k] = v
	}
	// --clearenv must precede every --setenv: it drops the ones before it.
	args = append(args, "--clearenv")
	args = append(args, setenvArgs(env)...)
	args = append(args, "--")
	args = append(args, spec.Argv...)

	cmd := exec.CommandContext(ctx, bwrap, args...)
	cmd.Env = []string{} // bwrap itself inherits nothing from the server
	setupProcessGroup(cmd)
	return cmd, nil
}

// forbiddenEnv reports whether key may not be passed through Spec.Env.
func forbiddenEnv(key string) bool {
	switch key {
	case "HOME", "PATH", "SSH_AUTH_SOCK", "GIT_ASKPASS":
		return true
	}
	for _, p := range []string{"LD_", "GIT_SSH", "OPENCODE_CONFIG"} {
		if strings.HasPrefix(key, p) {
			return true
		}
	}
	return strings.HasSuffix(key, "_TOKEN")
}

// protectedDsts are sandbox paths an ExtraROBinds destination may not equal or
// sit under, nor contain.
var protectedDsts = []string{"/", "/usr", "/etc", "/proc", "/dev", "/home", "/run", "/tmp", SandboxHome}

func within(parent, p string) bool {
	return p == parent || strings.HasPrefix(p, strings.TrimSuffix(parent, "/")+"/")
}

func (s Spec) validate() error {
	for name, p := range map[string]string{
		"Worktree": s.Worktree, "RepoGitDir": s.RepoGitDir,
		"WorktreeGitDir": s.WorktreeGitDir, "StageHome": s.StageHome,
	} {
		if !filepath.IsAbs(p) || filepath.Clean(p) != p {
			return fmt.Errorf("sandbox: %s must be an absolute, clean path, got %q", name, p)
		}
	}
	if within(s.Worktree, s.RepoGitDir) || within(s.RepoGitDir, s.Worktree) {
		return fmt.Errorf("sandbox: Worktree %q and RepoGitDir %q must not contain each other", s.Worktree, s.RepoGitDir)
	}
	if !within(filepath.Join(s.RepoGitDir, "worktrees"), s.WorktreeGitDir) || s.WorktreeGitDir == filepath.Join(s.RepoGitDir, "worktrees") {
		return fmt.Errorf("sandbox: WorktreeGitDir %q must be under %s/worktrees", s.WorktreeGitDir, s.RepoGitDir)
	}
	for _, b := range s.ExtraROBinds {
		if !filepath.IsAbs(b.Src) || !filepath.IsAbs(b.Dst) || filepath.Clean(b.Dst) != b.Dst {
			return fmt.Errorf("sandbox: ExtraROBinds need absolute paths and a clean Dst, got %q -> %q", b.Src, b.Dst)
		}
		for _, p := range append([]string{s.Worktree, s.RepoGitDir}, protectedDsts...) {
			if b.Dst == p || (p != "/" && (within(p, b.Dst) || within(b.Dst, p))) {
				return fmt.Errorf("sandbox: ExtraROBinds Dst %q overlaps %q", b.Dst, p)
			}
		}
	}
	for k := range s.Env {
		if forbiddenEnv(k) {
			return fmt.Errorf("sandbox: Spec.Env may not set %s", k)
		}
	}
	if len(s.Argv) == 0 {
		return errors.New("sandbox: empty Argv")
	}
	return nil
}

func exists(p string) bool {
	_, err := os.Stat(p)
	return err == nil
}
