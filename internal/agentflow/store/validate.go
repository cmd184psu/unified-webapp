package store

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// Roles that carry an ordered candidate model list (MB4).
var roles = []string{"worker", "reviewer", "planner", "architect", "critic", "compiler"}

var (
	// backendNameRE keeps the lane name "agentflow-<name>" within taskmaster's
	// 32-character limit (BK6).
	backendNameRE = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]{0,21}$`)
	slugRE        = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]{0,39}$`)

	reservedBackendNames = map[string]bool{"verify": true, "merge": true}
	apiTypes             = map[string]bool{"ollama": true, "openai_compatible": true, "anthropic": true, "opencode": true, "grok": true, "codex": true, "claude": true, "muse": true, "agy": true, "botchat_seat": true, "freebuff_seat": true}
)

const (
	minTimeout = 30 * time.Second
	maxTimeout = 4 * time.Hour
)

func validRole(role string) bool {
	for _, r := range roles {
		if r == role {
			return true
		}
	}
	return false
}

func validateBackendName(field, name string) error {
	if !backendNameRE.MatchString(name) {
		return Invalid(field, "must match "+backendNameRE.String())
	}
	if reservedBackendNames[name] {
		return Invalid(field, fmt.Sprintf("%q is reserved", name))
	}
	return nil
}

func validateHostURL(field, host string) error {
	u, err := url.Parse(host)
	if err != nil {
		return Invalid(field, "not a valid URL")
	}
	if (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return Invalid(field, "must be an http or https URL with a host")
	}
	if u.User != nil {
		return Invalid(field, "must not embed credentials")
	}
	return nil
}

func validateInt(field, v string, lo, hi int) (int, error) {
	n, err := strconv.Atoi(strings.TrimSpace(v))
	if err != nil {
		return 0, Invalid(field, "must be an integer")
	}
	if n < lo || n > hi {
		return 0, Invalid(field, fmt.Sprintf("must be between %d and %d", lo, hi))
	}
	return n, nil
}

func validateTimeout(field, v string) (time.Duration, error) {
	d, err := time.ParseDuration(strings.TrimSpace(v))
	if err != nil {
		return 0, Invalid(field, "must be a duration such as 30m")
	}
	if d < minTimeout || d > maxTimeout {
		return 0, Invalid(field, fmt.Sprintf("must be between %s and %s", minTimeout, maxTimeout))
	}
	return d, nil
}

// protectedPath is a directory a project repository may not equal, contain
// or sit inside, with the name used in the rejection message.
type protectedPath struct {
	label string
	path  string
}

func resolveDir(p string) (string, error) {
	abs, err := filepath.Abs(p)
	if err != nil {
		return "", err
	}
	return filepath.EvalSymlinks(abs)
}

// captureProtected resolves, once at Open, the directories a project
// repository must stay clear of (WC3): the server's working directory, the
// directory of its executable and the data dir (""  for an in-memory DB).
func captureProtected(dataDir string) ([]protectedPath, error) {
	cwd, err := os.Getwd()
	if err != nil {
		return nil, fmt.Errorf("working directory: %w", err)
	}
	cwd, err = resolveDir(cwd)
	if err != nil {
		return nil, fmt.Errorf("working directory: %w", err)
	}
	out := []protectedPath{{"server working directory", cwd}}
	exe, err := os.Executable()
	if err != nil {
		return nil, fmt.Errorf("executable path: %w", err)
	}
	if exe, err = resolveDir(filepath.Dir(exe)); err != nil {
		return nil, fmt.Errorf("executable directory: %w", err)
	}
	out = append(out, protectedPath{"server executable directory", exe})
	if dataDir != "" {
		dd, err := resolveDir(dataDir)
		if err != nil {
			return nil, fmt.Errorf("data directory: %w", err)
		}
		out = append(out, protectedPath{"data directory", dd})
	}
	return out, nil
}

// pathProblem words a filesystem failure, telling a missing path from one
// that exists but cannot be read.
func pathProblem(err error, missing string) string {
	if errors.Is(err, fs.ErrNotExist) {
		return missing
	}
	return "cannot be accessed: " + err.Error()
}

// within reports whether child equals or lies inside parent (both clean,
// absolute).
func within(parent, child string) bool {
	rel, err := filepath.Rel(parent, child)
	if err != nil {
		return false
	}
	return rel == "." || (rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)))
}

// ValidateRepoPath checks that p is an absolute path to an existing git
// repository that does not equal, contain or sit inside a protected directory
// (cwd, executable dir, data dir), and returns the symlink-resolved path (so
// two aliases of one repo collide on UNIQUE). A missing path and an
// unreadable one are reported differently; the overlap message names which
// protected directory was hit.
func (s *Store) ValidateRepoPath(p string) (string, error) {
	return s.validateRepoPath(p)
}

func (s *Store) validateRepoPath(p string) (string, error) {
	resolved, err := s.ValidateRepoDir(p)
	if err != nil {
		return "", err
	}
	// .git is a directory in a clone and a file in a linked worktree.
	if _, err := os.Stat(filepath.Join(resolved, ".git")); err != nil {
		return "", Invalid("repo_path", pathProblem(err, "is not a git repository"))
	}
	return resolved, nil
}

// ValidateRepoDir applies every ValidateRepoPath check except that the
// directory is a git repository, so the offer to run git init (PJ1) is
// refused for the same protected directories. It returns the resolved path.
func (s *Store) ValidateRepoDir(p string) (string, error) {
	if p == "" || !filepath.IsAbs(p) {
		return "", Invalid("repo_path", "must be an absolute path")
	}
	resolved, err := filepath.EvalSymlinks(filepath.Clean(p))
	if err != nil {
		return "", Invalid("repo_path", pathProblem(err, "does not exist"))
	}
	for _, prot := range s.protected {
		if within(prot.path, resolved) || within(resolved, prot.path) {
			return "", Invalid("repo_path", "must not equal, contain or sit inside the "+prot.label)
		}
	}
	fi, err := os.Stat(resolved)
	if err != nil {
		return "", Invalid("repo_path", pathProblem(err, "does not exist"))
	}
	if !fi.IsDir() {
		return "", Invalid("repo_path", "is not a directory")
	}
	return resolved, nil
}

// RecheckRepoPath re-validates a registered project's stored repo_path right
// now, for callers about to act on it (worktree creation): the path must
// still exist, still be a git repository, still clear every protected
// directory, and must not have turned into a symlink or moved behind one
// (its resolved form must equal what was stored).
func (s *Store) RecheckRepoPath(ctx context.Context, slug string) error {
	p, err := s.GetProject(ctx, slug)
	if err != nil {
		return err
	}
	resolved, err := s.validateRepoPath(p.RepoPath)
	if err != nil {
		return err
	}
	if resolved != p.RepoPath {
		return Invalid("repo_path", "resolves elsewhere than the registered path (a symlink changed)")
	}
	return nil
}
