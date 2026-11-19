package sandbox

import (
	"errors"
	"fmt"
	"os"
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
	// HideDirs are host directories the stage may not read (a seat: ~/.config). On Linux the
	// host home is hidden already, so they only matter on macOS.
	HideDirs []string
	// ReadFiles are single host files the stage may read even inside HideDirs (a seat
	// program's own large read-only files). On Linux each is bound read-only at its own path.
	// A credential path is never re-opened by this list.
	ReadFiles []string
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
	// A read-only stage may run in the repository itself (planning stages):
	// the worktree is then the repo, its .git is inside it, and both git dirs
	// are that .git.
	// Containment is judged on symlink-resolved paths: one caller may pass a
	// path through a link (macOS /var -> /private/var) and another its target.
	wt, repoGit, wtGit := resolved(s.Worktree), resolved(s.RepoGitDir), resolved(s.WorktreeGitDir)
	inRepo := s.ReadOnlyWorktree && wtGit == repoGit && repoGit == filepath.Join(wt, ".git")
	if !inRepo && (within(wt, repoGit) || within(repoGit, wt)) {
		return fmt.Errorf("sandbox: Worktree %q and RepoGitDir %q must not contain each other", s.Worktree, s.RepoGitDir)
	}
	if !inRepo && (!within(filepath.Join(repoGit, "worktrees"), wtGit) || wtGit == filepath.Join(repoGit, "worktrees")) {
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
	for _, p := range append(append([]string{}, s.HideDirs...), s.ReadFiles...) {
		if !filepath.IsAbs(p) || filepath.Clean(p) != p {
			return fmt.Errorf("sandbox: HideDirs and ReadFiles need absolute, clean paths, got %q", p)
		}
	}
	return nil
}

// resolved returns p with symlinks resolved, or p itself when it cannot be
// resolved (for example because it does not exist yet).
func resolved(p string) string {
	if r, err := filepath.EvalSymlinks(p); err == nil {
		return r
	}
	return p
}

func exists(p string) bool {
	_, err := os.Stat(p)
	return err == nil
}
