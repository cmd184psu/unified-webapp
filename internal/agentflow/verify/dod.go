package verify

import (
	"os"
	"path/filepath"
	"strings"
)

// DoD is a project's definition of done: the standard every task is held to,
// shown on the FRD and put in the worker and reviewer prompts. Source says
// where Items came from.
type DoD struct {
	Items      []string `json:"items"`
	Source     string   `json:"source"` // "detected" or "project"
	Toolchains []string `json:"toolchains"`
}

// fixedDoD are the lines that hold for every project whatever its toolchain.
var fixedDoD = []string{
	"No files outside the task's scope are changed.",
	"New code has tests.",
	"No secrets are added.",
}

// DefinitionOfDone derives the definition of done for the project files in
// dir. override, when not empty, replaces the derived items. Otherwise the
// items are the commands the engine runs after every task (stored, or
// detected from the toolchain) plus the fixed lines.
func DefinitionOfDone(dir string, stored, override []string) DoD {
	d := DoD{Toolchains: toolchains(dir)}
	if len(override) > 0 {
		d.Items, d.Source = append([]string{}, override...), "project"
		return d
	}
	d.Source = "detected"
	cmds := stored
	if len(cmds) == 0 {
		for _, a := range Detect(dir) {
			// The discard flag is plumbing, not part of what a person reads.
			cmds = append(cmds, strings.Replace(a.String(), " -o /dev/null", "", 1))
		}
	}
	for _, c := range cmds {
		d.Items = append(d.Items, "`"+c+"` succeeds.")
	}
	d.Items = append(d.Items, fixedDoD...)
	return d
}

// toolchains names the toolchains whose project files are in dir.
func toolchains(dir string) []string {
	out := []string{}
	has := func(name string) bool {
		fi, err := os.Stat(filepath.Join(dir, name))
		return err == nil && fi.Mode().IsRegular()
	}
	if has("go.mod") {
		out = append(out, "go")
	}
	if has("pyproject.toml") {
		out = append(out, "python")
	}
	if has("package.json") && hasNpmTest(filepath.Join(dir, "package.json")) {
		out = append(out, "node")
	}
	return out
}
