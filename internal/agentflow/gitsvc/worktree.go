package gitsvc

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Worktree is an agentflow-created worktree.
type Worktree struct {
	Path   string
	Branch string
	// Commit is the checked-out commit for a detached review worktree (AF-14).
	Commit string
}

// worktreeEntry is one record of `git worktree list --porcelain`.
type worktreeEntry struct {
	Path     string
	Head     string
	Branch   string // short name, empty when detached
	Prunable bool
}

func (s *Service) listWorktrees(ctx context.Context, repo string) ([]worktreeEntry, error) {
	out, err := s.run(ctx, repo, "worktree", "list", "--porcelain")
	if err != nil {
		return nil, err
	}
	var list []worktreeEntry
	var cur *worktreeEntry
	for _, line := range strings.Split(out, "\n") {
		key, val, _ := strings.Cut(line, " ")
		switch key {
		case "worktree":
			list = append(list, worktreeEntry{Path: val})
			cur = &list[len(list)-1]
		case "HEAD":
			if cur != nil {
				cur.Head = val
			}
		case "branch":
			if cur != nil {
				cur.Branch = strings.TrimPrefix(val, "refs/heads/")
			}
		case "prunable":
			if cur != nil {
				cur.Prunable = true
			}
		}
	}
	return list, nil
}

// underRoot reports whether p is strictly inside the worktree root.
func (s *Service) underRoot(p string) bool {
	rel, err := filepath.Rel(s.WorktreeRoot(), resolve(p))
	return err == nil && rel != "." && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

// registered finds the worktree entry whose path is wt.
func (s *Service) registered(ctx context.Context, repo, wt string) (worktreeEntry, error) {
	list, err := s.listWorktrees(ctx, repo)
	if err != nil {
		return worktreeEntry{}, err
	}
	want := resolve(wt)
	for _, e := range list {
		if resolve(e.Path) == want {
			return e, nil
		}
	}
	return worktreeEntry{}, fmt.Errorf("%w: %s", ErrNotRegistered, wt)
}

func (s *Service) taskPath(project, effort, task string) (string, error) {
	for _, n := range [][2]string{{"project", project}, {"effort", effort}, {"task", task}} {
		if err := validName(n[0], n[1]); err != nil {
			return "", err
		}
	}
	return filepath.Join(s.WorktreeRoot(), project, effort, task), nil
}

// IntegrationPath is where the effort's integration worktree lives. Task names
// cannot start with "_", so it never collides with a task.
func (s *Service) IntegrationPath(project, effort string) (string, error) {
	if err := validName("project", project); err != nil {
		return "", err
	}
	if err := validName("effort", effort); err != nil {
		return "", err
	}
	return filepath.Join(s.WorktreeRoot(), project, effort, "_integration"), nil
}

// AddTaskWorktree creates branch agentflow-task/<effort>/<task> at fromCommit in
// <data_dir>/worktrees/<project>/<effort>/<task>.
func (s *Service) AddTaskWorktree(ctx context.Context, repo, project, effort, task, fromCommit string) (Worktree, error) {
	path, err := s.taskPath(project, effort, task)
	if err != nil {
		return Worktree{}, err
	}
	branch := TaskBranch(effort, task)
	unlock := s.lockRepo(ctx, repo)
	defer unlock()
	if s.branchExists(ctx, repo, branch) {
		return Worktree{}, fmt.Errorf("%w: %s", ErrBranchExists, branch)
	}
	commit, err := s.resolveCommit(ctx, repo, fromCommit)
	if err != nil {
		return Worktree{}, err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return Worktree{}, err
	}
	if _, err := s.run(ctx, repo, "worktree", "add", "--no-track", "-b", branch, "--end-of-options", path, commit); err != nil {
		return Worktree{}, err
	}
	return Worktree{Path: path, Branch: branch}, nil
}

// AddIntegrationWorktree checks the integration branch out in the effort's
// _integration worktree, reusing it when it already exists. The branch must
// already exist (EnsureIntegrationBranch) and must not be checked out elsewhere.
func (s *Service) AddIntegrationWorktree(ctx context.Context, repo, project, effort, branch string) (Worktree, error) {
	path, err := s.IntegrationPath(project, effort)
	if err != nil {
		return Worktree{}, err
	}
	unlock := s.lockRepo(ctx, repo)
	defer unlock()
	if e, err := s.registered(ctx, repo, path); err == nil {
		if e.Branch != branch {
			return Worktree{}, fmt.Errorf("gitsvc: integration worktree is on %q, not %q", e.Branch, branch)
		}
		return Worktree{Path: path, Branch: branch}, nil
	}
	if !s.branchExists(ctx, repo, branch) {
		return Worktree{}, fmt.Errorf("gitsvc: integration branch %q does not exist", branch)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return Worktree{}, err
	}
	if _, err := s.run(ctx, repo, "worktree", "add", "--end-of-options", path, branch); err != nil {
		return Worktree{}, err
	}
	return Worktree{Path: path, Branch: branch}, nil
}

// AddReviewWorktree checks rev (a branch, tag or sha) out detached under
// <data_dir>/worktrees/<project>/_reviews/<name>. The API uses it so a review
// reads exactly the branch it was asked about and writes nothing back.
func (s *Service) AddReviewWorktree(ctx context.Context, repo, project, name, rev string) (Worktree, error) {
	if err := validName("project", project); err != nil {
		return Worktree{}, err
	}
	if err := validName("review", name); err != nil {
		return Worktree{}, err
	}
	if strings.TrimSpace(rev) == "" {
		return Worktree{}, fmt.Errorf("gitsvc: a review needs a branch or commit")
	}
	path := filepath.Join(s.WorktreeRoot(), project, "_reviews", name)
	unlock := s.lockRepo(ctx, repo)
	defer unlock()
	commit, err := s.resolveCommit(ctx, repo, rev)
	if err != nil {
		return Worktree{}, err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return Worktree{}, err
	}
	if _, err := s.run(ctx, repo, "worktree", "add", "--detach", "--end-of-options", path, commit); err != nil {
		return Worktree{}, err
	}
	return Worktree{Path: path, Commit: commit}, nil
}

// ReuseWorktree returns the existing worktree of a task for a repair attempt
// (PX2/D14). It fails unless git still has it registered on the task branch.
func (s *Service) ReuseWorktree(ctx context.Context, repo, project, effort, task string) (Worktree, error) {
	path, err := s.taskPath(project, effort, task)
	if err != nil {
		return Worktree{}, err
	}
	e, err := s.registered(ctx, repo, path)
	if err != nil {
		return Worktree{}, err
	}
	if want := TaskBranch(effort, task); e.Branch != want {
		return Worktree{}, fmt.Errorf("gitsvc: worktree %s is on %q, not %q", path, e.Branch, want)
	}
	if st, err := os.Stat(path); err != nil || !st.IsDir() {
		return Worktree{}, fmt.Errorf("%w: %s is missing on disk", ErrNotRegistered, path)
	}
	return Worktree{Path: path, Branch: e.Branch}, nil
}

// RemoveWorktree removes an agentflow worktree (ST4), then prunes stale
// administrative entries when that is safe. The path must lie under the
// worktree root and be registered; the main worktree can never match.
func (s *Service) RemoveWorktree(ctx context.Context, repo, wt string) error {
	if !s.underRoot(wt) {
		return fmt.Errorf("%w: %s", ErrOutsideDataDir, wt)
	}
	unlock := s.lockRepo(ctx, repo)
	defer unlock()
	if _, err := s.registered(ctx, repo, wt); err != nil {
		return err
	}
	if _, err := s.run(ctx, repo, "worktree", "remove", "--force", "--force", "--end-of-options", wt); err != nil {
		// git refuses when $WT/.git was tampered with. The path is already
		// known to be a registered agentflow worktree, so delete it ourselves
		// (never through a symlink) and let prune drop the admin entry.
		if st, lerr := os.Lstat(wt); lerr != nil || st.Mode()&os.ModeSymlink != 0 {
			return err
		}
		if rerr := os.RemoveAll(wt); rerr != nil {
			return rerr
		}
	}
	return s.pruneLocked(ctx, repo)
}

// PruneWorktrees drops administrative entries of agentflow worktrees whose
// directories vanished. It never prunes if any stale entry belongs to someone
// else, because git cannot scope `worktree prune` to a path.
func (s *Service) PruneWorktrees(ctx context.Context, repo string) error {
	unlock := s.lockRepo(ctx, repo)
	defer unlock()
	return s.pruneLocked(ctx, repo)
}

func (s *Service) pruneLocked(ctx context.Context, repo string) error {
	list, err := s.listWorktrees(ctx, repo)
	if err != nil {
		return err
	}
	stale := 0
	for _, e := range list {
		if !e.Prunable {
			continue
		}
		if !s.underRoot(e.Path) {
			return nil // a foreign stale entry: leave everything for its owner
		}
		stale++
	}
	if stale == 0 {
		return nil
	}
	_, err = s.run(ctx, repo, "worktree", "prune", "--expire", "now")
	return err
}
