package verify

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"go/parser"
	"go/token"
	"io"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"sort"
	"strings"

	"github.com/BurntSushi/toml"
	"go.yaml.in/yaml/v3"
)

// maxParseBytes bounds how much of one changed file is read for parsing and
// the conflict-marker scan.
const maxParseBytes = 8 << 20

// maxScanFiles bounds how many changed files are examined; the rest produce a
// warning.
const maxScanFiles = 5000

// emptyTree is git's well-known empty tree, used so attributes are never read
// from a model-writable tree.
const emptyTree = "4b825dc642cb6eb9a060e54bf8d69288fbee3904"

// Input describes one attempt to check. BaseCommit is the commit the attempt
// started from; changes are everything in the worktree relative to it,
// including untracked files.
type Input struct {
	Worktree string
	// GitDir is the trusted admin gitdir of the worktree, resolved from the
	// main repository (never from $WT/.git, which the model can rewrite).
	GitDir       string
	BaseCommit   string
	FilesTouched []string
	// OwnedByOthers are the files other tasks, which this one does not depend on, are to write. A change to
	// one of them that this task does not list itself fails the scope check: it would conflict at merge.
	OwnedByOthers []string
	// NoChangesReason is the worker's declaration that the files already
	// satisfy the task; it lets an unchanged tree pass no_changes.
	NoChangesReason string
}

// Structural runs the in-process checks over the worktree diff. It returns an
// error only when git itself cannot report the changed files; check failures
// are carried in the report.
func Structural(ctx context.Context, in Input) (VerifyReport, error) {
	root, err := filepath.EvalSymlinks(in.Worktree)
	if err != nil {
		return VerifyReport{}, fmt.Errorf("verify: worktree: %w", err)
	}
	if in.GitDir == "" {
		return VerifyReport{}, errors.New("verify: trusted git dir is required")
	}
	files, nested, err := changedFiles(ctx, in.GitDir, root, in.BaseCommit)
	if err != nil {
		return VerifyReport{}, err
	}

	rep := VerifyReport{Warnings: []string{}, NewFailures: []string{}, BaselineFailures: []string{}}
	if len(files) == 0 && len(nested) == 0 && in.NoChangesReason != "" {
		// A declaration of "nothing to change" is believed only when the files
		// the task names exist; a model that never wrote them says it too (H14).
		var missing []string
		for _, f := range in.FilesTouched {
			if _, err := os.Lstat(filepath.Join(root, filepath.FromSlash(f))); err != nil || strings.HasPrefix(filepath.Clean(f), "..") {
				missing = append(missing, f)
			}
		}
		if len(missing) > 0 {
			rep.Checks = []Check{{Name: CheckNoChanges, Pass: false, Details: []string{"the worker declares nothing to change, but these files of the task do not exist: " + strings.Join(missing, ", ")}}}
			return rep, nil
		}
		rep.Checks = []Check{{Name: CheckNoChanges, Pass: true}}
		rep.Warnings = append(rep.Warnings, "worker declares nothing to change: "+in.NoChangesReason)
		rep.Finalize()
		return rep, nil
	}
	if len(files) == 0 && len(nested) == 0 {
		rep.Checks = []Check{{Name: CheckNoChanges, Pass: false, Details: []string{"the worker stage changed no files"}}}
		return rep, nil
	}
	rep.Checks = append(rep.Checks, Check{Name: CheckNoChanges, Pass: true})

	if len(files) > maxScanFiles {
		rep.Warnings = append(rep.Warnings, fmt.Sprintf("%d files changed; only the first %d were checked", len(files), maxScanFiles))
		files = files[:maxScanFiles]
	}

	nest := Check{Name: CheckNestedRepo, Pass: len(nested) == 0, Details: nested}
	rep.Checks = append(rep.Checks, nest)

	scope := Check{Name: CheckScope, Pass: true}
	taken := map[string]bool{}
	for _, f := range outOfScope(files, in.OwnedByOthers, true) {
		if len(outOfScope([]string{f}, in.FilesTouched, false)) > 0 || len(in.FilesTouched) == 0 {
			taken[f] = true
			scope.Pass = false
			scope.Details = append(scope.Details, "belongs to another task that is not finished yet, so it is out of scope: "+f+" (do not change it; if this task cannot be done without that change, report blocked and name the file)")
		}
	}
	for _, f := range outOfScope(files, in.FilesTouched, false) {
		if !taken[f] {
			rep.Warnings = append(rep.Warnings, "changed file outside files_touched: "+f)
		}
	}
	rep.Checks = append(rep.Checks, scope)

	contain := Check{Name: CheckContainment, Pass: true}
	var present []string
	for _, f := range files {
		if reason := escapes(root, f); reason != "" {
			contain.Pass = false
			contain.Details = append(contain.Details, f+": "+reason)
			continue
		}
		present = append(present, f)
	}
	rep.Checks = append(rep.Checks, contain)

	parse := Check{Name: CheckParse, Pass: true}
	markers := Check{Name: CheckConflicts, Pass: true}
	for _, f := range present {
		data, ok, why := readCapped(filepath.Join(root, filepath.FromSlash(f)))
		if !ok {
			if why != "" {
				rep.Warnings = append(rep.Warnings, "not parsed: "+f+": "+why)
			}
			continue
		}
		if msg := parseFile(f, data); msg != "" {
			parse.Pass = false
			parse.Details = append(parse.Details, f+": "+msg)
		}
		if line := conflictMarkerLine(data); line > 0 {
			markers.Pass = false
			markers.Details = append(markers.Details, fmt.Sprintf("%s:%d", f, line))
		}
	}
	rep.Checks = append(rep.Checks, parse, markers)

	rep.Finalize()
	return rep, nil
}

// git runs read-only git with fixed argv and no shell. The repository is
// always the trusted gitDir; nothing is discovered from the worktree. Config,
// attributes and filters that could name an external program are neutralised.
func git(ctx context.Context, gitDir, root string, args ...string) ([]byte, error) {
	full := append([]string{
		"--git-dir=" + gitDir, "--work-tree=" + root,
		"--attr-source=" + emptyTree,
		"-c", "core.attributesFile=/dev/null",
		"-c", "core.fsmonitor=false",
		"-c", "core.quotepath=off",
	}, args...)
	cmd := exec.CommandContext(ctx, "git", full...)
	cmd.Dir = root
	cmd.Env = append(os.Environ(),
		"GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_NOSYSTEM=1", "GIT_ATTR_NOSYSTEM=1",
		"GIT_OPTIONAL_LOCKS=0", "GIT_TERMINAL_PROMPT=0")
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("verify: git %s: %w: %s", args[0], err, strings.TrimSpace(stderr.String()))
	}
	return out, nil
}

// changedFiles lists slash-separated paths changed since base (added,
// modified, deleted, type-changed) plus untracked files, sorted and unique.
// nested lists changes that are or sit inside another repository (gitlinks,
// nested repos, .git path components).
func changedFiles(ctx context.Context, gitDir, root, base string) (files, nested []string, err error) {
	if base == "" {
		return nil, nil, errors.New("verify: base commit is required")
	}
	raw, err := git(ctx, gitDir, root, "diff", "--raw", "-z", "--no-renames", "--no-ext-diff", "--no-textconv", base, "--")
	if err != nil {
		return nil, nil, err
	}
	untracked, err := git(ctx, gitDir, root, "ls-files", "--others", "--exclude-standard", "-z")
	if err != nil {
		return nil, nil, err
	}
	seen := map[string]bool{}
	add := func(p string) {
		// Tool state (.omc, .claude) is never part of the project.
		if top, _, _ := strings.Cut(p, "/"); top == ".omc" || top == ".claude" {
			return
		}
		if p != "" && !seen[p] {
			seen[p] = true
			files = append(files, p)
		}
	}
	// Records are ":oldmode newmode oldsha newsha status\0path\0".
	parts := strings.Split(string(raw), "\x00")
	for i := 0; i+1 < len(parts); i += 2 {
		meta := strings.Fields(parts[i])
		p := parts[i+1]
		if len(meta) >= 2 && (meta[0] == ":160000" || meta[1] == "160000") {
			nested = append(nested, p+": gitlink")
		}
		add(p)
	}
	for _, p := range strings.Split(string(untracked), "\x00") {
		if strings.HasSuffix(p, "/") {
			nested = append(nested, p+": nested repository")
			continue
		}
		add(p)
	}
	for _, p := range files {
		for _, c := range strings.Split(p, "/") {
			if c == ".git" {
				nested = append(nested, p+": .git path component")
				break
			}
		}
	}
	sort.Strings(files)
	sort.Strings(nested)
	return files, nested, nil
}

// outOfScope returns the changed files not covered by a files_touched entry, or
// with inside set the ones it does cover. An entry covers itself and, as a
// directory, everything beneath it. With no declared entries the answer is empty.
func outOfScope(changed, touched []string, inside bool) []string {
	if len(touched) == 0 {
		return nil
	}
	var out []string
	for _, f := range changed {
		covered := false
		for _, t := range touched {
			t = path.Clean(filepath.ToSlash(t))
			if t == "." || f == t || strings.HasPrefix(f, t+"/") {
				covered = true
				break
			}
		}
		if covered == inside {
			out = append(out, f)
		}
	}
	return out
}

// escapes reports why rel (slash-separated, relative to root) leaves the
// worktree, or "" when it stays inside. A missing file (deleted) is fine.
func escapes(root, rel string) string {
	clean := path.Clean(rel)
	if path.IsAbs(clean) || clean == ".." || strings.HasPrefix(clean, "../") {
		return "path outside the worktree"
	}
	full := filepath.Join(root, filepath.FromSlash(clean))
	if _, err := os.Lstat(full); errors.Is(err, os.ErrNotExist) {
		// Deleted, or a path below a dangling symlink; check the latter.
		if resolved, rerr := filepath.EvalSymlinks(filepath.Dir(full)); rerr == nil && !within(root, resolved) {
			return "parent directory resolves outside the worktree"
		}
		return ""
	}
	resolved, err := filepath.EvalSymlinks(full)
	if err != nil {
		return "symlink does not resolve inside the worktree"
	}
	if !within(root, resolved) {
		return "symlink escapes the worktree"
	}
	return ""
}

func within(root, p string) bool {
	rel, err := filepath.Rel(root, p)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

// readCapped reads a regular file up to maxParseBytes. ok is false when the
// file is not read; why is non-empty when that deserves a warning (a missing
// file, for example a deletion, does not).
func readCapped(p string) (data []byte, ok bool, why string) {
	fi, err := os.Stat(p)
	if err != nil {
		return nil, false, ""
	}
	if !fi.Mode().IsRegular() {
		return nil, false, "not a regular file"
	}
	if fi.Size() > maxParseBytes {
		return nil, false, "larger than 8 MiB"
	}
	f, err := os.Open(p)
	if err != nil {
		return nil, false, "unreadable: " + err.Error()
	}
	defer f.Close()
	data, err = io.ReadAll(io.LimitReader(f, maxParseBytes))
	if err != nil {
		return nil, false, "unreadable: " + err.Error()
	}
	return data, true, ""
}

// parseFile returns a parse error message for the file's extension, or "".
func parseFile(name string, data []byte) string {
	switch strings.ToLower(path.Ext(name)) {
	case ".go":
		if _, err := parser.ParseFile(token.NewFileSet(), name, data, parser.AllErrors); err != nil {
			return err.Error()
		}
	case ".json":
		if !json.Valid(data) {
			var v any
			return json.Unmarshal(data, &v).Error()
		}
	case ".yaml", ".yml":
		dec := yaml.NewDecoder(bytes.NewReader(data))
		for {
			var n yaml.Node
			err := dec.Decode(&n)
			if errors.Is(err, io.EOF) {
				break
			}
			if err != nil {
				return err.Error()
			}
		}
	case ".toml":
		var v map[string]any
		if _, err := toml.Decode(string(data), &v); err != nil {
			return err.Error()
		}
	}
	return ""
}

// conflictMarkerLine returns the 1-based line of the first git conflict start
// or end marker, or 0. Binary content is not scanned.
func conflictMarkerLine(data []byte) int {
	if bytes.IndexByte(data, 0) >= 0 {
		return 0
	}
	for i, line := range strings.Split(string(data), "\n") {
		for _, m := range []string{"<<<<<<<", ">>>>>>>"} {
			if rest, ok := strings.CutPrefix(line, m); ok && (rest == "" || rest[0] == ' ' || rest[0] == '\r') {
				return i + 1
			}
		}
	}
	return 0
}
