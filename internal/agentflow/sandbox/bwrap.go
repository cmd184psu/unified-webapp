//go:build !darwin

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
)

// systemDirs are bound read-only when present. Anything not listed here (/opt,
// /var, /srv, /mnt, the server's cwd and data dir) does not exist inside.
var systemDirs = []string{"/usr"}

// loopbackIP finds the host's ip(8), which the loopback shim runs inside the
// namespace. /usr is bound read-only, so the host path is the sandbox path.
func loopbackIP() (string, error) {
	for _, p := range []string{"/usr/sbin/ip", "/sbin/ip", "/usr/bin/ip", "/bin/ip"} {
		if fi, err := os.Stat(p); err == nil && fi.Mode().IsRegular() {
			return p, nil
		}
	}
	return "", errors.New("sandbox: LoopbackNet needs ip(8), which is not installed")
}

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
	argv := spec.Argv
	if !spec.Net {
		args = append(args, "--unshare-net")
	}
	if spec.LoopbackNet {
		// A fresh network namespace has a loopback device that is down. Bring
		// it up before the stage runs and nothing else: the namespace has no
		// route out, so 127.0.0.1/::1 are the only addresses that answer.
		ip, err := loopbackIP()
		if err != nil {
			return nil, err
		}
		argv = append([]string{"/bin/sh", "-c", ip + ` link set lo up; exec "$@"`, "sandbox"}, argv...)
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
	args = append(args, argv...)

	cmd := exec.CommandContext(ctx, bwrap, args...)
	cmd.Env = []string{} // bwrap itself inherits nothing from the server
	setupProcessGroup(cmd)
	return cmd, nil
}
