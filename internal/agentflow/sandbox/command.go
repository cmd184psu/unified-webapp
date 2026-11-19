package sandbox

// Command builds the command that runs one stage, choosing the backend the
// caller asked for: a container when Spec.Docker is set (AF-12), else the
// host sandbox (bwrap on Linux and the BSDs, Seatbelt on macOS).

import (
	"context"
	"os/exec"
)

// Command returns the command for spec. It validates the spec through the
// backend it selects and so also reports a misconfigured container before
// anything starts.
func Command(ctx context.Context, spec Spec) (*exec.Cmd, error) {
	if spec.Docker != nil {
		return dockerCommand(ctx, spec)
	}
	return hostCommand(ctx, spec)
}
