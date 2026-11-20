package model

import (
	"path"
	"strings"
)

// IsTestFile reports whether p (a repository-relative path) is a test file that sits next to the
// code it tests: Go's *_test.go, *.test.* and *.spec.* for JavaScript and TypeScript, Python's
// test_*.py and *_test.py, and C# test files (*Tests.cs, *Test.cs, or any .cs file in a
// directory whose name ends in Tests).
//
// Such a file is shared, not owned (AF-13): many tasks add tests to the same package's test
// file, and they are ordered by the merge stage, not by depends_on. The scope check and the
// compiler's shared-file rule leave them out.
func IsTestFile(p string) bool {
	p = path.Clean(strings.ReplaceAll(p, "\\", "/"))
	base := path.Base(p)
	ext := path.Ext(base)
	stem := strings.TrimSuffix(base, ext)
	switch ext {
	case ".go":
		return strings.HasSuffix(stem, "_test")
	case ".js", ".mjs", ".cjs", ".ts", ".tsx", ".jsx", ".mts":
		return strings.HasSuffix(stem, ".test") || strings.HasSuffix(stem, ".spec")
	case ".py":
		return strings.HasPrefix(stem, "test_") || strings.HasSuffix(stem, "_test")
	case ".cs":
		if strings.HasSuffix(stem, "Tests") || strings.HasSuffix(stem, "Test") {
			return true
		}
		for _, dir := range strings.Split(path.Dir(p), "/") {
			if strings.HasSuffix(dir, "Tests") {
				return true
			}
		}
	}
	return false
}
