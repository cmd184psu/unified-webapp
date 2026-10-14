package verify

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func writeFiles(t *testing.T, files map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	for name, body := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

var fixedItems = []string{"No files outside the task's scope are changed.", "New code has tests.", "No secrets are added."}

// N1: the definition of done follows the detected toolchain, with the fixed
// lines always last.
func TestN1_DefinitionOfDoneFollowsTheToolchain(t *testing.T) {
	cases := []struct {
		name  string
		files map[string]string
		want  []string
		tools []string
	}{
		{"go", map[string]string{"go.mod": "module x\n"},
			[]string{"`go build ./...` succeeds.", "`go vet ./...` succeeds.", "`go test ./...` succeeds."}, []string{"go"}},
		{"python", map[string]string{"pyproject.toml": "[project]\n"},
			[]string{"`python -m pytest -q` succeeds."}, []string{"python"}},
		{"npm with a test script", map[string]string{"package.json": `{"scripts":{"test":"jest"}}`},
			[]string{"`npm test --silent` succeeds."}, []string{"node"}},
		{"npm without a test script", map[string]string{"package.json": `{"scripts":{"build":"tsc"}}`}, nil, []string{}},
		{"go and python", map[string]string{"go.mod": "module x\n", "pyproject.toml": "x"},
			[]string{"`go build ./...` succeeds.", "`go vet ./...` succeeds.", "`go test ./...` succeeds.", "`python -m pytest -q` succeeds."}, []string{"go", "python"}},
		{"nothing detected", nil, nil, []string{}},
	}
	for _, c := range cases {
		d := DefinitionOfDone(writeFiles(t, c.files), nil, nil)
		if want := append(append([]string{}, c.want...), fixedItems...); !reflect.DeepEqual(d.Items, want) {
			t.Errorf("%s: items = %q, want %q", c.name, d.Items, want)
		}
		if d.Source != "detected" || !reflect.DeepEqual(d.Toolchains, c.tools) {
			t.Errorf("%s: source %q toolchains %v, want detected %v", c.name, d.Source, d.Toolchains, c.tools)
		}
	}
}

// N1: the project's stored feedback commands are what the engine runs, so they
// replace the detected ones; an override replaces everything.
func TestN1_StoredCommandsAndOverride(t *testing.T) {
	dir := writeFiles(t, map[string]string{"go.mod": "module x\n"})
	d := DefinitionOfDone(dir, []string{"make check"}, nil)
	if want := append([]string{"`make check` succeeds."}, fixedItems...); !reflect.DeepEqual(d.Items, want) || d.Source != "detected" {
		t.Fatalf("stored commands: %+v", d)
	}
	d = DefinitionOfDone(dir, []string{"make check"}, []string{"Docs are updated."})
	if !reflect.DeepEqual(d.Items, []string{"Docs are updated."}) || d.Source != "project" || !reflect.DeepEqual(d.Toolchains, []string{"go"}) {
		t.Fatalf("override: %+v", d)
	}
}
