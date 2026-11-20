package model

import (
	"reflect"
	"strings"
	"testing"
)

// AF-6b: a task's toolchains are what it declares plus what its files imply;
// the compiler's hint can add one but never remove an inferred one.
func TestTaskToolchainsDeclaredAndInferred(t *testing.T) {
	cases := []struct {
		name string
		def  TaskDef
		want []string
	}{
		{"csharp file infers dotnet", TaskDef{FilesTouched: []string{"src/App.cs"}}, []string{"dotnet"}},
		{"project file infers dotnet", TaskDef{FilesTouched: []string{"App/App.csproj"}}, []string{"dotnet"}},
		{"solution infers dotnet", TaskDef{FilesTouched: []string{"All.sln"}}, []string{"dotnet"}},
		{"extension is case-insensitive", TaskDef{FilesTouched: []string{"Program.CS"}}, []string{"dotnet"}},
		{"other files infer nothing", TaskDef{FilesTouched: []string{"main.go", "README.md"}}, nil},
		{"no files infer nothing", TaskDef{}, nil},
		{"declared survives a non-cs task", TaskDef{Toolchains: []string{"dotnet"}, FilesTouched: []string{"docs/x.md"}}, []string{"dotnet"}},
		{"declared and inferred dedupe", TaskDef{Toolchains: []string{"dotnet"}, FilesTouched: []string{"a.cs"}}, []string{"dotnet"}},
	}
	for _, c := range cases {
		if got := TaskToolchains(c.def); !reflect.DeepEqual(got, c.want) {
			t.Errorf("%s: TaskToolchains = %v, want %v", c.name, got, c.want)
		}
	}
}

func TestMergeToolchains(t *testing.T) {
	if got := MergeToolchains(nil, []string{"dotnet"}); !reflect.DeepEqual(got, []string{"dotnet"}) {
		t.Fatalf("project without toolchains + task dotnet = %v", got)
	}
	if got := MergeToolchains([]string{"dotnet"}, []string{"dotnet"}); !reflect.DeepEqual(got, []string{"dotnet"}) {
		t.Fatalf("dedupe = %v", got)
	}
	if got := MergeToolchains(nil, nil); len(got) != 0 {
		t.Fatalf("empty merge = %#v, want an empty list", got)
	}
	if !ValidToolchain("dotnet") || ValidToolchain("gcc") {
		t.Fatal("ValidToolchain does not match the known list")
	}
}

// The FRD template carries the optional "Toolchains:" line.
func TestRenderFRDToolchainsLine(t *testing.T) {
	md := RenderFRD(FRD{ID: "F1", Title: "t", Objective: "o", Requirements: []FRDRequirement{{ID: "R1", Description: "d"}}, Toolchains: []string{"dotnet"}})
	if !strings.Contains(md, "Toolchains: dotnet") {
		t.Fatalf("FRD markdown has no Toolchains line:\n%s", md)
	}
}
