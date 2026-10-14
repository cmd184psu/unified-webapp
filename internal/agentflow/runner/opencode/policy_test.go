package opencode

import (
	"regexp"
	"strings"
	"testing"
)

// matchPattern is a Go copy of opencode's permission wildcard matcher: the
// whole text must match, "*" is ".*", and a trailing " *" makes the arguments
// optional.
func matchPattern(pattern, text string) bool {
	text = strings.ReplaceAll(text, `\`, "/")
	esc := regexp.QuoteMeta(strings.ReplaceAll(pattern, `\`, "/"))
	esc = strings.ReplaceAll(esc, `\*`, ".*")
	esc = strings.ReplaceAll(esc, `\?`, ".")
	if strings.HasSuffix(esc, " .*") {
		esc = strings.TrimSuffix(esc, " .*") + "( .*)?"
	}
	return regexp.MustCompile("(?s)^" + esc + "$").MatchString(text)
}

// decide returns the action of the last matching rule, or "" for none.
func decide(rs rules, text string) string {
	action := ""
	for _, r := range rs {
		if matchPattern(r.Pattern, text) {
			action = r.Action
		}
	}
	return action
}

var splitCmd = regexp.MustCompile(`\s*(?:&&|\|\||;|\|)\s*`)

// allowed reports whether every sub-command of cmd is allowed.
func allowed(rs rules, cmd string) bool {
	for _, c := range splitCmd.Split(cmd, -1) {
		if decide(rs, strings.TrimSpace(c)) != "allow" {
			return false
		}
	}
	return true
}

func TestEffectivePolicy(t *testing.T) {
	cases := []struct {
		cmd           string
		worker, other bool
	}{
		{"git status", true, true},
		{"git status --short", true, true},
		{"git log --oneline -5", true, true},
		{"git diff HEAD~1", true, true},
		{"git branch --list", true, true},
		{"git branch --show-current", true, true},
		{"git commit -m x", false, false},
		{"git -C . commit -m x", false, false},
		{"git -C . log", false, false},
		{"git branch newb", false, false},
		{"git branch -d x", false, false},
		{"git add .", false, false},
		{"git stash", false, false},
		{"git push origin main", false, false},
		{"git status && git commit -m x", false, false},
		{"git diff --output=/tmp/x", false, false},
		{"git log --output=x", false, false},
		{"git grep -O vim foo", false, false},
		{"git grep foo --open-files-in-pager=vim", false, false},
		{"git grep foo", true, true},
		{"git grep --open=echo hi", false, false},
		{"git grep -iOecho hi", false, false},
		{"git grep --open=x y", false, false},
		{"git grep -iOx y", false, false},
		{"file --compile x", true, false},
		{"gh pr list", false, false},
		{"glab mr list", false, false},
		{"ssh host", false, false},
		{"ls -la", true, true},
		{"ls -la | head -5", true, true},
		{"cat a", true, true},
		{"cat a > b", true, false},
		{"cat a 2>&1", true, false},
		{"rm -rf x", true, false},
		{"sed -i s/a/b/ f", true, false},
		{"find . -name '*.go'", true, true},
		{"find -name x", true, true},
		{"find -delete", true, false},
		{"find . -delete", true, false},
		{"find -exec rm {} +", true, false},
		{"find . -execdir rm {} +", true, false},
		{"find -okdir rm {} +", true, false},
		{"find . -fprint out", true, false},
		{"find . -fls out", true, false},
		{"rg foo", true, true},
		{"rg --pre ./x foo", true, false},
		{"tree -L 2", true, true},
		{"tree -o out", true, false},
		{"file x", true, true},
		{"file -C x", true, false},
		{"/usr/bin/git commit -m x", true, false},
		{"sh -c 'git commit -m x'", true, false},
	}
	for _, role := range Roles {
		rs := permissionFor(role).Bash
		for _, c := range cases {
			want := c.other
			if writesWorktree(role) {
				want = c.worker
			}
			if got := allowed(rs, c.cmd); got != want {
				t.Errorf("%s: %q allowed = %v, want %v", role, c.cmd, got, want)
			}
		}
	}
}

func TestReadPolicyNeverAsks(t *testing.T) {
	rs := permissionFor("worker").Read
	for path, want := range map[string]string{"main.go": "allow", "dir/.env": "deny", "a/prod.env.local": "deny", "README.md": "allow"} {
		if got := decide(rs, path); got != want {
			t.Errorf("read %q = %q, want %q", path, got, want)
		}
	}
}
