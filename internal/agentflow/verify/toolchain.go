package verify

import (
	"path/filepath"
	"strings"

	"cmd184psu/unified-webapp/internal/agentflow/model"
)

// msbuildSingleNode keeps MSBuild on one node. Measured on a fresh copy of a
// C# tree inside the sandbox: a plain build stalls for five minutes and fails
// because the parallel worker nodes cannot complete their handshake when
// process spawning is confined, while the same build with -m:1 finishes in
// seconds. No environment variable caps them (AF-6).
const msbuildSingleNode = "-m:1"

// dotnetSubcommands are the dotnet verbs that run MSBuild and accept -m:1.
var dotnetSubcommands = map[string]bool{
	"build": true, "test": true, "restore": true, "run": true,
	"publish": true, "pack": true, "msbuild": true,
}

// ForToolchains adapts a project's feedback commands to the task's toolchains.
// A dotnet task gets -m:1 on every MSBuild invocation that does not already
// name a node limit, whether the project wrote the flag or not: the engine,
// not the project file, owns this part of running C# offline.
func ForToolchains(cmds []Argv, toolchains []string) []Argv {
	if !containsToolchain(toolchains, model.ToolchainDotnet) {
		return cmds
	}
	out := make([]Argv, len(cmds))
	for i, a := range cmds {
		out[i] = dotnetSingleNode(a)
	}
	return out
}

func dotnetSingleNode(a Argv) Argv {
	if len(a) < 2 || filepath.Base(a[0]) != "dotnet" || !dotnetSubcommands[a[1]] {
		return a
	}
	for _, arg := range a[2:] {
		if arg == "-m" || arg == "-maxcpucount" || arg == "--maxcpucount" ||
			strings.HasPrefix(arg, "-m:") || strings.HasPrefix(arg, "-maxcpucount:") ||
			strings.HasPrefix(arg, "--maxcpucount:") {
			return a
		}
	}
	out := append(Argv{}, a...)
	return append(out, msbuildSingleNode)
}

func containsToolchain(names []string, want string) bool {
	for _, n := range names {
		if n == want {
			return true
		}
	}
	return false
}
