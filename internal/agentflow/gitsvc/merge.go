package gitsvc

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// ErrConflict is wrapped by *ConflictError.
var ErrConflict = errors.New("gitsvc: merge conflict")

// ConflictError lists the files a merge could not combine.
type ConflictError struct{ Files []string }

func (e *ConflictError) Error() string {
	return fmt.Sprintf("%v: %s", ErrConflict, strings.Join(e.Files, ", "))
}

func (e *ConflictError) Is(target error) bool { return target == ErrConflict }

// conflicted lists the unmerged paths of the worktree.
func (s *Service) conflicted(ctx context.Context, repo, wt string, pre []string) ([]string, error) {
	out, err := s.runIn(ctx, repo, wt, pre, nil, "diff", "--name-only", "-z", "--diff-filter=U", "--no-ext-diff", "--no-textconv")
	if err != nil {
		return nil, err
	}
	var files []string
	for _, f := range strings.Split(out, "\x00") {
		if f != "" {
			files = append(files, f)
		}
	}
	return files, nil
}

// abortMerge ends a merge in progress. It ignores cancellation of ctx, so a
// cancelled caller cannot leave MERGE_HEAD behind, and is a no-op when no merge
// is in progress.
func (s *Service) abortMerge(ctx context.Context, repo, wt string, pre []string, admin string) error {
	if _, err := os.Stat(filepath.Join(admin, "MERGE_HEAD")); err != nil {
		return nil
	}
	_, err := s.runIn(context.WithoutCancel(ctx), repo, wt, pre, nil, "merge", "--abort")
	return err
}

func (s *Service) branchCommit(ctx context.Context, repo, branch string) (string, error) {
	if err := overrideCheck(branch); err != nil {
		return "", err
	}
	return s.resolveCommit(ctx, repo, "refs/heads/"+branch)
}

func overrideCheck(branch string) error {
	if !overrideRE.MatchString(branch) || strings.Contains(branch, "..") || strings.Contains(branch, "@{") {
		return fmt.Errorf("%w: branch %q", ErrInvalidName, branch)
	}
	return nil
}

// Merge merges taskBranch into the integration worktree, which must have
// integrationBranch checked out, with --no-ff as the
// engine (PX3). It takes the per-repository lock, so merges for one repository
// are serialised, and returns the new integration HEAD. On a conflict it
// aborts the merge, leaving the integration worktree clean, and returns a
// *ConflictError (errors.Is ErrConflict) naming the files.
func (s *Service) Merge(ctx context.Context, repo, integrationWT, integrationBranch, taskBranch string) (string, error) {
	if !strings.HasPrefix(taskBranch, "agentflow-task/") {
		return "", fmt.Errorf("%w: %q is not a task branch", ErrForbiddenRef, taskBranch)
	}
	unlock := s.lockRepo(ctx, repo)
	defer unlock()
	pre, admin, err := s.wtPre(ctx, repo, integrationWT)
	if err != nil {
		return "", err
	}
	if e, err := s.registered(ctx, repo, integrationWT); err != nil || e.Branch != integrationBranch || integrationBranch == "" {
		return "", fmt.Errorf("gitsvc: %s is not on integration branch %q", integrationWT, integrationBranch)
	}
	commit, err := s.branchCommit(ctx, repo, taskBranch)
	if err != nil {
		return "", err
	}
	_, err = s.runIn(ctx, repo, integrationWT, pre, nil,
		"-c", "user.name="+engineName, "-c", "user.email="+engineEmail,
		"merge", "--no-ff", "--no-verify", "--no-edit", "-m", "agentflow: merge "+taskBranch, commit)
	if err != nil {
		bg := context.WithoutCancel(ctx)
		files, cerr := s.conflicted(bg, repo, integrationWT, pre)
		aerr := s.abortMerge(ctx, repo, integrationWT, pre, admin)
		if cerr == nil && len(files) > 0 {
			return "", errors.Join(&ConflictError{Files: files}, aerr)
		}
		return "", errors.Join(err, aerr)
	}
	return s.runIn(ctx, repo, integrationWT, pre, nil, "rev-parse", "--verify", "HEAD")
}

// PrepareConflictRepair merges the integration branch into the task worktree
// without committing, so that a conflicted file carries markers for the repair
// attempt (PX3). It returns the conflicted files; none means the merge was
// clean and is staged, and the engine's next CommitAll completes it. Only the
// task worktree and its branch are touched.
func (s *Service) PrepareConflictRepair(ctx context.Context, repo, taskWT, integrationBranch string) ([]string, error) {
	pre, admin, err := s.wtPre(ctx, repo, taskWT)
	if err != nil {
		return nil, err
	}
	commit, err := s.branchCommit(ctx, repo, integrationBranch)
	if err != nil {
		return nil, err
	}
	_, err = s.runIn(ctx, repo, taskWT, pre, nil,
		"-c", "user.name="+engineName, "-c", "user.email="+engineEmail,
		"merge", "--no-ff", "--no-commit", "--no-verify", commit)
	if err == nil {
		return nil, nil
	}
	files, cerr := s.conflicted(context.WithoutCancel(ctx), repo, taskWT, pre)
	if cerr == nil && len(files) > 0 {
		return files, nil
	}
	return nil, errors.Join(err, s.abortMerge(ctx, repo, taskWT, pre, admin))
}
