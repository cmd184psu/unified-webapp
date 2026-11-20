package engine

import (
	"cmd184psu/unified-webapp/internal/agentflow/model"
	"cmd184psu/unified-webapp/internal/agentflow/store"
)

// The sandbox protections a dotnet task may relax, named on the stage so each
// one is auditable in the UI and the stage log (AF-6b). Everything else stays
// on: credentials and key files unreadable, the agentflow data dir and sibling
// stages hidden, git dirs read-only, no outbound internet, and writes only to
// the stage home and the verify copy.
const (
	relaxLoopback         = "loopback"
	relaxVerifyInCopy     = "verify_in_copy"
	relaxUnconfinedRunner = "unconfined_runner"
)

// dotnetTask reports whether the task needs the .NET toolchain, declared or
// inferred from its files.
func dotnetTask(t model.Task) bool { return model.HasToolchain(t.TaskDef, model.ToolchainDotnet) }

// verifyRelaxations lists what this task's verify stage is allowed to relax.
// A .NET verify needs loopback (the vstest testhost handshake) and a writable
// copy of the tree (MSBuild writes bin/ and obj/ next to the sources), because
// the worktree itself stays read-only. verify_in_copy is only a relaxation
// when the project has not already asked for it.
func verifyRelaxations(t model.Task, p store.Project) []string {
	if !dotnetTask(t) {
		return nil
	}
	out := []string{relaxLoopback}
	if !p.VerifyInCopy {
		out = append(out, relaxVerifyInCopy)
	}
	return out
}

// workerRelaxations lists what this task's work stage is allowed to relax.
// MSBuild's parallel worker nodes cannot complete their handshake when process
// spawning is confined, so the worker itself must run outside agentflow's
// sandbox; the candidate filter enforces that.
func workerRelaxations(t model.Task) []string {
	if !dotnetTask(t) {
		return nil
	}
	return []string{relaxUnconfinedRunner}
}

// sandboxedBackend reports whether the backend's agent runs inside agentflow's
// sandbox. The opencode adapter is the only one that does (hosted opencode
// backends such as Beam included); the codex, claude, muse, agy and grok
// adapters run their host CLI directly.
func sandboxedBackend(v *view, name string) bool {
	for _, b := range v.snap.Backends {
		if b.Name == name {
			return b.APIType == "opencode"
		}
	}
	return false
}
