package coord

import (
	"regexp"
	"strings"
)

// Ref is a branch a seat reported, with the commit it named when it named one.
type Ref struct {
	Branch string `json:"branch"`
	SHA    string `json:"sha,omitempty"`
}

var (
	// A branch with a known prefix, as coordinator.py took it (feature/x, fix/x), plus the other
	// prefixes seats use (af/af6b, bugfix/, hotfix/, chore/).
	reBranch = regexp.MustCompile(`(?:^|[\s(\x60'"])((?:feature|fix|bugfix|hotfix|chore|af|refactor)/[A-Za-z0-9._/-]+)`)
	// Any branch-like name followed by @sha or " at sha" (TASK-361 wrote af/af6b@6f5028a,
	// others "feature/x at abc1234").
	reAtSHA = regexp.MustCompile(`([A-Za-z0-9._-]+(?:/[A-Za-z0-9._-]+)+|[A-Za-z][A-Za-z0-9._-]*-[A-Za-z0-9._-]+)\s*(?:@|\s+at\s+(?:commit\s+)?)([0-9a-f]{7,40})\b`)
	reSHA   = regexp.MustCompile(`\b[0-9a-f]{7,40}\b`)
)

// Resolver finds the remote branches whose history contains sha (git branch -r --contains).
type Resolver func(sha string) []string

// BranchesIn reads the branches a DONE message names (AF-13 bug i: "no branch found in its DONE
// message: MERGE BY HAND" fired whenever a seat wrote it slightly differently). It accepts
// branch@sha, "branch at sha", a known-prefix branch alone, and a bare sha that resolve finds on
// a remote branch. base is never returned. When the message names nothing, fallback (the review
// task's text) is read the same way.
func BranchesIn(text, base string, resolve Resolver, fallback string) []Ref {
	out := refsIn(text, base, resolve)
	if len(out) == 0 && fallback != "" {
		out = refsIn(fallback, base, resolve)
	}
	return out
}

func refsIn(text, base string, resolve Resolver) []Ref {
	var out []Ref
	seen := map[string]int{}
	add := func(b, sha string) {
		b = strings.TrimRight(b, ".,;:)/-")
		b = strings.TrimPrefix(b, "origin/")
		if b == "" || b == base || !strings.ContainsAny(b, "/-") {
			return
		}
		if i, ok := seen[b]; ok {
			if out[i].SHA == "" {
				out[i].SHA = sha
			}
			return
		}
		seen[b] = len(out)
		out = append(out, Ref{Branch: b, SHA: sha})
	}
	named := map[string]bool{}
	for _, m := range reAtSHA.FindAllStringSubmatch(text, -1) {
		add(m[1], m[2])
		named[m[2]] = true
	}
	for _, m := range reBranch.FindAllStringSubmatch(text, -1) {
		b := m[1]
		if i := strings.IndexByte(b, '@'); i >= 0 {
			b = b[:i]
		}
		add(b, "")
	}
	if len(out) == 0 && resolve != nil {
		for _, sha := range reSHA.FindAllString(text, -1) {
			if named[sha] || !strings.ContainsAny(sha, "abcdef") || !strings.ContainsAny(sha, "0123456789") {
				continue // a word or a number, not a commit
			}
			for _, b := range resolve(sha) {
				add(b, sha)
			}
		}
	}
	return out
}
