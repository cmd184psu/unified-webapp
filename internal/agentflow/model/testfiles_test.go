package model

import "testing"

func TestIsTestFile(t *testing.T) {
	for p, want := range map[string]bool{
		"src/rules/peace_test.go":              true,
		"src/rules/peace.go":                   false,
		"web/app/js/board.test.ts":             true,
		"web/app/js/board.ts":                  false,
		"web/app/js/x.spec.js":                 true,
		"tools/test_parse.py":                  true,
		"tools/parse_test.py":                  true,
		"tools/testing.py":                     false,
		"EngineTests/CombatTests.cs":           true,
		"EngineTests/Helpers/Fixture.cs":       true,
		"Engine/Combat.cs":                     false,
		"Engine/Contest.cs":                    false,
		"QueryCiv3/QueryCiv3.Tests/SavTest.cs": true,
		"go.mod":                               false,
	} {
		if got := IsTestFile(p); got != want {
			t.Errorf("IsTestFile(%q) = %v, want %v", p, got, want)
		}
	}
}
