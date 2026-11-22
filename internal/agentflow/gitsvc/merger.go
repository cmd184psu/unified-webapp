package gitsvc

import (
	"context"
	"fmt"
	"strings"
)

// MergeBase returns the common ancestor of two refs.
func (s *Service) MergeBase(ctx context.Context, repo, a, b string) (string, error) {
	out, err := s.run(ctx, repo, "merge-base", a, b)
	if err != nil {
		return "", fmt.Errorf("gitsvc: merge-base %s %s: %w", a, b, err)
	}
	return strings.TrimSpace(out), nil
}

// LogRange is the one-line log of to since from, cut to maxBytes.
func (s *Service) LogRange(ctx context.Context, repo, from, to string, maxBytes int) (string, error) {
	out, err := s.run(ctx, repo, "log", "--oneline", "--no-decorate", from+".."+to)
	if err != nil {
		return "", fmt.Errorf("gitsvc: log %s..%s: %w", from, to, err)
	}
	return cut(out, maxBytes), nil
}

// WorktreeDiff is the uncommitted diff of a worktree against HEAD, cut to
// maxBytes. It is how the merge reviewer reads the merger's resolution.
func (s *Service) WorktreeDiff(ctx context.Context, repo, wt string, maxBytes int) (string, error) {
	pre, _, err := s.wtPre(ctx, repo, wt)
	if err != nil {
		return "", err
	}
	out, err := s.runIn(ctx, repo, wt, pre, nil, "diff", "--no-color", "--no-ext-diff", "--no-textconv", "--no-renames", "HEAD")
	if err != nil {
		return "", fmt.Errorf("gitsvc: diff HEAD in %s: %w", wt, err)
	}
	return cut(out, maxBytes), nil
}

// DiffRefs is the unified diff from one ref to another, cut to maxBytes.
func (s *Service) DiffRefs(ctx context.Context, repo, from, to string, maxBytes int) (string, error) {
	out, err := s.run(ctx, repo, "diff", "--no-color", "--no-ext-diff", "--no-textconv", "--no-renames", from, to)
	if err != nil {
		return "", fmt.Errorf("gitsvc: diff %s..%s: %w", from, to, err)
	}
	return cut(out, maxBytes), nil
}

func cut(s string, maxBytes int) string {
	if maxBytes > 0 && len(s) > maxBytes {
		return strings.ToValidUTF8(s[:maxBytes], "") + fmt.Sprintf("\n[truncated at %d bytes]", maxBytes)
	}
	return s
}
