package verify

import (
	"encoding/json"
	"os"
	"path/filepath"
)

// Detect returns the feedback commands implied by the worktree's project
// files, used when a project configures none: go.mod gives build, vet and
// test; pyproject.toml gives pytest; package.json gives npm test only when a
// test script exists.
func Detect(worktree string) []Argv {
	var cmds []Argv
	has := func(name string) bool {
		fi, err := os.Stat(filepath.Join(worktree, name))
		return err == nil && fi.Mode().IsRegular()
	}
	if has("go.mod") {
		cmds = append(cmds, Argv{"go", "build", "-o", "/dev/null", "./..."}, Argv{"go", "vet", "./..."}, Argv{"go", "test", "./..."})
	}
	if has("pyproject.toml") {
		cmds = append(cmds, Argv{"python", "-m", "pytest", "-q"})
	}
	if has("package.json") && hasNpmTest(filepath.Join(worktree, "package.json")) {
		cmds = append(cmds, Argv{"npm", "test", "--silent"})
	}
	return cmds
}

func hasNpmTest(p string) bool {
	b, err := readRegular(p)
	if err != nil {
		return false
	}
	var pkg struct {
		Scripts map[string]string `json:"scripts"`
	}
	return json.Unmarshal(b, &pkg) == nil && pkg.Scripts["test"] != ""
}

// Commands resolves the feedback commands for a run: the project's stored
// commands when set, else Detect.
func Commands(stored []string, worktree string) ([]Argv, error) {
	if len(stored) == 0 {
		return Detect(worktree), nil
	}
	cmds := make([]Argv, 0, len(stored))
	for _, s := range stored {
		a, err := ParseArgv(s)
		if err != nil {
			return nil, err
		}
		cmds = append(cmds, a)
	}
	return cmds, nil
}
