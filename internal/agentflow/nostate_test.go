package agentflow

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"strings"
	"testing"
)

// stateWriters are the only places allowed to write files or marshal indented
// JSON: stage artifacts and per-task bookkeeping markers under the data dir.
// Everything else lives in SQLite (DBS1). Keys are "<slash path>:<function>".
var stateWriters = map[string]string{
	"engine/stages.go:modelCall":                  "prompt.md and corrective.md of a stage",
	"engine/cleanup.go:checkpoint":                "<data>/tasks/<id>/cleanup_checkpoint_<attempt> marker",
	"engine/merge.go:recordPreMerge":              "pre_merge.json, the merge recovery marker",
	"engine/attempt.go:prepareAttempt":            "<data>/tasks/<id>/base_commit",
	"engine/attempt.go:prepareConflictRepair":     "conflict_prepared_<attempt> marker",
	"gitsvc/policy.go:DetectAndRevert":            "restores git's own MERGE_* files in the admin dir",
	"runner/opencode/run.go:installSandboxConfig": "copy of the frozen runner config in the stage home",
}

// AC-DBS1: no file-based state. Every os.WriteFile, os.Create and
// json.MarshalIndent call outside _test files must be in an allowlisted
// stage-artifact writer; a new one fails this test until it is reviewed.
func TestNoStateFiles(t *testing.T) {
	banned := map[string]bool{"os.WriteFile": true, "os.Create": true, "json.MarshalIndent": true}
	used := map[string]bool{}
	fset := token.NewFileSet()
	err := filepath.WalkDir(".", func(path string, d fs.DirEntry, err error) error {
		if err == nil && d.IsDir() && d.Name() == "testdata" {
			return fs.SkipDir
		}
		if err != nil || d.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return err
		}
		f, err := parser.ParseFile(fset, path, nil, 0)
		if err != nil {
			return err
		}
		for _, decl := range f.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || fn.Body == nil {
				continue
			}
			ast.Inspect(fn.Body, func(n ast.Node) bool {
				call, ok := n.(*ast.CallExpr)
				if !ok {
					return true
				}
				sel, ok := call.Fun.(*ast.SelectorExpr)
				if !ok {
					return true
				}
				pkg, ok := sel.X.(*ast.Ident)
				if !ok || !banned[pkg.Name+"."+sel.Sel.Name] {
					return true
				}
				key := filepath.ToSlash(path) + ":" + fn.Name.Name
				if _, ok := stateWriters[key]; ok {
					used[key] = true
					return true
				}
				t.Errorf("%s: %s.%s in %s is not an allowlisted stage-artifact writer", fset.Position(call.Pos()), pkg.Name, sel.Sel.Name, key)
				return true
			})
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	for key := range stateWriters {
		if !used[key] {
			t.Errorf("allowlist entry %s matches no call; remove it", key)
		}
	}
}
