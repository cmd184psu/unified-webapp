//go:build darwin

// Package sandbox runs one stage command in a sandbox. On macOS there is no
// bwrap, so the stage runs under Apple's sandbox-exec (Seatbelt) instead. The
// rules match the Linux backend where macOS allows it:
//
//   - writes only to the stage home, the stage's TMPDIR and (for a worker) the
//     worktree; the worktree's gitfile and every git admin dir stay read-only;
//   - credentials (~/.ssh, API key files, the opencode auth file, the server's
//     config) and the server's data dir, except this stage's own home and
//     worktree, cannot be read;
//   - an allowlisted environment, nothing inherited from the server;
//   - without Net only loopback is reachable;
//   - its own process group, signalled on cancel.
//
// Differences from bwrap, by design of the host: Seatbelt cannot remap paths,
// so the stage home is HOME itself (SandboxHome and every ExtraROBinds Dst are
// rewritten to their host paths in Env and Argv), and the rest of the
// filesystem stays readable because the macOS toolchains (Go, .NET, git) read
// system paths. There is no PID namespace: a descendant that leaves the
// process group can outlive a cancelled stage.
package sandbox

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
)

// sandboxExec is Apple's Seatbelt launcher, part of every macOS install.
const sandboxExec = "/usr/bin/sandbox-exec"

// darwinPath is PATH inside a macOS stage: Go, Homebrew and the system dirs.
const darwinPath = "/usr/local/go/bin:/opt/homebrew/bin:/usr/local/bin:/usr/bin:/bin"

// homeSecrets are paths under the server user's HOME that a stage may never
// read or write. Exact names; homeSecretRegex covers the key-file families.
var homeSecrets = []string{
	".ssh", ".gnupg", ".aws", ".netrc", ".git-credentials", ".config/gh",
	".local/share/opencode/auth.json", ".unified_common.pin", ".unified-webapp.json",
	".codex/auth.json", ".grok/auth.json", ".claude.json", ".claude/.credentials.json",
}

// homeSecretRegex matches dotfiles in HOME that hold API keys: ~/.x_api,
// ~/.x_api_key, ~/.x_chat_api, ~/.beam_*, ~/.reflection*.
const homeSecretRegex = `^%s/\.([A-Za-z0-9_-]*_api[A-Za-z0-9_]*|beam_[^/]*|reflection[^/]*|api_key[^/]*)$`

// Command builds the sandbox-exec command for spec. Cancelling ctx signals the
// process group (SIGTERM, then SIGKILL).
func Command(ctx context.Context, spec Spec) (*exec.Cmd, error) {
	if err := spec.validate(); err != nil {
		return nil, err
	}
	if _, err := os.Stat(sandboxExec); err != nil {
		return nil, fmt.Errorf("sandbox: %s is required on macOS: %w", sandboxExec, err)
	}
	wt, err := realDir(spec.Worktree)
	if err != nil {
		return nil, err
	}
	home, err := realDir(spec.StageHome)
	if err != nil {
		return nil, err
	}
	repoGit, err := realDir(spec.RepoGitDir)
	if err != nil {
		return nil, err
	}
	wtGit, err := realDir(spec.WorktreeGitDir)
	if err != nil {
		return nil, err
	}
	tmp := filepath.Join(home, "tmp")
	if err := os.MkdirAll(tmp, 0o700); err != nil {
		return nil, fmt.Errorf("sandbox: stage TMPDIR: %w", err)
	}

	// Seatbelt cannot remap paths: the sandbox paths callers use become the
	// host paths they stand for.
	rw := rewriter{{SandboxHome, home}}
	var binds []string
	for _, b := range spec.ExtraROBinds {
		src, err := filepath.EvalSymlinks(b.Src)
		if err != nil {
			return nil, fmt.Errorf("sandbox: ExtraROBinds source: %w", err)
		}
		rw = append(rw, [2]string{b.Dst, src})
		binds = append(binds, src)
	}

	path := darwinPath
	for _, d := range spec.PathDirs {
		path += ":" + rw.apply(d)
	}
	env := baseEnv(path)
	env["TMPDIR"] = tmp
	env["LANG"] = "en_US.UTF-8"
	for k, v := range spec.Env {
		env[k] = v
	}
	envList := make([]string, 0, len(env))
	for k, v := range env {
		envList = append(envList, k+"="+rw.apply(v))
	}
	sort.Strings(envList)

	profile, err := seatbeltProfile(spec, wt, home, repoGit, wtGit, binds)
	if err != nil {
		return nil, err
	}
	args := []string{"-p", profile}
	for _, a := range spec.Argv {
		args = append(args, rw.apply(a))
	}
	cmd := exec.CommandContext(ctx, sandboxExec, args...)
	cmd.Env = envList
	cmd.Dir = wt
	setupProcessGroup(cmd)
	return cmd, nil
}

// rewriter maps sandbox paths to host paths: a value equal to a sandbox path,
// or starting with it plus "/", is rewritten.
type rewriter [][2]string

// A colon-separated list (PATH-like) is rewritten element by element.
func (r rewriter) apply(v string) string {
	parts := strings.Split(v, ":")
	for i, p := range parts {
		parts[i] = r.applyOne(p)
	}
	return strings.Join(parts, ":")
}

func (r rewriter) applyOne(v string) string {
	for _, m := range r {
		if v == m[0] {
			return m[1]
		}
		if strings.HasPrefix(v, m[0]+"/") {
			return m[1] + v[len(m[0]):]
		}
	}
	return v
}

// realDir returns p with symlinks resolved (macOS /tmp is /private/tmp, and
// Seatbelt matches real paths only).
func realDir(p string) (string, error) {
	r, err := filepath.EvalSymlinks(p)
	if err != nil {
		return "", fmt.Errorf("sandbox: %w", err)
	}
	return r, nil
}

// sbStr quotes p as a Seatbelt string literal; a path that cannot be quoted
// safely is refused rather than escaped.
func sbStr(p string) (string, error) {
	if strings.ContainsAny(p, "\"\\\n\x00") {
		return "", fmt.Errorf("sandbox: path %q cannot be used in a Seatbelt profile", p)
	}
	return `"` + p + `"`, nil
}

// dataDirOf returns the agentflow data dir holding the stage home: the nearest
// ancestor that contains agentflow.db, or "" when there is none (tests).
func dataDirOf(home string) string {
	for d := filepath.Dir(home); d != "/" && d != "."; d = filepath.Dir(d) {
		if exists(filepath.Join(d, "agentflow.db")) {
			return d
		}
	}
	return ""
}

// seatbeltProfile renders the policy. In Seatbelt the last matching rule wins,
// so broad rules come first and each later rule narrows or re-opens them.
func seatbeltProfile(spec Spec, wt, home, repoGit, wtGit string, binds []string) (string, error) {
	var b strings.Builder
	var qerr error
	q := func(p string) string {
		s, err := sbStr(p)
		if err != nil && qerr == nil {
			qerr = err
		}
		return s
	}
	b.WriteString("(version 1)\n(allow default)\n")

	// Writes: nowhere, then the stage home (HOME and TMPDIR), the worktree for
	// a worker, and the device nodes every tool writes to.
	b.WriteString("(deny file-write*)\n")
	b.WriteString("(allow file-write* (subpath " + q(home) + ")")
	if !spec.ReadOnlyWorktree {
		b.WriteString(" (subpath " + q(wt) + ")")
	}
	b.WriteString(` (literal "/dev/null") (literal "/dev/zero") (literal "/dev/dtracehelper") (regex #"^/dev/tty") (regex #"^/dev/fd/"))` + "\n")
	// Git admin dirs and the worktree's gitfile stay read-only even for a worker.
	b.WriteString("(deny file-write* (subpath " + q(repoGit) + ") (subpath " + q(wtGit) + ") (literal " + q(filepath.Join(wt, ".git")) + "))\n")

	// The server's data dir is invisible except this stage's home and worktree
	// (and the git dirs, which a worktree under it needs).
	if dd := dataDirOf(home); dd != "" {
		b.WriteString("(deny file-read* file-write* (subpath " + q(dd) + "))\n")
		b.WriteString("(allow file-read* (subpath " + q(home) + ") (subpath " + q(wt) + ") (subpath " + q(repoGit) + ") (subpath " + q(wtGit) + "))\n")
		b.WriteString("(allow file-write* (subpath " + q(home) + ")")
		if !spec.ReadOnlyWorktree {
			b.WriteString(" (subpath " + q(wt) + ")")
		}
		b.WriteString(")\n")
		b.WriteString("(deny file-write* (subpath " + q(repoGit) + ") (subpath " + q(wtGit) + ") (literal " + q(filepath.Join(wt, ".git")) + "))\n")
	}

	// Credentials of the server user: last, so nothing above re-opens them.
	if uh, err := os.UserHomeDir(); err == nil {
		if real, err := filepath.EvalSymlinks(uh); err == nil {
			uh = real
		}
		b.WriteString("(deny file-read* file-write*")
		for _, s := range homeSecrets {
			b.WriteString(" (subpath " + q(filepath.Join(uh, s)) + ")")
		}
		b.WriteString(" (regex #\"" + fmt.Sprintf(homeSecretRegex, regexQuote(uh)) + "\"))\n")
	}

	// Read-only binds stay read-only.
	for _, src := range binds {
		b.WriteString("(deny file-write* (subpath " + q(src) + "))\n")
	}

	if !spec.Net {
		b.WriteString("(deny network-outbound (remote ip \"*:*\"))\n")
		b.WriteString("(allow network-outbound (remote ip \"localhost:*\"))\n")
	}
	if qerr != nil {
		return "", qerr
	}
	return b.String(), nil
}

// regexQuote escapes p for a Seatbelt regex literal.
func regexQuote(p string) string {
	var b strings.Builder
	for _, r := range p {
		if strings.ContainsRune(`.+*?()[]{}|^$\`, r) {
			b.WriteByte('\\')
		}
		b.WriteRune(r)
	}
	return b.String()
}
