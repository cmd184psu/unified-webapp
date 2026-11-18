package gitsvc

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
)

// Diff returns the unified diff of the task worktree against base, new
// (untracked, not ignored) files included, cut to maxBytes (0: no cut). It
// builds the diff in a throwaway index, so the worktree's real index, HEAD and
// refs are untouched and a snapshot taken around a model stage stays valid.
func (s *Service) Diff(ctx context.Context, repo, wt, base string, maxBytes int) (string, error) {
	pre, _, err := s.wtPre(ctx, repo, wt)
	if err != nil {
		return "", err
	}
	commit, err := s.resolveCommit(ctx, repo, base)
	if err != nil {
		return "", err
	}
	h, err := s.Hardened(ctx, repo)
	if err != nil {
		return "", err
	}
	tmp, err := os.MkdirTemp(s.dataDir, "diff-")
	if err != nil {
		return "", err
	}
	defer os.RemoveAll(tmp)
	env := append(slices.Clone(h.Env), "GIT_INDEX_FILE="+filepath.Join(tmp, "index"))
	run := func(args ...string) (string, error) {
		return s.exec(ctx, h.Args, env, wt, pre, nil, args)
	}
	for _, args := range [][]string{{"read-tree", commit}, addAllArgs} {
		if _, err := run(args...); err != nil {
			return "", err
		}
	}
	out, err := run("diff", "--cached", "--no-color", "--no-ext-diff", "--no-textconv", "--no-renames", commit)
	if err != nil {
		return "", err
	}
	if maxBytes > 0 && len(out) > maxBytes {
		out = strings.ToValidUTF8(out[:maxBytes], "") + fmt.Sprintf("\n[diff truncated at %d bytes]", maxBytes)
	}
	return out, nil
}
