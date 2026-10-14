// Package prompts embeds the agentflow role prompts so the module never
// depends on prompt files being present on disk.
package prompts

import (
	"embed"
	"fmt"
)

//go:embed *.md
var files embed.FS

// Roles lists the embedded prompt names. Every agent role has one; drafter is
// the prompt of the planner role's FRD draft, polish and split stages and is
// not a stage role of its own.
var Roles = []string{"worker", "reviewer", "planner", "architect", "critic", "compiler", "cleanup", "drafter"}

// Load returns the prompt text for a role.
func Load(role string) (string, error) {
	b, err := files.ReadFile(role + ".md")
	if err != nil {
		return "", fmt.Errorf("prompts: unknown role %q", role)
	}
	return string(b), nil
}
