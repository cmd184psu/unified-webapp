package model

import (
	"path"
	"sort"
	"strings"
)

// ToolchainDotnet is the host toolchain a .NET project brings into its sandbox.
const ToolchainDotnet = "dotnet"

// Toolchains are the toolchains a task or project may name. The sandbox mounts
// each of them read-only; anything else is refused at validation.
var Toolchains = []string{ToolchainDotnet}

// ValidToolchain reports whether name is a toolchain agentflow can mount.
func ValidToolchain(name string) bool {
	for _, t := range Toolchains {
		if t == name {
			return true
		}
	}
	return false
}

// dotnetExts are the file extensions whose presence means a task builds a .NET
// project: C# sources, project files and solutions.
var dotnetExts = map[string]bool{".cs": true, ".csproj": true, ".sln": true}

// InferToolchains returns the toolchains a task's files imply, sorted. It is
// the engine's backstop for a task whose definition does not name them: the
// compiler's hint can add a toolchain, never remove an inferred one.
func InferToolchains(filesTouched []string) []string {
	seen := map[string]bool{}
	for _, f := range filesTouched {
		if dotnetExts[strings.ToLower(path.Ext(strings.TrimSpace(f)))] {
			seen[ToolchainDotnet] = true
		}
	}
	return sortedToolchains(seen)
}

// TaskToolchains is the task's declared toolchains plus those its files
// impose. It is the only list the engine acts on, so an older queue whose
// tasks carry no toolchains still gets the dotnet treatment for .cs files.
func TaskToolchains(t TaskDef) []string {
	seen := map[string]bool{}
	for _, name := range t.Toolchains {
		if name != "" {
			seen[name] = true
		}
	}
	for _, name := range InferToolchains(t.FilesTouched) {
		seen[name] = true
	}
	return sortedToolchains(seen)
}

// HasToolchain reports whether the task needs name, declared or inferred.
func HasToolchain(t TaskDef, name string) bool {
	for _, n := range TaskToolchains(t) {
		if n == name {
			return true
		}
	}
	return false
}

// MergeToolchains is the union of two toolchain lists, sorted and deduplicated.
// The project's list and a task's list combine this way, so the task can add a
// mount the project did not configure but never drop one it did.
func MergeToolchains(a, b []string) []string {
	seen := map[string]bool{}
	for _, name := range append(append([]string{}, a...), b...) {
		if name != "" {
			seen[name] = true
		}
	}
	return sortedToolchains(seen)
}

func sortedToolchains(seen map[string]bool) []string {
	if len(seen) == 0 {
		return nil
	}
	out := make([]string, 0, len(seen))
	for name := range seen {
		out = append(out, name)
	}
	sort.Strings(out)
	return out
}
