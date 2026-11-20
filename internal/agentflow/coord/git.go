package coord

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// ExecGit runs git on the configured repositories. A merge never touches a repository's main
// checkout: it runs in a scratch worktree of the base branch under MergeDir, and only a merge
// whose gates all pass is pushed.
type ExecGit struct {
	Repos    []Repo
	MergeDir string
	// Env is added to every git and gate command (PATH for go, dotnet, node).
	Env []string
}

func (g ExecGit) run(ctx context.Context, dir string, timeout time.Duration, env []string, name string, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Dir = dir
	cmd.Env = append(append(os.Environ(), g.Env...), env...)
	out, err := cmd.CombinedOutput()
	return string(out), err
}

func (g ExecGit) git(ctx context.Context, dir string, args ...string) (string, error) {
	return g.run(ctx, dir, 5*time.Minute, nil, "git", args...)
}

func (g ExecGit) RemoteHas(ctx context.Context, r Repo, branch string) bool {
	out, err := g.git(ctx, r.Path, "ls-remote", "--heads", "origin", "refs/heads/"+branch)
	return err == nil && strings.Contains(out, "refs/heads/"+branch)
}

func (g ExecGit) Merged(ctx context.Context, r Repo, branch, base string) bool {
	_, err := g.git(ctx, r.Path, "merge-base", "--is-ancestor", "origin/"+branch, "origin/"+base)
	return err == nil
}

func (g ExecGit) Contains(ctx context.Context, sha string) []string {
	seen := map[string]bool{}
	var out []string
	for _, r := range g.Repos {
		res, err := g.git(ctx, r.Path, "branch", "-r", "--contains", sha)
		if err != nil {
			continue
		}
		for _, l := range strings.Split(res, "\n") {
			b := strings.TrimSpace(l)
			if b == "" || strings.Contains(b, "->") || seen[b] {
				continue
			}
			seen[b] = true
			out = append(out, b)
		}
	}
	sort.Strings(out)
	return out
}

func clip200(s string) string {
	s = strings.TrimSpace(s)
	if len(s) > 200 {
		return s[len(s)-200:]
	}
	return s
}

func (g ExecGit) Merge(ctx context.Context, r Repo, base, branch, tid string, approved bool) error {
	if _, err := g.git(ctx, r.Path, "fetch", "-q", "origin"); err != nil {
		return fmt.Errorf("fetch failed")
	}
	dir := g.MergeDir
	if dir == "" {
		dir = os.TempDir()
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	wt, err := os.MkdirTemp(dir, "coord-"+r.Name+"-")
	if err != nil {
		return err
	}
	if err := os.Remove(wt); err != nil { // git worktree add wants to create it
		return err
	}
	local := "coord-merge-" + filepath.Base(wt)
	if out, err := g.git(ctx, r.Path, "worktree", "add", "-q", "-B", local, wt, "origin/"+base); err != nil {
		return fmt.Errorf("worktree failed: %s", clip200(out))
	}
	defer func() {
		_, _ = g.git(context.Background(), r.Path, "worktree", "remove", "--force", wt)
		_, _ = g.git(context.Background(), r.Path, "branch", "-D", local)
	}()
	msg := fmt.Sprintf("Merge %s (%s)", branch, tid)
	if out, err := g.git(ctx, wt, "merge", "--no-ff", "-q", "origin/"+branch, "-m", msg); err != nil {
		if !g.union(ctx, wt, r, msg) {
			_, _ = g.git(ctx, wt, "merge", "--abort")
			return fmt.Errorf("merge conflict in %s: %s", r.Name, clip200(out))
		}
	}
	var env []string
	for k, v := range r.Env {
		env = append(env, k+"="+v)
	}
	if approved && r.ApprovedEnv != "" {
		env = append(env, r.ApprovedEnv+"="+tid)
	}
	for _, gate := range r.Gates {
		if out, err := g.run(ctx, wt, 20*time.Minute, env, "/bin/sh", "-c", gate); err != nil {
			return fmt.Errorf("%s gate failed (%s): %s", r.Name, clipStr(gate, 40), clip200(out))
		}
	}
	if out, err := g.git(ctx, wt, "push", "-q", "origin", "HEAD:"+base); err != nil {
		return fmt.Errorf("%s push failed: %s", r.Name, clip200(out))
	}
	return nil
}

// union resolves a conflict confined to the repository's union files (MANIFEST.md: every
// branch only appends rows) by keeping both sides' lines, and finishes the merge.
func (g ExecGit) union(ctx context.Context, wt string, r Repo, msg string) bool {
	if len(r.UnionFiles) == 0 {
		return false
	}
	out, err := g.git(ctx, wt, "diff", "--name-only", "--diff-filter=U")
	if err != nil {
		return false
	}
	files := strings.Fields(out)
	if len(files) == 0 {
		return false
	}
	for _, f := range files {
		if !in(f, r.UnionFiles...) {
			return false
		}
	}
	for _, f := range files {
		p := filepath.Join(wt, f)
		b, err := os.ReadFile(p)
		if err != nil {
			return false
		}
		var keep []string
		for _, l := range strings.Split(string(b), "\n") {
			if strings.HasPrefix(l, "<<<<<<< ") || l == "=======" || strings.HasPrefix(l, ">>>>>>> ") {
				continue
			}
			keep = append(keep, l)
		}
		if err := os.WriteFile(p, []byte(strings.Join(keep, "\n")), 0o644); err != nil {
			return false
		}
		if _, err := g.git(ctx, wt, "add", f); err != nil {
			return false
		}
	}
	_, err = g.git(ctx, wt, "commit", "-q", "--no-edit", "-m", msg+" ("+strings.Join(files, ", ")+" rows unioned)")
	return err == nil
}
