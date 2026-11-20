package verify

import (
	"reflect"
	"testing"
)

// AF-6: a dotnet task's MSBuild commands are capped at one node whether the
// project wrote the flag or not; every other command passes through.
func TestForToolchainsDotnetSingleNode(t *testing.T) {
	cases := []struct {
		name       string
		cmd        Argv
		toolchains []string
		want       Argv
	}{
		{"build gets the flag", Argv{"dotnet", "build", "App.csproj"}, []string{"dotnet"}, Argv{"dotnet", "build", "App.csproj", "-m:1"}},
		{"test gets the flag", Argv{"dotnet", "test", "EngineTests"}, []string{"dotnet"}, Argv{"dotnet", "test", "EngineTests", "-m:1"}},
		{"absolute dotnet gets the flag", Argv{"/sandbox/dotnet/dotnet", "build"}, []string{"dotnet"}, Argv{"/sandbox/dotnet/dotnet", "build", "-m:1"}},
		{"existing -m:2 is kept", Argv{"dotnet", "build", "-m:2"}, []string{"dotnet"}, Argv{"dotnet", "build", "-m:2"}},
		{"existing --maxcpucount is kept", Argv{"dotnet", "build", "--maxcpucount:4"}, []string{"dotnet"}, Argv{"dotnet", "build", "--maxcpucount:4"}},
		{"non-msbuild verb is untouched", Argv{"dotnet", "--info"}, []string{"dotnet"}, Argv{"dotnet", "--info"}},
		{"non-dotnet command is untouched", Argv{"go", "test", "./..."}, []string{"dotnet"}, Argv{"go", "test", "./..."}},
		{"no toolchain means no change", Argv{"dotnet", "build"}, nil, Argv{"dotnet", "build"}},
	}
	for _, c := range cases {
		got := ForToolchains([]Argv{c.cmd}, c.toolchains)
		if !reflect.DeepEqual(got[0], c.want) {
			t.Errorf("%s: %v -> %v, want %v", c.name, c.cmd, got[0], c.want)
		}
	}
}

func TestForToolchainsKeepsTheInputSlice(t *testing.T) {
	cmds := []Argv{{"dotnet", "build"}}
	in := append(Argv{}, cmds[0]...)
	ForToolchains(cmds, []string{"dotnet"})
	if !reflect.DeepEqual(cmds[0], in) {
		t.Fatalf("ForToolchains mutated its input: %v", cmds[0])
	}
}
