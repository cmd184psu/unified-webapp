package gitsvc

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

const (
	engineName  = "agentflow"
	engineEmail = "noreply@agentflow.local"
)

// engineIdent is the argv prefix that makes a commit as the engine, passed as
// -c options so repository config is never modified.
func engineIdent() []string {
	return []string{
		"-c", "user.name=" + engineName,
		"-c", "user.email=" + engineEmail,
		"commit", "--no-verify", "--allow-empty", "-m", "agentflow: initial commit",
	}
}

// Trailers are appended to the engine commit message (GIT3).
type Trailers struct {
	Task    string
	Attempt int
}

// message renders the commit message with its trailers.
func message(msg string, tr Trailers) (string, error) {
	var kept []string
	for _, l := range strings.Split(msg, "\n") {
		if !strings.HasPrefix(strings.ToLower(strings.TrimSpace(l)), "agentflow-") {
			kept = append(kept, l)
		}
	}
	msg = strings.TrimSpace(strings.Join(kept, "\n"))
	if msg == "" {
		return "", fmt.Errorf("gitsvc: empty commit message")
	}
	if err := validName("task", tr.Task); err != nil {
		return "", err
	}
	if tr.Attempt < 1 {
		return "", fmt.Errorf("gitsvc: attempt must be >= 1")
	}
	return msg + "\n\nAgentflow-Task: " + tr.Task + "\nAgentflow-Attempt: " + strconv.Itoa(tr.Attempt) + "\n", nil
}

// AdminGitDir finds the repository-side gitdir of a registered worktree. It
// never trusts $WT/.git, which the model can rewrite (S4): the worktree must be
// listed by `git worktree list` in the main repository, $WT/.git must not be a
// symlink, and the admin directory is the single one under the common git dir
// whose gitdir file points back at <wt>/.git. The ".git" component is never
// symlink-resolved, so a link to another worktree's .git cannot match.
func (s *Service) AdminGitDir(ctx context.Context, repo, wt string) (string, error) {
	if _, err := s.registered(ctx, repo, wt); err != nil {
		return "", err
	}
	if st, err := os.Lstat(filepath.Join(wt, ".git")); err == nil && st.Mode()&os.ModeSymlink != 0 {
		return "", fmt.Errorf("%w: %s/.git is a symlink", ErrTamperedWorktree, wt)
	}
	common, err := s.raw(ctx, repo, "rev-parse", "--path-format=absolute", "--git-common-dir")
	if err != nil {
		return "", err
	}
	entries, err := os.ReadDir(filepath.Join(common, "worktrees"))
	if err != nil {
		return "", fmt.Errorf("%w: %s", ErrNotRegistered, wt)
	}
	want := filepath.Join(resolve(wt), ".git")
	var found []string
	for _, e := range entries {
		dir := filepath.Join(common, "worktrees", e.Name())
		b, err := os.ReadFile(filepath.Join(dir, "gitdir"))
		if err != nil {
			continue
		}
		p := strings.TrimSpace(string(b))
		if !filepath.IsAbs(p) {
			p = filepath.Join(dir, p)
		}
		if filepath.Join(resolve(filepath.Dir(p)), ".git") == want {
			found = append(found, dir)
		}
	}
	if len(found) != 1 {
		return "", fmt.Errorf("%w: %d admin directories match %s", ErrNotRegistered, len(found), wt)
	}
	return found[0], nil
}

// GitlinkError reports nested repositories (mode 160000) staged from a
// worktree. The policy layer can report Paths to the user.
type GitlinkError struct{ Paths []string }

func (e *GitlinkError) Error() string {
	return fmt.Sprintf("%v: %s", ErrGitlink, strings.Join(e.Paths, ", "))
}

func (e *GitlinkError) Is(target error) bool { return target == ErrGitlink }

// CommitAll stages everything in the agentflow worktree wt and commits it as
// agentflow <noreply@agentflow.local> with the Agentflow-Task/Attempt trailers
// (GIT3). It returns the new commit hash, ErrNothingToCommit, or a
// *GitlinkError (the index is reset first). It uses the admin directory from
// AdminGitDir, never $WT/.git, runs with hooks disabled and --no-verify, and
// never touches repository config. Attribute sources and filter drivers are
// disabled (see Hardened), so file bytes are committed raw: no git-lfs, no
// eol conversion.
func (s *Service) CommitAll(ctx context.Context, repo, wt, msg string, tr Trailers) (string, error) {
	if !s.underRoot(wt) {
		return "", fmt.Errorf("%w: %s", ErrOutsideDataDir, wt)
	}
	full, err := message(msg, tr)
	if err != nil {
		return "", err
	}
	admin, err := s.AdminGitDir(ctx, repo, wt)
	if err != nil {
		return "", err
	}
	pre := []string{"--git-dir=" + admin, "--work-tree=" + resolve(wt)}
	if _, err := s.runIn(ctx, repo, wt, pre, nil, addAllArgs...); err != nil {
		return "", err
	}
	raw, err := s.runIn(ctx, repo, wt, pre, nil, "diff", "--cached", "--raw", "--no-ext-diff", "--no-textconv", "--no-renames")
	if err != nil {
		return "", err
	}
	if raw == "" {
		return "", ErrNothingToCommit
	}
	var links []string
	for _, line := range strings.Split(raw, "\n") {
		meta, path, _ := strings.Cut(line, "\t")
		if f := strings.Fields(meta); len(f) >= 2 && f[1] == "160000" {
			links = append(links, path)
		}
	}
	if len(links) > 0 {
		s.runIn(ctx, repo, wt, pre, nil, "reset", "-q")
		return "", &GitlinkError{Paths: links}
	}
	if _, err := s.runIn(ctx, repo, wt, pre, []byte(full),
		"-c", "user.name="+engineName, "-c", "user.email="+engineEmail,
		"commit", "--no-verify", "--cleanup=verbatim", "-F", "-"); err != nil {
		return "", err
	}
	return s.runIn(ctx, repo, wt, pre, nil, "rev-parse", "--verify", "HEAD")
}

// toolStateDirs are directories tools write into a worktree (orchestrator and
// agent state). They are never part of a project, so the engine never stages
// them.
var toolStateDirs = []string{".omc", ".claude"}

// addAllArgs stages everything except toolStateDirs.
var addAllArgs = func() []string {
	args := []string{"add", "-A", "--", "."}
	for _, d := range toolStateDirs {
		args = append(args, ":(exclude)"+d)
	}
	return args
}()
