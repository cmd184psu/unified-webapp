package sandbox

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// fakeToolchainHome installs a dotnet SDK and a NuGet cache under a temporary
// HOME, which is where ApplyToolchains looks for them.
func fakeToolchainHome(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	if err := os.MkdirAll(filepath.Join(home, ".dotnet"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(home, ".nuget", "packages"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(home, ".dotnet", "dotnet"), []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	return home
}

// TestApplyToolchainsMountsDotnetReadOnly: the dotnet toolchain adds read-only
// mounts for the SDK and the NuGet cache, the SDK on PATH and DOTNET_ROOT, and
// sets NUGET_PACKAGES at the cache with the CLI's writable state in the stage
// home (AF-6).
func TestApplyToolchainsMountsDotnetReadOnly(t *testing.T) {
	home := fakeToolchainHome(t)
	var s Spec
	if err := ApplyToolchains(&s, []string{"dotnet"}); err != nil {
		t.Fatal(err)
	}
	want := []Bind{
		{Src: filepath.Join(home, ".dotnet"), Dst: dotnetBind},
		{Src: filepath.Join(home, ".nuget", "packages"), Dst: nugetBind},
	}
	if len(s.ExtraROBinds) != len(want) {
		t.Fatalf("binds = %+v, want %+v", s.ExtraROBinds, want)
	}
	for i := range want {
		if s.ExtraROBinds[i] != want[i] {
			t.Errorf("bind %d = %+v, want %+v", i, s.ExtraROBinds[i], want[i])
		}
	}
	if len(s.PathDirs) != 1 || s.PathDirs[0] != dotnetBind {
		t.Errorf("PathDirs = %v, want [%s]", s.PathDirs, dotnetBind)
	}
	for k, v := range map[string]string{
		"DOTNET_ROOT":                       dotnetBind,
		"DOTNET_CLI_HOME":                   SandboxHome,
		"NUGET_PACKAGES":                    nugetBind,
		"DOTNET_NOLOGO":                     "1",
		"DOTNET_CLI_TELEMETRY_OPTOUT":       "1",
		"DOTNET_SKIP_FIRST_TIME_EXPERIENCE": "1",
	} {
		if s.Env[k] != v {
			t.Errorf("%s = %q, want %q", k, s.Env[k], v)
		}
	}
}

// TestApplyToolchainsErrorsNameTheToolchain: an unknown toolchain and one that
// is not installed are errors naming it, not silent omissions (AF-6).
func TestApplyToolchainsErrorsNameTheToolchain(t *testing.T) {
	if err := ApplyToolchains(&Spec{}, []string{"ruby"}); err == nil || !strings.Contains(err.Error(), "unknown toolchain") {
		t.Fatalf("unknown toolchain: %v", err)
	}
	t.Setenv("HOME", t.TempDir())
	if err := ApplyToolchains(&Spec{}, []string{"dotnet"}); err == nil || !strings.Contains(err.Error(), "not an executable") {
		t.Fatalf("missing dotnet: %v", err)
	}
}

// TestApplyToolchainsNoNamesChangesNothing: a project with no toolchains gets
// the same spec it had.
func TestApplyToolchainsNoNamesChangesNothing(t *testing.T) {
	s := Spec{Env: map[string]string{"GOPROXY": "off"}}
	if err := ApplyToolchains(&s, nil); err != nil {
		t.Fatal(err)
	}
	if len(s.ExtraROBinds) != 0 || len(s.PathDirs) != 0 || len(s.Env) != 1 || s.Env["GOPROXY"] != "off" {
		t.Fatalf("spec changed: %+v", s)
	}
}
