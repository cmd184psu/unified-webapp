// Package prompts embeds the agentflow role prompts so the module never
// depends on prompt files being present on disk.
package prompts

import (
	"embed"
	"fmt"
)

//go:embed *.md
var files embed.FS

// Roles lists the embedded prompt names, one per agent role.
var Roles = []string{"worker", "reviewer", "planner", "architect", "critic", "compiler", "cleanup"}

// Load returns the prompt text for a role.
func Load(role string) (string, error) {
	b, err := files.ReadFile(role + ".md")
	if err != nil {
		return "", fmt.Errorf("prompts: unknown role %q", role)
	}
	return string(b), nil
}
