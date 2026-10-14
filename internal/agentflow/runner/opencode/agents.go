// Package opencode is the opencode adapter for the runner interface. Every
// opencode-specific detail lives here (AR2): the generated configuration, the
// provider rendering, the embedded agent definitions, the permission rules and
// the isolation environment. Nothing outside this package may import it except
// the composition root (TestImportBoundary).
//
// Layout of a stage directory (all under <stage>, never under the real HOME):
// xdg/opencode/opencode.json is the frozen generated config (XDG_CONFIG_HOME is
// <stage>/xdg) and home/ is the private HOME.
//
// Permission rules are a first layer only. They are matched against the shell
// command text, so they cannot see grouped redirections ("(cat a) > b",
// "{ cat a; } > b", loops), "/usr/bin/git", "env git" or "sh -c". The real
// guarantee for non-worker roles is the sandbox's read-only worktree (B8
// Spec.ReadOnlyWorktree), and for git GIT5 layers 2 and 3. external_directory
// does not cover reads done through bash; B8's deny-by-default root does.
package opencode

import (
	"bytes"
	"embed"
	"encoding/json"
	"fmt"
)

//go:embed embed/agents/*.md
var agentFS embed.FS

// Roles are the agent roles agentflow ships definitions for (OA1).
var Roles = []string{"worker", "reviewer", "planner", "architect", "critic", "compiler", "cleanup"}

// AgentName is the opencode agent name for a role. The adapter selects it with
// --agent in B7.
func AgentName(role string) string { return "af-" + role }

// rule is one permission pattern and its action.
type rule struct{ Pattern, Action string }

// rules is an ordered permission rule list. opencode matches the LAST matching
// rule (spike S2), so order is part of the meaning and is preserved when
// marshalled; a Go map would sort the keys and break the semantics.
type rules []rule

func (rs rules) MarshalJSON() ([]byte, error) {
	var buf bytes.Buffer
	buf.WriteByte('{')
	for i, r := range rs {
		if i > 0 {
			buf.WriteByte(',')
		}
		k, err := marshalNoEscape(r.Pattern)
		if err != nil {
			return nil, err
		}
		v, err := marshalNoEscape(r.Action)
		if err != nil {
			return nil, err
		}
		buf.Write(k)
		buf.WriteByte(':')
		buf.Write(v)
	}
	buf.WriteByte('}')
	return buf.Bytes(), nil
}

func marshalNoEscape(v any) ([]byte, error) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(v); err != nil {
		return nil, err
	}
	return bytes.TrimRight(buf.Bytes(), "\n"), nil
}

// permission is an agent's permission block.
type permission struct {
	Edit              string `json:"edit"`
	Bash              rules  `json:"bash"`
	Read              rules  `json:"read"`
	ExternalDirectory string `json:"external_directory"`
	Task              string `json:"task"`
	WebFetch          string `json:"webfetch"`
	WebSearch         string `json:"websearch"`
}

// check rejects duplicate patterns, which would make the last-match-wins
// meaning ambiguous.
func (rs rules) check() error {
	seen := map[string]bool{}
	for _, r := range rs {
		if seen[r.Pattern] {
			return fmt.Errorf("opencode: duplicate permission pattern %q", r.Pattern)
		}
		seen[r.Pattern] = true
	}
	return nil
}

// gitWriteDenies are the git write verbs GIT5 forbids (layer 1). They are all
// also caught by the "git *" catch-all; listing them keeps the policy auditable
// and the deny holds if the catch-all is ever narrowed.
var gitWriteDenies = []string{
	"git add*", "git commit*", "git branch -d*", "git branch -D*", "git branch -m*",
	"git branch -M*", "git branch -c*", "git branch -C*", "git tag*", "git stash*",
	"git push*", "git reset*", "git checkout*", "git switch*", "git merge*",
	"git rebase*", "git cherry-pick*", "git worktree*", "git config*",
	"git restore*", "git rm*", "git mv*", "git clean*", "git apply*", "git am*",
	"git fetch*", "git pull*", "git clone*", "git remote*", "git revert*",
	"git update-ref*", "git submodule*",
}

// gitReadOnly are GIT5's read-only commands, allowed after the "git *" deny.
var gitReadOnly = []string{
	"git status*", "git diff*", "git log*", "git show*", "git blame*", "git grep*",
	"git ls-files*", "git ls-tree*", "git rev-parse*", "git cat-file*",
	"git branch --list*", "git branch --show-current*",
}

// gitFlagDenies are flags that make an otherwise read-only git command write a
// file or run a program; denied after the read-only allows. git grep fails
// closed: "--o*" also catches abbreviations of --open-files-in-pager (and, as
// a side effect, --or), and " -*O*" catches bundled short flags such as -iOcmd
// (and any later capital O in the arguments, so searching for "FOO" is denied
// too). Accepted: a denied search is retried differently, a run program is not.
var gitFlagDenies = []string{"git *--output*", "git grep*--o*", "git grep* -*O*"}

// otherDenies are remote-capable tools no model may run.
var otherDenies = []string{"gh *", "glab *", "ssh *"}

// readOnlyCommands are the shell commands non-worker roles may run besides
// read-only git. Shell redirection is denied last for them.
var readOnlyCommands = []string{
	"ls*", "cat *", "head *", "tail *", "wc *", "grep *", "rg *", "find *",
	"diff *", "tree*", "stat *", "file *", "pwd",
}

// findWriters are find actions that write or execute; denied after "find *".
var findWriters = []string{"find *-exec*", "find *-execdir*", "find *-delete*", "find *-fprint*", "find *-fls*", "find *-ok*"}

// readOnlyFlagDenies are write or exec flags of the read-only commands.
var readOnlyFlagDenies = []string{"rg *--pre*", "tree *-o*", "file *-C*", "file *--compile*"}

// writesWorktree reports whether the role edits the worktree: the worker and
// the optional cleanup pass (AT11). Every other role is read-only.
func writesWorktree(role string) bool { return role == "worker" || role == "cleanup" }

// permissionFor builds the permission block for a role. Rules are ordered for
// last-match-wins: catch-all first, then denies, then the specific allows.
func permissionFor(role string) permission {
	var bash rules
	add := func(action string, patterns ...string) {
		for _, p := range patterns {
			bash = append(bash, rule{p, action})
		}
	}
	if writesWorktree(role) {
		add("allow", "*")
	} else {
		add("deny", "*")
		add("allow", readOnlyCommands...)
		add("deny", findWriters...)
		add("deny", readOnlyFlagDenies...)
	}
	add("deny", "git *")
	add("deny", gitWriteDenies...)
	add("allow", gitReadOnly...)
	add("deny", gitFlagDenies...)
	add("deny", otherDenies...)
	edit := "deny"
	if writesWorktree(role) {
		edit = "allow"
	} else {
		// edit: deny removes the edit tools but not shell writes, so shell
		// redirection is closed for read-only roles as well.
		add("deny", "*>*")
	}
	// Every key is explicit and none is "ask": a rejected ask ends the run
	// silently (S2). Reads are allowed except dotenv files.
	read := rules{{"*", "allow"}, {"*.env", "deny"}, {"*.env.*", "deny"}}
	return permission{Edit: edit, Bash: bash, Read: read, ExternalDirectory: "deny",
		Task: "deny", WebFetch: "deny", WebSearch: "deny"}
}

// agentDef is one entry of the generated config's "agent" object.
type agentDef struct {
	Mode        string     `json:"mode"`
	Description string     `json:"description"`
	Prompt      string     `json:"prompt"`
	Permission  permission `json:"permission"`
}

func agentFor(role string) (agentDef, error) {
	b, err := agentFS.ReadFile("embed/agents/" + role + ".md")
	if err != nil {
		return agentDef{}, fmt.Errorf("opencode: no agent definition for role %q: %w", role, err)
	}
	perm := permissionFor(role)
	if err := perm.Bash.check(); err != nil {
		return agentDef{}, err
	}
	if err := perm.Read.check(); err != nil {
		return agentDef{}, err
	}
	return agentDef{
		Mode:        "primary",
		Description: "agentflow " + role,
		Prompt:      string(b),
		Permission:  perm,
	}, nil
}
