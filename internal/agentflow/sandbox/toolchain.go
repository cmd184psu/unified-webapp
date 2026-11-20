package sandbox

import (
	"fmt"
	"os"
	"path/filepath"

	"cmd184psu/unified-webapp/internal/agentflow/model"
)

// Toolchains are the host toolchains a project or task may bring into its
// sandbox (AF-6). "dotnet" is the first: a C# project's verify commands need
// the SDK and its offline NuGet cache, which live under the server user's HOME
// and are otherwise outside the sandbox. The names live in model so a task
// definition and its sandbox cannot disagree about them.
var Toolchains = model.Toolchains

// The sandbox destinations a toolchain is mounted at. bwrap never binds the
// host HOME (the stage home replaces it), so each toolchain gets its own
// read-only mount; on macOS the same destinations are rewritten to the host
// paths they stand for.
const (
	dotnetBind = "/sandbox/dotnet"
	nugetBind  = "/sandbox/nuget"
)

// ValidToolchain reports whether name is a toolchain agentflow can mount.
func ValidToolchain(name string) bool { return model.ValidToolchain(name) }

// ApplyToolchains adds the mounts, PATH entries and environment of every named
// toolchain to spec. Nothing on the host is made writable: the mounts are
// read-only and a toolchain's own state goes to the stage home. An unknown or
// missing toolchain is an error naming it, so a misconfigured project fails
// with a reason instead of at its first feedback command.
func ApplyToolchains(spec *Spec, names []string) error {
	for _, name := range names {
		switch name {
		case model.ToolchainDotnet:
			if err := applyDotnet(spec); err != nil {
				return err
			}
		default:
			return fmt.Errorf("sandbox: unknown toolchain %q", name)
		}
	}
	return nil
}

// applyDotnet mounts the host SDK read-only on PATH and the NuGet package cache
// read-only as NUGET_PACKAGES, with the CLI's writable state in the stage home.
func applyDotnet(spec *Spec) error {
	home, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf("sandbox: toolchain dotnet: %w", err)
	}
	root := filepath.Join(home, ".dotnet")
	exe := filepath.Join(root, "dotnet")
	if fi, err := os.Stat(exe); err != nil || !fi.Mode().IsRegular() || fi.Mode().Perm()&0o111 == 0 {
		return fmt.Errorf("sandbox: toolchain dotnet: %s is not an executable", exe)
	}
	packages := filepath.Join(home, ".nuget", "packages")
	if fi, err := os.Stat(packages); err != nil || !fi.IsDir() {
		return fmt.Errorf("sandbox: toolchain dotnet: NuGet cache %s is missing; restore the project once on the host", packages)
	}
	spec.ExtraROBinds = append(spec.ExtraROBinds, Bind{Src: root, Dst: dotnetBind}, Bind{Src: packages, Dst: nugetBind})
	spec.PathDirs = append(spec.PathDirs, dotnetBind)
	if spec.Env == nil {
		spec.Env = map[string]string{}
	}
	for k, v := range map[string]string{
		"DOTNET_ROOT":                       dotnetBind,
		"DOTNET_CLI_HOME":                   SandboxHome,
		"NUGET_PACKAGES":                    nugetBind,
		"DOTNET_SKIP_FIRST_TIME_EXPERIENCE": "1",
		"DOTNET_CLI_TELEMETRY_OPTOUT":       "1",
		"DOTNET_NOLOGO":                     "1",
	} {
		spec.Env[k] = v
	}
	return nil
}
