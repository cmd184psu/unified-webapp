// Package gitsvc is the only place agentflow runs git (GIT1). Every call is a
// fixed argv handed to exec.CommandContext, never a shell, with a scrubbed
// environment and an explicit -C directory. The package never modifies a
// repository's main working tree or checked-out branch (GIT2): all writes go
// to agentflow branches, in worktrees under the data directory. There is no
// push code path (GIT4).
package gitsvc

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
)

// Sentinel errors, matchable with errors.Is.
var (
	ErrInvalidName      = errors.New("gitsvc: invalid name")
	ErrNotRepo          = errors.New("gitsvc: not a git repository")
	ErrOutsideDataDir   = errors.New("gitsvc: path is outside the agentflow worktree root")
	ErrNotRegistered    = errors.New("gitsvc: path is not a registered worktree of the repository")
	ErrBranchExists     = errors.New("gitsvc: branch already exists")
	ErrForbiddenRef     = errors.New("gitsvc: refusing to touch a branch agentflow does not own")
	ErrNothingToCommit  = errors.New("gitsvc: nothing to commit")
	ErrTamperedWorktree = errors.New("gitsvc: worktree .git entry was tampered with")
	ErrGitlink          = errors.New("gitsvc: worktree contains a nested repository (gitlink)")
	ErrForeignBranch    = errors.New("gitsvc: branch exists and was not created by agentflow")
	ErrNotFound         = errors.New("gitsvc: revision or branch not found")
)

// GitError is a failed git invocation.
type GitError struct {
	Args   []string
	Stderr string
	Err    error
}

func (e *GitError) Error() string {
	return fmt.Sprintf("git %s: %v: %s", strings.Join(e.Args, " "), e.Err, strings.TrimSpace(e.Stderr))
}

func (e *GitError) Unwrap() error { return e.Err }

// Service runs engine git operations. Create one with New.
type Service struct {
	dataDir string // absolute, symlinks resolved when it exists
	// argvHook, when set, sees every argv before it runs (tests only).
	argvHook func(argv []string)

	mu    sync.Mutex
	repos map[string]*sync.Mutex
}

// New returns a Service whose worktrees live under <dataDir>/worktrees.
func New(dataDir string) (*Service, error) {
	abs, err := filepath.Abs(dataDir)
	if err != nil {
		return nil, err
	}
	return &Service{dataDir: resolve(abs), repos: map[string]*sync.Mutex{}}, nil
}

// resolve evaluates symlinks on the longest existing prefix of p.
func resolve(p string) string {
	p = filepath.Clean(p)
	var tail []string
	for cur := p; ; cur = filepath.Dir(cur) {
		if r, err := filepath.EvalSymlinks(cur); err == nil {
			for i := len(tail) - 1; i >= 0; i-- {
				r = filepath.Join(r, tail[i])
			}
			return r
		}
		if filepath.Dir(cur) == cur {
			return p
		}
		tail = append(tail, filepath.Base(cur))
	}
}

// WorktreeRoot is the directory that holds every agentflow worktree.
func (s *Service) WorktreeRoot() string { return filepath.Join(s.dataDir, "worktrees") }

// lockRepo serialises worktree add/remove/prune, branch create/delete and
// merge for one repository. It is keyed by the absolute common git dir, so a
// repo and any alias or linked worktree of it share one mutex. It returns the
// unlock function.
func (s *Service) lockRepo(ctx context.Context, repo string) func() {
	key, err := s.raw(ctx, repo, "rev-parse", "--path-format=absolute", "--git-common-dir")
	if err != nil {
		key = repo
	}
	key = resolve(key)
	s.mu.Lock()
	m, ok := s.repos[key]
	if !ok {
		m = &sync.Mutex{}
		s.repos[key] = m
	}
	s.mu.Unlock()
	m.Lock()
	return m.Unlock
}

// gitEnv is the complete environment git sees: nothing is inherited except PATH.
func (s *Service) gitEnv() []string {
	return []string{
		"PATH=" + os.Getenv("PATH"),
		"HOME=" + s.dataDir,
		"LANG=C",
		"LC_ALL=C",
		"GIT_CONFIG_NOSYSTEM=1",
		"GIT_CONFIG_GLOBAL=/dev/null",
		"GIT_ATTR_NOSYSTEM=1",
		"GIT_TERMINAL_PROMPT=0",
		"GIT_OPTIONAL_LOCKS=0",
	}
}

// baseConfig is prepended to every invocation: no hooks, no credential
// helpers, no signing, no fsmonitor program, no ssh command, no external
// protocols, no attributes file.
var baseConfig = []string{
	"-c", "core.hooksPath=/dev/null",
	"-c", "core.fsmonitor=false",
	"-c", "core.attributesFile=/dev/null",
	"-c", "core.sshCommand=false",
	"-c", "credential.helper=",
	"-c", "commit.gpgsign=false",
	"-c", "protocol.ext.allow=never",
	"-c", "protocol.file.allow=user",
}

// Hardening is the argv prefix and environment every engine git invocation
// uses. Callers append `-C <dir>` and the git arguments. B10 and verify use
// the same value so that no git process runs with weaker settings.
type Hardening struct {
	Args []string
	Env  []string
}

// Hardened returns the hardening for repo. Besides baseConfig it disables
// every attribute source and every filter driver the repository's config
// defines: a model-written .gitattributes (or info/attributes) must not make
// the engine run clean/smudge/process commands outside the sandbox. As a
// consequence engine commits and checkouts store raw bytes: no git-lfs, no
// git-crypt, no eol conversion.
func (s *Service) Hardened(ctx context.Context, repo string) (Hardening, error) {
	args := append([]string{}, baseConfig...)
	empty, err := s.exec(ctx, baseConfig, s.gitEnv(), repo, nil, []byte{}, []string{"hash-object", "-t", "tree", "--stdin"})
	if err != nil {
		return Hardening{}, err
	}
	args = append(args, "--attr-source="+empty)
	out, err := s.raw(ctx, repo, "config", "--get-regexp", `^filter\.`)
	var ge *GitError
	var ee *exec.ExitError
	if err != nil && !(errors.As(err, &ge) && errors.As(ge.Err, &ee) && ee.ExitCode() == 1) {
		return Hardening{}, err
	}
	seen := map[string]bool{}
	for _, line := range strings.Split(out, "\n") {
		key, _, _ := strings.Cut(line, " ")
		rest, ok := strings.CutPrefix(key, "filter.")
		dot := strings.LastIndex(rest, ".")
		if !ok || dot <= 0 || seen[rest[:dot]] {
			continue
		}
		name := rest[:dot]
		seen[name] = true
		for _, v := range []string{"clean=", "smudge=", "process=", "required=false"} {
			args = append(args, "-c", "filter."+name+"."+v)
		}
	}
	return Hardening{Args: args, Env: s.gitEnv()}, nil
}

// raw runs git with only baseConfig, for the probes Hardened itself needs.
func (s *Service) raw(ctx context.Context, dir string, args ...string) (string, error) {
	return s.exec(ctx, baseConfig, s.gitEnv(), dir, nil, nil, args)
}

// run executes hardened git with -C dir and returns trimmed stdout. dir is
// also the repository whose config is consulted for filter overrides.
func (s *Service) run(ctx context.Context, dir string, args ...string) (string, error) {
	return s.runIn(ctx, dir, dir, nil, nil, args...)
}

// runIn is run for a worktree dir of repo, with extra leading options (for
// example --git-dir) and stdin. Hardening comes from the main repo, never from
// the worktree.
func (s *Service) runIn(ctx context.Context, repo, dir string, pre []string, stdin []byte, args ...string) (string, error) {
	h, err := s.Hardened(ctx, repo)
	if err != nil {
		return "", err
	}
	return s.exec(ctx, h.Args, h.Env, dir, pre, stdin, args)
}

func (s *Service) exec(ctx context.Context, cfg, env []string, dir string, pre []string, stdin []byte, args []string) (string, error) {
	argv := make([]string, 0, len(cfg)+len(pre)+len(args)+2)
	argv = append(argv, cfg...)
	argv = append(argv, "-C", dir)
	argv = append(argv, pre...)
	argv = append(argv, args...)
	if s.argvHook != nil {
		s.argvHook(append([]string{"git"}, argv...))
	}
	cmd := exec.CommandContext(ctx, "git", argv...)
	cmd.Env = env
	if stdin != nil {
		cmd.Stdin = bytes.NewReader(stdin)
	}
	var out, errb bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &errb
	if err := cmd.Run(); err != nil {
		return "", &GitError{Args: append(append([]string{}, pre...), args...), Stderr: errb.String(), Err: err}
	}
	return strings.TrimRight(out.String(), "\n"), nil
}

var nameRE = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,63}$`)

func validName(kind, v string) error {
	if !nameRE.MatchString(v) || strings.Contains(v, "..") || strings.HasSuffix(v, ".lock") || strings.HasSuffix(v, ".") {
		return fmt.Errorf("%w: %s %q", ErrInvalidName, kind, v)
	}
	return nil
}

// IntegrationBranch is the effort's integration branch: agentflow/<effort>, or
// the user-named override (GIT2).
func IntegrationBranch(effort, override string) string {
	if override != "" {
		return override
	}
	return "agentflow/" + effort
}

// TaskBranch is the branch of one task. It is agentflow-task/<effort>/<task>
// rather than agentflow/<effort>/<task> because git cannot hold both
// refs/heads/agentflow/<effort> and refs/heads/agentflow/<effort>/<task>
// (a ref cannot be both a file and a directory).
func TaskBranch(effort, task string) string {
	return "agentflow-task/" + effort + "/" + task
}

// ownedBranch reports whether name is a branch agentflow created by convention.
func ownedBranch(name string) bool {
	return strings.HasPrefix(name, "agentflow/") || strings.HasPrefix(name, "agentflow-task/")
}

// IsRepo reports whether path is the top of, or inside, a git work tree or repo.
func (s *Service) IsRepo(ctx context.Context, path string) bool {
	if st, err := os.Stat(path); err != nil || !st.IsDir() {
		return false
	}
	out, err := s.run(ctx, path, "rev-parse", "--is-inside-work-tree")
	return err == nil && out == "true"
}

// Init creates a repository at path (PJ1's offer) with an empty engine commit
// on branch main so that worktrees have something to branch from. It refuses
// to touch a path that is already inside a repository.
func (s *Service) Init(ctx context.Context, path string) error {
	if st, err := os.Stat(path); err != nil || !st.IsDir() {
		return fmt.Errorf("gitsvc: %s is not a directory", path)
	}
	if s.IsRepo(ctx, path) {
		return fmt.Errorf("gitsvc: %s is already a git repository", path)
	}
	if _, err := s.run(ctx, path, "init", "--initial-branch=main"); err != nil {
		return err
	}
	_, err := s.run(ctx, path, engineIdent()...)
	return err
}

// HeadInfo is the commit and branch HEAD points at. Branch is empty when HEAD
// is detached.
type HeadInfo struct {
	Commit string
	Branch string
}

// Head reads HEAD of the repository at path without modifying anything.
func (s *Service) Head(ctx context.Context, repo string) (HeadInfo, error) {
	commit, err := s.run(ctx, repo, "rev-parse", "--verify", "HEAD^{commit}")
	if err != nil {
		return HeadInfo{}, err
	}
	branch, err := s.run(ctx, repo, "branch", "--show-current")
	if err != nil {
		return HeadInfo{}, err
	}
	return HeadInfo{Commit: commit, Branch: branch}, nil
}

// Fetch updates the repository's remote-tracking branches. A repository
// without a remote is left alone, so a local-only project still gets a base.
func (s *Service) Fetch(ctx context.Context, repo string) error {
	if _, err := s.run(ctx, repo, "remote", "get-url", "origin"); err != nil {
		return nil
	}
	_, err := s.run(ctx, repo, "fetch", "--prune", "origin")
	return err
}

// BranchTip returns the commit a branch names, preferring the freshly fetched
// origin copy and falling back to the local branch. base is a plain branch
// name such as feature/diplomacy-screen; empty means HEAD.
func (s *Service) BranchTip(ctx context.Context, repo, base string) (string, error) {
	if base == "" {
		h, err := s.Head(ctx, repo)
		return h.Commit, err
	}
	for _, ref := range []string{"refs/remotes/origin/" + base, "refs/heads/" + base} {
		if !strings.HasPrefix(base, "-") {
			if sha, err := s.resolveCommit(ctx, repo, ref); err == nil {
				return sha, nil
			}
		}
	}
	return "", fmt.Errorf("branch %q: %w", base, ErrNotFound)
}

// BranchContains reports whether branch's history contains ref (a commit or
// another branch), trying the remote copy first. It never modifies the repo.
func (s *Service) BranchContains(ctx context.Context, repo, branch, ref string) (bool, error) {
	if strings.HasPrefix(branch, "-") || strings.HasPrefix(ref, "-") {
		return false, ErrInvalidName
	}
	branchRef := refsTry(ctx, s, repo, branch)
	if branchRef == "" {
		return false, fmt.Errorf("branch %q: %w", branch, ErrNotFound)
	}
	refCommit := ""
	for _, cand := range []string{ref, "refs/remotes/origin/" + ref, "refs/heads/" + ref} {
		if c, err := s.resolveCommit(ctx, repo, cand); err == nil {
			refCommit = c
			break
		}
	}
	if refCommit == "" {
		return false, fmt.Errorf("revision %q: %w", ref, ErrNotFound)
	}
	if _, err := s.run(ctx, repo, "merge-base", "--is-ancestor", refCommit, branchRef); err != nil {
		var ge *GitError
		var ee *exec.ExitError
		if (errors.As(err, &ge) && errors.As(ge.Err, &ee) && ee.ExitCode() == 1) || errors.As(err, &ee) && ee.ExitCode() == 1 {
			return false, nil
		}
		return false, err
	}
	return true, nil
}

func refsTry(ctx context.Context, s *Service, repo, branch string) string {
	for _, ref := range []string{"refs/remotes/origin/" + branch, "refs/heads/" + branch} {
		if _, err := s.resolveCommit(ctx, repo, ref); err == nil {
			return ref
		}
	}
	return ""
}

// resolveCommit verifies rev names a commit and returns its full hash.
func (s *Service) resolveCommit(ctx context.Context, repo, rev string) (string, error) {
	if rev == "" || strings.HasPrefix(rev, "-") {
		return "", fmt.Errorf("%w: revision %q", ErrInvalidName, rev)
	}
	return s.run(ctx, repo, "rev-parse", "--verify", "--end-of-options", rev+"^{commit}")
}

func (s *Service) branchExists(ctx context.Context, repo, branch string) bool {
	_, err := s.run(ctx, repo, "show-ref", "--verify", "--quiet", "refs/heads/"+branch)
	return err == nil
}

var overrideRE = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._/-]*$`)

// ownerRef is the marker ref recording that agentflow created a user-named
// integration branch. Convention-named branches (agentflow/, agentflow-task/)
// need no marker.
func ownerRef(branch string) string { return "refs/agentflow/owned/" + branch }

func (s *Service) refExists(ctx context.Context, repo, ref string) bool {
	_, err := s.run(ctx, repo, "show-ref", "--verify", "--quiet", ref)
	return err == nil
}

// owned reports whether agentflow created branch.
func (s *Service) owned(ctx context.Context, repo, branch string) bool {
	return ownedBranch(branch) || s.refExists(ctx, repo, ownerRef(branch))
}

// EnsureIntegrationBranch creates the effort's integration branch at base if it
// does not exist yet and returns its name. override names a user-chosen branch
// (GIT2); it must be a plain, strictly valid ref name and must not already
// exist unless agentflow created it earlier, so the user's own branches are
// never written. Only refs are created; no working tree is touched.
func (s *Service) EnsureIntegrationBranch(ctx context.Context, repo, effort, base, override string) (string, error) {
	if err := validName("effort", effort); err != nil {
		return "", err
	}
	branch := IntegrationBranch(effort, override)
	if override != "" {
		if !overrideRE.MatchString(override) || strings.Contains(override, "@{") {
			return "", fmt.Errorf("%w: branch %q", ErrInvalidName, override)
		}
		canon, err := s.run(ctx, repo, "check-ref-format", "--branch", override)
		if err != nil || canon != override {
			return "", fmt.Errorf("%w: branch %q", ErrInvalidName, override)
		}
		branch = canon
	}
	unlock := s.lockRepo(ctx, repo)
	defer unlock()
	if s.branchExists(ctx, repo, branch) {
		if override != "" && !s.owned(ctx, repo, branch) {
			return "", fmt.Errorf("%w: %q", ErrForeignBranch, branch)
		}
		return branch, nil
	}
	commit, err := s.resolveCommit(ctx, repo, base)
	if err != nil {
		return "", err
	}
	if _, err := s.run(ctx, repo, "branch", "--no-track", "--end-of-options", branch, commit); err != nil {
		return "", err
	}
	if override != "" {
		if _, err := s.run(ctx, repo, "update-ref", ownerRef(branch), commit); err != nil {
			return "", err
		}
	}
	return branch, nil
}

// DeleteBranch force-deletes a branch agentflow created. It refuses any other
// branch and the branch currently checked out in the main repository.
func (s *Service) DeleteBranch(ctx context.Context, repo, branch string) error {
	unlock := s.lockRepo(ctx, repo)
	defer unlock()
	if !s.owned(ctx, repo, branch) {
		return fmt.Errorf("%w: %q", ErrForbiddenRef, branch)
	}
	if h, err := s.Head(ctx, repo); err == nil && h.Branch == branch {
		return fmt.Errorf("%w: %q is checked out", ErrForbiddenRef, branch)
	}
	if _, err := s.run(ctx, repo, "branch", "-D", "--end-of-options", branch); err != nil {
		return err
	}
	if s.refExists(ctx, repo, ownerRef(branch)) {
		_, err := s.run(ctx, repo, "update-ref", "-d", ownerRef(branch))
		return err
	}
	return nil
}
