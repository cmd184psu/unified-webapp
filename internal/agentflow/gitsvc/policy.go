package gitsvc

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Violation kinds reported by DetectAndRevert.
const (
	ViolationHead  = "head"  // the worktree's HEAD or its task branch moved (commit, reset, checkout)
	ViolationIndex = "index" // the index differs and HEAD did not move (git add, rm, restore --staged)
	ViolationMerge = "merge" // a merge, cherry-pick or revert was started
)

// Violation is one policy breach found after a model stage (GIT5, layer 2).
type Violation struct {
	Kind   string
	Detail string
}

// stateFiles are the in-progress operation markers in the admin git dir.
var stateFiles = []string{"MERGE_HEAD", "CHERRY_PICK_HEAD", "REVERT_HEAD"}

// auxFiles are written with a marker and restored with it.
var auxFiles = []string{"MERGE_MSG", "MERGE_MODE"}

// Snapshot is the state of one stage worktree that a read-only model must not
// change. It covers only that worktree (its HEAD, the branch HEAD names, its
// index and operation markers), never the rest of the repository, so
// concurrent engine writes by other tasks are never mistaken for violations.
type Snapshot struct {
	repo, wt, admin string
	headRef         string // symbolic HEAD target, empty when detached
	headCommit      string // commit HEAD resolves to
	branchCommit    string // commit headRef points at, empty when detached
	index           string // `ls-files --stage` output
	markers         map[string]bool
	saved           map[string][]byte // contents of every marker and aux file present
}

func (s *Service) wtPre(ctx context.Context, repo, wt string) ([]string, string, error) {
	if !s.underRoot(wt) {
		return nil, "", fmt.Errorf("%w: %s", ErrOutsideDataDir, wt)
	}
	admin, err := s.AdminGitDir(ctx, repo, wt)
	if err != nil {
		return nil, "", err
	}
	return []string{"--git-dir=" + admin, "--work-tree=" + resolve(wt)}, admin, nil
}

// Snapshot records the policed state of the agentflow worktree wt. Take it
// before a model stage and pass it to DetectAndRevert after.
func (s *Service) Snapshot(ctx context.Context, repo, wt string) (Snapshot, error) {
	pre, admin, err := s.wtPre(ctx, repo, wt)
	if err != nil {
		return Snapshot{}, err
	}
	snap := Snapshot{repo: repo, wt: wt, admin: admin, markers: map[string]bool{}, saved: map[string][]byte{}}
	if snap.headRef, err = s.runIn(ctx, repo, wt, pre, nil, "symbolic-ref", "-q", "HEAD"); err != nil {
		var ge *GitError
		if !errors.As(err, &ge) { // exit 1 means detached HEAD
			return Snapshot{}, err
		}
		snap.headRef = ""
	}
	if snap.headCommit, err = s.runIn(ctx, repo, wt, pre, nil, "rev-parse", "--verify", "HEAD"); err != nil {
		return Snapshot{}, err
	}
	if snap.headRef != "" {
		snap.branchCommit = snap.headCommit
	}
	if snap.index, err = s.runIn(ctx, repo, wt, pre, nil, "ls-files", "--stage", "--full-name"); err != nil {
		return Snapshot{}, err
	}
	for _, f := range stateFiles {
		_, serr := os.Stat(filepath.Join(admin, f))
		snap.markers[f] = serr == nil
	}
	for _, f := range append(append([]string{}, stateFiles...), auxFiles...) {
		if b, rerr := os.ReadFile(filepath.Join(admin, f)); rerr == nil {
			snap.saved[f] = b
		}
	}
	return snap, nil
}

// DetectAndRevert compares the worktree with before and undoes every change a
// model made through git: HEAD and the task branch go back to the snapshot
// (working-tree file edits are kept), the index is restored, and any started
// merge, cherry-pick or revert is dropped. It returns one Violation per kind of
// breach; none means the stage kept to read-only git. A moved HEAD implies a
// changed index, so the index is only reported on its own.
func (s *Service) DetectAndRevert(ctx context.Context, before Snapshot) ([]Violation, error) {
	repo, wt := before.repo, before.wt
	pre, admin, err := s.wtPre(ctx, repo, wt)
	if err != nil {
		return nil, err
	}
	after, err := s.Snapshot(ctx, repo, wt)
	if err != nil {
		// The model left HEAD unreadable; restore it below from the snapshot.
		after = Snapshot{}
	}
	var out []Violation
	headMoved := after.headRef != before.headRef || after.headCommit != before.headCommit
	if headMoved {
		out = append(out, Violation{ViolationHead, fmt.Sprintf("HEAD %s@%s, expected %s@%s",
			orDetached(after.headRef), short(after.headCommit), orDetached(before.headRef), short(before.headCommit))})
	}
	var appeared, vanished []string
	for _, f := range stateFiles {
		if !before.markers[f] && after.markers[f] {
			appeared = append(appeared, f)
		}
		if before.markers[f] && !after.markers[f] {
			vanished = append(vanished, f)
		}
	}
	if len(appeared)+len(vanished) > 0 {
		d := ""
		if len(appeared) > 0 {
			d = strings.Join(appeared, ", ") + " appeared"
		}
		if len(vanished) > 0 {
			if d != "" {
				d += "; "
			}
			d += strings.Join(vanished, ", ") + " removed"
		}
		out = append(out, Violation{ViolationMerge, d})
	}
	if !headMoved && len(appeared)+len(vanished) == 0 && after.index != before.index {
		out = append(out, Violation{ViolationIndex, "index changed"})
	}

	if headMoved {
		if before.headRef != "" {
			if _, err := s.runIn(ctx, repo, wt, pre, nil, "symbolic-ref", "HEAD", before.headRef); err != nil {
				return out, err
			}
			if _, err := s.runIn(ctx, repo, wt, pre, nil, "update-ref", before.headRef, before.branchCommit); err != nil {
				return out, err
			}
		} else if _, err := s.runIn(ctx, repo, wt, pre, nil, "update-ref", "--no-deref", "HEAD", before.headCommit); err != nil {
			return out, err
		}
	}
	for _, f := range appeared {
		if err := os.Remove(filepath.Join(admin, f)); err != nil {
			return out, err
		}
		os.Remove(filepath.Join(admin, "MERGE_MODE"))
		os.Remove(filepath.Join(admin, "MERGE_MSG"))
	}
	if len(vanished) > 0 { // e.g. `git merge --abort` during conflict repair
		for f, b := range before.saved {
			if err := os.WriteFile(filepath.Join(admin, f), b, 0o644); err != nil {
				return out, err
			}
		}
	}
	if headMoved || after.index != before.index || len(vanished) > 0 {
		if _, err := s.runIn(ctx, repo, wt, pre, nil, "read-tree", "--empty"); err != nil {
			return out, err
		}
		if before.index != "" {
			if _, err := s.runIn(ctx, repo, wt, pre, []byte(before.index+"\n"), "update-index", "--index-info"); err != nil {
				return out, err
			}
		}
	}
	return out, nil
}

func orDetached(ref string) string {
	if ref == "" {
		return "detached"
	}
	return strings.TrimPrefix(ref, "refs/heads/")
}

func short(c string) string {
	if len(c) > 10 {
		return c[:10]
	}
	return c
}
