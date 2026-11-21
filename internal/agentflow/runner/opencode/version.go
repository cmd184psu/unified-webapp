package opencode

import (
	"context"
	"fmt"
	"os/exec"
	"regexp"
	"strings"

	"cmd184psu/unified-webapp/internal/agentflow/runner"
)

var versionRe = regexp.MustCompile(`^v?\d+\.\d+\.\d+\S*$`)

// parseVersion extracts the version from `opencode --version` output: the last
// field of the first non-empty line, which must look like 1.2.3.
func parseVersion(out string) (string, error) {
	for _, line := range strings.Split(out, "\n") {
		f := strings.Fields(line)
		if len(f) == 0 {
			continue
		}
		v := f[len(f)-1]
		if !versionRe.MatchString(v) {
			break
		}
		return strings.TrimPrefix(v, "v"), nil
	}
	return "", fmt.Errorf("opencode: cannot parse version from %q", strings.TrimSpace(out))
}

// Version runs `opencode --version` (OC1). It is a plain host call with a
// minimal environment; it touches no stage and no model.
func (a *Adapter) Version(ctx context.Context) (string, error) {
	path, err := a.path()
	if err != nil {
		return "", err
	}
	cmd := exec.CommandContext(ctx, path, "--version")
	cmd.Env = a.versionEnv()
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("opencode: %s --version: %w", path, err)
	}
	return parseVersion(string(out))
}

// BuiltinModels runs `opencode models` and returns the model names of
// opencode's own provider (the free ones among them), without the provider
// prefix. Only lines that start with "opencode/" count: opencode 2.x no longer
// takes a provider argument (it prints its help), and its full list mixes in
// every other configured provider. Like Version it is a plain host call that touches no stage.
func (a *Adapter) BuiltinModels(ctx context.Context) ([]string, error) {
	path, err := a.path()
	if err != nil {
		return nil, err
	}
	return a.models(ctx, path, BuiltinProvider, false)
}

// BuiltinModelsFor lists the models of the runner serving apiType. Kilo's own
// list already carries the kilo/ prefix, which is also the --model value, so
// its names are kept whole.
func (a *Adapter) BuiltinModelsFor(ctx context.Context, apiType string) ([]string, error) {
	return a.BuiltinModelsForPath(ctx, apiType, "")
}

// BuiltinModelsForPath is BuiltinModelsFor with the backend's own CLI path.
func (a *Adapter) BuiltinModelsForPath(ctx context.Context, apiType, cliPath string) ([]string, error) {
	path, err := a.pathFor(runner.Backend{APIType: apiType, CLIPath: cliPath})
	if err != nil {
		return nil, err
	}
	if apiType == KiloAPIType {
		return a.models(ctx, path, KiloAPIType, true)
	}
	return a.models(ctx, path, BuiltinProvider, false)
}

func (a *Adapter) models(ctx context.Context, path, provider string, keepPrefix bool) ([]string, error) {
	cmd := exec.CommandContext(ctx, path, "models")
	cmd.Env = a.versionEnv()
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("opencode: %s models: %w", path, err)
	}
	var names []string
	for _, line := range strings.Split(string(out), "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, provider+"/") {
			continue
		}
		n := line
		if !keepPrefix {
			n = strings.TrimPrefix(line, provider+"/")
		}
		if n != "" && !strings.ContainsAny(n, " \t") {
			names = append(names, n)
		}
	}
	return names, nil
}
