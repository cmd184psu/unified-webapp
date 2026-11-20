package opencode

import (
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const adapterImport = "cmd184psu/unified-webapp/internal/agentflow/runner/opencode"

// moduleRoot walks up from the working directory to the directory with go.mod.
func moduleRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("go.mod not found")
		}
		dir = parent
	}
}

// TestImportBoundary (AC-AR1, boundary half): across the whole module only the
// composition root (internal/agentflow itself) and the adapter import the
// opencode adapter package.
func TestImportBoundary(t *testing.T) {
	root := moduleRoot(t)
	fset := token.NewFileSet()
	seen := 0
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if n := d.Name(); path != root && (n == "node_modules" || n == "vendor" || strings.HasPrefix(n, ".")) {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") {
			return nil
		}
		seen++
		f, err := parser.ParseFile(fset, path, nil, parser.ImportsOnly)
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(root, filepath.Dir(path))
		rel = filepath.ToSlash(rel)
		// testdata/smoke is the operator-run live smoke (scripts/agentflow-opencode-smoke.sh):
		// never built by ./..., never part of the server.
		allowed := rel == "internal/agentflow" || rel == "internal/agentflow/runner/opencode" ||
			rel == "internal/agentflow/runner/opencode/testdata/smoke"
		for _, imp := range f.Imports {
			if strings.Trim(imp.Path.Value, `"`) == adapterImport && !allowed {
				t.Errorf("%s imports the opencode adapter outside the composition root", path)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if seen < 100 {
		t.Fatalf("walked only %d files; boundary test is not looking at the module", seen)
	}
}
